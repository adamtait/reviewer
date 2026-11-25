// SPDX-License-Identifier: MIT

package navigation

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/navigation/scip"
	"github.com/adamtait/reviewer/internal/workspace/source"
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

// whatIndex is the cache's name for a commit's index.
const whatIndex = "index"

// How long a state other than "ready" is believed before GitHub is asked
// again. Short for a state that changes by itself, so a running job is noticed
// soon after it ends; longer for an artifact found unusable, which costs a
// download to find out again.
const (
	rememberState    = 10 * time.Second
	rememberUnusable = 60 * time.Second
)

// Status is what is known about a commit's index.
type Status struct {
	State string
	// Reason says why there is no index although a run succeeded. Empty otherwise.
	Reason string
	// RunID is the workflow run the state rests on: the one that built the
	// index, is building it, or failed. 0 when there is none.
	RunID int64
	// ToolVersion and Documents describe a loaded index.
	ToolVersion string
	Documents   int
}

// GitHub is the calls navigation makes, as the signed-in user.
type GitHub interface {
	source.Fetcher
	ListWorkflowRuns(ctx context.Context, repo ghclient.Repo, workflowFile, headSHA, event string) ([]ghclient.WorkflowRun, error)
	ListRunArtifacts(ctx context.Context, repo ghclient.Repo, runID int64, name string) ([]ghclient.Artifact, error)
	DownloadArtifact(ctx context.Context, repo ghclient.Repo, artifactID int64) (io.ReadCloser, error)
}

// Config is what the service needs to know to find an index.
type Config struct {
	// WorkflowFile is the index workflow's file name, as `reviewer init`
	// writes it under .github/workflows/.
	WorkflowFile string
	// ArtifactName is the name the workflow saves the index under.
	ArtifactName string
	// IndexBytes bounds the artifact, the file inside it, and the index built
	// from that.
	IndexBytes int64
}

// Service finds, fetches and holds indexes.
type Service struct {
	cfg   Config
	cache *source.Cache
	store *source.Store
	now   func() time.Time

	mu   sync.Mutex
	memo map[source.Key]remembered
}

type remembered struct {
	status Status
	until  time.Time
}

// loaded is an index as the cache holds it: with the run it came from.
type loaded struct {
	*Index
	runID int64
}

// NewService returns a Service that keeps indexes in cache, beside the commits
// store holds there, so one limit bounds both.
func NewService(cfg Config, cache *source.Cache, store *source.Store) *Service {
	return &Service{cfg: cfg, cache: cache, store: store, now: time.Now, memo: map[source.Key]remembered{}}
}

// SetClock replaces the clock, for tests.
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// unusable is why a successful run's artifact gave no index.
type unusable struct{ reason string }

func (u *unusable) Error() string { return u.reason }

// Lookup returns the index for a commit, if it has one, and its status.
//
// The index is nil unless the status is StateReady. The caller must already
// have checked that the user behind gh may read the repository.
func (s *Service) Lookup(ctx context.Context, gh GitHub, owner, repo, sha string) (*Index, Status, error) {
	key := source.NewKey(owner, repo, sha, whatIndex)
	if held, ok := s.cache.Peek(key); ok {
		return held.(*loaded).Index, statusOf(held.(*loaded)), nil
	}
	if status, ok := s.recall(key); ok {
		return nil, status, nil
	}

	at := ghclient.Repo{Owner: owner, Name: repo}
	runs, err := gh.ListWorkflowRuns(ctx, at, s.cfg.WorkflowFile, sha, "push")
	if err != nil {
		return nil, Status{}, fmt.Errorf("listing the index workflow's runs: %w", err)
	}
	state, run := classify(runs, sha, ".github/workflows/"+s.cfg.WorkflowFile)
	if state != StateReady {
		status := Status{State: state}
		if run != nil {
			status.RunID = run.ID
		}
		s.remember(key, status, rememberState)
		return nil, status, nil
	}

	// The commit's files first, and outside the load below: the index is
	// checked against them, and fetching them inside the load would have one
	// load waiting on another for a place in the cache's queue.
	commit, err := s.store.Commit(ctx, gh, owner, repo, sha)
	if err != nil {
		return nil, Status{}, err
	}

	held, err := s.cache.Get(ctx, key, func(ctx context.Context) (source.Sized, error) {
		index, err := s.fetch(ctx, gh, at, run.ID, commit)
		if err != nil {
			return nil, err
		}
		return &loaded{Index: index, runID: run.ID}, nil
	})
	var why *unusable
	switch {
	case errors.As(err, &why):
		status := Status{State: StateNone, Reason: why.reason, RunID: run.ID}
		s.remember(key, status, rememberUnusable)
		return nil, status, nil
	case err != nil:
		return nil, Status{}, err
	}
	return held.(*loaded).Index, statusOf(held.(*loaded)), nil
}

