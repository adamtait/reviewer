// SPDX-License-Identifier: MIT

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/reporters"
	"github.com/adamtait/reviewer/internal/risk"
)

const (
	riskMergeBase = "mbmbmbmbmbmbmbmbmbmbmbmbmbmbmbmbmbmbmbmb"
	riskHead      = "hdhdhdhdhdhdhdhdhdhdhdhdhdhdhdhdhdhdhdhd"
	riskSymbol    = "scip-typescript npm app 1.0.0 src/auth/`session.ts`/verify()."
)

// fakeRisk is the GitHub a risk run talks to. Every method can be made to
// fail by name, and every write is recorded.
type fakeRisk struct {
	pr        ghclient.PullRequest
	files     []ghclient.ChangedFile
	contents  map[string]string
	commits   map[string][]ghclient.Commit
	runs      []ghclient.WorkflowRun
	artifacts []ghclient.Artifact
	archive   []byte
	comments  []ghclient.IssueComment
	viewer    string
	fail      map[string]error

	calls   []string
	created []string
	updated map[int64]string
	added   []string
	removed []string
}

func (f *fakeRisk) call(name string) error {
	f.calls = append(f.calls, name)
	return f.fail[name]
}

func (f *fakeRisk) Viewer(context.Context) (ghclient.User, error) {
	if err := f.call("Viewer"); err != nil {
		return ghclient.User{}, err
	}
	return ghclient.User{Login: f.viewer, Type: "Bot"}, nil
}
func (f *fakeRisk) GetPullRequest(context.Context, ghclient.Repo, int) (ghclient.PullRequest, error) {
	return f.pr, f.call("GetPullRequest")
}
func (f *fakeRisk) ListFiles(context.Context, ghclient.Repo, int) ([]ghclient.ChangedFile, error) {
	if err := f.call("ListFiles"); err != nil {
		return nil, err
	}
	return f.files, nil
}
func (f *fakeRisk) Compare(_ context.Context, _ ghclient.Repo, base, head string) (string, error) {
	if err := f.call("Compare"); err != nil {
		return "", err
	}
	if base != "base" || head != riskHead {
		return "", fmt.Errorf("compared %s...%s", base, head)
	}
	return riskMergeBase, nil
}
func (f *fakeRisk) GetContentsAt(_ context.Context, _ ghclient.Repo, path, sha string) ([]byte, bool, error) {
	if err := f.call("GetContentsAt"); err != nil {
		return nil, false, err
	}
	if sha != riskMergeBase {
		return nil, false, fmt.Errorf("read %s at %s, not the merge base", path, sha)
	}
	body, ok := f.contents[path]
	return []byte(body), ok, nil
}
func (f *fakeRisk) ListCommits(_ context.Context, _ ghclient.Repo, ref, path string, _ time.Time) ([]ghclient.Commit, error) {
	if err := f.call("ListCommits"); err != nil {
		return nil, err
	}
	if ref != riskMergeBase {
		return nil, fmt.Errorf("history of %s read from %s", path, ref)
	}
	return f.commits[path], nil
}
func (f *fakeRisk) ListWorkflowRuns(context.Context, ghclient.Repo, string, string, string) ([]ghclient.WorkflowRun, error) {
	if err := f.call("ListWorkflowRuns"); err != nil {
		return nil, err
	}
	return f.runs, nil
}
func (f *fakeRisk) ListRunArtifacts(context.Context, ghclient.Repo, int64, string) ([]ghclient.Artifact, error) {
	if err := f.call("ListRunArtifacts"); err != nil {
		return nil, err
	}
	return f.artifacts, nil
}
func (f *fakeRisk) DownloadArtifact(context.Context, ghclient.Repo, int64) (io.ReadCloser, error) {
	if err := f.call("DownloadArtifact"); err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(f.archive)), nil
}
func (f *fakeRisk) ListIssueComments(context.Context, ghclient.Repo, int) ([]ghclient.IssueComment, error) {
	if err := f.call("ListIssueComments"); err != nil {
		return nil, err
	}
	return f.comments, nil
}
func (f *fakeRisk) CreateIssueComment(_ context.Context, _ ghclient.Repo, _ int, body string) (ghclient.IssueComment, error) {
	if err := f.call("CreateIssueComment"); err != nil {
		return ghclient.IssueComment{}, err
	}
	f.created = append(f.created, body)
	return ghclient.IssueComment{ID: 99, Body: body}, nil
}
func (f *fakeRisk) UpdateIssueComment(_ context.Context, _ ghclient.Repo, id int64, body string) (ghclient.IssueComment, error) {
	if err := f.call("UpdateIssueComment"); err != nil {
		return ghclient.IssueComment{}, err
	}
	if f.updated == nil {
		f.updated = map[int64]string{}
	}
	f.updated[id] = body
	return ghclient.IssueComment{ID: id, Body: body}, nil
}
func (f *fakeRisk) AddLabels(_ context.Context, _ ghclient.Repo, _ int, labels []string) error {
	if err := f.call("AddLabels"); err != nil {
		return err
	}
	f.added = append(f.added, labels...)
	return nil
}
func (f *fakeRisk) RemoveLabel(_ context.Context, _ ghclient.Repo, _ int, label string) error {
	if err := f.call("RemoveLabel"); err != nil {
		return err
	}
	f.removed = append(f.removed, label)
	return nil
}

