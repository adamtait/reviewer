// SPDX-License-Identifier: MIT

package risk

import (
	"fmt"
	"sort"
	"strings"

	"github.com/adamtait/reviewer/internal/codeowners"
	"github.com/adamtait/reviewer/internal/glob"
)

// The thresholds, from the design's label table.
const (
	// WideFiles: more than this many changed files is wide.
	WideFiles = 40
	// WideOwnerGroups: this many CODEOWNERS groups or more is wide.
	WideOwnerGroups = 3
	// ReachFiles: a symbol referenced from this many other files or more is
	// reach.
	ReachFiles = 10
)

// reviewConfig is what decides how this record, and future ones, are
// computed: the review configuration, the workflows that run it, and who must
// approve. Fixed, not part of the risk map, so a repository cannot widen or
// narrow it.
var reviewConfig = []glob.Pattern{
	glob.MustCompile(".review/"),
	glob.MustCompile(".github/"),
	glob.MustCompile("**/CODEOWNERS"),
}

// File is one changed file.
type File struct {
	Path string
	// PreviousPath is where a renamed file was. Empty for any other change.
	PreviousPath string
}

// Input is everything the record is computed from. The command reads all of
// it from the pull request and its merge base; nothing here does I/O.
type Input struct {
	Files []File
	// Map is the risk map. The zero Map is the defaults alone.
	Map Map
	// Owners is the merge base's CODEOWNERS. Nil when there is none, or it
	// could not be read; OwnersUnavailable then says which.
	Owners            *codeowners.File
	OwnersUnavailable string
	// Reverted holds the paths that had a revert on the base branch in the
	// last 180 days. HistoryUnavailable says why it could not be asked, if it
	// could not.
	Reverted           map[string]bool
	HistoryUnavailable string
	// Reach is, for each changed path the index describes, the symbols the
	// pull request changes there. ReachUnavailable says why reach could not be
	// computed, for the whole pull request or some of its files.
	Reach            map[string][]SymbolReach
	ReachUnavailable string
}

// Record is the risk record: the labels that apply and why.
type Record struct {
	// Labels are in the order of All.
	Labels []Label
	// Reasons are, for each label in Labels, the paths, owner groups or
	// symbols behind it.
	Reasons map[Label][]string
	// Warnings name each signal that could not be computed. A label that is
	// absent because its signal was not available is not a label that does
	// not apply, and the comment must be able to say so.
	Warnings []string
}

// Has reports whether the record carries a label.
func (r Record) Has(label Label) bool {
	for _, l := range r.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// Compute decides the record. It is a pure function of its input.
func Compute(in Input) Record {
	rec := Record{Reasons: map[Label][]string{}}
	add := func(label Label, why string) {
		for _, seen := range rec.Reasons[label] {
			if seen == why {
				return
			}
		}
		rec.Reasons[label] = append(rec.Reasons[label], why)
	}

	// Both ends of a rename: moving a file out of a sensitive path changes
	// that path as much as moving one in.
	for _, f := range in.Files {
		for _, end := range ends(f) {
			for _, label := range PathLabels {
				if in.Map.Match(label, end.path) {
					add(label, end.why)
				}
			}
			if glob.MatchList(reviewConfig, end.path) {
				add(LabelReviewConfig, end.why)
			}
			if in.Reverted[end.path] {
				add(LabelHistory, end.why)
			}
		}
	}
	if in.HistoryUnavailable != "" {
		rec.Warnings = append(rec.Warnings, "file history not available: "+in.HistoryUnavailable)
	}

	if len(in.Files) > WideFiles {
		add(LabelWide, fmt.Sprintf("%d files changed, more than %d", len(in.Files), WideFiles))
	}
	if in.Owners == nil {
		why := in.OwnersUnavailable
		if why == "" {
			why = "no CODEOWNERS file"
		}
		rec.Warnings = append(rec.Warnings, "owner groups not available: "+why)
	} else if groups := ownerGroups(*in.Owners, in.Files); len(groups) >= WideOwnerGroups {
		add(LabelWide, fmt.Sprintf("%d CODEOWNERS groups: %s", len(groups), strings.Join(groups, "; ")))
	}

	var reached []string
	for path := range in.Reach {
		reached = append(reached, path)
	}
	sort.Strings(reached)
	for _, path := range reached {
		for _, s := range in.Reach[path] {
			if s.Files >= ReachFiles {
				add(LabelReach, fmt.Sprintf("%s in %s: referenced from %d other files", s.Symbol, path, s.Files))
			}
		}
	}
	if in.ReachUnavailable != "" {
		rec.Warnings = append(rec.Warnings, "reach not available: "+in.ReachUnavailable)
	}

	for _, label := range All {
		if len(rec.Reasons[label]) > 0 {
			rec.Labels = append(rec.Labels, label)
		}
	}
	return rec
}

type end struct{ path, why string }

// ends are the paths a change touches, each with how a reason names it.
func ends(f File) []end {
	out := []end{{f.Path, f.Path}}
	if f.PreviousPath != "" && f.PreviousPath != f.Path {
		out = append(out, end{f.PreviousPath, f.PreviousPath + " (renamed to " + f.Path + ")"})
	}
	return out
}

// ownerGroups returns the distinct sets of owners the changed paths fall
// under, sorted. A group is the owner list of the rule that decides a path,
// compared without regard to order or case, as GitHub compares logins. A path
// that no rule owns is in no group.
func ownerGroups(owners codeowners.File, files []File) []string {
	groups := map[string]bool{}
	for _, f := range files {
		for _, e := range ends(f) {
			list, _ := owners.Owners(e.path)
			if len(list) == 0 {
				continue
			}
			set := map[string]bool{}
			for _, o := range list {
				set[strings.ToLower(o)] = true
			}
			var names []string
			for o := range set {
				names = append(names, o)
			}
			sort.Strings(names)
			groups[strings.Join(names, " ")] = true
		}
	}
	var out []string
	for g := range groups {
		out = append(out, g)
	}
	sort.Strings(out)
	return out
}
