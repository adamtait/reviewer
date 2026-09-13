// SPDX-License-Identifier: MIT

package laneb

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/diff"
	"github.com/adamtait/reviewer/internal/testfixture"
	"github.com/adamtait/reviewer/pkg/finding"
)

var update = flag.Bool("update", false, "rewrite the golden file")

// The sample is everything the assembler can be given, so the golden file is the
// whole contract in one place: what gets sent to a model provider is the most
// consequential thing this tool does, and a reviewer should be able to read it
// rather than infer it.
func sample() Input {
	return Input{
		Base: "origin/main",
		Changed: []diff.File{
			{Path: "src/domain/order.ts", Status: diff.StatusModified, Ranges: [][2]int{{10, 14}}},
			{Path: "src/infra/http.ts", Status: diff.StatusRenamed, OldPath: "src/infra/fetch.ts"},
		},
		Diff: strings.Join([]string{
			"diff --git a/src/domain/order.ts b/src/domain/order.ts",
			"@@ -8,4 +8,6 @@ export class Order {",
			"   private readonly id: string;",
			"+  async total(): Promise<number> {",
			"+    return 0;",
			"+  }",
			" }",
		}, "\n"),
		Findings: []finding.Finding{
			{
				RuleID: "types/TS2322", Lane: finding.LaneDeterministic,
				Confidence: finding.ConfidenceHigh, Severity: finding.SeverityError,
				File: "src/domain/order.ts", Line: 12,
				Message: "Type 'number' is not assignable to type 'string'.\nSecond line is dropped.",
			},
		},
		Guidance: []Document{
			{Path: "docs/conventions.md", Body: "# Conventions\n\nThe domain layer takes a client.\n"},
		},
		Docs: []StaleDoc{
			{Path: "docs/architecture.md", References: []string{"src/infra/http.ts", "src/domain/order.ts"}},
		},
	}
}

func TestAssembleGolden(t *testing.T) {
	got := Assemble(sample())
	golden(t, "assemble.txt", got)
}

// The same input must produce the same bytes, on a laptop and on a runner. A
// prompt that varies run to run makes every finding unreproducible and every
// cached response useless.
func TestAssembleIsDeterministic(t *testing.T) {
	first := Assemble(sample())

	// Re-ordered inputs, same content: the assembler sorts everything it prints.
	shuffled := sample()
	shuffled.Changed[0], shuffled.Changed[1] = shuffled.Changed[1], shuffled.Changed[0]
	shuffled.Docs[0].References[0], shuffled.Docs[0].References[1] =
		shuffled.Docs[0].References[1], shuffled.Docs[0].References[0]

	if second := Assemble(shuffled); second != first {
		t.Errorf("input order changed the prompt:\n%s", diffFirstLine(first, second))
	}
}

// The exit criterion. An absolute path leaks a directory layout and a username; an
// environment value leaks whatever happened to be set. Neither belongs in something
// sent to a third party.
func TestTheAssembledPromptCarriesNothingAboutThisMachine(t *testing.T) {
	got := Assemble(sample())

	if strings.Contains(got, string(os.PathSeparator)+"home") || regexp.MustCompile(`(?m)^\s*/[A-Za-z]`).MatchString(got) {
		t.Errorf("an absolute path reached the prompt:\n%s", got)
	}
	for _, name := range []string{"GITHUB_TOKEN", "REVIEW_MODEL_API_KEY", "REVIEW_MODEL_BASE_URL", "PATH="} {
		if strings.Contains(got, name) {
			t.Errorf("the prompt names the environment variable %s", name)
		}
	}
}