// The SCIP index is encoded by hand in the protobuf wire format, as
// internal/risk's reach test does, so the test depends on no encoder.

func pbVarint(v uint64) []byte {
	var out []byte
	for v >= 0x80 {
		out = append(out, byte(v)|0x80)
		v >>= 7
	}
	return append(out, byte(v))
}

func pbField(number int, b []byte) []byte {
	out := pbVarint(uint64(number)<<3 | 2)
	return append(append(out, pbVarint(uint64(len(b)))...), b...)
}

func pbJoin(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func pbOccurrence(symbol string, definition bool, line int) []byte {
	roles := uint64(0)
	if definition {
		roles = 1
	}
	rng := pbJoin(pbVarint(uint64(line)), pbVarint(0), pbVarint(5))
	return pbField(2, pbJoin(pbField(1, rng), pbField(2, []byte(symbol)), pbVarint(3<<3), pbVarint(roles)))
}

func pbDocument(path string, occurrences ...[]byte) []byte {
	return pbField(2, pbJoin(pbField(1, []byte(path)), pbJoin(occurrences...)))
}

// riskIndex is an index in which src/auth/session.ts defines riskSymbol on
// line 2 (0-based 1) and users other files refer to it.
func riskIndex(users int) []byte {
	docs := [][]byte{pbDocument("src/auth/session.ts", pbOccurrence(riskSymbol, true, 1))}
	for i := 0; i < users; i++ {
		docs = append(docs, pbDocument(fmt.Sprintf("src/user%02d.ts", i), pbOccurrence(riskSymbol, false, 0)))
	}
	return pbJoin(docs...)
}

func zipped(t *testing.T, files map[string][]byte) []byte {
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

func revert(subject string) ghclient.Commit {
	var c ghclient.Commit
	c.SHA = "r1"
	c.Commit.Message = subject + "\n\nThis reverts commit x."
	return c
}

// happyRisk is a pull request that changes an auth file with a recent revert
// and wide reach, and the risk map: every input is readable.
func happyRisk(t *testing.T) *fakeRisk {
	t.Helper()
	return &fakeRisk{
		pr: ghclient.PullRequest{
			Number: 7, Head: ghclient.Ref{SHA: riskHead}, Base: ghclient.Ref{Ref: "main", SHA: "base"},
			ChangedFiles: 2,
		},
		files: []ghclient.ChangedFile{
			{Filename: "src/auth/session.ts", Status: "modified", Changes: 2, Deletions: 1,
				Patch: "@@ -1,3 +1,3 @@\n a\n-export function verify() {}\n+export function verify(x) {}\n c"},
			{Filename: ".review/risk.yaml", Status: "modified", Changes: 1, Patch: "@@ -1 +1 @@\n-a\n+b"},
		},
		contents: map[string]string{
			".review/risk.yaml":  "version: 1\nadd:\n  risk/data: [\"db/**\"]\n",
			".github/CODEOWNERS": "* @o/team\n",
		},
		commits: map[string][]ghclient.Commit{
			"src/auth/session.ts": {revert(`Revert "Add sessions"`)},
		},
		runs: []ghclient.WorkflowRun{{
			ID: 3, HeadSHA: riskMergeBase, Event: "push", Path: ".github/workflows/review-index.yml",
			Status: "completed", Conclusion: "success", CreatedAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		}},
		artifacts: []ghclient.Artifact{{ID: 5, Name: "review-index", SizeInBytes: 100}},
		archive:   zipped(t, map[string][]byte{"index.scip": riskIndex(risk.ReachFiles)}),
		viewer:    "risk-bot[bot]",
	}
}

func runJob(t *testing.T, f *fakeRisk, dryRun bool) (stdout, stderr string) {
	t.Helper()
	var out, log bytes.Buffer
	riskJob{
		gh: f, repo: ghclient.Repo{Owner: "o", Name: "r"}, number: 7, dryRun: dryRun,
		runURL: "https://github.example/o/r/actions/runs/1",
		now:    time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), indexBytes: defaultIndexBytes,
		out: &out, log: &log,
	}.run(context.Background())
	return out.String(), log.String()
}

// rendered is the comment the run produced: what it posted, or, when it could
// not post, what it printed in the job log instead.
func rendered(f *fakeRisk, stdout, stderr string) string {
	for _, body := range f.updated {
		return body
	}
	if len(f.created) > 0 {
		return f.created[len(f.created)-1]
	}
	return stdout + stderr
}

func TestRiskRecordWithEveryInput(t *testing.T) {
	f := happyRisk(t)
	stdout, stderr := runJob(t, f, false)
	if len(f.created) != 1 {
		t.Fatalf("want one comment created, got %d; log:\n%s", len(f.created), stderr)
	}
	body := f.created[0]
	want := []string{"risk/auth", "risk/ci", "risk/reach", "risk/history", "risk/review-config"}
	if !reflect.DeepEqual(f.added, want) {
		t.Errorf("added %v, want %v", f.added, want)
	}
	for _, must := range []string{
		reporters.RiskMarker, riskHead, "actions/runs/1", reporters.NoLabelSentence,
		"referenced from 10 other files",
		"<!-- rv:risk-labels risk/auth,risk/ci,risk/reach,risk/history,risk/review-config -->",
	} {
		if !strings.Contains(body, must) {
			t.Errorf("want %q in the comment:\n%s", must, body)
		}
	}
	if strings.Contains(body, "Not everything") {
		t.Errorf("every input was readable, so no warning:\n%s", body)
	}
	if !strings.Contains(stdout, "created") {
		t.Errorf("stdout = %q", stdout)
	}
}

// G3: after the first call to GitHub, nothing fails the run. Each failure the
// command can meet is caught, the run still exits 0, and the comment (or, when
// the comment cannot be posted, the job log) says what went wrong.
func TestRiskErrorWalk(t *testing.T) {
	boom := errors.New("boom")
	forbidden := fmt.Errorf("%w: GET /repos/o/r/actions/runs/3/artifacts: 403 Forbidden", ghclient.ErrForbidden)
	for _, tc := range []struct {
		name  string
		setup func(f *fakeRisk)
		want  []string
	}{
		{"pull request", func(f *fakeRisk) { f.fail = map[string]error{"GetPullRequest": boom} },
			[]string{"could not read the pull request", "unknown head commit"}},
		{"files", func(f *fakeRisk) { f.fail = map[string]error{"ListFiles": boom} },
			[]string{"could not list the changed files"}},
		{"merge base", func(f *fakeRisk) { f.fail = map[string]error{"Compare": boom} },
			[]string{"could not find the merge base", "owner groups not available", "reach not available", "file history not available"}},
		{"contents", func(f *fakeRisk) { f.fail = map[string]error{"GetContentsAt": boom} },
			[]string{"could not read .review/risk.yaml at the merge base", "could not read .github/CODEOWNERS at the merge base"}},
		{"history", func(f *fakeRisk) { f.fail = map[string]error{"ListCommits": boom} },
			[]string{"could not read the history of src/auth/session.ts"}},
		{"index runs", func(f *fakeRisk) { f.fail = map[string]error{"ListWorkflowRuns": boom} },
			[]string{"could not list the review-index.yml runs"}},
		{"artifacts forbidden", func(f *fakeRisk) { f.fail = map[string]error{"ListRunArtifacts": forbidden} },
			[]string{"could not list the index run's artifacts", "403 Forbidden"}},
		{"download", func(f *fakeRisk) { f.fail = map[string]error{"DownloadArtifact": boom} },
			[]string{"could not download the index"}},
		{"viewer", func(f *fakeRisk) { f.fail = map[string]error{"Viewer": boom} },
			[]string{"could not tell which account this token posts as", "github.selfLogin"}},
		{"comments", func(f *fakeRisk) { f.fail = map[string]error{"ListIssueComments": boom} },
			[]string{"could not read the pull request's comments", "not posted"}},
		{"add labels", func(f *fakeRisk) { f.fail = map[string]error{"AddLabels": boom} },
			[]string{"could not add risk/auth"}},
		{"remove label", func(f *fakeRisk) {
			f.fail = map[string]error{"RemoveLabel": boom}
			f.pr.Labels = []ghclient.Label{{Name: "risk/infra"}}
			f.comments = []ghclient.IssueComment{ownComment(f, risk.LabelInfra)}
		}, []string{"could not remove risk/infra"}},
		{"create comment", func(f *fakeRisk) { f.fail = map[string]error{"CreateIssueComment": boom} },
			[]string{"could not post the risk comment", reporters.RiskMarker}},
		{"update comment", func(f *fakeRisk) {
			f.fail = map[string]error{"UpdateIssueComment": boom}
			f.comments = []ghclient.IssueComment{ownComment(f)}
		}, []string{"could not post the risk comment", reporters.RiskMarker}},
		{"bad risk.yaml", func(f *fakeRisk) { f.contents[".review/risk.yaml"] = "version: 1\nremove:\n  risk/auth: [x]\n" },
			[]string{".review/risk.yaml at the merge base was not used", "default risk map"}},
		{"no CODEOWNERS", func(f *fakeRisk) { delete(f.contents, ".github/CODEOWNERS") },
			[]string{"no CODEOWNERS file at the merge base"}},
		{"no index run", func(f *fakeRisk) { f.runs = nil },
			[]string{"no review-index.yml run on a push of the merge base mbmbmbm"}},
		{"corrupt artifact", func(f *fakeRisk) { f.archive = []byte("not a zip") },
			[]string{"reach not available: the index artifact is not a zip archive"}},
		{"index not decodable", func(f *fakeRisk) {
			f.archive = zipped(t, map[string][]byte{"index.scip": {0xff, 0xff, 0xff}})
		}, []string{"the index could not be read"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := happyRisk(t)
			tc.setup(f)
			var out, log bytes.Buffer
			err := runRiskWith(f, &out, &log)
			if code := exitCode(err); err != nil || code != 0 {
				t.Fatalf("want exit 0 and no error, got %d, %v", code, err)
			}
			body := rendered(f, out.String(), log.String())
			for _, w := range tc.want {
				if !strings.Contains(body, w) {
					t.Errorf("want %q in the rendered comment:\n%s", w, body)
				}
			}
			if !strings.Contains(body, reporters.NoLabelSentence) {
				t.Errorf("even a failed record says no label is not safe:\n%s", body)
			}
		})
	}
}

// runRiskWith runs the job the way runRisk does, and returns what runRisk
// would: nothing, whatever happened after the first call.
func runRiskWith(f *fakeRisk, out, log io.Writer) error {
	riskJob{
		gh: f, repo: ghclient.Repo{Owner: "o", Name: "r"}, number: 7,
		now: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), indexBytes: defaultIndexBytes,
		out: out, log: log,
	}.run(context.Background())
	return nil
}

