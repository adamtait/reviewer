// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/fingerprint"
)

// thread is one review conversation as the GraphQL reply shapes it.
type thread struct {
	rule     string
	resolved bool
	outdated bool
	// mine is false for a human's conversation, which must not be counted.
	mine bool
}

// fakeGitHub serves the two calls metrics makes.
func fakeGitHub(t *testing.T, updated time.Time, threads []thread) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "graphql") {
			nodes := make([]map[string]any, 0, len(threads))
			for i, th := range threads {
				body := "a human wrote this"
				if th.mine {
					body = "the finding\n" + fingerprint.Marker(fmt.Sprintf("%06x", i), th.rule)
				}
				nodes = append(nodes, map[string]any{
					"id": fmt.Sprintf("T%d", i), "isResolved": th.resolved, "isOutdated": th.outdated,
					"comments": map[string]any{"nodes": []any{map[string]any{
						"databaseId": i + 1, "body": body,
						"author": map[string]string{"login": "reviewer[bot]"},
					}}},
				})
			}
			reply := map[string]any{"data": map[string]any{"repository": map[string]any{
				"pullRequest": map[string]any{"reviewThreads": map[string]any{
					"nodes":    nodes,
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				}},
			}}}
			_ = json.NewEncoder(w).Encode(reply)
			return
		}

		// The pull request list. One inside the window, one well outside it.
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "state": "closed", "updated_at": updated.Format(time.RFC3339)},
			{"number": 2, "state": "closed", "updated_at": time.Now().AddDate(-1, 0, 0).Format(time.RFC3339)},
		})
	}))
}

func runMetrics(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	root := t.TempDir()
	writeConfig(t, root, strings.Join([]string{
		"github:",
		"  apiBaseUrl: " + server.URL,
		"  graphqlUrl: " + server.URL + "/graphql",
		"  repo: owner/name",
		"",
	}, "\n"))

	var stdout, stderr bytes.Buffer
	err := run(context.Background(), append([]string{"metrics", "--root", root}, args...),
		&stdout, &stderr, func(k string) string {
			if k == "GITHUB_TOKEN" {
				return "ghp_test0123456789"
			}
			return ""
		})
	return stdout.String(), stderr.String(), err
}

// The exit criterion, and the thing that decides whether a rule can be deleted on
// this table: a rule nobody has had the chance to act on is not a rule at 0%.
func TestARuleWithNothingDecidedShowsNotApplicable(t *testing.T) {
	server := fakeGitHub(t, time.Now(), []thread{
		{rule: "conventions/no-console", outdated: true, mine: true},
		{rule: "conventions/no-console", outdated: true, mine: true},
	})
	defer server.Close()

	out, _, err := runMetrics(t, server)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "n/a") {
		t.Errorf("want n/a for a rule with nothing decided:\n%s", out)
	}
	if strings.Contains(out, "0% (0/0)") {
		t.Errorf("reporting 0%% is how a new rule gets deleted for being new:\n%s", out)
	}
	// Outdated is still counted and shown: it is evidence about the branch's
	// velocity, and hiding it would make the totals not add up.
	if !strings.Contains(out, "| 2 | 0 | 0 | 2 |") {
		t.Errorf("want the outdated threads counted:\n%s", out)
	}
}

func TestAcceptanceExcludesOutdatedThreads(t *testing.T) {
	server := fakeGitHub(t, time.Now(), []thread{
		{rule: "types/TS2322", resolved: true, mine: true},
		{rule: "types/TS2322", resolved: true, mine: true},
		{rule: "types/TS2322", mine: true},
		// The code moved underneath this one, so nobody decided anything.
		{rule: "types/TS2322", outdated: true, mine: true},
	})
	defer server.Close()

	out, _, err := runMetrics(t, server)
	if err != nil {
		t.Fatal(err)
	}
	// 2 resolved of 3 decided, not of 4 posted.
	if !strings.Contains(out, "67% (2/3)") {
		t.Errorf("want outdated excluded from the ratio:\n%s", out)
	}
	if !strings.Contains(out, "| 4 | 2 | 1 | 1 |") {
		t.Errorf("want every bucket shown:\n%s", out)
	}
}

// A thread a human started is their conversation, not this tool's measurement.
func TestOnlyOurOwnThreadsAreCounted(t *testing.T) {
	server := fakeGitHub(t, time.Now(), []thread{
		{rule: "types/TS2322", resolved: true, mine: true},
		{mine: false},
		{mine: false},
	})
	defer server.Close()

	out, _, err := runMetrics(t, server)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| 1 | 1 | 0 | 0 |") {
		t.Errorf("want only our own thread counted:\n%s", out)
	}
}