func TestAssembleWithNothingToSay(t *testing.T) {
	got := Assemble(Input{Diff: ""})

	// Still a valid prompt: a repository with no guidance and no lane A findings
	// is the normal case, not a degraded one.
	for _, want := range []string{"# The change under review", "staged changes", "## The diff"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	for _, absent := range []string{"own guidance", "Already reported", "may have invalidated"} {
		if strings.Contains(got, absent) {
			t.Errorf("want no %q section when there is nothing to put in it:\n%s", absent, got)
		}
	}
}

// Budgets are shared across guidance files and cut at a line boundary, so one
// repository naming eight documents does not send eight times as much as one
// naming a single document.
func TestTheGuidanceBudgetIsSharedAndCutsAtALine(t *testing.T) {
	in := Input{
		MaxGuidanceBytes: 40,
		Guidance: []Document{
			{Path: "a.md", Body: strings.Repeat("first document line\n", 10)},
			{Path: "b.md", Body: strings.Repeat("second document line\n", 10)},
		},
	}
	got := Assemble(in)

	if !strings.Contains(got, "_(truncated)_") {
		t.Errorf("want the truncation stated:\n%s", got)
	}
	if strings.Contains(got, "first document lin\n") {
		t.Error("the cut landed mid-line")
	}
	if strings.Contains(got, "second document line") {
		t.Error("the second document consumed budget the first had already spent")
	}
}

func TestTheDiffBudgetIsStatedWhenItBites(t *testing.T) {
	got := Assemble(Input{MaxDiffBytes: 30, Diff: strings.Repeat("+ a line of diff\n", 20)})

	if !strings.Contains(got, "cut off here") {
		t.Errorf("want the truncation stated rather than silent:\n%s", got)
	}
}

// A multi-line analyzer message becomes one line: the section exists to tell the
// model what is already covered, and a stack trace in it spends the budget on
// nothing.
func TestFindingMessagesAreOneLine(t *testing.T) {
	got := Assemble(sample())
	if strings.Contains(got, "Second line is dropped") {
		t.Errorf("want only the first line of an analyzer message:\n%s", got)
	}
	if !strings.Contains(got, "not assignable to type 'string'. …") {
		t.Errorf("want the elision marked:\n%s", got)
	}
}

// --- Gather ---

func TestGatherReadsOnlyWhatConfigNames(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	write(t, repo.Root, "docs/conventions.md", "# Conventions\n\nThe domain layer takes a client.\n")
	write(t, repo.Root, "docs/secret-plans.md", "do not send this\n")

	cfg := config.Defaults()
	cfg.Root = repo.Root
	cfg.Guidance = []string{"docs/conventions.md"}

	files, err := diff.Changed(context.Background(), repo.Root, repo.Base)
	if err != nil {
		t.Fatal(err)
	}
	in, warnings := Gather(context.Background(), cfg, repo.Base, files)
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}

	prompt := Assemble(in)
	if !strings.Contains(prompt, "The domain layer takes a client") {
		t.Error("the named guidance file is missing from the prompt")
	}
	// The one that matters: a document nobody named must not be in the prompt.
	if strings.Contains(prompt, "do not send this") {
		t.Errorf("a file config did not name reached the prompt:\n%s", prompt)
	}
	if in.Diff == "" {
		t.Error("want the diff gathered")
	}
}