// ownComment is a risk comment this tool's login wrote on an earlier run,
// listing labels as its own.
func ownComment(f *fakeRisk, labels ...risk.Label) ghclient.IssueComment {
	body := reporters.RenderRisk(reporters.RiskComment{HeadSHA: "old", Owned: labels})
	return ghclient.IssueComment{ID: 11, Body: body, User: ghclient.User{Login: f.viewer, Type: "Bot"}}
}

// Labels are added for every label in the record the pull request lacks, and
// removed only when this tool's own earlier list names them. A person's label
// is never touched, and a person's label that agrees with the record is not
// claimed.
func TestRiskLabelSync(t *testing.T) {
	f := happyRisk(t)
	f.pr.Labels = []ghclient.Label{
		{Name: "risk/auth"},  // set by a person, and the record agrees
		{Name: "risk/data"},  // set by this tool, no longer applies
		{Name: "risk/infra"}, // set by a person
		{Name: "bug"},
	}
	f.comments = []ghclient.IssueComment{ownComment(f, risk.LabelData, risk.LabelCI)}
	runJob(t, f, false)

	if want := []string{"risk/ci", "risk/reach", "risk/history", "risk/review-config"}; !reflect.DeepEqual(f.added, want) {
		t.Errorf("added %v, want %v", f.added, want)
	}
	if want := []string{"risk/data"}; !reflect.DeepEqual(f.removed, want) {
		t.Errorf("removed %v, want only this tool's own %v", f.removed, want)
	}
	body := f.updated[11]
	if body == "" {
		t.Fatalf("want the earlier comment updated in place; created %d", len(f.created))
	}
	owned, ok := reporters.ParseRiskLabels(body)
	want := []risk.Label{risk.LabelCI, risk.LabelReach, risk.LabelHistory, risk.LabelReviewConfig}
	if !ok || !reflect.DeepEqual(owned, want) {
		t.Errorf("the new list is %v, want %v: risk/auth is the person's", owned, want)
	}
}

