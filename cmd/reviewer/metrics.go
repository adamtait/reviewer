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

	// Validated before any network work. A typo should not cost a full scan of the
	// repository before it is reported.
	switch o.format {
	case "", "markdown", "csv":
	default:
		return errUsage{fmt.Errorf("--format is markdown or csv, not %q", o.format)}
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

	self := identify(ctx, client, cfg)
	tally, scanned, gatherErr := gather(ctx, client, repo, since, self)

	// A partial measurement is printed, not discarded. One pull request that could
	// not be read — a secondary rate limit is the likely cause, and forty
	// sequential GraphQL calls is how you meet one — is not a reason to report
	// nothing about the other thirty-nine. The gap is stated so nobody reads the
	// table as complete.
	if gatherErr != nil {
		fmt.Fprintf(stderr, "reviewer: the table below is incomplete: %v\n", gatherErr)
	}
	if scanned == 0 && gatherErr == nil {
		fmt.Fprintf(stderr, "reviewer: no pull requests updated since %s\n", since.Format(time.DateOnly))
	}

	if o.format == "csv" {
		return writeCSV(stdout, tally)
	}
	return writeMarkdown(stdout, tally, repo, since, scanned, gatherErr != nil)
}

// identify works out which account this tool posts as, so a human's thread is not
// counted as one of ours.
//
// Mirrors the reporter's own identification: GET /user answers for a personal
// token and returns 403 for the installation token an Action runs with, which is
// why github.selfLogin exists to be stated instead.
func identify(ctx context.Context, client *ghclient.REST, cfg config.Config) string {
	if user, err := client.Viewer(ctx); err == nil && user.Login != "" {
		return user.Login
	}
	return cfg.GitHub.SelfLogin
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
	// Summarised are findings that went into the collapsed summary comment rather
	// than inline, because their confidence was below high (ADR-0017).
	//
	// They are counted and shown and they contribute to no ratio, because a summary
	// carries no per-finding outcome: there is no thread to resolve, so nothing
	// records whether anybody agreed. Leaving them out entirely was worse — the four
	// model categories capped at medium would have had no row at all, and a rule
	// that fired two hundred times would have looked like one that never fires.
	Summarised int
}

func (c counts) posted() int { return c.Resolved + c.Open + c.Outdated + c.Summarised }

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
func gather(ctx context.Context, client *ghclient.REST, repo ghclient.Repo, since time.Time, self string) (map[string]*counts, int, error) {
	tally := map[string]*counts{}

	prs, err := client.ListPullRequestsUpdatedSince(ctx, repo, since)
	if err != nil {
		return tally, 0, fmt.Errorf("listing pull requests: %w", err)
	}

	scanned := 0
	for _, pr := range prs {
		scanned++

		threads, err := client.ReviewThreads(ctx, repo, pr.Number)
		if err != nil {
			return tally, scanned, fmt.Errorf("reading the threads of #%d: %w", pr.Number, err)
		}
		for _, thread := range threads {
			rule, ours := ruleOf(thread, self)
			if !ours {
				continue
			}
			bucket(tally, rule).count(thread)
		}

		// The summary comment, where everything below high confidence lands
		// (ADR-0017). Without this the table has no row at all for the four model
		// categories capped at medium — so a rule that fired two hundred times
		// would look like one that never fires.
		comments, err := client.ListIssueComments(ctx, repo, pr.Number)
		if err != nil {
			return tally, scanned, fmt.Errorf("reading the comments of #%d: %w", pr.Number, err)
		}
		for _, c := range comments {
			if self != "" && c.User.Login != self {
				continue
			}
			for _, mark := range fingerprint.ParseMarks(c.Body) {
				bucket(tally, ruleName(mark.RuleID)).Summarised++
			}
		}
	}
	return tally, scanned, nil
}

func bucket(tally map[string]*counts, rule string) *counts {
	if tally[rule] == nil {
		tally[rule] = &counts{}
	}
	return tally[rule]
}

func (c *counts) count(thread ghclient.ReviewThread) {
	switch {
	case thread.IsResolved:
		c.Resolved++
	case thread.IsOutdated:
		c.Outdated++
	default:
		c.Open++
	}
}

// ruleOf identifies the rule a thread came from, and whether it is ours at all.
//
// Only the first comment is read. A thread this tool started is one where *our*
// comment opened it; a human quoting the marker in a reply does not make their
// conversation ours to measure.
func ruleOf(thread ghclient.ReviewThread, self string) (string, bool) {
	if len(thread.Comments) == 0 {
		return "", false
	}
	first := thread.Comments[0]
	// Authorship, not just the marker. A human who quotes one of our comments in
	// their own thread has written a conversation about this tool, not one of its
	// findings — and counting it charges a rule for an outcome nobody attributed to
	// it. The reporter refuses to resolve such a thread for the same reason.
	if self != "" && first.User.Login != self {
		return "", false
	}
	marks := fingerprint.ParseMarks(first.Body)
	if len(marks) == 0 {
		return "", false
	}
	return ruleName(marks[0].RuleID), true
}

// ruleName gives a visible home to a finding posted before the marker carried a
// rule id, so the totals still add up and the reason the row exists is legible. A
// parsed rule id can never contain a space, so this cannot collide with a real one.
func ruleName(id string) string {
	if id == "" {
		return "(rule not recorded)"
	}
	return id
}

func writeMarkdown(w io.Writer, tally map[string]*counts, repo ghclient.Repo, since time.Time, scanned int, partial bool) error {
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

	if partial {
		fmt.Fprintf(b, "**This table is incomplete**: at least one pull request could not be read.\n\n")
	}

	fmt.Fprintf(b, "| rule | posted | resolved | open | outdated | summarised | acceptance |\n")
	fmt.Fprintf(b, "|---|--:|--:|--:|--:|--:|--:|\n")
	for _, rule := range sortedRules(tally) {
		c := tally[rule]
		fmt.Fprintf(b, "| `%s` | %d | %d | %d | %d | %d | %s |\n",
			rule, c.posted(), c.Resolved, c.Open, c.Outdated, c.Summarised, percentOf(c))
	}

	fmt.Fprintf(b, "\nAcceptance is resolved ÷ (resolved + open). Outdated threads are excluded: "+
		"the code moved underneath the comment, so nobody decided anything. Summarised findings "+
		"are excluded too — they went into the collapsed summary comment rather than a thread, so "+
		"there is nothing to resolve and no record of whether anybody agreed. A rule with nothing "+
		"decided shows `n/a` rather than 0%%.\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func writeCSV(w io.Writer, tally map[string]*counts) error {
	out := csv.NewWriter(w)
	if err := out.Write([]string{"rule", "posted", "resolved", "open", "outdated", "summarised", "acceptance"}); err != nil {
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
			strconv.Itoa(c.Open), strconv.Itoa(c.Outdated), strconv.Itoa(c.Summarised), acceptance,
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
	original := s
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
	// Bounded as well as positive. `time.Duration(n) * unit` overflows int64 for a
	// large n, which lands the cutoff in the *future* — so every pull request is
	// filtered out and the command prints a plausible-looking empty table instead
	// of saying the window was nonsense. Ten years is longer than any repository
	// this measurement is useful for.
	const maxHours = 24 * 365 * 10
	if err != nil || n <= 0 || float64(n)*unit.Hours() > maxHours {
		return time.Time{}, fmt.Errorf("--since takes a window like 7d, 2w or 36h, not %q", original)
	}
	return time.Now().Add(-time.Duration(n) * unit), nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
