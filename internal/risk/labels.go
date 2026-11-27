// SPDX-License-Identifier: MIT

// Package risk computes the advisory risk record for a pull request: which of
// a fixed set of GitHub labels apply, and the paths or symbols behind each.
//
// Everything here is pure. Reading the pull request's files, the merge base's
// CODEOWNERS and risk.yaml, the base branch's reverts and the CI-built index
// is the command's job; this package decides what they add up to.
package risk

// Label is one of the GitHub labels the record can set.
type Label string

// The labels. The set is closed: a repository can add patterns to the five
// path labels and cannot add a label, so a label on a pull request always
// means what this table says.
const (
	LabelAuth         Label = "risk/auth"
	LabelData         Label = "risk/data"
	LabelCI           Label = "risk/ci"
	LabelDeps         Label = "risk/deps"
	LabelInfra        Label = "risk/infra"
	LabelWide         Label = "risk/wide"
	LabelReach        Label = "risk/reach"
	LabelHistory      Label = "risk/history"
	LabelReviewConfig Label = "risk/review-config"
)

// All is every label, in the order a record lists them.
var All = []Label{LabelAuth, LabelData, LabelCI, LabelDeps, LabelInfra, LabelWide, LabelReach, LabelHistory, LabelReviewConfig}

// PathLabels are the labels set by matching a changed path against the risk
// map, and the only ones `.review/risk.yaml` may add patterns to.
var PathLabels = []Label{LabelAuth, LabelData, LabelCI, LabelDeps, LabelInfra}

// isPathLabel reports whether a name is one of PathLabels.
func isPathLabel(name string) bool {
	for _, l := range PathLabels {
		if string(l) == name {
			return true
		}
	}
	return false
}
