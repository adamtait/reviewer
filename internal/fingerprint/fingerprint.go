// SPDX-License-Identifier: MIT

// Package fingerprint gives a finding a stable identity across force-pushes.
//
// The problem it solves is social rather than technical. A reviewer's branch gets
// rebased, amended and force-pushed several times before it merges, and every one
// of those changes every line number in it. Identity based on position means the
// tool re-posts the same comment on every push, and a bot that repeats itself is
// a bot people mute (ADR-0015).
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/adamtait/reviewer/pkg/finding"
)

// Length is how many hex characters of the digest are used in a comment marker.
// Six gives 16.7M values, which is ample for the few hundred comments a
// repository will ever carry, and keeps the marker short enough to read.
const Length = 6

// Marker is the HTML comment embedded in a posted comment so a later run can
// recognise its own work. Invisible in rendered markdown.
func Marker(fp string) string {
	return "<!-- rv:" + fp + " -->"
}

var markerRe = regexp.MustCompile(`<!--\s*rv:([0-9a-f]{4,64})\s*-->`)

// ParseMarker extracts the fingerprint from a comment body, or "" when there is
// none — which is how a human's comment is told apart from this tool's.
func ParseMarker(body string) string {
	m := markerRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return m[1]
}

// ParseAllMarkers extracts every fingerprint in a body. The collapsed summary
// comment lists many findings at once, so a finding later promoted from the
// summary to an inline comment must still be recognised as already said.
func ParseAllMarkers(body string) []string {
	var out []string
	for _, m := range markerRe.FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// Compute derives a finding's identity from its rule, its file, and the
// normalised source it points at.
//
// Deliberately excluded: the line number, so a shift does not change identity;
// and the message, so rewording a rule's text does not orphan every comment it
// ever posted.
func Compute(root string, f finding.Finding) string {
	snippet := normalise(readSpan(root, f))
	h := sha256.New()
	// Length-prefixed so that a rule id ending in a path separator cannot collide
	// with a different rule and file that concatenate to the same bytes.
	writeField(h, f.RuleID)
	writeField(h, filepath.ToSlash(f.File))
	writeField(h, snippet)
	return hex.EncodeToString(h.Sum(nil))[:Length]
}

// ComputeAll fills in the fingerprint of every finding.
func ComputeAll(root string, findings []finding.Finding) {
	for i := range findings {
		findings[i].Fingerprint = Compute(root, findings[i])
	}
}

func writeField(h interface{ Write([]byte) (int, error) }, s string) {
	_, _ = h.Write([]byte(s))
	_, _ = h.Write([]byte{0})
}

// readSpan returns the source lines a finding covers.
//
// An unreadable file yields an empty snippet rather than an error: the
// fingerprint is then based on the rule and path alone, which is weaker but still
// stable, and a finding that cannot be deduped is better than a finding that
// cannot be posted.
func readSpan(root string, f finding.Finding) string {
	body, err := os.ReadFile(filepath.Join(root, f.File))
	if err != nil {
		return ""
	}
	lines := strings.Split(string(body), "\n")
	start, end := f.Span()
	if start < 1 || start > len(lines) {
		return ""
	}
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[start-1:end], "\n")
}

var (
	// All horizontal whitespace. Collapsing runs to a single space is not enough:
	// a formatter adds space where there was none ("call(x,y)" becomes
	// "call(x, y)"), so any normalisation that preserves one space still changes
	// the digest.
	horizontalSpace = regexp.MustCompile(`[ \t]+`)
	// The trailing comma a formatter adds or removes before a closing bracket.
	trailingComma = regexp.MustCompile(`,([)\]}])`)
)

// normalise removes the differences a formatter introduces, so that reformatting a
// block does not orphan the comments attached to it.
//
// Horizontal whitespace is removed entirely rather than collapsed. The result is
// unreadable, which does not matter — it is only ever hashed — and it is the only
// way "call(x,y)" and "call(x, y)" reach the same digest.
//
// It deliberately does *not* normalise quotes, identifiers or operators: those are
// changes to the code, and a comment about the old code should be replaced rather
// than carried forward. Line structure is preserved, so a two-line span and a
// one-line span are different findings.
func normalise(snippet string) string {
	if snippet == "" {
		return ""
	}
	var out []string
	for _, line := range strings.Split(snippet, "\n") {
		line = horizontalSpace.ReplaceAllString(line, "")
		line = trailingComma.ReplaceAllString(line, "$1")
		if line != "" {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
