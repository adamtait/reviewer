// SPDX-License-Identifier: MIT

package risk

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/codeowners"
)

func owners(t *testing.T, text string) *codeowners.File {
	t.Helper()
	f, err := codeowners.Parse(strings.NewReader(text))
	if err != nil {
		t.Fatal(err)
	}
	return &f
}

func changed(paths ...string) []File {
	var out []File
	for _, p := range paths {
		out = append(out, File{Path: p})
	}
	return out
}

// many is n files under one directory.
func many(n int) []File {
	var out []File
	for i := 0; i < n; i++ {
		out = append(out, File{Path: fmt.Sprintf("src/f%d.go", i)})
	}
	return out
}

// complete is an input with every signal available, so that a test sees no
// warnings it did not ask for.
func complete(t *testing.T, files []File) Input {
	t.Helper()
	return Input{Files: files, Owners: owners(t, "* @everyone\n"), Reach: map[string][]SymbolReach{}}
}

func TestOneLabelEach(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     func(Input) Input
		label  Label
		reason string
	}{
		{"auth", func(in Input) Input { in.Files = changed("src/auth/login.go"); return in }, LabelAuth, "src/auth/login.go"},
		{"data", func(in Input) Input { in.Files = changed("db/migrations/0002.sql"); return in }, LabelData, "db/migrations/0002.sql"},
		{"ci", func(in Input) Input { in.Files = changed("CLAUDE.md"); return in }, LabelCI, "CLAUDE.md"},
		{"deps", func(in Input) Input { in.Files = changed("plugins/ts/package-lock.json"); return in }, LabelDeps, "plugins/ts/package-lock.json"},
		{"infra", func(in Input) Input { in.Files = changed("deploy/helm/values.yaml"); return in }, LabelInfra, "deploy/helm/values.yaml"},
		{"wide", func(in Input) Input { in.Files = many(41); return in }, LabelWide, "41 files changed"},
		{"reach", func(in Input) Input {
			in.Files = changed("src/lib.ts")
			in.Reach = map[string][]SymbolReach{"src/lib.ts": {{Symbol: symParse, Files: 12}}}
			return in
		}, LabelReach, symParse + " in src/lib.ts: referenced from 12 other files"},
		{"history", func(in Input) Input {
			in.Files = changed("src/main.go")
			in.Reverted = map[string]bool{"src/main.go": true}
			return in
		}, LabelHistory, "src/main.go"},
		{"review-config", func(in Input) Input { in.Files = changed("docs/CODEOWNERS"); return in }, LabelReviewConfig, "docs/CODEOWNERS"},
	} {
		rec := Compute(tc.in(complete(t, nil)))
		if !reflect.DeepEqual(rec.Labels, []Label{tc.label}) {
			t.Errorf("%s: labels %v, want only %s", tc.name, rec.Labels, tc.label)
			continue
		}
		if got := rec.Reasons[tc.label]; len(got) != 1 || !strings.HasPrefix(got[0], tc.reason) {
			t.Errorf("%s: reasons %q, want one starting %q", tc.name, got, tc.reason)
		}
		if len(rec.Warnings) != 0 {
			t.Errorf("%s: warnings %q", tc.name, rec.Warnings)
		}
	}
}

func TestReviewConfigOverlapsCIOnPurpose(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []Label
	}{
		{".review/config.yaml", []Label{LabelCI, LabelReviewConfig}},
		{".github/workflows/review.yml", []Label{LabelCI, LabelReviewConfig}},
		{".github/CODEOWNERS", []Label{LabelCI, LabelReviewConfig}},
		{"CODEOWNERS", []Label{LabelReviewConfig}},
		{"docs/CODEOWNERS", []Label{LabelReviewConfig}},
		{"AGENTS.md", []Label{LabelCI}},
		{"src/CODEOWNERS.md", nil},
	} {
		if got := Compute(complete(t, changed(tc.path))).Labels; !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %v, want %v", tc.path, got, tc.want)
		}
	}
}