// The worst rule is the first row: the table exists to answer "what should I
// delete", and the answer should not need sorting by hand.
func TestTheWorstRuleSortsFirstAndUndecidedSortsLast(t *testing.T) {
	server := fakeGitHub(t, time.Now(), []thread{
		{rule: "good/rule", resolved: true, mine: true},
		{rule: "bad/rule", mine: true},
		{rule: "bad/rule", mine: true},
		{rule: "unknown/rule", outdated: true, mine: true},
	})
	defer server.Close()

	out, _, err := runMetrics(t, server)
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Index(out, "bad/rule")
	good := strings.Index(out, "good/rule")
	unknown := strings.Index(out, "unknown/rule")
	if !(bad < good && good < unknown) {
		t.Errorf("want worst first and undecided last:\n%s", out)
	}
}

func TestCSVIsUnroundedForReaggregation(t *testing.T) {
	server := fakeGitHub(t, time.Now(), []thread{
		{rule: "types/TS2322", resolved: true, mine: true},
		{rule: "types/TS2322", mine: true},
		{rule: "types/TS2322", mine: true},
		{rule: "never/decided", outdated: true, mine: true},
	})
	defer server.Close()

	out, _, err := runMetrics(t, server, "--format", "csv")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if lines[0] != "rule,posted,resolved,open,outdated,acceptance" {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.Contains(out, "types/TS2322,3,1,2,0,0.3333") {
		t.Errorf("want the unrounded ratio:\n%s", out)
	}
	// An empty field rather than a number: a spreadsheet must not average "n/a"
	// into a zero.
	if !strings.Contains(out, "never/decided,1,0,0,1,\n") {
		t.Errorf("want an empty acceptance for a rule with nothing decided:\n%s", out)
	}
}

// The window is the whole point of --since: a rule's acceptance a year ago says
// nothing about whether to keep it now.
func TestPullRequestsOutsideTheWindowAreNotRead(t *testing.T) {
	var graphQLCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "graphql") {
			graphQLCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"repository": map[string]any{"pullRequest": map[string]any{
					"reviewThreads": map[string]any{"nodes": []any{},
						"pageInfo": map[string]any{"hasNextPage": false}}}}}})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"number": 1, "updated_at": time.Now().Format(time.RFC3339)},
			{"number": 2, "updated_at": time.Now().AddDate(0, 0, -30).Format(time.RFC3339)},
			{"number": 3, "updated_at": time.Now().AddDate(0, 0, -60).Format(time.RFC3339)},
		})
	}))
	defer server.Close()

	if _, _, err := runMetrics(t, server, "--since", "7d"); err != nil {
		t.Fatal(err)
	}
	if graphQLCalls != 1 {
		t.Errorf("read %d pull requests, want only the one in the window", graphQLCalls)
	}
}

func TestParseSince(t *testing.T) {
	for _, tc := range []struct {
		in      string
		hours   float64
		wantErr bool
	}{
		{in: "", hours: 24 * 7},
		{in: "7d", hours: 24 * 7},
		{in: "2w", hours: 24 * 14},
		{in: "36h", hours: 36},
		{in: "30", hours: 24 * 30},
		{in: "0d", wantErr: true},
		{in: "-1d", wantErr: true},
		{in: "soon", wantErr: true},
	} {
		got, err := parseSince(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseSince(%q) = %v, want an error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseSince(%q): %v", tc.in, err)
			continue
		}
		if elapsed := time.Since(got).Hours(); elapsed < tc.hours-1 || elapsed > tc.hours+1 {
			t.Errorf("parseSince(%q) is %.0f hours ago, want %.0f", tc.in, elapsed, tc.hours)
		}
	}
}

func TestMetricsFlagsBelongToMetrics(t *testing.T) {
	for _, args := range [][]string{{"--since", "7d"}, {"--format", "csv"}, {"--repo", "a/b"}} {
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), args, &stdout, &stderr, noEnv)
		var usageErr errUsage
		if !errors.As(err, &usageErr) {
			t.Errorf("%v on a review: want a usage error, got %v", args, err)
		}
	}
}

func TestAnUnknownFormatIsMisuse(t *testing.T) {
	server := fakeGitHub(t, time.Now(), nil)
	defer server.Close()

	_, _, err := runMetrics(t, server, "--format", "json")
	var usageErr errUsage
	if !errors.As(err, &usageErr) {
		t.Fatalf("want a usage error, got %v", err)
	}
}

// Nothing to report is a sentence, not an empty table.
func TestNoFindingsInTheWindowSaysSo(t *testing.T) {
	server := fakeGitHub(t, time.Now(), nil)
	defer server.Close()

	out, _, err := runMetrics(t, server)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No findings from this tool were posted") {
		t.Errorf("want it stated plainly:\n%s", out)
	}
}
