// SPDX-License-Identifier: MIT

package scipfetch

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

const (
	sha      = "1111111111111111111111111111111111111111"
	workflow = ".github/workflows/review-index.yml"
)

func run(id int64, head, status, conclusion string, minutesAgo int) ghclient.WorkflowRun {
	return ghclient.WorkflowRun{
		ID: id, HeadSHA: head, Event: "push", Path: workflow, Status: status, Conclusion: conclusion,
		CreatedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC).Add(-time.Duration(minutesAgo) * time.Minute),
	}
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
		state, bound := Classify(tc.runs, sha, workflow)
		var id int64
		if bound != nil {
			id = bound.ID
		}
		if state != tc.state || id != tc.runID {
			t.Errorf("%s: state %q from run %d, want %q from run %d", name, state, id, tc.state, tc.runID)
		}
	}
}

// The navigation package's tests reach Unzip through a lookup; these call it
// directly, as `reviewer risk` will.
func TestUnzip(t *testing.T) {
	const limit = 64
	index := []byte("an index, as far as this function cares")
	for name, tc := range map[string]struct {
		archive []byte
		problem string
	}{
		"the one file":                   {zipOf(t, map[string][]byte{IndexFile: index}), ""},
		"exactly the limit":              {zipOf(t, map[string][]byte{IndexFile: bytes.Repeat([]byte{1}, limit)}), ""},
		"one byte past the limit":        {zipOf(t, map[string][]byte{IndexFile: bytes.Repeat([]byte{1}, limit+1)}), "larger than"},
		"not a zip":                      {[]byte("<html>error</html>"), "not a zip"},
		"two files":                      {zipOf(t, map[string][]byte{IndexFile: index, "extra.txt": []byte("x")}), "exactly one file"},
		"one file with another name":     {zipOf(t, map[string][]byte{"index.scip.bak": index}), "exactly one file"},
		"the right name inside a folder": {zipOf(t, map[string][]byte{"out/" + IndexFile: index}), "exactly one file"},
		"no file":                        {zipOf(t, nil), "exactly one file"},
	} {
		raw, problem := Unzip(tc.archive, limit)
		switch {
		case tc.problem == "" && problem != "":
			t.Errorf("%s: refused with %q", name, problem)
		case tc.problem != "" && !strings.Contains(problem, tc.problem):
			t.Errorf("%s: problem %q, want one containing %q", name, problem, tc.problem)
		case tc.problem != "" && raw != nil:
			t.Errorf("%s: refused and still returned %d bytes", name, len(raw))
		case name == "the one file" && !bytes.Equal(raw, index):
			t.Errorf("%s: returned %q", name, raw)
		}
	}
}
