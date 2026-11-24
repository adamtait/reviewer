// SPDX-License-Identifier: MIT

package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// entry is one thing to put in a test archive.
type entry struct {
	name string
	body string
	// link, if set, makes the entry a symbolic link to this target.
	link string
	// dir makes the entry a directory.
	dir bool
}

// archive builds a gzip tar the way GitHub lays one out: a global header, then
// everything under one folder.
func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeXGlobalHeader, Name: "pax_global_header",
		PAXRecords: map[string]string{"comment": strings.Repeat("a", 40)},
	}))
	must(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "acme-app-abc1234/", Mode: 0o755}))
	for _, e := range entries {
		name := "acme-app-abc1234/" + e.name
		switch {
		case e.dir:
			must(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o755}))
		case e.link != "":
			must(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: name, Linkname: e.link, Mode: 0o777}))
		default:
			must(tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: int64(len(e.body)), Mode: 0o644}))
			_, err := tw.Write([]byte(e.body))
			must(err)
		}
	}
	must(tw.Close())
	must(zw.Close())
	return buf.Bytes()
}

var roomy = Limits{ArchiveBytes: 1 << 20, FileBytes: 1 << 10}

func unpack(t *testing.T, raw []byte, limits Limits) *Commit {
	t.Helper()
	c, err := Unpack(bytes.NewReader(raw), limits)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	return c
}

func TestUnpackSortsEveryEntryIntoAKind(t *testing.T) {
	c := unpack(t, archive(t,
		entry{name: "src", dir: true},
		entry{name: "src/app.ts", body: "export const a = 1;\n"},
		entry{name: "README.md", body: "# héllo\n"},
		entry{name: "logo.png", body: "\x89PNG\r\n\x1a\n\xff\xfe"},
		entry{name: "data.bin", body: "looks like text\x00but is not"},
		entry{name: "big.json", body: strings.Repeat("x", 2000)},
		entry{name: "current", link: "src/app.ts"},
		entry{name: "empty.txt", body: ""},
	), roomy)

	want := map[string]Kind{
		"README.md":  KindText,
		"big.json":   KindTooLarge,
		"current":    KindOther,
		"data.bin":   KindBinary,
		"empty.txt":  KindText,
		"logo.png":   KindBinary,
		"src/app.ts": KindText,
	}
	if len(c.Entries()) != len(want) {
		t.Fatalf("entries = %v, want %d of them", c.Entries(), len(want))
	}
	previous := ""
	for _, e := range c.Entries() {
		if want[e.Path] != e.Kind {
			t.Errorf("%s is %q, want %q", e.Path, e.Kind, want[e.Path])
		}
		if e.Path <= previous {
			t.Errorf("entries are not sorted: %q after %q", e.Path, previous)
		}
		previous = e.Path
	}

	if text, ok := c.Text("src/app.ts"); !ok || text != "export const a = 1;\n" {
		t.Errorf("Text(src/app.ts) = %q, %v", text, ok)
	}
	if text, ok := c.Text("empty.txt"); !ok || text != "" {
		t.Errorf("an empty file is text with no content; got %q, %v", text, ok)
	}
	for _, withheld := range []string{"logo.png", "data.bin", "big.json", "current", "absent.ts", "src"} {
		if text, ok := c.Text(withheld); ok {
			t.Errorf("Text(%s) returned %q; only text files have text", withheld, text)
		}
	}
	// The link's target is recorded nowhere and resolved never.
	if kind, ok := c.Kind("current"); !ok || kind != KindOther {
		t.Errorf("a link is listed as %q, %v; want other", kind, ok)
	}
	if _, ok := c.Kind("src"); ok {
		t.Error("a directory is listed; only files are")
	}
}

