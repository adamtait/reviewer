// SPDX-License-Identifier: MIT

package navigation

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/source"
	"github.com/adamtait/reviewer/internal/workspace/testgithub"
)

const (
	sha      = "1111111111111111111111111111111111111111"
	otherSHA = "2222222222222222222222222222222222222222"
	workflow = ".github/workflows/review-index.yml"
)

// fakeGitHub answers navigation's four calls from memory and counts them.
type fakeGitHub struct {
	mu        sync.Mutex
	archive   []byte
	runs      []ghclient.WorkflowRun
	artifacts map[int64][]ghclient.Artifact
	zips      map[int64][]byte
	failRuns  error
	calls     map[string]int
}

func (f *fakeGitHub) count(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[call]++
}

func (f *fakeGitHub) made(call string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[call]
}

func (f *fakeGitHub) Archive(context.Context, ghclient.Repo, string) (io.ReadCloser, error) {
	f.count("archive")
	return io.NopCloser(bytes.NewReader(f.archive)), nil
}

func (f *fakeGitHub) ListWorkflowRuns(_ context.Context, _ ghclient.Repo, file, head, event string) ([]ghclient.WorkflowRun, error) {
	f.count("runs")
	if f.failRuns != nil {
		return nil, f.failRuns
	}
	if file != "review-index.yml" || event != "push" {
		return nil, errors.New("the wrong workflow or event was asked for")
	}
	_ = head // Deliberately not filtered: the service must check each run itself.
	return f.runs, nil
}

func (f *fakeGitHub) ListRunArtifacts(_ context.Context, _ ghclient.Repo, runID int64, _ string) ([]ghclient.Artifact, error) {
	f.count("artifacts")
	return f.artifacts[runID], nil
}

func (f *fakeGitHub) DownloadArtifact(_ context.Context, _ ghclient.Repo, id int64) (io.ReadCloser, error) {
	f.count("download")
	body, ok := f.zips[id]
	if !ok {
		return nil, ghclient.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func run(id int64, head, status, conclusion string, minutesAgo int) ghclient.WorkflowRun {
	return ghclient.WorkflowRun{
		ID: id, HeadSHA: head, Event: "push", Path: workflow, Status: status, Conclusion: conclusion,
		CreatedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC).Add(-time.Duration(minutesAgo) * time.Minute),
	}
}

type world struct {
	gh      *fakeGitHub
	service *Service
	cache   *source.Cache
	now     time.Time
}

// newWorld is a repository whose commit is the fixture project, with one
// successful run that saved the fixture's full index.
func newWorld(t *testing.T) *world {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "index.full.scip"))
	if err != nil {
		t.Fatal(err)
	}
	gh := &fakeGitHub{
		archive:   testgithub.Archive(t, project(t)),
		runs:      []ghclient.WorkflowRun{run(10, sha, "completed", "success", 5)},
		artifacts: map[int64][]ghclient.Artifact{10: {{ID: 100, Name: "review-index", SizeInBytes: int64(len(raw))}}},
		zips:      map[int64][]byte{100: zipOf(t, map[string][]byte{IndexFile: raw})},
	}
	cache := source.NewCache(1<<24, time.Minute)
	w := &world{gh: gh, cache: cache, now: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)}
	w.service = NewService(Config{WorkflowFile: "review-index.yml", ArtifactName: "review-index", IndexBytes: 1 << 20},
		cache, source.NewStore(cache, limits))
	w.service.SetClock(func() time.Time { return w.now })
	return w
}

func (w *world) lookup(t *testing.T) (*Index, Status) {
	t.Helper()
	index, status, err := w.service.Lookup(context.Background(), w.gh, "example", "alpha", sha)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if (index != nil) != (status.State == StateReady) {
		t.Fatalf("index %v with state %q: an index is returned exactly when the state is ready", index != nil, status.State)
	}
	return index, status
}

func TestAReadyIndexIsLoadedOnceAndHeld(t *testing.T) {
	w := newWorld(t)
	index, status := w.lookup(t)
	want := Status{State: StateReady, RunID: 10, ToolVersion: "0.6.2", Documents: 6}
	if status != want {
		t.Errorf("status = %+v, want %+v", status, want)
	}
	if index.Documents() != 6 {
		t.Errorf("%d documents", index.Documents())
	}

	for i := 0; i < 3; i++ {
		again, status := w.lookup(t)
		if again != index || status != want {
			t.Error("a second lookup did not return the held index")
		}
	}
	for call, n := range map[string]int{"runs": 1, "artifacts": 1, "download": 1, "archive": 1} {
		if got := w.gh.made(call); got != n {
			t.Errorf("%d %s calls, want %d: a held index needs no call to GitHub", got, call, n)
		}
	}
	// The commit's files and the index: two entries under one byte limit.
	if w.cache.Stats().Entries != 2 {
		t.Errorf("%d cache entries, want the commit and its index", w.cache.Stats().Entries)
	}
}

