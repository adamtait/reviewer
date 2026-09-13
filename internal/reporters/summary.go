// SPDX-License-Identifier: MIT

package reporters

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/adamtait/reviewer/internal/fingerprint"
	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/pkg/finding"
)

// SummaryMarker identifies the one sticky comment this tool maintains, so a
// second run updates it rather than adding another.
const SummaryMarker = "<!-- rv:summary -->"

// summary renders the findings that did not earn an inline comment.
//
// Everything about the shape is aimed at being *read*. It is collapsed, so it
// costs nothing to scroll past; it leads with a count, so the decision to expand
// is informed; it groups by rule, because five instances of one rule is one
// thought and not five; and it is one comment rather than a run of them, because a
// bot that posts a comment per push is a bot people mute.
func summary(findings []finding.Finding, gateReason string) string {
	if len(findings) == 0 && gateReason == "" {
		return ""
	}

	var b strings.Builder
	b.WriteString(SummaryMarker)
	b.WriteString("\n")

	// The gate's reason goes above the fold: a developer who has just committed a
	// credential needs to know the diff was not sent anywhere, and must not have
	// to expand anything to find out.
	if gateReason != "" {
		b.WriteString("**")
		b.WriteString(gateReason)
		b.WriteString("**\n\n")
	}

	if len(findings) == 0 {
		return b.String()
	}

	byRule := map[string][]finding.Finding{}
	for _, f := range findings {
		byRule[f.RuleID] = append(byRule[f.RuleID], f)
	}
	rules := make([]string, 0, len(byRule))
	for rule := range byRule {
		rules = append(rules, rule)
	}
	sort.Strings(rules)

	fmt.Fprintf(&b, "<details>\n<summary>%d further observation%s across %d rule%s — not posted inline because they are judgements rather than facts</summary>\n\n",
		len(findings), plural(len(findings)), len(rules), plural(len(rules)))

	for _, rule := range rules {
		group := byRule[rule]
		fmt.Fprintf(&b, "**%s** · %s confidence\n\n", rule, group[0].Confidence)
		for _, f := range group {
			start, end := f.Span()
			where := fmt.Sprintf("%s:%d", f.File, start)
			if end != start {
				where = fmt.Sprintf("%s:%d-%d", f.File, start, end)
			}
			fmt.Fprintf(&b, "- `%s` — %s %s\n", where, oneLine(f.Message), fingerprint.Marker(f.Fingerprint))
			if f.Evidence != "" {
				// Evidence is why a model-lane finding is worth showing at all, so
				// it is never hidden a second level down.
				fmt.Fprintf(&b, "  - %s\n", oneLine(f.Evidence))
			}
		}
		b.WriteString("\n")
	}

	b.WriteString("</details>\n")
	return b.String()
}

// oneLine flattens a message so a multi-line analyzer message cannot break the
// list markup.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// upsertSummary maintains exactly one summary comment per pull request.
//
// An empty summary deletes nothing: it rewrites the existing comment to say there
// is nothing left, because a comment that vanishes leaves a reader wondering
// whether the tool ran.
func (g GitHub) upsertSummary(ctx context.Context, body string) (action string, err error) {
	existing, err := g.Client.ListIssueComments(ctx, g.Repo, g.Number)
	if err != nil {
		return "", err
	}
	var found *ghclient.IssueComment
	for i := range existing {
		if strings.Contains(existing[i].Body, SummaryMarker) {
			found = &existing[i]
			break
		}
	}

	switch {
	case body == "" && found == nil:
		return "none needed", nil
	case body == "" && found != nil:
		if _, err := g.Writer.UpdateIssueComment(ctx, g.Repo, found.ID,
			SummaryMarker+"\nNo further observations on the current revision.\n"); err != nil {
			return "", err
		}
		return "cleared", nil
	case found != nil:
		if found.Body == body {
			// Nothing changed. Rewriting it would bump the comment's timestamp and
			// re-notify every subscriber for no reason.
			return "unchanged", nil
		}
		if _, err := g.Writer.UpdateIssueComment(ctx, g.Repo, found.ID, body); err != nil {
			return "", err
		}
		return "updated", nil
	default:
		if _, err := g.Writer.CreateIssueComment(ctx, g.Repo, g.Number, body); err != nil {
			return "", err
		}
		return "created", nil
	}
}