func TestUnpackReadsGitHubsOwnLayout(t *testing.T) {
	raw, err := os.ReadFile("testdata/archive.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	c := unpack(t, raw, roomy)
	if got := c.Entries(); len(got) != 1 || got[0] != (Entry{Path: "README.md", Kind: KindText}) {
		t.Fatalf("entries = %v, want README.md as text and nothing else", got)
	}
	if text, _ := c.Text("README.md"); !strings.Contains(text, "reviewer") {
		t.Errorf("README.md = %q", text)
	}
}

func TestUnpackRefusesACommitOverTheLimit(t *testing.T) {
	raw := archive(t,
		entry{name: "a.txt", body: strings.Repeat("a", 600)},
		entry{name: "b.txt", body: strings.Repeat("b", 600)},
	)
	_, err := Unpack(bytes.NewReader(raw), Limits{ArchiveBytes: 1000, FileBytes: 1000})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	// Files that are not kept do not count: the limit is on what is held.
	c := unpack(t, raw, Limits{ArchiveBytes: 1000, FileBytes: 500})
	if c.Size() != int64(len("a.txt")+len("b.txt")) {
		t.Errorf("Size = %d; two paths and no text were kept", c.Size())
	}
}

func TestUnpackDoesNotTrustTheSizeInAHeader(t *testing.T) {
	// A header that claims 10 bytes over a body of 600. The tar reader stops at
	// the claim, so what is kept is within the limit either way; what matters
	// is that nothing past the limit is ever held.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "top/a.txt", Size: 600, Mode: 0o644})
	_, _ = tw.Write([]byte(strings.Repeat("a", 600)))
	_ = tw.Close()
	_ = zw.Close()

	c := unpack(t, buf.Bytes(), Limits{ArchiveBytes: 1 << 20, FileBytes: 100})
	if kind, _ := c.Kind("a.txt"); kind != KindTooLarge {
		t.Errorf("kind = %q, want too_large", kind)
	}
	if _, ok := c.Text("a.txt"); ok {
		t.Error("text was kept for a file over the limit")
	}
}

func TestUnpackKeepsPathsInsideTheCommit(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, name := range []string{
		"top/ok.txt",
		"top/../../etc/passwd",
		"top/a/../../b.txt",
		"/top/abs.txt",
		"top/./dot.txt",
		"lonely.txt",
		"top/",
	} {
		_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: 1, Mode: 0o644})
		_, _ = tw.Write([]byte("x"))
	}
	_ = tw.Close()
	_ = zw.Close()

	c := unpack(t, buf.Bytes(), roomy)
	for _, e := range c.Entries() {
		if strings.Contains(e.Path, "..") || strings.HasPrefix(e.Path, "/") || e.Path == "" {
			t.Errorf("a path left the commit: %q", e.Path)
		}
	}
	for _, want := range []string{"ok.txt", "abs.txt", "dot.txt"} {
		if _, ok := c.Kind(want); !ok {
			t.Errorf("%s is missing; entries: %v", want, c.Entries())
		}
	}
	for _, refused := range []string{"lonely.txt", "passwd", "etc/passwd", "b.txt"} {
		if _, ok := c.Kind(refused); ok {
			t.Errorf("%s was kept: an entry outside the top folder, or one whose path climbs", refused)
		}
	}
	if len(c.Entries()) != 3 {
		t.Errorf("entries = %v, want three", c.Entries())
	}
}

func TestUnpackRejectsWhatIsNotAnArchive(t *testing.T) {
	if _, err := Unpack(strings.NewReader("<html>not found</html>"), roomy); err == nil {
		t.Error("a web page was accepted as an archive")
	}
	raw := archive(t, entry{name: "a.txt", body: "hello"})
	if _, err := Unpack(bytes.NewReader(raw[:len(raw)/2]), roomy); err == nil {
		t.Error("half an archive was accepted")
	}
}

func TestSearchFindsAMatchInAFileThePullRequestDoesNotChange(t *testing.T) {
	// Search is over the commit, not over the diff: the point of it is to find
	// the callers a change did not touch.
	c := unpack(t, archive(t,
		entry{name: "src/changed.ts", body: "export function renamed() {}\n"},
		entry{name: "src/untouched.ts", body: "import { renamed } from './changed';\n\nrenamed();\n"},
		entry{name: "docs/notes.md", body: "Nothing here.\n"},
		entry{name: "blob.bin", body: "renamed\x00"},
	), roomy)

	matches, truncated := c.Search(Query{Text: "renamed"})
	if truncated {
		t.Error("truncated with three matches")
	}
	want := []Match{
		{Path: "src/changed.ts", Line: 0, Text: "export function renamed() {}"},
		{Path: "src/untouched.ts", Line: 0, Text: "import { renamed } from './changed';"},
		{Path: "src/untouched.ts", Line: 2, Text: "renamed();"},
	}
	if len(matches) != len(want) {
		t.Fatalf("matches = %+v, want %+v", matches, want)
	}
	for i := range want {
		if matches[i] != want[i] {
			t.Errorf("match %d = %+v, want %+v", i, matches[i], want[i])
		}
	}
}