func TestTheFourStates(t *testing.T) {
	for name, tc := range map[string]struct {
		runs  []ghclient.WorkflowRun
		state string
		runID int64
	}{
		"no run at all": {nil, StateNone, 0},
		"one success":   {[]ghclient.WorkflowRun{run(10, sha, "completed", "success", 5)}, StateReady, 10},
		"still running": {[]ghclient.WorkflowRun{run(11, sha, "in_progress", "", 1)}, StateRunning, 11},
		"queued":        {[]ghclient.WorkflowRun{run(11, sha, "queued", "", 1)}, StateRunning, 11},
		"failed":        {[]ghclient.WorkflowRun{run(12, sha, "completed", "failure", 3)}, StateFailed, 12},
		"cancelled":     {[]ghclient.WorkflowRun{run(12, sha, "completed", "cancelled", 3)}, StateFailed, 12},
		// A success wins over a newer run that failed or is still going: the
		// index it built is of this commit, and that is what was asked for.
		"a success and a newer failure": {[]ghclient.WorkflowRun{
			run(13, sha, "completed", "failure", 1), run(10, sha, "completed", "success", 5)}, StateReady, 10},
		"a success and a newer run in progress": {[]ghclient.WorkflowRun{
			run(14, sha, "in_progress", "", 0), run(10, sha, "completed", "success", 5)}, StateReady, 10},
		// Two successes: the newest.
		"two successes": {[]ghclient.WorkflowRun{
			run(10, sha, "completed", "success", 5), run(15, sha, "completed", "success", 2)}, StateReady, 15},
		// A failure and a re-run still going: running, not failed.
		"a failure and a newer run in progress": {[]ghclient.WorkflowRun{
			run(12, sha, "completed", "failure", 3), run(16, sha, "in_progress", "", 0)}, StateRunning, 16},
		"two failures": {[]ghclient.WorkflowRun{
			run(12, sha, "completed", "failure", 3), run(17, sha, "completed", "failure", 1)}, StateFailed, 17},
	} {
		state, bound := classify(tc.runs, sha, workflow)
		var id int64
		if bound != nil {
			id = bound.ID
		}
		if state != tc.state || id != tc.runID {
			t.Errorf("%s: state %q from run %d, want %q from run %d", name, state, id, tc.state, tc.runID)
		}
	}
}

// The binding. An index is accepted only from a run GitHub reports as a push
// run of the index workflow file for the commit on screen. Each of these runs
// succeeded, and none may be used.
func TestARunThatIsNotBoundToThisCommitIsRefused(t *testing.T) {
	another := run(20, otherSHA, "completed", "success", 1)
	pullRequest := run(21, sha, "completed", "success", 1)
	pullRequest.Event = "pull_request"
	otherWorkflow := run(22, sha, "completed", "success", 1)
	otherWorkflow.Path = ".github/workflows/something-else.yml"
	lookalike := run(23, sha, "completed", "success", 1)
	lookalike.Path = "review-index.yml"

	for name, r := range map[string]ghclient.WorkflowRun{
		"a run for another commit":           another,
		"a run a pull request started":       pullRequest,
		"a run of another workflow file":     otherWorkflow,
		"a run of a file with the same name": lookalike,
		"a run whose commit differs by case": run(24, strings.ToUpper(sha[:1])+sha[1:]+"x", "completed", "success", 1),
	} {
		w := newWorld(t)
		w.gh.runs = []ghclient.WorkflowRun{r}
		w.gh.artifacts[r.ID] = w.gh.artifacts[10]
		index, status := w.lookup(t)
		if index != nil || status.State != StateNone {
			t.Errorf("%s: state %q; an index from it was accepted", name, status.State)
		}
		if w.gh.made("download") != 0 {
			t.Errorf("%s: its artifact was downloaded", name)
		}
	}
}

func TestAnArtifactThatCannotBeUsedIsNoIndexWithAReason(t *testing.T) {
	raw, _ := os.ReadFile(filepath.Join("testdata", "index.full.scip"))
	for name, tc := range map[string]struct {
		change func(w *world)
		reason string
	}{
		"no artifact": {func(w *world) { w.gh.artifacts[10] = nil }, "saved no index"},
		"an artifact with another name": {func(w *world) {
			w.gh.artifacts[10] = []ghclient.Artifact{{ID: 100, Name: "something-else"}}
		}, "saved no index"},
		"an expired artifact":               {func(w *world) { w.gh.artifacts[10][0].Expired = true }, "expired"},
		"an artifact reported as too large": {func(w *world) { w.gh.artifacts[10][0].SizeInBytes = 1 << 30 }, "larger than"},
		"an artifact that is too large whatever it reports": {func(w *world) {
			w.gh.zips[100] = bytes.Repeat([]byte{'x'}, 1<<20+10)
		}, "larger than"},
		"not a zip": {func(w *world) { w.gh.zips[100] = []byte("<html>error</html>") }, "not a zip"},
		"two files": {func(w *world) {
			w.gh.zips[100] = zipOf(t, map[string][]byte{IndexFile: raw, "extra.txt": []byte("x")})
		}, "exactly one file"},
		"one file with another name": {func(w *world) {
			w.gh.zips[100] = zipOf(t, map[string][]byte{"index.scip.bak": raw})
		}, "exactly one file"},
		"the right name inside a folder": {func(w *world) {
			w.gh.zips[100] = zipOf(t, map[string][]byte{"out/" + IndexFile: raw})
		}, "exactly one file"},
		"a file that expands past the limit": {func(w *world) {
			w.gh.zips[100] = zipOf(t, map[string][]byte{IndexFile: bytes.Repeat([]byte{0}, 1<<20+1)})
		}, "larger than"},
		"a file that is not an index": {func(w *world) {
			w.gh.zips[100] = zipOf(t, map[string][]byte{IndexFile: []byte("this is not protobuf at all \xff\xff\xff")})
		}, "could not be read"},
		"an index of some other repository": {func(w *world) {
			w.gh.archive = testgithub.Archive(t, map[string]string{"README.md": "nothing the index describes\n"})
		}, "describes no file"},
	} {
		w := newWorld(t)
		tc.change(w)
		index, status := w.lookup(t)
		if index != nil || status.State != StateNone || !strings.Contains(status.Reason, tc.reason) {
			t.Errorf("%s: state %q, reason %q; want none and a reason containing %q", name, status.State, status.Reason, tc.reason)
		}
		if status.RunID != 10 {
			t.Errorf("%s: the status names run %d, want the run whose artifact it was", name, status.RunID)
		}
	}
}

