// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/config"
	"github.com/adamtait/reviewer/internal/fingerprint"
	"github.com/adamtait/reviewer/internal/ghclient"
)

// metrics answers the only question that keeps a rule honest: is anybody acting on
// what it says?
//
// A rule that fires often and is dismissed every time is worse than no rule. It
// costs a reviewer's attention on every pull request and teaches them to skim the
// rest, so the cheapest thing this project can do for its own quality is measure
// per rule and delete what nobody wants (ADR-0024).
func metrics(ctx context.Context, o options, stdout, stderr io.Writer, getenv func(string) string) error {
	since, err := parseSince(o.since)
	if err != nil {
		return errUsage{err}
	}

	cfg, secrets, err := config.Resolve(o.root, o.config, getenv)
	if err != nil {
		return err
	}
	if o.repo != "" {
		cfg.GitHub.Repo = o.repo
	}
	client, repo, err := githubClient(cfg, secrets)
	if err != nil {
		return err
	}

	tally, scanned, err := gather(ctx, client, repo, since)
	if err != nil {
		return err
	}
	if scanned == 0 {
		fmt.Fprintf(stderr, "reviewer: no pull requests updated since %s\n", since.Format(time.DateOnly))
	}

	switch o.format {
	case "", "markdown":
		return writeMarkdown(stdout, tally, repo, since, scanned)
	case "csv":
		return writeCSV(stdout, tally)
	default:
		return errUsage{fmt.Errorf("--format is markdown or csv, not %q", o.format)}
	}
}

// counts is one rule's record.
//
// The three buckets are not three shades of the same thing. Resolved means somebody
// read the finding and marked it dealt with. Open means somebody read it, or did
// not, and left it. Outdated means the code moved underneath it — GitHub detached
// the comment — so nobody decided anything, which is why it is counted and then
// kept out of the ratio.
type counts struct {
	Resolved int
	Open     int
	Outdated int
}

func (c counts) posted() int { return c.Resolved + c.Open + c.Outdated }

// decided is the denominator: the findings somebody actually acted on or left. An
// outdated finding is evidence about a branch's velocity, not about a rule.
func (c counts) decided() int { return c.Resolved + c.Open }

// acceptance is the proportion of decided findings that were resolved, or -1 when
// nothing has been decided yet.
//
// -1 rather than 0, because a rule nobody has had the chance to act on is not a
// rule at 0%. Reporting it as 0% is how a new rule gets deleted for being new.
func (c counts) acceptance() float64 {
	if c.decided() == 0 {
		return -1
	}
	return float64(c.Resolved) / float64(c.decided())
}

// gather reads every pull request updated in the window and tallies our threads.
func gather(ctx context.Context, client *ghclient.REST, repo ghclient.Repo, since time.Time) (map[string]*counts, int, error) {
	prs, err := client.ListPullRequests(ctx, repo, "all")
	if err != nil {
		return nil, 0, fmt.Errorf("listing pull requests: %w", err)
	}

	tally := map[string]*counts{}
	scanned := 0
	for _, pr := range prs {
		if pr.UpdatedAt.Before(since) {
			continue
		}
		scanned++

		threads, err := client.ReviewThreads(ctx, repo, pr.Number)
		if err != nil {
			// One unreadable pull request is not a reason to report nothing about
			// the rest, and a partial measurement that says so is more useful than
			// an error.
			return tally, scanned, fmt.Errorf("reading the threads of #%d: %w", pr.Number, err)
		}
		for _, thread := range threads {
			rule, ours := ruleOf(thread)
			if !ours {
				continue
			}
			if tally[rule] == nil {
				tally[rule] = &counts{}
			}
			switch {
			case thread.IsResolved:
				tally[rule].Resolved++
			case thread.IsOutdated:
				tally[rule].Outdated++
			default:
				tally[rule].Open++
			}
		}
	}
	return tally, scanned, nil
}

// ruleOf identifies the rule a thread came from, and whether it is ours at all.
//
// Only the first comment is read. A thread this tool started is one where *our*
// comment opened it; a human quoting the marker in a reply does not make their
// conversation ours to measure.
func ruleOf(thread ghclient.ReviewThread) (string, bool) {
	if len(thread.Comments) == 0 {
		return "", false
	}
	marks := fingerprint.ParseMarks(thread.Comments[0].Body)
	if len(marks) == 0 {
		return "", false
	}
	if marks[0].RuleID == "" {
		// Ours, but posted before the marker carried a rule id. Counted under a
		// visible name rather than dropped, so the totals still add up and the
		// reason a row exists is legible.
		return "(rule not recorded)", true
	}
	return marks[0].RuleID, true
}

