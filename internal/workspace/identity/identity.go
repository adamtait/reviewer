// SPDX-License-Identifier: MIT

// Package identity says who a user is and what they may read.
//
// Everything behind it — the catalog, the source reader, navigation, the
// assistant — is handed a GitHub client and a login, and does not know how the
// user signed in. That is the seam a second sign-in method would use (ADR-0032).
package identity

import (
	"context"
	"errors"
	"time"

	"github.com/adamtait/reviewer/internal/ghclient"
)

// Session is one signed-in user. It exists only inside the session cookie and,
// for the length of a request, in memory: the service keeps no session store.
type Session struct {
	Login string
	// Token is the user's own GitHub token. Every read the service makes of
	// GitHub for this user is made with it, so GitHub's access rules are the
	// service's access rules.
	Token   string
	Expires time.Time
}

// The ways a sign-in is refused. Each has its own message for the user, because
// each has a different fix.
var (
	// ErrNotFineGrained: the credential is not a fine-grained personal access
	// token. A classic token is refused because it can carry write access to
	// every repository its owner can reach.
	ErrNotFineGrained = errors.New("not a fine-grained personal access token")
	// ErrRejected: GitHub does not accept the token. Expired, revoked or mistyped.
	ErrRejected = errors.New("GitHub did not accept the token")
	// ErrNoRepository: the token works and reads no connected repository. A token
	// still waiting for an organisation owner's approval looks like this, because
	// until it is approved it reads nothing private.
	ErrNoRepository = errors.New("the token cannot read any connected repository")
	// ErrNotListed: the workspace admits listed accounts only, and this is not
	// one of them. Said as itself and not as ErrNoRepository: the fix is for
	// the operator to add the account, not for the user to change their token.
	ErrNotListed = errors.New("this account is not one the workspace admits")
)

// Provider is the identity interface: its three operations are all the rest of
// the service knows about sign-in.
type Provider interface {
	// SignIn turns a credential into a session, or refuses it with one of the
	// errors above. Any other error means GitHub could not be asked.
	SignIn(ctx context.Context, credential string) (Session, error)
	// CanRead reports whether the session's user may see a repository's data.
	// See access.Checker.CanRead for what false and a non-nil error mean.
	CanRead(ctx context.Context, s Session, owner, name string) (bool, error)
	// Client returns a GitHub client that acts as the session's user.
	Client(s Session) (*ghclient.REST, error)
}