func TestSearchOptions(t *testing.T) {
	c := unpack(t, archive(t,
		entry{name: "a/one.ts", body: "const Total = 1;\r\nconst total = 2;\n"},
		entry{name: "b/two.ts", body: "TOTAL\n"},
		entry{name: "c/pattern.ts", body: "a.*b\naxxb\n"},
		entry{name: "d/İ.ts", body: "İstanbul total\n"},
	), roomy)

	count := func(q Query) int {
		m, _ := c.Search(q)
		return len(m)
	}
	if got := count(Query{Text: "total"}); got != 4 {
		t.Errorf("folding case: %d matches, want 4", got)
	}
	if got := count(Query{Text: "total", CaseSensitive: true}); got != 2 {
		t.Errorf("exact case: %d matches, want 2", got)
	}
	if got := count(Query{Text: "total", PathPrefix: "a/"}); got != 2 {
		t.Errorf("under a/: %d matches, want 2", got)
	}
	if got := count(Query{Text: "a.*b"}); got != 1 {
		t.Errorf("the query is literal, not a pattern: %d matches, want 1", got)
	}
	if got := count(Query{Text: ""}); got != 0 {
		t.Errorf("an empty query matched %d lines", got)
	}
	// A carriage return is the line's ending, not its text.
	if m, _ := c.Search(Query{Text: "Total", CaseSensitive: true}); len(m) != 1 || m[0].Text != "const Total = 1;" {
		t.Errorf("matches = %+v", m)
	}
	// Lower-casing "İ" changes its length in bytes. The line is still found
	// and returned whole.
	if m, _ := c.Search(Query{Text: "TOTAL", PathPrefix: "d/"}); len(m) != 1 || m[0].Text != "İstanbul total" {
		t.Errorf("matches = %+v", m)
	}
}

func TestSearchStopsAtTheLimitAndClipsLongLines(t *testing.T) {
	limits := Limits{ArchiveBytes: 1 << 20, FileBytes: 1 << 16}
	c := unpack(t, archive(t,
		entry{name: "many.txt", body: strings.Repeat("needle\n", MaxMatches+5)},
		entry{name: "wide.txt", body: "needle " + strings.Repeat("é", 1000) + "\n"},
	), limits)

	matches, truncated := c.Search(Query{Text: "needle"})
	if len(matches) != MaxMatches || !truncated {
		t.Errorf("%d matches, truncated %v; want %d and true", len(matches), truncated, MaxMatches)
	}

	matches, truncated = c.Search(Query{Text: "needle", PathPrefix: "wide"})
	if len(matches) != 1 || truncated {
		t.Fatalf("matches = %d, truncated %v", len(matches), truncated)
	}
	text := matches[0].Text
	if len(text) > maxMatchLine+len("…") || !strings.HasSuffix(text, "…") {
		t.Errorf("a long line was returned at %d bytes", len(text))
	}
	if !strings.HasPrefix(text, "needle é") || strings.ContainsRune(text, '�') {
		t.Errorf("the clipped line cut a character in half: %q", text[len(text)-12:])
	}
}

// fetcher counts archive calls and can be made to wait.
type fetcher struct {
	mu      sync.Mutex
	calls   []string
	body    []byte
	err     error
	release chan struct{}
	started chan struct{}
}