// Anybody can write the marker and a list. A list in somebody else's comment
// is ignored, and their comment is not edited: this tool writes its own.
func TestRiskLabelSyncIgnoresAListFromAnotherAuthor(t *testing.T) {
	f := happyRisk(t)
	f.pr.Labels = []ghclient.Label{{Name: "risk/infra"}, {Name: "risk/data"}}
	spoof := reporters.RenderRisk(reporters.RiskComment{HeadSHA: "x", Owned: []risk.Label{risk.LabelInfra, risk.LabelData}})
	f.comments = []ghclient.IssueComment{{ID: 12, Body: spoof, User: ghclient.User{Login: "mallory", Type: "User"}}}
	runJob(t, f, false)

	if len(f.removed) != 0 {
		t.Errorf("removed %v on the word of someone else's comment", f.removed)
	}
	if _, edited := f.updated[12]; edited {
		t.Error("edited someone else's comment")
	}
	if len(f.created) != 1 {
		t.Errorf("want this tool's own comment created, got %d", len(f.created))
	}
}

// When the token cannot say who it is and no login is configured, nothing is
// trusted, so nothing is removed, and the run says how to fix it.
func TestRiskLabelSyncWithoutAnIdentityRemovesNothing(t *testing.T) {
	f := happyRisk(t)
	f.fail = map[string]error{"Viewer": errors.New("403")}
	f.pr.Labels = []ghclient.Label{{Name: "risk/data"}}
	f.comments = []ghclient.IssueComment{ownComment(f, risk.LabelData)}
	runJob(t, f, false)
	if len(f.removed) != 0 {
		t.Errorf("removed %v without knowing whose list it was", f.removed)
	}
	if body := f.updated[11]; !strings.Contains(body, "github.selfLogin") {
		t.Errorf("want the bot's comment updated, with the fix named:\n%s", body)
	}
}