func TestAStateIsRememberedBrieflyAndThenAskedAgain(t *testing.T) {
	w := newWorld(t)
	w.gh.runs = []ghclient.WorkflowRun{run(11, sha, "in_progress", "", 1)}

	if _, status := w.lookup(t); status.State != StateRunning || status.RunID != 11 {
		t.Fatalf("status = %+v", status)
	}
	// Ten lookups in the same moment, as a page full of them would make, ask
	// GitHub once.
	for i := 0; i < 10; i++ {
		w.lookup(t)
	}
	if got := w.gh.made("runs"); got != 1 {
		t.Errorf("%d calls for the runs, want 1", got)
	}

	// The job finishes. Within the remembered period nothing changes; after
	// it, the index is found without anyone reloading anything.
	w.gh.runs = []ghclient.WorkflowRun{run(11, sha, "completed", "success", 1)}
	w.gh.artifacts[11] = w.gh.artifacts[10]
	w.now = w.now.Add(rememberState - time.Second)
	if _, status := w.lookup(t); status.State != StateRunning {
		t.Errorf("before the period ended: %q", status.State)
	}
	w.now = w.now.Add(2 * time.Second)
	if index, status := w.lookup(t); status.State != StateReady || index == nil || status.RunID != 11 {
		t.Errorf("after the period ended: %+v", status)
	}
}

func TestAnUnusableArtifactIsNotDownloadedOnEveryLookup(t *testing.T) {
	w := newWorld(t)
	w.gh.zips[100] = []byte("not a zip")
	for i := 0; i < 5; i++ {
		w.lookup(t)
	}
	if got := w.gh.made("download"); got != 1 {
		t.Errorf("%d downloads of an artifact known to be unusable, want 1", got)
	}
	// A re-run that saves a good index is picked up once the period ends.
	raw, _ := os.ReadFile(filepath.Join("testdata", "index.full.scip"))
	w.gh.runs = append(w.gh.runs, run(30, sha, "completed", "success", 0))
	w.gh.artifacts[30] = []ghclient.Artifact{{ID: 300, Name: "review-index"}}
	w.gh.zips[300] = zipOf(t, map[string][]byte{IndexFile: raw})
	w.now = w.now.Add(rememberUnusable + time.Second)
	if index, status := w.lookup(t); index == nil || status.RunID != 30 {
		t.Errorf("after a re-run: %+v", status)
	}
}

func TestWhenGitHubCannotBeAskedThatIsAnErrorNotAState(t *testing.T) {
	w := newWorld(t)
	w.gh.failRuns = ghclient.ErrForbidden
	_, _, err := w.service.Lookup(context.Background(), w.gh, "example", "alpha", sha)
	if !errors.Is(err, ghclient.ErrForbidden) {
		t.Errorf("err = %v; want GitHub's refusal passed on, so the user is told what to fix", err)
	}
	// And it is not remembered as "no index".
	w.gh.failRuns = nil
	if index, _ := w.lookup(t); index == nil {
		t.Error("after GitHub recovered, the index was not loaded")
	}
}

func TestIndexesOfTwoRepositoriesAtOneHashAreKeptApart(t *testing.T) {
	w := newWorld(t)
	if _, status, err := w.service.Lookup(context.Background(), w.gh, "example", "alpha", sha); err != nil || status.State != StateReady {
		t.Fatalf("alpha: %+v, %v", status, err)
	}
	// Another repository, with no runs, asked about the same hash.
	empty := &fakeGitHub{archive: w.gh.archive}
	index, status, err := w.service.Lookup(context.Background(), empty, "example", "beta", sha)
	if err != nil || index != nil || status.State != StateNone {
		t.Errorf("beta was answered with %+v, %v: alpha's index must not be found under beta's name", status, err)
	}
	if empty.made("runs") != 1 {
		t.Error("beta's state was answered without asking GitHub about beta")
	}
}