// A guidance path is configuration, and configuration is a file a pull request can
// change. `../../../etc/passwd` as a guidance path is a way to have this tool read
// a file and send it to a third party.
func TestGatherRefusesAGuidancePathOutsideTheRepository(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	outside := filepath.Join(t.TempDir(), "stolen.txt")
	if err := os.WriteFile(outside, []byte("a secret from elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.Root = repo.Root
	cfg.Guidance = []string{"../../../../../../" + strings.TrimPrefix(outside, "/")}

	in, warnings := Gather(context.Background(), cfg, repo.Base, nil)
	if len(in.Guidance) != 0 {
		t.Fatalf("a path outside the repository was read: %+v", in.Guidance)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "outside the repository") {
		t.Errorf("want the refusal stated, got %v", warnings)
	}
}

// A guidance file the repository names and does not have is worth saying: the
// review is quietly weaker than its configuration claims.
func TestGatherReportsAMissingGuidanceFile(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	cfg := config.Defaults()
	cfg.Root = repo.Root
	cfg.Guidance = []string{"docs/nope.md"}

	_, warnings := Gather(context.Background(), cfg, repo.Base, nil)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "docs/nope.md") {
		t.Errorf("want the missing file named, got %v", warnings)
	}
}

func TestStaleDocsFindsDocumentationAboutChangedCode(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	write(t, repo.Root, "docs/architecture.md", strings.Join([]string{
		"# Architecture",
		"",
		"The order aggregate lives in [order.ts](../src/domain/order.ts).",
		"Transport is `src/infra/http.ts`.",
		"See also <https://example.invalid/spec> and the `Order` type.",
		"Nothing here touches [the readme](../README.md).",
		"",
	}, "\n"))

	cfg := config.Defaults()
	cfg.Root = repo.Root
	cfg.Guidance = []string{"docs/architecture.md"}

	files := []diff.File{
		{Path: "src/domain/order.ts", Status: diff.StatusModified},
		{Path: "src/infra/http.ts", Status: diff.StatusModified},
	}
	docs := staleDocs(cfg, files)

	if len(docs) != 1 {
		t.Fatalf("want the one document, got %+v", docs)
	}
	if got := strings.Join(docs[0].References, ","); got != "src/domain/order.ts,src/infra/http.ts" {
		t.Errorf("references = %q", got)
	}
}

// A document the change itself edits is being kept up to date, which is the
// opposite of the problem this section reports.
func TestStaleDocsIgnoresADocumentTheChangeEdits(t *testing.T) {
	repo := testfixture.Build(t, "tiny-ts-repo")
	write(t, repo.Root, "docs/architecture.md", "See [order](../src/domain/order.ts).\n")

	cfg := config.Defaults()
	cfg.Root = repo.Root
	cfg.Guidance = []string{"docs/architecture.md"}

	docs := staleDocs(cfg, []diff.File{
		{Path: "src/domain/order.ts", Status: diff.StatusModified},
		{Path: "docs/architecture.md", Status: diff.StatusModified},
	})
	if len(docs) != 0 {
		t.Errorf("want nothing reported, got %+v", docs)
	}
}

// A markdown link is relative to the document; a path in backticks is almost
// always written from the repository root. Both spellings have to resolve, and a
// wrong guess has to match nothing rather than invent a reference.
func TestRefCandidates(t *testing.T) {
	for _, tc := range []struct{ from, ref, want string }{
		{from: "docs/a.md", ref: "../src/x.ts", want: "src/x.ts"},
		{from: "docs/a.md", ref: "b.md", want: "docs/b.md,b.md"},
		{from: "docs/a.md", ref: "/src/x.ts", want: "src/x.ts"},
		{from: "docs/a.md", ref: "../src/x.ts#anchor", want: "src/x.ts"},
		// Written from the root, as people write paths in prose.
		{from: "docs/a.md", ref: "src/infra/http.ts", want: "docs/src/infra/http.ts,src/infra/http.ts"},
		{from: "README.md", ref: "src/x.ts", want: "src/x.ts"},
		{from: "docs/a.md", ref: "https://example.invalid/x.ts", want: ""},
		{from: "docs/a.md", ref: "#section", want: ""},
		{from: "docs/a.md", ref: "Order", want: ""},
		{from: "docs/a.md", ref: "npm install foo", want: ""},
		{from: "docs/a.md", ref: "../../outside.ts", want: ""},
	} {
		if got := strings.Join(refCandidates(tc.from, tc.ref), ","); got != tc.want {
			t.Errorf("refCandidates(%q, %q) = %q, want %q", tc.from, tc.ref, got, tc.want)
		}
	}
}

// --- helpers ---

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "golden", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("golden file updated")
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs:\n%s", name, diffFirstLine(string(want), got))
	}
}

func diffFirstLine(want, got string) string {
	w := strings.Split(want, "\n")
	g := strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		a, b := at(w, i), at(g, i)
		if a != b {
			return "line " + itoa(i+1) + ":\n  want: " + a + "\n  got:  " + b
		}
	}
	return "(identical)"
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return "(end)"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