// A rename is seen at both ends: moving a file out of a sensitive path is a
// change to that path.
func TestRenamesAreSeenAtBothEnds(t *testing.T) {
	out := Compute(complete(t, []File{{Path: "src/session.go", PreviousPath: "src/auth/session.go"}}))
	if !out.Has(LabelAuth) {
		t.Fatalf("a rename out of auth/: %v", out.Labels)
	}
	if want := []string{"src/auth/session.go (renamed to src/session.go)"}; !reflect.DeepEqual(out.Reasons[LabelAuth], want) {
		t.Errorf("reasons %q, want %q", out.Reasons[LabelAuth], want)
	}

	in := Compute(complete(t, []File{{Path: "src/auth/session.go", PreviousPath: "src/session.go"}}))
	if !reflect.DeepEqual(in.Reasons[LabelAuth], []string{"src/auth/session.go"}) {
		t.Errorf("a rename into auth/: %v", in.Reasons)
	}

	// Moving the review configuration away is a change to it too.
	moved := Compute(complete(t, []File{{Path: "old/config.yaml", PreviousPath: ".review/config.yaml"}}))
	if !moved.Has(LabelReviewConfig) || !moved.Has(LabelCI) {
		t.Errorf("a rename out of .review/: %v", moved.Labels)
	}

	// The revert history of the file's old path counts.
	inp := complete(t, []File{{Path: "src/new.go", PreviousPath: "src/old.go"}})
	inp.Reverted = map[string]bool{"src/old.go": true}
	if !Compute(inp).Has(LabelHistory) {
		t.Error("a revert of the file at its old path was not seen")
	}

	// Each end in its own owner group: a rename that crosses owners crosses
	// both.
	inp = Input{
		Files:  []File{{Path: "b/x.go", PreviousPath: "a/x.go"}, {Path: "c/y.go"}},
		Owners: owners(t, "/a/ @team-a\n/b/ @team-b\n/c/ @team-c\n"),
		Reach:  map[string][]SymbolReach{},
	}
	if rec := Compute(inp); !rec.Has(LabelWide) {
		t.Errorf("a rename across owners plus one more owner: %v", rec.Labels)
	}
}

func TestTheFileCountBoundary(t *testing.T) {
	for _, tc := range []struct {
		files int
		want  bool
	}{{39, false}, {40, false}, {41, true}} {
		rec := Compute(complete(t, many(tc.files)))
		if rec.Has(LabelWide) != tc.want {
			t.Errorf("%d files: risk/wide %v, want %v", tc.files, rec.Has(LabelWide), tc.want)
		}
	}
}

func TestTheOwnerGroupBoundary(t *testing.T) {
	const file = "/a/ @team-a\n/b/ @team-b\n/c/ @team-c\n/d/ @Team-B @team-a\n/e/ @team-a @team-b\n/vendor/\n"
	for _, tc := range []struct {
		name  string
		files []File
		want  bool
	}{
		{"two groups", changed("a/1.go", "b/1.go"), false},
		{"three groups", changed("a/1.go", "b/1.go", "c/1.go"), true},
		// The same owners in another order or case are one group.
		{"two groups, one written two ways", changed("a/1.go", "d/1.go", "e/1.go"), false},
		// An unowned path, whether no rule matches it or a rule with no owners
		// does, is in no group.
		{"two groups and unowned paths", changed("a/1.go", "b/1.go", "README.md", "vendor/x.go"), false},
		{"many files in one group", changed("a/1.go", "a/2.go", "a/3.go", "a/4/5.go"), false},
	} {
		rec := Compute(Input{Files: tc.files, Owners: owners(t, file), Reach: map[string][]SymbolReach{}})
		if rec.Has(LabelWide) != tc.want {
			t.Errorf("%s: risk/wide %v, want %v", tc.name, rec.Has(LabelWide), tc.want)
		}
	}
	rec := Compute(Input{Files: changed("a/1.go", "b/1.go", "c/1.go"), Owners: owners(t, file)})
	if want := "3 CODEOWNERS groups: @team-a; @team-b; @team-c"; !reflect.DeepEqual(rec.Reasons[LabelWide], []string{want}) {
		t.Errorf("reason %q, want %q", rec.Reasons[LabelWide], want)
	}
}

