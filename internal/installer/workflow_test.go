// SPDX-License-Identifier: MIT

package installer

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/adamtait/reviewer/internal/testfixture"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// renderedWorkflow is review.yml as init writes it for the npm fixture.
func renderedWorkflow(t *testing.T) string {
	t.Helper()
	p := BuildPlan(mustDetect(t, testfixture.Destination(t, "tiny-monorepo")), releaseV, Options{})
	files, err := Render(p, releaseV)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.Path == ".github/workflows/review.yml" {
			return string(f.Content)
		}
	}
	t.Fatal("review.yml was not rendered")
	return ""
}

// The whole generated workflow, so a change to it is seen in review as a diff
// of what a repository receives.
func TestReviewWorkflowGolden(t *testing.T) {
	got := renderedWorkflow(t)
	path := filepath.Join("..", "..", "testdata", "golden", "review.yml")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Skip("golden file updated")
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (rerun with -update to create it)", err)
	}
	if got != string(want) {
		t.Fatalf("review.yml differs from %s; rerun with -update if intended\n--- got ---\n%s", path, got)
	}
}

type workflow struct {
	On struct {
		PullRequest struct {
			Types []string `yaml:"types"`
		} `yaml:"pull_request"`
	} `yaml:"on"`
	Permissions map[string]string `yaml:"permissions"`
	Jobs        map[string]struct {
		If          string            `yaml:"if"`
		Needs       any               `yaml:"needs"`
		Permissions map[string]string `yaml:"permissions"`
		Steps       []struct {
			Uses string            `yaml:"uses"`
			With map[string]string `yaml:"with"`
			Env  map[string]string `yaml:"env"`
		} `yaml:"steps"`
	} `yaml:"jobs"`
}

// The workflow as written: the review job alone, with the review job's
// permissions, and a trigger that includes ready_for_review so the risk job
// runs when a draft is marked ready.
func TestReviewWorkflowAsWritten(t *testing.T) {
	var wf workflow
	if err := yaml.Unmarshal([]byte(renderedWorkflow(t)), &wf); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(wf.On.PullRequest.Types, ","); got != "opened,synchronize,reopened,ready_for_review" {
		t.Errorf("trigger types = %s", got)
	}
	if len(wf.Jobs) != 1 || wf.Jobs["review"].Steps == nil {
		t.Errorf("want only the review job active, got %v", len(wf.Jobs))
	}
	if len(wf.Permissions) != 2 || wf.Permissions["contents"] != "read" || wf.Permissions["pull-requests"] != "write" {
		t.Errorf("the review job's permissions changed: %v (ADR-0018)", wf.Permissions)
	}
}

// Uncommented, the risk job is a valid job: no needs, so it runs beside the
// review; skips drafts; holds its own permissions, with actions: read for the
// index; and runs the same action, pinned the same way, with mode: risk.
func TestReviewWorkflowRiskJobUncommented(t *testing.T) {
	body := renderedWorkflow(t)
	start := strings.Index(body, "  # risk:\n")
	if start < 0 {
		t.Fatalf("no commented-out risk job:\n%s", body)
	}
	uncommented := strings.Split(body[:start], "\n")
	for _, line := range strings.Split(strings.TrimRight(body[start:], "\n"), "\n") {
		if !strings.HasPrefix(line, "  # ") {
			t.Fatalf("the risk job should be the last thing in the file, commented out; found %q", line)
		}
		uncommented = append(uncommented, "  "+strings.TrimPrefix(line, "  # "))
	}

	var wf workflow
	if err := yaml.Unmarshal([]byte(strings.Join(uncommented, "\n")), &wf); err != nil {
		t.Fatalf("the risk job does not parse once uncommented: %v", err)
	}
	job, ok := wf.Jobs["risk"]
	if !ok {
		t.Fatal("no risk job once uncommented")
	}
	if job.Needs != nil {
		t.Errorf("the risk job runs beside the review, so it has no needs: %v", job.Needs)
	}
	if job.If != "github.event.pull_request.draft == false" {
		t.Errorf("the risk job should skip drafts, got if: %q", job.If)
	}
	want := map[string]string{"contents": "read", "pull-requests": "write", "actions": "read"}
	if len(job.Permissions) != len(want) {
		t.Errorf("risk job permissions = %v, want %v", job.Permissions, want)
	}
	for k, v := range want {
		if job.Permissions[k] != v {
			t.Errorf("risk job permissions = %v, want %v", job.Permissions, want)
		}
	}
	if len(job.Steps) != 2 || job.Steps[0].Uses != "actions/checkout@v4" {
		t.Fatalf("want checkout then the action, got %+v", job.Steps)
	}
	action := job.Steps[1]
	review := wf.Jobs["review"].Steps[len(wf.Jobs["review"].Steps)-1]
	if action.Uses != review.Uses || action.With["version"] != review.With["version"] || action.With["checksum"] != review.With["checksum"] {
		t.Errorf("the risk job should pin the action as the review does: %+v vs %+v", action, review)
	}
	if action.With["mode"] != "risk" || action.Env["GITHUB_TOKEN"] == "" {
		t.Errorf("want mode: risk and the token, got %+v", action)
	}
	if _, ok := wf.Permissions["actions"]; ok {
		t.Error("actions: read belongs to the risk job alone, not the workflow")
	}
}
