// SPDX-License-Identifier: MIT

// Package scipfetch holds the two decisions about a CI-built SCIP index that
// do not depend on who asks for it: which workflow run an index for a commit
// rests on, and how the index is taken out of the artifact that run saved.
//
// Both the review workspace and `reviewer risk` read the same index, uploaded
// by the same workflow. They live here, not in the workspace's navigation
// package, so that the CLI does not depend on a workspace package and the two
// cannot drift apart on what counts as an index for a commit.
package scipfetch

import (
	"archive/zip"
	"bytes"
	"io"
	"sort"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// The four states an index for a commit can be in.
const (
	// StateNone: there is no index for this commit. Lookups use text search.
	StateNone = "none"
	// StateRunning: CI is still building it.
	StateRunning = "running"
	// StateFailed: the index job ran and did not succeed.
	StateFailed = "failed"
	// StateReady: the index is loaded.
	StateReady = "ready"
)

// IndexFile is the one file an index artifact must hold.
const IndexFile = "index.scip"

// Classify applies the four-state rule to a workflow's runs for a commit, and
// returns the run the state rests on.
//
// A run counts only if GitHub reports it as a push run of the index workflow
// file for this very commit. That binding is all that ties an index to the
// commit on screen, so it is checked here on the run's own fields and not left
// to the filters the list was asked with.
//
// The first rule that applies decides:
//
//  1. at least one run that succeeded: ready, from the newest such run;
//  2. otherwise at least one run that has not finished: running;
//  3. otherwise at least one finished run: failed, with the newest run;
//  4. no run: none.
func Classify(runs []ghclient.WorkflowRun, sha, workflowPath string) (string, *ghclient.WorkflowRun) {
	var bound []ghclient.WorkflowRun
	for _, run := range runs {
		if run.HeadSHA == sha && run.Event == "push" && run.Path == workflowPath {
			bound = append(bound, run)
		}
	}
	// Newest first. The identifier breaks a tie between runs created in the
	// same second: GitHub gives later runs larger ones.
	sort.SliceStable(bound, func(i, j int) bool {
		if !bound[i].CreatedAt.Equal(bound[j].CreatedAt) {
			return bound[i].CreatedAt.After(bound[j].CreatedAt)
		}
		return bound[i].ID > bound[j].ID
	})

	for i := range bound {
		if bound[i].Status == "completed" && bound[i].Conclusion == "success" {
			return StateReady, &bound[i]
		}
	}
	for i := range bound {
		if bound[i].Status != "completed" {
			return StateRunning, &bound[i]
		}
	}
	if len(bound) > 0 {
		return StateFailed, &bound[0]
	}
	return StateNone, nil
}

// Unzip takes the index out of an artifact's zip archive. The archive must
// hold exactly one file, named index.scip, no larger than limit. It returns
// the file, or the reason the archive cannot be used.
func Unzip(archive []byte, limit int64) (raw []byte, problem string) {
	const wrongShape = "the index artifact does not hold exactly one file named " + IndexFile
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, "the index artifact is not a zip archive"
	}
	var files []*zip.File
	for _, f := range reader.File {
		if !f.FileInfo().IsDir() {
			files = append(files, f)
		}
	}
	if len(files) != 1 || files[0].Name != IndexFile {
		return nil, wrongShape
	}
	if files[0].UncompressedSize64 > uint64(limit) {
		return nil, "the index is larger than the workspace is set to read"
	}
	opened, err := files[0].Open()
	if err != nil {
		return nil, "the index artifact could not be read"
	}
	defer func() { _ = opened.Close() }()
	// The size in a zip's directory is a claim. The limit holds whatever it says.
	raw, err = io.ReadAll(io.LimitReader(opened, limit+1))
	if err != nil {
		return nil, "the index artifact could not be read"
	}
	if int64(len(raw)) > limit {
		return nil, "the index is larger than the workspace is set to read"
	}
	return raw, ""
}