func statusOf(l *loaded) Status {
	return Status{State: StateReady, RunID: l.runID, ToolVersion: l.ToolVersion, Documents: l.Documents()}
}

func (s *Service) recall(key source.Key) (Status, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.memo[key]
	if !ok || !s.now().Before(r.until) {
		return Status{}, false
	}
	return r.status, true
}

func (s *Service) remember(key source.Key, status Status, forHowLong time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for k, r := range s.memo {
		if !now.Before(r.until) {
			delete(s.memo, k)
		}
	}
	s.memo[key] = remembered{status: status, until: now.Add(forHowLong)}
}

// classify applies the four-state rule to a workflow's runs for a commit, and
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
func classify(runs []ghclient.WorkflowRun, sha, workflowPath string) (string, *ghclient.WorkflowRun) {
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

// fetch downloads a run's index artifact, decodes it and checks it against the
// commit. An artifact that cannot be used gives an *unusable error with the
// reason; any other error means GitHub could not be asked.
func (s *Service) fetch(ctx context.Context, gh GitHub, repo ghclient.Repo, runID int64, commit *source.Commit) (*Index, error) {
	artifacts, err := gh.ListRunArtifacts(ctx, repo, runID, s.cfg.ArtifactName)
	if err != nil {
		return nil, fmt.Errorf("listing the run's artifacts: %w", err)
	}
	var artifact *ghclient.Artifact
	for i := range artifacts {
		// The name is checked again here: the list was asked for by name, and
		// what came back is what counts.
		if artifacts[i].Name == s.cfg.ArtifactName && (artifact == nil || artifacts[i].ID > artifact.ID) {
			artifact = &artifacts[i]
		}
	}
	switch {
	case artifact == nil:
		return nil, &unusable{"the index job succeeded and saved no index"}
	case artifact.Expired:
		return nil, &unusable{"the index has expired on GitHub; push again or re-run the index job to rebuild it"}
	case artifact.SizeInBytes > s.cfg.IndexBytes:
		return nil, &unusable{"the index is larger than the workspace is set to read"}
	}

	body, err := gh.DownloadArtifact(ctx, repo, artifact.ID)
	if err != nil {
		return nil, fmt.Errorf("downloading the index: %w", err)
	}
	defer func() { _ = body.Close() }()
	// One byte past the limit, so that "exactly the limit" and "more than the
	// limit" can be told apart without trusting the size GitHub reported.
	archive, err := io.ReadAll(io.LimitReader(body, s.cfg.IndexBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("downloading the index: %w", ctx.Err())
		}
		return nil, fmt.Errorf("downloading the index: %w", err)
	}
	if int64(len(archive)) > s.cfg.IndexBytes {
		return nil, &unusable{"the index is larger than the workspace is set to read"}
	}

	raw, problem := unzipIndex(archive, s.cfg.IndexBytes)
	if problem != "" {
		return nil, &unusable{problem}
	}
	decoded, err := scip.Decode(raw, scip.Limits{Occurrences: int(s.cfg.IndexBytes / occurrenceCost)})
	switch {
	case errors.Is(err, scip.ErrTooLarge):
		return nil, &unusable{"the index is larger than the workspace is set to read"}
	case err != nil:
		return nil, &unusable{"the index could not be read; it is not in the format the workspace expects"}
	}
	index := Build(decoded, commit)
	if index.Size() > s.cfg.IndexBytes {
		return nil, &unusable{"the index is larger than the workspace is set to read"}
	}
	if index.Documents() == 0 {
		return nil, &unusable{"the index describes no file of this commit"}
	}
	return index, nil
}

// unzipIndex takes the index out of an artifact's zip archive. The archive
// must hold exactly one file, named index.scip, no larger than limit. It
// returns the file, or the reason the archive cannot be used.
func unzipIndex(archive []byte, limit int64) (raw []byte, problem string) {
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