func (f *fetcher) Archive(ctx context.Context, repo ghclient.Repo, sha string) (io.ReadCloser, error) {
	f.mu.Lock()
	f.calls = append(f.calls, repo.String()+"@"+sha)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.release != nil {
		select {
		case <-f.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader(f.body)), nil
}

func (f *fetcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestTwoRequestsForOneCommitMakeOneFetch(t *testing.T) {
	gh := &fetcher{
		body:    archive(t, entry{name: "a.txt", body: "hello"}),
		release: make(chan struct{}),
		started: make(chan struct{}, 8),
	}
	store := NewStore(NewCache(1<<20, time.Minute), roomy)

	const callers = 6
	var wg sync.WaitGroup
	var failed atomic.Int32
	commits := make([]*Commit, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c, err := store.Commit(context.Background(), gh, "acme", "app", shaA)
			if err != nil {
				failed.Add(1)
				return
			}
			commits[i] = c
		}(i)
	}
	<-gh.started
	// The first fetch is now held open. Give the others time to arrive and
	// find it running.
	time.Sleep(50 * time.Millisecond)
	close(gh.release)
	wg.Wait()

	if failed.Load() != 0 {
		t.Fatalf("%d callers failed", failed.Load())
	}
	if gh.count() != 1 {
		t.Errorf("%d fetches for one commit, want 1", gh.count())
	}
	for i := 1; i < callers; i++ {
		if commits[i] != commits[0] {
			t.Error("callers were handed different copies of one commit")
		}
	}
	// And afterwards it is answered from memory.
	if _, err := store.Commit(context.Background(), gh, "Acme", "App", strings.ToUpper(shaA)); err != nil {
		t.Fatal(err)
	}
	if gh.count() != 1 {
		t.Errorf("a second spelling of the same repository fetched again: %d fetches", gh.count())
	}
}

func TestTheSameHashUnderTwoRepositoriesIsTwoEntries(t *testing.T) {
	// The test for the access gap a hash-only key would open: memory must
	// never answer for a repository other than the one that was asked about.
	cache := NewCache(1<<20, time.Minute)
	store := NewStore(cache, roomy)
	first := &fetcher{body: archive(t, entry{name: "secret.txt", body: "repository one"})}
	second := &fetcher{body: archive(t, entry{name: "secret.txt", body: "repository two"})}

	one, err := store.Commit(context.Background(), first, "acme", "one", shaA)
	if err != nil {
		t.Fatal(err)
	}
	two, err := store.Commit(context.Background(), second, "acme", "two", shaA)
	if err != nil {
		t.Fatal(err)
	}
	if first.count() != 1 || second.count() != 1 {
		t.Errorf("fetches: %d and %d, want 1 and 1", first.count(), second.count())
	}
	if a, _ := one.Text("secret.txt"); a != "repository one" {
		t.Errorf("repository one's file reads %q", a)
	}
	if b, _ := two.Text("secret.txt"); b != "repository two" {
		t.Errorf("repository two's file reads %q: it was answered from the other repository's entry", b)
	}
	if got := cache.Stats().Entries; got != 2 {
		t.Errorf("%d entries, want 2", got)
	}
	// A different owner with the same repository name is a third.
	third := &fetcher{body: archive(t, entry{name: "secret.txt", body: "another owner"})}
	if _, err := store.Commit(context.Background(), third, "other", "one", shaA); err != nil {
		t.Fatal(err)
	}
	if third.count() != 1 || cache.Stats().Entries != 3 {
		t.Errorf("fetches %d, entries %d; want 1 and 3", third.count(), cache.Stats().Entries)
	}
}

func TestTheCacheDropsTheLeastRecentlyUsed(t *testing.T) {
	// Each commit costs 405 bytes: 400 of text and a five-byte path.
	body := archive(t, entry{name: "a.txt", body: strings.Repeat("a", 400)})
	cache := NewCache(1000, time.Minute)
	store := NewStore(cache, roomy)
	gh := &fetcher{body: body}
	get := func(sha string) {
		t.Helper()
		if _, err := store.Commit(context.Background(), gh, "acme", "app", sha); err != nil {
			t.Fatal(err)
		}
	}

	get(shaA)
	get(shaB)
	get(shaA) // A is now the more recently used.
	if gh.count() != 2 {
		t.Fatalf("%d fetches, want 2", gh.count())
	}
	get(strings.Repeat("c", 40)) // No room for three: B goes.
	if stats := cache.Stats(); stats.Entries != 2 || stats.Bytes != 810 {
		t.Errorf("stats = %+v, want 2 entries and 810 bytes", stats)
	}
	get(shaA)
	if gh.count() != 3 {
		t.Errorf("A was fetched again; it should have been kept. %d fetches", gh.count())
	}
	get(shaB)
	if gh.count() != 4 {
		t.Errorf("B should have been dropped and fetched again. %d fetches", gh.count())
	}
	if stats := cache.Stats(); stats.Bytes > 1000 {
		t.Errorf("the cache holds %d bytes, over its limit of 1000", stats.Bytes)
	}
}

func TestACommitOverTheLimitIsRememberedAndAFailureIsNot(t *testing.T) {
	store := NewStore(NewCache(1<<20, time.Minute), Limits{ArchiveBytes: 100, FileBytes: 100})
	big := &fetcher{body: archive(t,
		entry{name: "a.txt", body: strings.Repeat("a", 90)},
		entry{name: "b.txt", body: strings.Repeat("b", 90)},
	)}
	for i := 0; i < 3; i++ {
		if _, err := store.Commit(context.Background(), big, "acme", "app", shaA); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	}
	if big.count() != 1 {
		t.Errorf("a commit known to be too large was fetched %d times", big.count())
	}

	down := &fetcher{err: errors.New("GitHub is down")}
	for i := 0; i < 2; i++ {
		if _, err := store.Commit(context.Background(), down, "acme", "app", shaB); err == nil {
			t.Fatal("no error")
		}
	}
	if down.count() != 2 {
		t.Errorf("a failed fetch was remembered: %d fetches, want 2", down.count())
	}
	// And once GitHub is back, the same commit loads.
	down.err, down.body = nil, archive(t, entry{name: "a.txt", body: "ok"})
	if _, err := store.Commit(context.Background(), down, "acme", "app", shaB); err != nil {
		t.Errorf("after recovery: %v", err)
	}
}

func TestAnAbandonedRequestDoesNotFailTheOthers(t *testing.T) {
	gh := &fetcher{
		body:    archive(t, entry{name: "a.txt", body: "hello"}),
		release: make(chan struct{}),
		started: make(chan struct{}, 2),
	}
	store := NewStore(NewCache(1<<20, time.Minute), roomy)

	first, abandon := context.WithCancel(context.Background())
	firstDone := make(chan error, 1)
	go func() {
		_, err := store.Commit(first, gh, "acme", "app", shaA)
		firstDone <- err
	}()
	<-gh.started

	secondDone := make(chan error, 1)
	go func() {
		_, err := store.Commit(context.Background(), gh, "acme", "app", shaA)
		secondDone <- err
	}()
	time.Sleep(20 * time.Millisecond)

	abandon()
	if err := <-firstDone; !errors.Is(err, context.Canceled) {
		t.Errorf("the abandoned request returned %v, want its own cancellation", err)
	}
	close(gh.release)
	if err := <-secondDone; err != nil {
		t.Errorf("the request that stayed got %v", err)
	}
	if gh.count() != 1 {
		t.Errorf("%d fetches, want 1", gh.count())
	}
}

func TestAFetchThatTakesTooLongIsStopped(t *testing.T) {
	gh := &fetcher{release: make(chan struct{})} // never released
	store := NewStore(NewCache(1<<20, 30*time.Millisecond), roomy)

	started := time.Now()
	_, err := store.Commit(context.Background(), gh, "acme", "app", shaA)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want a deadline", err)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("gave up after %v", took)
	}
}

