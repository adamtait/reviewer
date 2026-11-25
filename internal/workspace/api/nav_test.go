// SPDX-License-Identifier: MIT

package api

import (
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/workspace/api/apitypes"
	"github.com/adamtait/reviewer/internal/workspace/testgithub"
)

// navSHA is a commit of example/alpha whose files are the navigation fixture
// project, and which has a run of the index workflow that saved the fixture's
// index. See ../navigation/testdata/README.md.
const navSHA = "5555555555555555555555555555555555555555"

const navFixture = "../navigation/testdata"

func (w *world) withIndex() *world {
	w.t.Helper()
	files := map[string]string{}
	root := filepath.Join(navFixture, "tsproject")
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
		w.t.Fatal(err)
	}
	index, err := os.ReadFile(filepath.Join(navFixture, "index.full.scip"))
	if err != nil {
		w.t.Fatal(err)
	}
	w.gh.SetArchive("example", "alpha", navSHA, testgithub.Archive(w.t, files))
	w.gh.AddRun("example", "alpha", testgithub.Run{
		ID: 7001, HeadSHA: navSHA, Workflow: "review-index.yml", Conclusion: "success",
		Created: time.Date(2026, 11, 20, 9, 31, 0, 0, time.UTC),
		Artifacts: []testgithub.Artifact{
			{ID: 9001, Name: "review-index", Zip: testgithub.Zip(w.t, map[string][]byte{"index.scip": index})},
		},
	})
	return w
}

func lookup(kind, sha, path string, line, character int) string {
	return fmt.Sprintf("%s/commits/%s/%s?path=%s&line=%d&character=%d", alpha, sha, kind, url.QueryEscape(path), line, character)
}

