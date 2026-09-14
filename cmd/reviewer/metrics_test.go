// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/fingerprint"
)

// selfLogin is the account this tool posts as.
const selfLogin = "reviewer[bot]"

// thread is one review conversation as the GraphQL reply shapes it.
type thread struct {
	rule     string
	resolved bool
	outdated bool
	// mine is false for a human's conversation, which must not be counted.
	mine bool
	// author overrides who wrote the first comment. A human quoting one of our
	// markers is the case that matters, and it is invisible unless authorship
	// varies — which is why the earlier version of this file, where every thread
	// was authored by the bot, passed while the check was missing.
	author string
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
				author := th.author
				if author == "" {
					author = selfLogin
				}
				nodes = append(nodes, map[string]any{
					"id": fmt.Sprintf("T%d", i), "isResolved": th.resolved, "isOutdated": th.outdated,
					"comments": map[string]any{"nodes": []any{map[string]any{
						"databaseId": i + 1, "body": body,
						"author": map[string]string{"login": author},
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

		if strings.HasSuffix(r.URL.Path, "/user") {
			_ = json.NewEncoder(w).Encode(map[string]string{"login": selfLogin})
			return
		}
		if strings.Contains(r.URL.Path, "/issues/") {
			// No summary comment unless a test adds one.
			_ = json.NewEncoder(w).Encode([]any{})
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
	if !strings.Contains(out, "| 2 | 0 | 0 | 2 | 0 |") {
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
	if !strings.Contains(out, "| 4 | 2 | 1 | 1 | 0 |") {
		t.Errorf("want every bucket shown:\n%s", out)
	}
}

// A thread a human started is their conversation, not this tool's measurement.
//
// Two ways for a thread not to be ours, and the second is the one that was missing:
// no marker at all, and a marker a human *quoted* in a thread they opened. The
// reporter already refuses to resolve the second kind; the measurement must refuse
// to charge a rule for it.
func TestOnlyOurOwnThreadsAreCounted(t *testing.T) {
	server := fakeGitHub(t, time.Now(), []thread{
		{rule: "types/TS2322", resolved: true, mine: true},
		{mine: false},
		// A human's thread that quotes one of our comments, marker and all. Left
		// unresolved, it would otherwise be charged to the rule as a rejection.
		{rule: "types/TS2322", mine: true, author: "alice"},
	})
	defer server.Close()

	out, _, err := runMetrics(t, server)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| 1 | 1 | 0 | 0 | 0 |") {
		t.Errorf("want only our own thread counted:\n%s", out)
	}
	// "100% (1/1)" contains "0% ", so the check has to be about the denominator:
	// the human's thread would have made it 1/2.
	if !strings.Contains(out, "100% (1/1)") {
		t.Errorf("a human's quoted marker was charged to the rule:\n%s", out)
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
	if lines[0] != "rule,posted,resolved,open,outdated,summarised,acceptance" {
		t.Fatalf("header = %q", lines[0])
	}
	if !strings.Contains(out, "types/TS2322,3,1,2,0,0,0.3333") {
		t.Errorf("want the unrounded ratio:\n%s", out)
	}
	// An empty field rather than a number: a spreadsheet must not average "n/a"
	// into a zero.
	if !strings.Contains(out, "never/decided,1,0,0,1,0,\n") {
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
		// Overflows int64 and lands the cutoff in the future, which filtered out
		// every pull request and printed a plausible-looking empty table.
		{in: "1000000d", wantErr: true},
		{in: "9000000h", wantErr: true},
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

// A rule capped below high confidence never produces an inline thread, so reading
// only threads gave it no row at all — a rule that fired two hundred times looked
// like one that never fires, which is the opposite of what this table is for.
func TestSummaryFindingsAreCounted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			_ = json.NewEncoder(w).Encode(map[string]string{"login": selfLogin})
		case strings.Contains(r.URL.Path, "graphql"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"repository": map[string]any{"pullRequest": map[string]any{
					"reviewThreads": map[string]any{"nodes": []any{},
						"pageInfo": map[string]any{"hasNextPage": false}}}}}})
		case strings.Contains(r.URL.Path, "/issues/"):
			body := "<!-- rv:summary -->\n" +
				"- `a.ts:1` — something " + fingerprint.Marker("aaa111", "quality/sloppy") + "\n" +
				"- `b.ts:2` — something " + fingerprint.Marker("bbb222", "quality/sloppy") + "\n"
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "body": body, "user": map[string]string{"login": selfLogin}},
				// A human's comment that quotes a marker is not ours.
				{"id": 2, "body": "why? " + fingerprint.Marker("ccc333", "quality/sloppy"),
					"user": map[string]string{"login": "alice"}},
			})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 1, "updated_at": time.Now().Format(time.RFC3339)},
			})
		}
	}))
	defer server.Close()

	out, _, err := runMetrics(t, server)
	if err != nil {
		t.Fatal(err)
	}
	// Two summarised, nothing decided: the rule is visible and honestly has no
	// acceptance, because a summary carries no per-finding outcome.
	if !strings.Contains(out, "| `quality/sloppy` | 2 | 0 | 0 | 0 | 2 | n/a |") {
		t.Errorf("want the summarised findings counted:\n%s", out)
	}
	if strings.Contains(out, "| 3 |") {
		t.Errorf("a human's quoted marker was counted:\n%s", out)
	}
}

