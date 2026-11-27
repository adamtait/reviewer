// SPDX-License-Identifier: MIT

package navigation

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/scip"
	"github.com/adamtait/reviewer/internal/workspace/source"
	"github.com/adamtait/reviewer/internal/workspace/testgithub"
)

// fixtureRoot is where the fixture's indexes were built. An index records the
// path it was built at, and the README beside the fixture says to build there.
const fixtureRoot = "file:///tmp/review-workspace-fixture/tsproject"

var limits = source.Limits{ArchiveBytes: 1 << 22, FileBytes: 1 << 20}

// commitOf builds a commit from files, through the same unpacking a real
// archive goes through.
func commitOf(t testing.TB, files map[string]string) *source.Commit {
	t.Helper()
	tt, ok := t.(*testing.T)
	if !ok {
		t.Fatal("commitOf needs a *testing.T")
	}
	commit, err := source.Unpack(bytes.NewReader(testgithub.Archive(tt, files)), limits)
	if err != nil {
		t.Fatal(err)
	}
	return commit
}

// project reads the fixture project's files.
func project(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	root := filepath.Join("testdata", "tsproject")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		files[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func load(t *testing.T, name string, commit *source.Commit) *Index {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := scip.Decode(raw, scip.Limits{Occurrences: 100000})
	if err != nil {
		t.Fatal(err)
	}
	return Build(decoded, commit)
}

type goldenRow struct {
	Path      string   `json:"path"`
	Line      int      `json:"line"`
	Character int      `json:"character"`
	Label     string   `json:"label"`
	Targets   [][3]any `json:"targets"`
	Package   string   `json:"package"`
}

func golden(t *testing.T, name string) []goldenRow {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var rows []goldenRow
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row goldenRow
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rows = append(rows, row)
	}
	return rows
}

func spots(targets []Location) []string {
	out := make([]string, 0, len(targets))
	for _, at := range targets {
		out = append(out, fmt.Sprintf("%s:%d:%d", at.Path, at.Line, at.Character))
	}
	sort.Strings(out)
	return out
}

// The Go rules against the reference: for every identifier in the fixture, the
// same label and the same targets as docs/designs/trials/scip_lookup.py, whose
// answers were scored against the TypeScript compiler.
func TestTheRulesMatchTheReferenceForEveryIdentifier(t *testing.T) {
	commit := commitOf(t, project(t))
	for _, tc := range []struct{ index, golden string }{
		{"index.full.scip", "golden.full.jsonl"},
		{"index.partial.scip", "golden.partial.jsonl"},
	} {
		ix := load(t, tc.index, commit)
		rows := golden(t, tc.golden)
		if len(rows) < 50 {
			t.Fatalf("%s holds %d rows; the fixture has more identifiers than that", tc.golden, len(rows))
		}
		for _, row := range rows {
			got := FindDefinition(ix, commit, row.Path, row.Line, row.Character)
			where := fmt.Sprintf("%s %s:%d:%d", tc.index, row.Path, row.Line+1, row.Character+1)

			wantLabel := row.Label
			switch wantLabel {
			case "definition":
				// The reference names this case; the service answers it as a
				// result from the index, pointing at the place asked about.
				wantLabel = LabelIndex
			case "text_search":
				// The reference stops at "search". The service runs the search,
				// and says "none" when it finds nothing.
				if got.Label == LabelNone {
					wantLabel = LabelNone
				}
			}
			if got.Label != wantLabel {
				t.Errorf("%s: label %q, want %q", where, got.Label, wantLabel)
				continue
			}
			var want []string
			for _, target := range row.Targets {
				want = append(want, fmt.Sprintf("%v:%v:%v", target[0], target[1], target[2]))
			}
			sort.Strings(want)
			if got.Label == LabelIndex && strings.Join(spots(got.Targets), " ") != strings.Join(want, " ") {
				t.Errorf("%s: targets %v, want %v", where, spots(got.Targets), want)
			}
			if got.Label == LabelExternal && got.Package+" "+got.Version != row.Package {
				t.Errorf("%s: package %q %q, want %q", where, got.Package, got.Version, row.Package)
			}
		}
	}
}

// The reference's own answers against the compiler's, so that the golden file
// cannot drift into agreeing with the rules and disagreeing with TypeScript.
func TestTheRulesNeverContradictTheCompiler(t *testing.T) {
	commit := commitOf(t, project(t))
	ix := load(t, "index.full.scip", commit)

	f, err := os.Open(filepath.Join("testdata", "compiler.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	type def struct {
		Path string `json:"path"`
		Line int    `json:"line"`
	}
	var right, unresolved, outside, checked int
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var row struct {
			Path      string `json:"path"`
			Line      int    `json:"line"`
			Character int    `json:"character"`
			Text      string `json:"text"`
			Defs      []def  `json:"defs"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		checked++
		inRepo := map[string]bool{}
		for _, d := range row.Defs {
			if !strings.HasPrefix(d.Path, "..") && !strings.Contains(d.Path, "node_modules") {
				inRepo[fmt.Sprintf("%s:%d", d.Path, d.Line)] = true
			}
		}
		got := FindDefinition(ix, commit, row.Path, row.Line, row.Character)
		switch {
		case got.Label != LabelIndex:
			if len(inRepo) > 0 {
				unresolved++
			} else {
				outside++
			}
		case len(inRepo) == 0:
			// The compiler places it outside the repository, or nowhere, and
			// the rules pointed inside. Unless the position is its own
			// definition, that is a wrong answer.
			if !(len(got.Targets) == 1 && got.Targets[0].Path == row.Path && got.Targets[0].Line == row.Line) {
				t.Errorf("%s:%d %q: the rules point at %v and the compiler has no definition in the repository",
					row.Path, row.Line+1, row.Text, spots(got.Targets))
			}
		default:
			agreed := false
			for _, target := range got.Targets {
				if inRepo[fmt.Sprintf("%s:%d", target.Path, target.Line)] {
					agreed = true
				}
			}
			if !agreed && !(got.Targets[0].Path == row.Path && got.Targets[0].Line == row.Line) {
				t.Errorf("%s:%d %q: the rules point at %v, the compiler at %v",
					row.Path, row.Line+1, row.Text, spots(got.Targets), inRepo)
			} else {
				right++
			}
		}
	}
	if checked < 60 {
		t.Fatalf("only %d identifiers were checked", checked)
	}
	// Not resolving is allowed, and is honest: the user gets a text search.
	// Pointing at the wrong place is what must never happen.
	if right < 50 {
		t.Errorf("%d of %d identifiers resolved to where the compiler says; expected at least 50 (unresolved %d, outside %d)",
			right, checked, unresolved, outside)
	}
}

func TestTheFixtureIndexesWereBuiltAtTheFixedPath(t *testing.T) {
	commit := commitOf(t, project(t))
	for _, name := range []string{"index.full.scip", "index.partial.scip"} {
		ix := load(t, name, commit)
		if ix.ProjectRoot != fixtureRoot {
			t.Errorf("%s was built at %q; rebuild it at %s, as testdata/README.md says, so the index does not record a contributor's own path",
				name, ix.ProjectRoot, fixtureRoot)
		}
		if ix.ToolName != "scip-typescript" || ix.ToolVersion != "0.6.2" {
			t.Errorf("%s was built by %s %s; the installer pins scip-typescript 0.6.2", name, ix.ToolName, ix.ToolVersion)
		}
	}
}

// find returns the position of the nth occurrence of word in a file, counted
// in UTF-16 units as a browser would send it.
func find(t *testing.T, files map[string]string, path, word string, nth int) (line, character int) {
	t.Helper()
	for n, text := range strings.Split(files[path], "\n") {
		from := 0
		for {
			i := strings.Index(text[from:], word)
			if i < 0 {
				break
			}
			if nth == 0 {
				units := 0
				for _, r := range text[:from+i] {
					if r >= 0x10000 {
						units += 2
					} else {
						units++
					}
				}
				return n, units
			}
			nth--
			from += i + len(word)
		}
	}
	t.Fatalf("%s does not contain %q that many times", path, word)
	return 0, 0
}

func TestEachLabel(t *testing.T) {
	files := project(t)
	commit := commitOf(t, files)
	ix := load(t, "index.full.scip", commit)
	definition := func(path, word string, nth int) Definition {
		line, character := find(t, files, path, word, nth)
		return FindDefinition(ix, commit, path, line, character)
	}

	t.Run("from the index", func(t *testing.T) {
		got := definition("src/cart.ts", "add(sum", 0)
		if got.Label != LabelIndex || strings.Join(spots(got.Targets), " ") != "src/money.ts:12:16" {
			t.Errorf("add = %+v", got)
		}
	})

	t.Run("a name that is both a type and a value gives both", func(t *testing.T) {
		got := definition("src/cart.ts", "Money, add", 0)
		if got.Label != LabelIndex || strings.Join(spots(got.Targets), " ") != "src/money.ts:1:17 src/money.ts:6:13" {
			t.Errorf("Money in an import = %+v; want the interface and the constant", got)
		}
	})

	t.Run("a quoted import path opens the file", func(t *testing.T) {
		got := definition("src/cart.ts", "'./money'", 0)
		if got.Label != LabelIndex || strings.Join(spots(got.Targets), " ") != "src/money.ts:0:0" {
			t.Errorf("'./money' = %+v", got)
		}
	})

	t.Run("new Cart() goes to the constructor", func(t *testing.T) {
		got := definition("src/receipt.ts", "Cart(prices)", 0)
		if got.Label != LabelIndex || strings.Join(spots(got.Targets), " ") != "src/cart.ts:7:2" {
			t.Errorf("new Cart = %+v", got)
		}
		if text, _ := textAt(commit, got.Targets[0]); text != "constructor" {
			t.Errorf("the target reads %q", text)
		}
	})

	t.Run("a namespace import is not sent to the top of a file", func(t *testing.T) {
		// The index points `tax` at the whole of tax.ts. `tax` is not a quoted
		// path, so the whole-file target is refused and the word is searched.
		got := definition("src/cart.ts", "tax.withTax", 0)
		if got.Label != LabelTextSearch || len(got.Matches) == 0 {
			t.Errorf("tax = %+v; want a text search", got)
		}
	})

	t.Run("defined outside the repository", func(t *testing.T) {
		got := definition("src/receipt.ts", "format(total", 0)
		if got.Label != LabelExternal || got.Package != "tiny-format" || got.Version != "2.1.0" || len(got.Targets) != 0 {
			t.Errorf("format = %+v", got)
		}
	})

	t.Run("text search, for a file the index does not cover", func(t *testing.T) {
		got := definition("docs/notes.md", "withTax", 0)
		if got.Label != LabelTextSearch {
			t.Fatalf("withTax in the notes = %+v", got)
		}
		paths := map[string]bool{}
		for _, m := range got.Matches {
			paths[m.Path] = true
		}
		if !paths["src/tax.ts"] || !paths["src/cart.ts"] {
			t.Errorf("matches in %v; want the definition and the caller", paths)
		}
	})

	t.Run("not found", func(t *testing.T) {
		withOdd := map[string]string{}
		for k, v := range files {
			withOdd[k] = v
		}
		withOdd["docs/odd.md"] = "zzyzx appears once\n\n  ==> \n"
		odd := commitOf(t, withOdd)
		// A position on no word has nothing to search for.
		if got := FindDefinition(ix, odd, "docs/odd.md", 1, 0); got.Label != LabelNone {
			t.Errorf("a position on an empty line = %+v", got)
		}
		if got := FindDefinition(ix, odd, "docs/odd.md", 2, 3); got.Label != LabelNone {
			t.Errorf("a position on punctuation = %+v", got)
		}
		if got := FindDefinition(ix, odd, "docs/absent.md", 0, 0); got.Label != LabelNone {
			t.Errorf("a file that is not there = %+v", got)
		}
		if got := FindDefinition(ix, odd, "docs/odd.md", 99, 0); got.Label != LabelNone {
			t.Errorf("a line past the end = %+v", got)
		}
	})

	t.Run("a position that is itself a definition answers with itself", func(t *testing.T) {
		line, character := find(t, files, "src/money.ts", "add(a", 0)
		got := FindDefinition(ix, commit, "src/money.ts", line, character)
		if got.Label != LabelIndex || len(got.Targets) != 1 || got.Targets[0].Line != line || got.Targets[0].Character != character {
			t.Errorf("add at its definition = %+v", got)
		}
	})

	t.Run("positions count UTF-16 units", func(t *testing.T) {
		// `pad` on the last line of strings.ts follows a character outside the
		// basic plane, which is two units and four bytes.
		line, character := find(t, files, "src/util/strings.ts", "pad(greeting", 0)
		got := FindDefinition(ix, commit, "src/util/strings.ts", line, character)
		if got.Label != LabelIndex || strings.Join(spots(got.Targets), " ") != "src/util/strings.ts:0:16" {
			t.Errorf("pad after a wide character, at %d:%d = %+v", line, character, got)
		}
	})
}

func TestWithNoIndexEveryLookupIsATextSearch(t *testing.T) {
	files := project(t)
	commit := commitOf(t, files)
	line, character := find(t, files, "src/cart.ts", "add(sum", 0)
	got := FindDefinition(nil, commit, "src/cart.ts", line, character)
	if got.Label != LabelTextSearch || len(got.Targets) != 0 || len(got.Matches) < 2 {
		t.Errorf("with no index = %+v", got)
	}
	// Whole words only: `add` is not found in `padStart`.
	for _, m := range got.Matches {
		if strings.Contains(m.Text, "padStart") {
			t.Errorf("a search for add matched %q", m.Text)
		}
	}
	refs := FindReferences(nil, commit, "src/cart.ts", line, character)
	if refs.Label != LabelTextSearch || len(refs.References) != 0 || len(refs.Matches) < 2 {
		t.Errorf("references with no index = %+v", refs)
	}
}

// The fault the own-package check exists for. The partial index covers two
// files. Symbols from the rest of the repository have no definition in it and
// name the repository's own package, exactly as a dependency's would.
func TestAPartialIndexDoesNotCallTheRepositorysOwnCodeExternal(t *testing.T) {
	files := project(t)
	commit := commitOf(t, files)
	ix := load(t, "index.partial.scip", commit)
	if ix.Documents() != 2 {
		t.Fatalf("the partial index covers %d files, want 2", ix.Documents())
	}

	line, character := find(t, files, "src/cart.ts", "add(sum", 0)
	got := FindDefinition(ix, commit, "src/cart.ts", line, character)
	if got.Label == LabelExternal {
		t.Fatalf("add, which is defined in src/money.ts, was labelled as outside the repository: %+v", got)
	}
	if got.Label != LabelTextSearch {
		t.Errorf("add = %+v; want a text search", got)
	}
	found := false
	for _, m := range got.Matches {
		if m.Path == "src/money.ts" && strings.Contains(m.Text, "function add") {
			found = true
		}
	}
	if !found {
		t.Error("the text search did not find the real definition")
	}

	// A real dependency is still external.
	line, character = find(t, files, "src/receipt.ts", "format(total", 0)
	if got := FindDefinition(ix, commit, "src/receipt.ts", line, character); got.Label != LabelExternal || got.Package != "tiny-format" {
		t.Errorf("format = %+v", got)
	}

	// Without a package.json that names the package, the same symbol would be
	// external: the check is what made the difference.
	delete(files, "package.json")
	bare := commitOf(t, files)
	line, character = find(t, files, "src/cart.ts", "add(sum", 0)
	if got := FindDefinition(load(t, "index.partial.scip", bare), bare, "src/cart.ts", line, character); got.Label != LabelExternal {
		t.Errorf("with no package.json, add = %+v; the test would prove nothing", got)
	}
}

func TestReferencesListTheIndexBesideTheTextMatches(t *testing.T) {
	files := project(t)
	commit := commitOf(t, files)
	ix := load(t, "index.full.scip", commit)

	line, character := find(t, files, "src/money.ts", "add(a", 0)
	got := FindReferences(ix, commit, "src/money.ts", line, character)
	if got.Label != LabelIndex || got.Truncated {
		t.Fatalf("references = %+v", got)
	}
	want := "src/cart.ts:0:16 src/cart.ts:14:12 src/index.ts:1:16 src/money.ts:12:16"
	if strings.Join(spots(got.References), " ") != want {
		t.Errorf("references = %v, want %s", spots(got.References), want)
	}
	// The text matches are there too, so a partial index is never the last word.
	if len(got.Matches) < 4 {
		t.Errorf("%d text matches beside the index's list", len(got.Matches))
	}

	// A local variable's references stay in its own file.
	line, character = find(t, files, "src/cart.ts", "sum = Money", 0)
	local := FindReferences(ix, commit, "src/cart.ts", line, character)
	for _, at := range local.References {
		if at.Path != "src/cart.ts" {
			t.Errorf("a local variable has a reference in %s", at.Path)
		}
	}
	if len(local.References) != 4 {
		t.Errorf("sum has %d references in the index, want 4: %v", len(local.References), spots(local.References))
	}
}

func TestReferencesStopAtTheLimit(t *testing.T) {
	var body strings.Builder
	body.WriteString("export const x = 1;\n")
	for i := 0; i < MaxReferences+20; i++ {
		body.WriteString("x;\n")
	}
	commit := commitOf(t, map[string]string{"a.ts": body.String()})
	doc := scip.Document{Path: "a.ts", UTF16: true}
	doc.Occurrences = append(doc.Occurrences, scip.Occurrence{Range: scip.Range{StartLine: 0, StartChar: 13, EndLine: 0, EndChar: 14}, Symbol: "s m p 1 x.", Definition: true})
	for i := 1; i <= MaxReferences+20; i++ {
		doc.Occurrences = append(doc.Occurrences, scip.Occurrence{Range: scip.Range{StartLine: int32(i), StartChar: 0, EndLine: int32(i), EndChar: 1}, Symbol: "s m p 1 x."})
	}
	ix := Build(&scip.Index{Documents: []scip.Document{doc}}, commit)
	got := FindReferences(ix, commit, "a.ts", 0, 13)
	if len(got.References) != MaxReferences || !got.Truncated {
		t.Errorf("%d references, truncated %v; want %d and true", len(got.References), got.Truncated, MaxReferences)
	}
}

func TestBuildDropsWhatDoesNotFitTheCommit(t *testing.T) {
	commit := commitOf(t, map[string]string{
		"a.ts":     "const total = 1;\nexport { total };\n",
		"logo.png": "\x89PNG\x00",
	})
	symbol := "s m p 1 total."
	occ := func(line, start, end int32, def bool) scip.Occurrence {
		return scip.Occurrence{Range: scip.Range{StartLine: line, StartChar: start, EndLine: line, EndChar: end}, Symbol: symbol, Definition: def}
	}
	ix := Build(&scip.Index{Documents: []scip.Document{
		{Path: "a.ts", UTF16: true, Occurrences: []scip.Occurrence{
			occ(0, 6, 11, true),  // kept: "total"
			occ(1, 9, 14, false), // kept
			occ(1, 9, 99, false), // past the end of its line
			occ(7, 0, 5, false),  // past the end of the file
			{Range: scip.Range{StartLine: 0, StartChar: 0, EndLine: 1, EndChar: 3}, Symbol: symbol}, // spans two lines
		}},
		{Path: "absent.ts", UTF16: true, Occurrences: []scip.Occurrence{occ(0, 0, 5, true)}},
		{Path: "logo.png", UTF16: true, Occurrences: []scip.Occurrence{occ(0, 0, 1, true)}},
		{Path: "../../etc/passwd", UTF16: true, Occurrences: []scip.Occurrence{occ(0, 0, 4, true)}},
		{Path: "a.ts", UTF16: false, Occurrences: []scip.Occurrence{occ(0, 0, 5, true)}},
	}}, commit)

	if ix.Documents() != 1 {
		t.Errorf("%d documents kept, want 1", ix.Documents())
	}
	want := Dropped{NotInCommit: 3, NotUTF16: 1, Occurrences: 3}
	if ix.Dropped != want {
		t.Errorf("dropped = %+v, want %+v", ix.Dropped, want)
	}
	// A definition in a dropped document is not a target.
	got := FindDefinition(ix, commit, "a.ts", 1, 10)
	if got.Label != LabelIndex || strings.Join(spots(got.Targets), " ") != "a.ts:0:6" {
		t.Errorf("total = %+v", got)
	}
}

// The name check, on an index made to mislead: every name in a file is pointed
// at one line of another file. No lookup follows it.
func TestAFalseIndexCannotSendALookupToUnrelatedCode(t *testing.T) {
	commit := commitOf(t, map[string]string{
		"src/login.ts":  "export function checkPassword(given: string) { return verify(given); }\n",
		"src/verify.ts": "export function verify(given: string) { return true; }\n",
		"evil.ts":       "export const anything = 'look here instead';\n",
	})
	lie := func(path string, line, start, end int32, symbol string, def bool) scip.Document {
		return scip.Document{Path: path, UTF16: true, Occurrences: []scip.Occurrence{
			{Range: scip.Range{StartLine: line, StartChar: start, EndLine: line, EndChar: end}, Symbol: symbol, Definition: def},
		}}
	}
	ix := Build(&scip.Index{Documents: []scip.Document{
		// `verify` in login.ts is said to be defined at `anything` in evil.ts.
		lie("src/login.ts", 0, 54, 60, "s m p 1 lie.", false),
		lie("evil.ts", 0, 13, 21, "s m p 1 lie.", true),
	}}, commit)

	got := FindDefinition(ix, commit, "src/login.ts", 0, 55)
	if got.Label == LabelIndex {
		t.Fatalf("the lookup followed the false index to %v", spots(got.Targets))
	}
	if got.Label != LabelTextSearch {
		t.Fatalf("verify = %+v; want a text search", got)
	}
	found := false
	for _, m := range got.Matches {
		if m.Path == "src/verify.ts" {
			found = true
		}
		if m.Path == "evil.ts" {
			t.Error("the text search for verify returned the file the index pointed at")
		}
	}
	if !found {
		t.Error("the text search did not find the real definition")
	}
}

// A lookup scans one file's occurrences. This holds it to the design's bound
// on an index far larger than any the trials saw in one file.
func TestALookupInALargeIndexIsQuick(t *testing.T) {
	const lines = 100000
	var body strings.Builder
	for i := 0; i < lines; i++ {
		body.WriteString("name;\n")
	}
	commit, err := source.Unpack(bytes.NewReader(testgithub.Archive(t, map[string]string{"big.ts": body.String()})),
		source.Limits{ArchiveBytes: 1 << 24, FileBytes: 1 << 24})
	if err != nil {
		t.Fatal(err)
	}
	doc := scip.Document{Path: "big.ts", UTF16: true, Occurrences: make([]scip.Occurrence, 0, lines)}
	for i := 0; i < lines; i++ {
		doc.Occurrences = append(doc.Occurrences, scip.Occurrence{
			Range: scip.Range{StartLine: int32(i), StartChar: 0, EndLine: int32(i), EndChar: 4}, Symbol: "s m p 1 name.", Definition: i == 0,
		})
	}
	ix := Build(&scip.Index{Documents: []scip.Document{doc}}, commit)
	if ix.Size() < lines*occurrenceCost {
		t.Errorf("Size = %d for %d occurrences", ix.Size(), lines)
	}

	started := time.Now()
	const lookups = 20
	for i := 0; i < lookups; i++ {
		got := FindDefinition(ix, commit, "big.ts", lines-1-i, 2)
		if got.Label != LabelIndex || len(got.Targets) != 1 || got.Targets[0].Line != 0 {
			t.Fatalf("lookup %d = %+v", i, got.Label)
		}
	}
	if each := time.Since(started) / lookups; each > 200*time.Millisecond {
		t.Errorf("a lookup took %v; the bound is 200ms", each)
	}
}