func TestALoadThatPanicsIsAnErrorNotACrash(t *testing.T) {
	cache := NewCache(1<<20, time.Minute)
	_, err := cache.Get(context.Background(), NewKey("a", "b", shaA, "test"), func(context.Context) (Sized, error) {
		panic("a malformed archive")
	})
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(err.Error(), "malformed") {
		t.Errorf("the panic's text reached the caller: %v", err)
	}
	// The key is free again.
	value, err := cache.Get(context.Background(), NewKey("a", "b", shaA, "test"), func(context.Context) (Sized, error) {
		return &Commit{size: 1}, nil
	})
	if err != nil || value == nil {
		t.Errorf("after a panic the key could not be loaded: %v", err)
	}
}

func TestSearchForAWholeWord(t *testing.T) {
	c := unpack(t, archive(t,
		entry{name: "a.ts", body: "add(a, b)\naddress = 1\nreadd()\nx.add\n$add\nadd_one\n  add\nañadd add!\n"},
	), roomy)
	matches, _ := c.Search(Query{Text: "add", CaseSensitive: true, WholeWord: true})
	var lines []int
	for _, m := range matches {
		lines = append(lines, m.Line)
	}
	// "add(a, b)", "x.add", "  add" and the second word of the last line.
	want := []int{0, 3, 6, 7}
	if len(lines) != len(want) {
		t.Fatalf("matched lines %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("matched lines %v, want %v", lines, want)
		}
	}
	if m, _ := c.Search(Query{Text: "add"}); len(m) != 8 {
		t.Errorf("without WholeWord: %d lines, want all 8", len(m))
	}
}

func TestPeekDoesNotLoad(t *testing.T) {
	cache := NewCache(1<<20, time.Minute)
	key := NewKey("a", "b", shaA, "test")
	if _, ok := cache.Peek(key); ok {
		t.Fatal("an empty cache held something")
	}
	if _, err := cache.Get(context.Background(), key, func(context.Context) (Sized, error) { return &Commit{size: 7}, nil }); err != nil {
		t.Fatal(err)
	}
	if v, ok := cache.Peek(key); !ok || v.Size() != 7 {
		t.Errorf("Peek = %v, %v", v, ok)
	}
	// A remembered failure is not a value.
	bad := NewKey("a", "b", shaB, "test")
	_, _ = cache.Get(context.Background(), bad, func(context.Context) (Sized, error) { return nil, Permanent{Err: ErrTooLarge} })
	if _, ok := cache.Peek(bad); ok {
		t.Error("Peek returned a remembered failure as a value")
	}
}