func TestTheReachBoundary(t *testing.T) {
	for _, tc := range []struct {
		files int
		want  bool
	}{{9, false}, {10, true}} {
		in := complete(t, changed("src/lib.ts"))
		in.Reach = map[string][]SymbolReach{"src/lib.ts": {{Symbol: symFormat, Files: 1}, {Symbol: symParse, Files: tc.files}}}
		rec := Compute(in)
		if rec.Has(LabelReach) != tc.want {
			t.Errorf("%d other files: risk/reach %v, want %v", tc.files, rec.Has(LabelReach), tc.want)
		}
		if tc.want && len(rec.Reasons[LabelReach]) != 1 {
			t.Errorf("reasons %q: only the symbol at or over the threshold belongs", rec.Reasons[LabelReach])
		}
	}
}

// A signal that could not be computed is a warning, never a quiet "no label".
func TestWhatIsNotAvailableIsSaid(t *testing.T) {
	in := Input{
		Files:              changed("src/lib.ts"),
		ReachUnavailable:   "no index for the merge base",
		HistoryUnavailable: "the commits API refused",
	}
	rec := Compute(in)
	want := []string{
		"file history not available: the commits API refused",
		"owner groups not available: no CODEOWNERS file",
		"reach not available: no index for the merge base",
	}
	if !reflect.DeepEqual(rec.Warnings, want) {
		t.Errorf("warnings %q, want %q", rec.Warnings, want)
	}
	if len(rec.Labels) != 0 {
		t.Errorf("labels %v", rec.Labels)
	}

	in.OwnersUnavailable = "CODEOWNERS line 3: negation"
	if got := Compute(in).Warnings[1]; got != "owner groups not available: CODEOWNERS line 3: negation" {
		t.Errorf("warning %q", got)
	}
}

// With no owner groups, the file count still decides risk/wide.
func TestWideByFileCountWithoutCODEOWNERS(t *testing.T) {
	if !Compute(Input{Files: many(41)}).Has(LabelWide) {
		t.Error("41 files without a CODEOWNERS file is not wide")
	}
}

func TestAnInvalidRiskFileMeansTheDefaults(t *testing.T) {
	m, _, err := Parse([]byte("version: 1\nadd:\n  risk/auth: [\"src/session/**\"]\nremove:\n  risk/auth: [\"**/auth/**\"]\n"))
	if err == nil {
		t.Fatal("parsed")
	}
	in := complete(t, changed("src/session/store.go", "src/auth/login.go"))
	in.Map = m
	rec := Compute(in)
	if want := []string{"src/auth/login.go"}; !reflect.DeepEqual(rec.Reasons[LabelAuth], want) {
		t.Errorf("reasons %q, want only the default's match %q", rec.Reasons[LabelAuth], want)
	}

	good, _, err := Parse([]byte("version: 1\nadd:\n  risk/auth: [\"src/session/**\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	in.Map = good
	if got := Compute(in).Reasons[LabelAuth]; len(got) != 2 {
		t.Errorf("with a valid file: reasons %q, want both paths", got)
	}
}

func TestAnEmptyDiff(t *testing.T) {
	rec := Compute(complete(t, nil))
	if len(rec.Labels) != 0 || len(rec.Reasons) != 0 || len(rec.Warnings) != 0 {
		t.Errorf("an empty diff: %+v", rec)
	}
}

func TestLabelsAreInTheOrderOfAll(t *testing.T) {
	in := complete(t, append(many(41), changed(".github/workflows/x.yml", "src/auth/a.go", "go.mod", "x.sql", "main.tf")...))
	in.Reverted = map[string]bool{"go.mod": true}
	in.Reach = map[string][]SymbolReach{"src/auth/a.go": {{Symbol: symParse, Files: 50}}}
	rec := Compute(in)
	if !reflect.DeepEqual(rec.Labels, All) {
		t.Errorf("labels %v, want all of them in order %v", rec.Labels, All)
	}
}

// One path matching several patterns of a label is one reason.
func TestAReasonIsGivenOnce(t *testing.T) {
	rec := Compute(complete(t, changed("src/auth/auth.go")))
	if got := rec.Reasons[LabelAuth]; len(got) != 1 {
		t.Errorf("reasons %q", got)
	}
}
