// SPDX-License-Identifier: MIT

// Package access answers one question: can this user read this repository?
//
// The answer is GitHub's, asked with the user's own token. The workspace keeps
// no access table of its own, because a second table is a second thing to get
// wrong: a row left behind after someone leaves a team would go on granting
// what GitHub has taken away.
package access

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/wsconfig"
)

// ErrTokenRejected means GitHub no longer accepts the user's token: it expired
// or was revoked. The user has to sign in again, which is a different answer
// from "you cannot read this repository".
var ErrTokenRejected = errors.New("GitHub no longer accepts this token")

// RepoGetter is the one GitHub call the check makes, as the user.
type RepoGetter interface {
	GetRepo(ctx context.Context, repo ghclient.Repo) (ghclient.RepoInfo, error)
}

// Checker holds the connected repositories and what is currently believed about
// who can read them.
type Checker struct {
	ttl time.Duration
	now func() time.Time
	// onPublic is called once for each connected repository found to be public.
	onPublic func(owner, name string)

	mu        sync.Mutex
	connected map[string]wsconfig.Repository // by Slug
	order     []string                       // Slugs, in configuration order
	allowed   map[string]time.Time           // login + NUL + slug -> when the answer expires
	public    map[string]bool                // Slugs refused for everyone
}

// New returns a Checker for the configured repositories. A positive answer is
// believed for ttl. onPublic may be nil.
func New(repos []wsconfig.Repository, ttl time.Duration, onPublic func(owner, name string)) *Checker {
	c := &Checker{
		ttl:       ttl,
		now:       time.Now,
		onPublic:  onPublic,
		connected: map[string]wsconfig.Repository{},
		allowed:   map[string]time.Time{},
		public:    map[string]bool{},
	}
	for _, r := range repos {
		if _, dup := c.connected[r.Slug()]; !dup {
			c.order = append(c.order, r.Slug())
		}
		c.connected[r.Slug()] = r
	}
	return c
}

// SetClock replaces the clock, for tests.
func (c *Checker) SetClock(now func() time.Time) { c.now = now }

// Connected returns the configured repositories, in configuration order.
func (c *Checker) Connected() []wsconfig.Repository {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]wsconfig.Repository, 0, len(c.order))
	for _, slug := range c.order {
		out = append(out, c.connected[slug])
	}
	return out
}

// Lookup returns the connected repository with this owner and name, matched
// the way GitHub matches them: without regard to case.
func (c *Checker) Lookup(owner, name string) (wsconfig.Repository, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r, ok := c.connected[slug(owner, name)]
	return r, ok
}

func slug(owner, name string) string {
	return strings.ToLower(owner) + "/" + strings.ToLower(name)
}

// CanRead reports whether the user may see the repository's data.
//
// False with a nil error covers three cases that a caller must not be able to
// tell apart, and so neither can a user: the repository is not connected,
// GitHub says this user cannot see it, or it is public. The error is non-nil
// only when no answer was reached: the token was rejected (ErrTokenRejected),
// or GitHub could not be asked.
func (c *Checker) CanRead(ctx context.Context, login string, gh RepoGetter, owner, name string) (bool, error) {
	s := slug(owner, name)
	key := login + "\x00" + s

	c.mu.Lock()
	repo, connected := c.connected[s]
	refused := c.public[s]
	expires, cached := c.allowed[key]
	now := c.now()
	c.mu.Unlock()

	if !connected || refused {
		return false, nil
	}
	if cached && now.Before(expires) {
		return true, nil
	}

	// Asked under the configured spelling, so the request GitHub sees is the one
	// the operator wrote down.
	info, err := gh.GetRepo(ctx, ghclient.Repo{Owner: repo.Owner, Name: repo.Name})
	switch {
	case errors.Is(err, ghclient.ErrUnauthorized):
		return false, ErrTokenRejected
	case errors.Is(err, ghclient.ErrNotFound), errors.Is(err, ghclient.ErrForbidden):
		// Not remembered. Access granted on GitHub should take effect on the
		// next request, and a "no" costs one call to repeat.
		c.forget(key)
		return false, nil
	case err != nil:
		return false, err
	}

	if !info.Private {
		// Every GitHub token can read a public repository, so serving one would
		// let any GitHub account in. Refused for everyone, until a restart: this
		// is a mistake in the configuration, not a fact about this user.
		c.mu.Lock()
		first := !c.public[s]
		c.public[s] = true
		c.mu.Unlock()
		if first && c.onPublic != nil {
			c.onPublic(repo.Owner, repo.Name)
		}
		return false, nil
	}

	c.mu.Lock()
	c.allowed[key] = c.now().Add(c.ttl)
	c.sweep()
	c.mu.Unlock()
	return true, nil
}

func (c *Checker) forget(key string) {
	c.mu.Lock()
	delete(c.allowed, key)
	c.mu.Unlock()
}

// sweep drops expired answers, so the map is bounded by the users and
// repositories seen within one ttl. The caller holds the lock.
func (c *Checker) sweep() {
	now := c.now()
	for key, expires := range c.allowed {
		if !now.Before(expires) {
			delete(c.allowed, key)
		}
	}
}