func writeMarkdown(w io.Writer, tally map[string]*counts, repo ghclient.Repo, since time.Time, scanned int) error {
	b := &strings.Builder{}
	fmt.Fprintf(b, "# Acceptance by rule\n\n")
	fmt.Fprintf(b, "%s, %s updated since %s.\n\n",
		repo.Owner+"/"+repo.Name,
		plural(scanned, "pull request", "pull requests"),
		since.Format(time.DateOnly))

	if len(tally) == 0 {
		fmt.Fprintf(b, "No findings from this tool were posted in the window.\n")
		_, err := io.WriteString(w, b.String())
		return err
	}

	fmt.Fprintf(b, "| rule | posted | resolved | open | outdated | acceptance |\n")
	fmt.Fprintf(b, "|---|--:|--:|--:|--:|--:|\n")
	for _, rule := range sortedRules(tally) {
		c := tally[rule]
		fmt.Fprintf(b, "| `%s` | %d | %d | %d | %d | %s |\n",
			rule, c.posted(), c.Resolved, c.Open, c.Outdated, percentOf(c))
	}

	fmt.Fprintf(b, "\nAcceptance is resolved ÷ (resolved + open). Outdated threads are excluded: "+
		"the code moved underneath the comment, so nobody decided anything. A rule with nothing "+
		"decided yet shows `n/a` rather than 0%%.\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeCSV(w io.Writer, tally map[string]*counts) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"rule", "posted", "resolved", "open", "outdated", "acceptance"}); err != nil {
		return err
	}
	for _, rule := range sortedRules(tally) {
		c := tally[rule]
		acceptance := ""
		if a := c.acceptance(); a >= 0 {
			// Unrounded in CSV: the markdown table is for reading and this is for
			// putting in a spreadsheet, where a pre-rounded number cannot be
			// re-aggregated.
			acceptance = strconv.FormatFloat(a, 'f', 4, 64)
		}
		if err := out.Write([]string{
			rule,
			strconv.Itoa(c.posted()), strconv.Itoa(c.Resolved),
			strconv.Itoa(c.Open), strconv.Itoa(c.Outdated), acceptance,
		}); err != nil {
			return err
		}
	}
	out.Flush()
	return out.Error()
}

// sortedRules orders by acceptance, worst first, then by how much attention the
// rule has cost. The rule most worth deleting is the first row.
func sortedRules(tally map[string]*counts) []string {
	rules := make([]string, 0, len(tally))
	for rule := range tally {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		a, b := tally[rules[i]], tally[rules[j]]
		// A rule with nothing decided sorts last: there is no evidence about it
		// either way, and putting it at the top would read as an indictment.
		ai, bi := a.acceptance(), b.acceptance()
		switch {
		case ai < 0 && bi >= 0:
			return false
		case bi < 0 && ai >= 0:
			return true
		case ai != bi:
			return ai < bi
		case a.posted() != b.posted():
			return a.posted() > b.posted()
		default:
			return rules[i] < rules[j]
		}
	})
	return rules
}

func percentOf(c *counts) string {
	a := c.acceptance()
	if a < 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.0f%% (%d/%d)", a*100, c.Resolved, c.decided())
}

// parseSince reads a window as a duration in days, weeks or hours.
//
// Days rather than Go's duration syntax, because nobody asks for the last 168
// hours. `7d`, `2w` and `36h` all work; a bare number is days.
func parseSince(s string) (time.Time, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		s = "7d"
	}
	unit := time.Hour * 24
	switch {
	case strings.HasSuffix(s, "d"):
		s = strings.TrimSuffix(s, "d")
	case strings.HasSuffix(s, "w"):
		s, unit = strings.TrimSuffix(s, "w"), time.Hour*24*7
	case strings.HasSuffix(s, "h"):
		s, unit = strings.TrimSuffix(s, "h"), time.Hour
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("--since takes a window like 7d, 2w or 36h, not %q", s)
	}
	return time.Now().Add(-time.Duration(n) * unit), nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