// A label whose signal could not be computed on this run is kept: absent is
// not the same as not applying.
func TestRiskLabelSyncKeepsALabelItCouldNotJudge(t *testing.T) {
	f := happyRisk(t)
	f.fail = map[string]error{"ListCommits": errors.New("rate limit")}
	f.runs = nil // and no index, so reach is not judged either
	f.pr.Labels = []ghclient.Label{{Name: "risk/history"}, {Name: "risk/reach"}}
	f.comments = []ghclient.IssueComment{ownComment(f, risk.LabelHistory, risk.LabelReach)}
	runJob(t, f, false)
	if len(f.removed) != 0 {
		t.Errorf("removed %v on a run that could not have set them", f.removed)
	}
	owned, _ := reporters.ParseRiskLabels(f.updated[11])
	for _, l := range []risk.Label{risk.LabelHistory, risk.LabelReach} {
		if !contains(owned, l) {
			t.Errorf("%s should stay on the list, got %v", l, owned)
		}
	}
}

// History is read for at most 20 files, most-changed first, and never for a
// file the pull request adds. The cap is said, and, being a choice rather than
// a failure, does not keep a risk/history label that no longer applies.
func TestRiskHistoryReadsTheTwentyMostChanged(t *testing.T) {
	f := happyRisk(t)
	f.files = []ghclient.ChangedFile{{Filename: "docs/new.md", Status: "added", Changes: 1000}}
	for i := 1; i <= 25; i++ {
		f.files = append(f.files, ghclient.ChangedFile{Filename: fmt.Sprintf("docs/f%02d.md", i), Status: "modified", Changes: i})
	}
	f.pr.ChangedFiles = len(f.files)
	f.commits = map[string][]ghclient.Commit{"docs/f01.md": {revert(`Revert "x"`)}} // least changed: not read
	f.pr.Labels = []ghclient.Label{{Name: "risk/history"}}
	f.comments = []ghclient.IssueComment{ownComment(f, risk.LabelHistory)}
	runJob(t, f, false)

	reads := 0
	for _, c := range f.calls {
		if c == "ListCommits" {
			reads++
		}
	}
	if reads != historyFiles {
		t.Errorf("read the history of %d files, want %d", reads, historyFiles)
	}
	if !reflect.DeepEqual(f.removed, []string{"risk/history"}) {
		t.Errorf("removed %v; the cap should not keep risk/history", f.removed)
	}
	if body := f.updated[11]; !strings.Contains(body, "5 of the 25 files that existed before were not read") {
		t.Errorf("want the cap said:\n%s", body)
	}
}

