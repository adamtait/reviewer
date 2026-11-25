// SPDX-License-Identifier: MIT

package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
	"github.com/adamtait/reviewer/internal/workspace/access"
)

// tokenPrefix is how every fine-grained personal access token begins.
const tokenPrefix = "github_pat_"

// maxTokenLength bounds what is sent to GitHub as a token. Real ones are under
// a hundred characters.
const maxTokenLength = 512

// TokenMethod signs a user in with a fine-grained personal access token they
// paste. It is the first Provider, chosen because it needs nothing registered
// with GitHub.
type TokenMethod struct {
	// NewClient returns a GitHub client that acts as the token's owner.
	NewClient func(token string) (*ghclient.REST, error)
	Access    *access.Checker
	// SessionLength is how long a sign-in lasts.
	SessionLength time.Duration
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

func (m *TokenMethod) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// SignIn accepts a token only if all of these hold: it is fine-grained, GitHub
// says whose it is, that account is one the workspace admits, and the token can
// read at least one connected repository.
//
// The third check is what keeps a stranger out. Without it anyone with any
// fine-grained token could hold a session — one that showed them nothing, but a
// session all the same.
func (m *TokenMethod) SignIn(ctx context.Context, credential string) (Session, error) {
	token := strings.TrimSpace(credential)
	if !strings.HasPrefix(token, tokenPrefix) || len(token) > maxTokenLength {
		// Decided here, before the credential is sent anywhere.
		return Session{}, ErrNotFineGrained
	}
	client, err := m.NewClient(token)
	if err != nil {
		return Session{}, err
	}
	user, err := client.Viewer(ctx)
	switch {
	case errors.Is(err, ghclient.ErrUnauthorized):
		return Session{}, ErrRejected
	case err != nil:
		return Session{}, err
	}
	if !m.Access.Permitted(user.Login) {
		return Session{}, ErrNotListed
	}

	var failure error
	for _, repo := range m.Access.Connected() {
		ok, err := m.Access.CanRead(ctx, user.Login, client, repo.Owner, repo.Name)
		switch {
		case errors.Is(err, access.ErrTokenRejected):
			return Session{}, ErrRejected
		case err != nil:
			// Keep looking: one repository GitHub could not answer for does not
			// decide the others.
			failure = err
		case ok:
			return Session{Login: user.Login, Token: token, Expires: m.now().Add(m.SessionLength)}, nil
		}
	}
	if failure != nil {
		// No repository said yes, and at least one could not be asked. That is
		// "try again", not "you have no access".
		return Session{}, failure
	}
	return Session{}, ErrNoRepository
}

// CanRead asks the access checker, as the session's user.
func (m *TokenMethod) CanRead(ctx context.Context, s Session, owner, name string) (bool, error) {
	client, err := m.NewClient(s.Token)
	if err != nil {
		return false, err
	}
	return m.Access.CanRead(ctx, s.Login, client, owner, name)
}

// Client returns a GitHub client that acts as the session's user.
func (m *TokenMethod) Client(s Session) (*ghclient.REST, error) {
	return m.NewClient(s.Token)
}
