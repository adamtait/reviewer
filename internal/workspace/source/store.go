// SPDX-License-Identifier: MIT

package source

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// errLoadPanicked is what a load that panicked is reported as.
var errLoadPanicked = errors.New("reading the fetched content failed")

func isPermanent(err error) bool {
	var p Permanent
	return errors.As(err, &p)
}

// whatFiles is the Key.What of a commit's files.
const whatFiles = "files"

// Fetcher is the one GitHub call the store makes, as the user who asked.
type Fetcher interface {
	// Archive returns a commit's files as a gzip-compressed tar stream.
	Archive(ctx context.Context, repo ghclient.Repo, sha string) (io.ReadCloser, error)
}

// Store answers "what are this commit's files" from memory, fetching the
// commit's archive from GitHub the first time it is asked.
type Store struct {
	cache  *Cache
	limits Limits
	// Observe, if set, is told of every fetch from GitHub once it ends. It is
	// how the operator learns what a first open costs: nothing else records it.
	Observe func(Fetch)
}

// Fetch describes one fetch of an archive from GitHub.
type Fetch struct {
	Owner, Repo, SHA string
	Took             time.Duration
	// Bytes and Entries are what was kept. Both are zero when Err is set.
	Bytes   int64
	Entries int
	Err     error
}

// NewStore returns a Store that keeps what it fetches in cache.
func NewStore(cache *Cache, limits Limits) *Store {
	return &Store{cache: cache, limits: limits}
}

// Commit returns a commit's files.
//
// The caller must already have checked that the user behind gh may read the
// repository. The fetch is made with that user's token, so GitHub checks again;
// a request answered from memory makes no call, and the first check is then the
// only one.
//
// The error is ErrTooLarge for a commit over the limit, context.DeadlineExceeded
// when GitHub did not send the archive in time, and otherwise GitHub's own.
func (s *Store) Commit(ctx context.Context, gh Fetcher, owner, repo, sha string) (*Commit, error) {
	value, err := s.cache.Get(ctx, NewKey(owner, repo, sha, whatFiles), func(ctx context.Context) (Sized, error) {
		started := time.Now()
		commit, err := s.fetch(ctx, gh, owner, repo, sha)
		if s.Observe != nil {
			seen := Fetch{Owner: owner, Repo: repo, SHA: sha, Took: time.Since(started), Err: err}
			if err == nil {
				seen.Bytes, seen.Entries = commit.Size(), len(commit.Entries())
			}
			s.Observe(seen)
		}
		if err != nil {
			return nil, err
		}
		return commit, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*Commit), nil
}

func (s *Store) fetch(ctx context.Context, gh Fetcher, owner, repo, sha string) (*Commit, error) {
	body, err := gh.Archive(ctx, ghclient.Repo{Owner: owner, Name: repo}, sha)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	commit, err := Unpack(body, s.limits)
	switch {
	case errors.Is(err, ErrTooLarge):
		return nil, Permanent{Err: err}
	case err != nil && ctx.Err() != nil:
		// The deadline cut the stream short. Said as that, and not as the read
		// error it surfaced as, so the caller can tell the user to try again
		// rather than that the archive was bad.
		return nil, fmt.Errorf("fetching the archive: %w", ctx.Err())
	case err != nil:
		return nil, err
	}
	return commit, nil
}