func contains(labels []risk.Label, l risk.Label) bool {
	for _, x := range labels {
		if x == l {
			return true
		}
	}
	return false
}

// A draft gets no record, and the run says so.
func TestRiskSkipsADraft(t *testing.T) {
	f := happyRisk(t)
	f.pr.Draft = true
	stdout, _ := runJob(t, f, false)
	if !strings.Contains(stdout, "is a draft") {
		t.Errorf("want the skip said, got %q", stdout)
	}
	for _, c := range f.calls {
		if c != "GetPullRequest" {
			t.Errorf("a draft should cost one read, but also called %s", c)
		}
	}
}

// --dry-run prints the comment and the label changes and writes nothing.
func TestRiskDryRunWritesNothing(t *testing.T) {
	f := happyRisk(t)
	stdout, _ := runJob(t, f, true)
	for _, c := range f.calls {
		switch c {
		case "CreateIssueComment", "UpdateIssueComment", "AddLabels", "RemoveLabel":
			t.Errorf("a dry run called %s", c)
		}
	}
	for _, must := range []string{reporters.RiskMarker, "would add: risk/auth, risk/ci", "would remove: none"} {
		if !strings.Contains(stdout, must) {
			t.Errorf("want %q in:\n%s", must, stdout)
		}
	}
}

// Mistakes found before the first call to GitHub are usage errors, exit 2.
func TestRiskUsageErrors(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	env := func(k string) string {
		if k == "GITHUB_TOKEN" {
			return "t"
		}
		return ""
	}
	for name, tc := range map[string]struct {
		config string
		args   []string
	}{
		"no --pr":         {"github: {apiBaseUrl: http://127.0.0.1:1, repo: o/r}\n", []string{"risk"}},
		"unknown key":     {"githb: {}\n", []string{"risk", "--pr", "1"}},
		"no API base URL": {"github: {repo: o/r}\n", []string{"risk", "--pr", "1"}},
		"no repo":         {"github: {apiBaseUrl: http://127.0.0.1:1, repo: nonsense}\n", []string{"risk", "--pr", "1"}},
	} {
		t.Run(name, func(t *testing.T) {
			write(".review/config.yaml", tc.config)
			var out, errOut bytes.Buffer
			err := run(context.Background(), append(tc.args, "--root", root), &out, &errOut, env)
			if exitCode(err) != exitUsage {
				t.Errorf("exit %d (%v), want %d", exitCode(err), err, exitUsage)
			}
		})
	}
}