func TestTheIndexStateAndLookups(t *testing.T) {
	w := newWorld(t).withPull().withIndex()
	alice := w.signIn("alice")

	rec := w.do(http.MethodGet, alpha+"/commits/"+navSHA+"/index", nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("index: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "index_state", rec)
	state := decode[apitypes.IndexState](t, rec.Body.Bytes())
	if state.State != apitypes.IndexReady || state.Documents != 6 || state.ToolVersion != "0.6.2" {
		t.Errorf("state = %+v", state)
	}
	// The link to the run is built from configuration and the run's number.
	if state.RunURL != "https://github.example/example/alpha/actions/runs/7001" || strings.Contains(rec.Body.String(), "elsewhere") {
		t.Errorf("runUrl = %q", state.RunURL)
	}
	if w.gh.TokenLeaks() != 0 {
		t.Error("the user's token followed the artifact download to another host")
	}

	// `add(sum, item)` in src/cart.ts, line 15 as an editor shows it.
	w.log.Reset()
	rec = w.do(http.MethodGet, lookup("definition", navSHA, "src/cart.ts", 14, 12), nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("definition: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "definition", rec)
	def := decode[apitypes.Definition](t, rec.Body.Bytes())
	if def.Label != apitypes.LabelIndex || len(def.Targets) != 1 || def.Targets[0].Path != "src/money.ts" || def.Targets[0].Line != 12 {
		t.Errorf("definition = %+v", def)
	}
	// The label is in the request's log line, where the feedback week counts it.
	lines := logLines(t, w.log)
	if len(lines) != 1 || lines[0]["route"] != "definition" || lines[0]["label"] != "index" || lines[0]["sha"] != navSHA {
		t.Errorf("log line = %v", lines)
	}

	rec = w.do(http.MethodGet, lookup("references", navSHA, "src/money.ts", 12, 16), nil, alice, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("references: status %d: %s", rec.Code, rec.Body.String())
	}
	example(t, "references", rec)
	refs := decode[apitypes.References](t, rec.Body.Bytes())
	if refs.Label != apitypes.LabelIndex || len(refs.References) != 4 || len(refs.Matches) < 4 {
		t.Errorf("references = %+v", refs)
	}

	// Each of the other labels, through the API.
	for name, tc := range map[string]struct {
		address string
		label   string
	}{
		"a dependency":             {lookup("definition", navSHA, "src/receipt.ts", 7, 28), apitypes.LabelExternal},
		"a file the index skips":   {lookup("definition", navSHA, "docs/notes.md", 2, 3), apitypes.LabelTextSearch},
		"a position on nothing":    {lookup("definition", navSHA, "docs/notes.md", 1, 0), apitypes.LabelNone},
		"a file that is not there": {lookup("definition", navSHA, "src/absent.ts", 0, 0), apitypes.LabelNone},
	} {
		rec := w.do(http.MethodGet, tc.address, nil, alice, false)
		got := decode[apitypes.Definition](t, rec.Body.Bytes())
		if rec.Code != http.StatusOK || got.Label != tc.label {
			t.Errorf("%s: status %d, label %q, want %q", name, rec.Code, got.Label, tc.label)
		}
		// Lists are lists even when empty, so the browser can loop over them.
		if got.Targets == nil || got.Matches == nil {
			t.Errorf("%s: a null list in %s", name, rec.Body.String())
		}
	}
	external := decode[apitypes.Definition](t, w.do(http.MethodGet, lookup("definition", navSHA, "src/receipt.ts", 7, 28), nil, alice, false).Body.Bytes())
	if external.Package != "tiny-format" || external.Version != "2.1.0" || len(external.Targets) != 0 {
		t.Errorf("external = %+v", external)
	}

	// The index was downloaded once for all of that.
	if n := w.gh.CallsTo("/repos/example/alpha/actions/artifacts/"); n != 1 {
		t.Errorf("%d artifact downloads, want 1", n)
	}
}

func TestACommitWithNoIndexFallsBackToTextSearch(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")

	rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/index", nil, alice, false)
	state := decode[apitypes.IndexState](t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || state.State != apitypes.IndexNone || state.RunURL != "" {
		t.Fatalf("status %d, state %+v", rec.Code, state)
	}

	// `sum` in `return sum(prices);`.
	rec = w.do(http.MethodGet, lookup("definition", headSHA, "src/cart.ts", 3, 9), nil, alice, false)
	def := decode[apitypes.Definition](t, rec.Body.Bytes())
	if def.Label != apitypes.LabelTextSearch || len(def.Targets) != 0 {
		t.Fatalf("definition = %+v", def)
	}
	found := false
	for _, m := range def.Matches {
		if m.Path == "src/sum.ts" {
			found = true
		}
	}
	if !found {
		t.Errorf("the text search did not find the definition: %+v", def.Matches)
	}
}

func TestTheIndexStatesBeforeAnIndexExists(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	state := func() apitypes.IndexState {
		t.Helper()
		rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/index", nil, alice, false)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		return decode[apitypes.IndexState](t, rec.Body.Bytes())
	}
	// Each state is remembered for a few seconds. The test moves the clock on.
	clock := time.Now()
	w.server.Navigation.SetClock(func() time.Time { return clock })
	later := func() { clock = clock.Add(time.Minute) }

	run := testgithub.Run{ID: 8001, HeadSHA: headSHA, Workflow: "review-index.yml", Status: "in_progress", Created: time.Now()}
	w.gh.AddRun("example", "alpha", run)
	if got := state(); got.State != apitypes.IndexRunning || got.RunURL != "https://github.example/example/alpha/actions/runs/8001" {
		t.Errorf("while the job runs: %+v", got)
	}

	later()
	run.Status, run.Conclusion = "completed", "failure"
	w.gh.AddRun("example", "alpha", run)
	if got := state(); got.State != apitypes.IndexFailed || got.RunURL == "" {
		t.Errorf("after the job failed: %+v", got)
	}

	// The job succeeded and its artifact has expired.
	later()
	run.Conclusion = "success"
	run.Artifacts = []testgithub.Artifact{{ID: 9002, Name: "review-index", Expired: true}}
	w.gh.AddRun("example", "alpha", run)
	if got := state(); got.State != apitypes.IndexNone || !strings.Contains(got.Reason, "expired") {
		t.Errorf("with an expired artifact: %+v", got)
	}

	// A run of the same workflow for another commit is not this commit's.
	later()
	run.HeadSHA = baseSHA
	w.gh.AddRun("example", "alpha", run)
	if got := state(); got.State != apitypes.IndexNone || got.Reason != "" || got.RunURL != "" {
		t.Errorf("with a run for another commit only: %+v", got)
	}
}

func TestALookupNeedsAPathALineAndACharacter(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	base := alpha + "/commits/" + headSHA
	for _, query := range []string{
		"",
		"?path=src/cart.ts",
		"?path=src/cart.ts&line=1",
		"?line=1&character=1",
		"?path=src/cart.ts&line=-1&character=0",
		"?path=src/cart.ts&line=0&character=-1",
		"?path=src/cart.ts&line=one&character=0",
		"?path=src/cart.ts&line=1.5&character=0",
		"?path=src/cart.ts&line=99999999999999999999&character=0",
	} {
		for _, kind := range []string{"definition", "references"} {
			if rec := w.do(http.MethodGet, base+"/"+kind+query, nil, alice, false); rec.Code != http.StatusBadRequest {
				t.Errorf("%s%s: status %d, want 400", kind, query, rec.Code)
			}
		}
	}
	if rec := w.do(http.MethodGet, alpha+"/commits/main/definition?path=a&line=0&character=0", nil, alice, false); rec.Code != http.StatusBadRequest {
		t.Errorf("a branch name: status %d, want 400", rec.Code)
	}
}

func TestAReportIsALogLineThatNamesTheLookupAndCarriesNoText(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	w.log.Reset()

	report := apitypes.Report{SHA: headSHA, Path: "src/cart.ts", Line: 3, Character: 9, Kind: "definition", Label: "index"}
	rec := w.do(http.MethodPost, alpha+"/navigation/reports", report, alice, false)
	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}

	lines := logLines(t, w.log)
	if len(lines) != 2 {
		t.Fatalf("%d log lines, want the report and the request: %v", len(lines), lines)
	}
	reported := lines[0]
	want := map[string]any{
		"msg": "navigation_reported_wrong", "owner": "example", "repo": "alpha", "sha": headSHA,
		"path": "src/cart.ts", "line": float64(3), "character": float64(9), "kind": "definition", "label": "index",
	}
	for key, value := range want {
		if reported[key] != value {
			t.Errorf("report line: %s = %v, want %v", key, reported[key], value)
		}
	}
	// Nothing else: no field for text a user typed, because there is none.
	for key := range reported {
		if _, known := want[key]; !known && key != "time" && key != "level" {
			t.Errorf("the report line carries %q", key)
		}
	}
	if lines[1]["route"] != "navigation_report" || lines[1]["outcome"] != "reported_wrong" || lines[1]["user"] != "alice" {
		t.Errorf("request line = %v", lines[1])
	}

	// A body with more in it is accepted and the extra is dropped, not logged.
	w.log.Reset()
	rec = w.do(http.MethodPost, alpha+"/navigation/reports", map[string]any{
		"sha": headSHA, "path": "src/cart.ts", "line": 3, "character": 9, "kind": "references", "label": "text_search",
		"note": "const password = 'hunter2'",
	}, alice, false)
	if rec.Code != http.StatusNoContent || strings.Contains(w.log.String(), "hunter2") {
		t.Errorf("status %d; log: %s", rec.Code, w.log.String())
	}

	for name, bad := range map[string]apitypes.Report{
		"a short hash":     {SHA: headSHA[:7], Path: "a.ts", Kind: "definition", Label: "index"},
		"no path":          {SHA: headSHA, Kind: "definition", Label: "index"},
		"an unknown kind":  {SHA: headSHA, Path: "a.ts", Kind: "hover", Label: "index"},
		"an unknown label": {SHA: headSHA, Path: "a.ts", Kind: "definition", Label: "certain"},
		"a negative line":  {SHA: headSHA, Path: "a.ts", Line: -1, Kind: "definition", Label: "index"},
	} {
		if rec := w.do(http.MethodPost, alpha+"/navigation/reports", bad, alice, false); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
	if rec := w.do(http.MethodPost, alpha+"/navigation/reports", report, alice, true); rec.Code != http.StatusBadRequest {
		t.Errorf("a report without the workspace's header: status %d, want 400", rec.Code)
	}
}

// An index in memory for one repository is not reachable at another's address.
func TestAnIndexNeverAnswersForAnotherRepository(t *testing.T) {
	w := newWorld(t).withPull().withIndex()
	alice, carol := w.signIn("alice"), w.signIn("carol")
	if rec := w.do(http.MethodGet, lookup("definition", navSHA, "src/cart.ts", 14, 12), nil, alice, false); rec.Code != http.StatusOK {
		t.Fatalf("alice's own lookup: status %d", rec.Code)
	}
	absent := w.do(http.MethodGet, "/api/v1/no/such/address", nil, carol, false)
	beta := strings.Replace(alpha, "alpha", "beta", 1)
	for _, address := range []string{
		lookup("definition", navSHA, "src/cart.ts", 14, 12),
		lookup("references", navSHA, "src/money.ts", 12, 16),
		alpha + "/commits/" + navSHA + "/index",
		strings.Replace(lookup("definition", navSHA, "src/cart.ts", 14, 12), alpha, beta, 1),
		beta + "/commits/" + navSHA + "/index",
	} {
		rec := w.do(http.MethodGet, address, nil, carol, false)
		if rec.Code != http.StatusNotFound || rec.Body.String() != absent.Body.String() {
			t.Errorf("%s: status %d: %s", address, rec.Code, rec.Body.String())
		}
	}
}

func TestWhenTheTokenCannotReadActionsTheUserIsToldWhatToFix(t *testing.T) {
	w := newWorld(t).withPull()
	alice := w.signIn("alice")
	w.gh.Fail("/repos/example/alpha/actions/", http.StatusForbidden)
	rec := w.do(http.MethodGet, alpha+"/commits/"+headSHA+"/index", nil, alice, false)
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "permission") {
		t.Errorf("status %d: %s", rec.Code, rec.Body.String())
	}
}