// One pull request that could not be read is not a reason to report nothing about
// the rest. The comment said so; the caller discarded the partial tally.
func TestAPartialMeasurementIsPrintedAndSaysSo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			_ = json.NewEncoder(w).Encode(map[string]string{"login": selfLogin})
		case strings.Contains(r.URL.Path, "graphql"):
			// The first pull request answers; the second hits a rate limit.
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"number":2`) || strings.Contains(string(body), `"number": 2`) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"errors": []any{map[string]string{"message": "was submitted too quickly"}}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"repository": map[string]any{"pullRequest": map[string]any{
					"reviewThreads": map[string]any{
						"nodes": []any{map[string]any{
							"id": "t1", "isResolved": true, "isOutdated": false,
							"comments": map[string]any{"nodes": []any{map[string]any{
								"databaseId": 1,
								"body":       "f\n" + fingerprint.Marker("aaa111", "types/TS2322"),
								"author":     map[string]string{"login": selfLogin},
							}}}}},
						"pageInfo": map[string]any{"hasNextPage": false}}}}}})
		case strings.Contains(r.URL.Path, "/issues/"):
			_ = json.NewEncoder(w).Encode([]any{})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 1, "updated_at": time.Now().Format(time.RFC3339)},
				{"number": 2, "updated_at": time.Now().Format(time.RFC3339)},
			})
		}
	}))
	defer server.Close()

	out, errOut, err := runMetrics(t, server)
	if err != nil {
		t.Fatalf("a partial measurement is not a failed command: %v", err)
	}
	if !strings.Contains(out, "types/TS2322") {
		t.Errorf("the readable pull request's tally was discarded:\n%s", out)
	}
	// And nobody reads it as complete.
	if !strings.Contains(out, "This table is incomplete") {
		t.Errorf("want the gap stated in the table:\n%s", out)
	}
	if !strings.Contains(errOut, "incomplete") {
		t.Errorf("want the gap on stderr too:\n%s", errOut)
	}
}

// Cost should scale with the window, not with the repository. The list is sorted by
// update time descending, so the first one older than the cutoff ends it.
func TestPaginationStopsAtTheWindow(t *testing.T) {
	var restPages, graphQLCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			_ = json.NewEncoder(w).Encode(map[string]string{"login": selfLogin})
		case strings.Contains(r.URL.Path, "graphql"):
			graphQLCalls++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"repository": map[string]any{"pullRequest": map[string]any{
					"reviewThreads": map[string]any{"nodes": []any{},
						"pageInfo": map[string]any{"hasNextPage": false}}}}}})
		case strings.Contains(r.URL.Path, "/issues/"):
			_ = json.NewEncoder(w).Encode([]any{})
		default:
			restPages++
			// One page: two recent, then one old. The old one ends it, and the
			// Link header offers more pages that must never be fetched.
			w.Header().Set("Link", `<`+r.Host+`/next>; rel="next"`)
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"number": 1, "updated_at": time.Now().Format(time.RFC3339)},
				{"number": 2, "updated_at": time.Now().Add(-time.Hour).Format(time.RFC3339)},
				{"number": 3, "updated_at": time.Now().AddDate(0, 0, -60).Format(time.RFC3339)},
			})
		}
	}))
	defer server.Close()

	if _, _, err := runMetrics(t, server, "--since", "7d"); err != nil {
		t.Fatal(err)
	}
	if restPages != 1 {
		t.Errorf("fetched %d pages; the window ended on the first", restPages)
	}
	if graphQLCalls != 2 {
		t.Errorf("read %d pull requests, want the two in the window", graphQLCalls)
	}
}

// A typo should not cost a full scan of the repository before it is reported.
func TestAnUnknownFormatIsRefusedBeforeAnyNetworkWork(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	_, _, err := runMetrics(t, server, "--format", "json")
	var usageErr errUsage
	if !errors.As(err, &usageErr) {
		t.Fatalf("want a usage error, got %v", err)
	}
	if calls != 0 {
		t.Errorf("made %d requests before rejecting the flag", calls)
	}
}