// The index limit is the workspace's, as its example configuration ships it.
func TestRiskIndexLimitIsTheWorkspaces(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "examples", "workspace.config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^\s*indexBytes:\s*(\d+)`).FindSubmatch(raw)
	if m == nil {
		t.Fatal("no indexBytes in examples/workspace.config.yaml")
	}
	if n, _ := strconv.ParseInt(string(m[1]), 10, 64); n != defaultIndexBytes {
		t.Errorf("the example says %d, reviewer risk reads up to %d", n, defaultIndexBytes)
	}
}

func TestRunURL(t *testing.T) {
	env := map[string]string{"GITHUB_SERVER_URL": "https://github.example/", "GITHUB_REPOSITORY": "o/r", "GITHUB_RUN_ID": "9"}
	if got := runURL(func(k string) string { return env[k] }); got != "https://github.example/o/r/actions/runs/9" {
		t.Errorf("runURL = %q", got)
	}
	delete(env, "GITHUB_RUN_ID")
	if got := runURL(func(k string) string { return env[k] }); got != "" {
		t.Errorf("without a run id, want no link, got %q", got)
	}
}

// The whole command, through the real client against a stand-in GitHub: the
// index run's artifacts answer 403, as they would for a job without
// actions: read. The run exits 0, posts its comment, and says why reach is
// missing.
func TestRiskEndToEndWithArtifactsForbidden(t *testing.T) {
	var posted, labelled string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch p := r.URL.Path; {
		case p == "/user":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
		case p == "/repos/o/r/pulls/7":
			fmt.Fprintf(w, `{"number":7,"changed_files":1,"head":{"sha":%q},"base":{"ref":"main","sha":"base"},"labels":[]}`, riskHead)
		case p == "/repos/o/r/pulls/7/files":
			fmt.Fprint(w, `[{"filename":"src/auth/session.ts","status":"modified","changes":2,"deletions":1,"patch":"@@ -1,2 +1,2 @@\n-a\n+b\n c"}]`)
		case strings.HasPrefix(p, "/repos/o/r/compare/"):
			fmt.Fprintf(w, `{"merge_base_commit":{"sha":%q}}`, riskMergeBase)
		case strings.HasPrefix(p, "/repos/o/r/contents/"):
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"Not Found"}`)
		case p == "/repos/o/r/commits":
			fmt.Fprint(w, `[]`)
		case p == "/repos/o/r/actions/workflows/review-index.yml/runs":
			fmt.Fprintf(w, `{"workflow_runs":[{"id":3,"head_sha":%q,"event":"push","path":".github/workflows/review-index.yml","status":"completed","conclusion":"success"}]}`, riskMergeBase)
		case p == "/repos/o/r/actions/runs/3/artifacts":
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
		case p == "/repos/o/r/issues/7/comments" && r.Method == http.MethodGet:
			fmt.Fprint(w, `[]`)
		case p == "/repos/o/r/issues/7/comments" && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			posted = string(raw)
			fmt.Fprint(w, `{"id":1}`)
		case p == "/repos/o/r/issues/7/labels" && r.Method == http.MethodPost:
			raw, _ := io.ReadAll(r.Body)
			labelled = string(raw)
			fmt.Fprint(w, `[]`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer server.Close()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".review"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf("github:\n  apiBaseUrl: %s\n  repo: o/r\n  selfLogin: github-actions[bot]\n", server.URL)
	if err := os.WriteFile(filepath.Join(root, ".review", "config.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "GITHUB_TOKEN" {
			return "t"
		}
		return ""
	}
	var out, errOut bytes.Buffer
	err := run(context.Background(), []string{"risk", "--pr", "7", "--root", root}, &out, &errOut, env)
	if code := exitCode(err); err != nil || code != 0 {
		t.Fatalf("want exit 0, got %d, %v\n%s", code, err, errOut.String())
	}
	for _, must := range []string{"could not list the index run's artifacts", "403 Forbidden", "risk/auth", "no CODEOWNERS file"} {
		if !strings.Contains(posted, must) {
			t.Errorf("want %q in the posted comment:\n%s", must, posted)
		}
	}
	if labelled != `{"labels":["risk/auth"]}` {
		t.Errorf("labels request = %s", labelled)
	}
}
