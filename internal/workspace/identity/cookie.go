// SPDX-License-Identifier: MIT

package identity

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// CookieName is the session cookie's name.
const CookieName = "ws_session"

// ErrNoSession means the request carries no session the service will accept:
// no cookie, one sealed with another key, one that has been altered, or one
// that has expired. The four are deliberately one error. A caller that could
// tell them apart could probe for which.
var ErrNoSession = errors.New("no valid session")

// Cookies seals a Session into a cookie and opens it again.
//
// The session is the cookie: nothing about it is kept on the server. That
// means there is no table of tokens to protect or to leak, and it has three
// consequences worth knowing. Signing out clears the cookie in that browser
// only. A copied cookie works until it expires or its token is revoked on
// GitHub. And changing the key signs everyone out.
type Cookies struct {
	aead cipher.AEAD
	// secure is false only when the service is reached over plain http, which
	// the configuration allows for the local machine alone.
	secure bool
	now    func() time.Time
}

// NewCookies returns a Cookies using a 32-byte key. Set secure whenever the
// service is reached over https.
func NewCookies(key []byte, secure bool) (*Cookies, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("the cookie key must be 32 bytes, not %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cookies{aead: aead, secure: secure, now: time.Now}, nil
}

// SetClock replaces the clock, for tests.
func (c *Cookies) SetClock(now func() time.Time) { c.now = now }

// sealed is what goes inside the cookie. Short keys: it is sent on every request.
type sealed struct {
	Token   string `json:"t"`
	Login   string `json:"l"`
	Expires int64  `json:"e"`
}

// Seal returns the cookie value for a session.
func (c *Cookies) Seal(s Session) (string, error) {
	plain, err := json.Marshal(sealed{Token: s.Token, Login: s.Login, Expires: s.Expires.Unix()})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	// The cookie's name is bound in as additional data, so a value sealed for
	// one purpose could never be accepted for another.
	return base64.RawURLEncoding.EncodeToString(c.aead.Seal(nonce, nonce, plain, []byte(CookieName))), nil
}

// Open returns the session inside a cookie value, or ErrNoSession.
func (c *Cookies) Open(value string) (Session, error) {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(raw) < c.aead.NonceSize() {
		return Session{}, ErrNoSession
	}
	nonce, box := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	plain, err := c.aead.Open(nil, nonce, box, []byte(CookieName))
	if err != nil {
		return Session{}, ErrNoSession
	}
	var in sealed
	if err := json.Unmarshal(plain, &in); err != nil || in.Token == "" || in.Login == "" {
		return Session{}, ErrNoSession
	}
	expires := time.Unix(in.Expires, 0)
	if !c.now().Before(expires) {
		return Session{}, ErrNoSession
	}
	return Session{Login: in.Login, Token: in.Token, Expires: expires}, nil
}

// Read returns the session a request carries, or ErrNoSession.
func (c *Cookies) Read(r *http.Request) (Session, error) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return Session{}, ErrNoSession
	}
	return c.Open(cookie.Value)
}

// Set writes the session cookie.
func (c *Cookies) Set(w http.ResponseWriter, s Session) error {
	value, err := c.Seal(s)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:  CookieName,
		Value: value,
		Path:  "/",
		// The browser drops it when the session ends; Open refuses it then too,
		// so a browser that kept it would gain nothing.
		Expires: s.Expires,
		// No script can read it, so a flaw in the page cannot hand out a token.
		HttpOnly: true,
		Secure:   c.secure,
		// Sent on a link followed from GitHub, and not on a request another site
		// makes in the background.
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Clear removes the session cookie from the browser.
func (c *Cookies) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: c.secure, SameSite: http.SameSiteLaxMode,
	})
}
