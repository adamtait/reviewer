// SPDX-License-Identifier: MIT

package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adamtait/reviewer/internal/workspace/access"
	"github.com/adamtait/reviewer/internal/workspace/testgithub"
	"github.com/adamtait/reviewer/internal/workspace/wsconfig"
)

var testNow = time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)

// method returns a TokenMethod over a stand-in GitHub with two connected
// repositories, one private and one public.
func method(t *testing.T) (*TokenMethod, *testgithub.Server) {
	t.Helper()
	gh := testgithub.New(t)
	gh.SetRepo("example", "private-repo", testgithub.Repo{Private: true, Readers: []string{"alice"}})
	gh.SetRepo("example", "public-repo", testgithub.Repo{Private: false})
	repos := []wsconfig.Repository{{Owner: "example", Name: "private-repo"}, {Owner: "example", Name: "public-repo"}}
	return &TokenMethod{
		NewClient:     gh.Client,
		Access:        access.New(repos, time.Minute, nil),
		SessionLength: 30 * 24 * time.Hour,
		Now:           func() time.Time { return testNow },
	}, gh
}

// A classic token can carry write access to every repository its owner can
// reach. It is refused on its shape alone, before it is sent anywhere.
func TestAClassicTokenIsRefusedWithoutBeingSentToGitHub(t *testing.T) {
	m, gh := method(t)
	for _, credential := range []string{"gh" + "p_notfinegrained", "", "   ", "some words"} {
		if _, err := m.SignIn(context.Background(), credential); !errors.Is(err, ErrNotFineGrained) {
			t.Errorf("%q: want ErrNotFineGrained, got %v", credential, err)
		}
	}
	if calls := gh.Calls(); len(calls) != 0 {
		t.Errorf("a refused credential was sent to GitHub: %v", calls)
	}
}

func TestATokenGitHubDoesNotKnowIsRejected(t *testing.T) {
	m, _ := method(t)
	if _, err := m.SignIn(context.Background(), testgithub.Token("nobody")); !errors.Is(err, ErrRejected) {
		t.Errorf("want ErrRejected, got %v", err)
	}
}

// This is the check that keeps a stranger out. Bob's token is perfectly valid,
// and can read the public repository like any token can — and that is not
// enough to hold a session.
func TestAValidTokenThatReadsNoPrivateConnectedRepositoryIsRefused(t *testing.T) {
	m, gh := method(t)
	bob := gh.AddUser("bob")
	if _, err := m.SignIn(context.Background(), bob); !errors.Is(err, ErrNoRepository) {
		t.Errorf("want ErrNoRepository, got %v", err)
	}
}

func TestSignInGivesASessionThatLastsTheConfiguredTime(t *testing.T) {
	m, gh := method(t)
	alice := gh.AddUser("alice")
	// Pasted with surrounding whitespace, as a pasted token often is.
	s, err := m.SignIn(context.Background(), "  "+alice+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if s.Login != "alice" || s.Token != alice {
		t.Errorf("session = %+v", s)
	}
	if want := testNow.Add(30 * 24 * time.Hour); !s.Expires.Equal(want) {
		t.Errorf("expires %v, want %v", s.Expires, want)
	}

	ok, err := m.CanRead(context.Background(), s, "example", "private-repo")
	if !ok || err != nil {
		t.Errorf("want alice able to read the private repository, got %v, %v", ok, err)
	}
	if ok, _ := m.CanRead(context.Background(), s, "example", "public-repo"); ok {
		t.Error("a public repository must be refused even to a signed-in user")
	}
	if client, err := m.Client(s); err != nil || client.Token != alice {
		t.Errorf("want a client that acts as alice, got %v", err)
	}
}

// "GitHub is down" must not be reported as "your token is bad" or "you have no
// access": the user's fix for each is different.
func TestWhenGitHubCannotBeAskedSignInSaysSo(t *testing.T) {
	m, gh := method(t)
	alice := gh.AddUser("alice")
	gh.Close()
	_, err := m.SignIn(context.Background(), alice)
	if err == nil || errors.Is(err, ErrRejected) || errors.Is(err, ErrNoRepository) || errors.Is(err, ErrNotFineGrained) {
		t.Errorf("want a failure that is none of the three refusals, got %v", err)
	}
}

func cookies(t *testing.T, key byte, secure bool) *Cookies {
	t.Helper()
	c, err := NewCookies(bytes.Repeat([]byte{key}, 32), secure)
	if err != nil {
		t.Fatal(err)
	}
	c.SetClock(func() time.Time { return testNow })
	return c
}

func TestACookieRoundTrips(t *testing.T) {
	c := cookies(t, 1, true)
	in := Session{Login: "alice", Token: testgithub.Token("alice"), Expires: testNow.Add(time.Hour)}
	value, err := c.Seal(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(value, "alice") || strings.Contains(value, in.Token) {
		t.Error("the cookie value shows what is sealed inside it")
	}
	out, err := c.Open(value)
	if err != nil {
		t.Fatal(err)
	}
	if out.Login != in.Login || out.Token != in.Token || !out.Expires.Equal(in.Expires) {
		t.Errorf("opened %+v, sealed %+v", out, in)
	}
	// Two seals of one session differ, so a cookie cannot be recognised by value.
	if again, _ := c.Seal(in); again == value {
		t.Error("sealing twice gave the same value")
	}
}

// Different things wrong with a cookie, and one answer for all of them.
func TestACookieThatIsNotOursIsNoSession(t *testing.T) {
	c := cookies(t, 1, true)
	expired, _ := c.Seal(Session{Login: "alice", Token: "t", Expires: testNow.Add(-time.Second)})
	otherKey, _ := cookies(t, 2, true).Seal(Session{Login: "alice", Token: "t", Expires: testNow.Add(time.Hour)})

	for name, value := range map[string]string{
		"expired": expired, "sealed with another key": otherKey,
		"empty": "", "not base64": "!!!", "too short": "AAAA",
	} {
		if _, err := c.Open(value); !errors.Is(err, ErrNoSession) {
			t.Errorf("%s: want ErrNoSession, got %v", name, err)
		}
	}
}

// Any one bit changed anywhere in a cookie, and it is no session.
//
// An earlier form of this test replaced the value's last two characters with
// "AA", which about once in a few hundred runs spelled the bytes that were
// already there, and the test failed. Every bit is flipped here instead, so
// each value tried differs from the sealed one.
func TestAnAlteredCookieIsNoSession(t *testing.T) {
	c := cookies(t, 1, true)
	good, _ := c.Seal(Session{Login: "alice", Token: "t", Expires: testNow.Add(time.Hour)})
	raw, err := base64.RawURLEncoding.DecodeString(good)
	if err != nil {
		t.Fatal(err)
	}
	for i := range raw {
		for bit := range 8 {
			changed := bytes.Clone(raw)
			changed[i] ^= 1 << bit
			if _, err := c.Open(base64.RawURLEncoding.EncodeToString(changed)); !errors.Is(err, ErrNoSession) {
				t.Fatalf("byte %d, bit %d flipped: want ErrNoSession, got %v", i, bit, err)
			}
		}
	}
}

// One sealed value has one spelling: the same bytes written with the unused
// bits of the last character set are refused.
func TestACookieSpelledAnotherWayIsNoSession(t *testing.T) {
	c := cookies(t, 1, true)
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	tried := 0
	// Sessions of different lengths, so that the value ends part-way through
	// a group of four characters and its last character has bits to spare.
	for _, login := range []string{"a", "ab", "abc"} {
		good, _ := c.Seal(Session{Login: login, Token: "t", Expires: testNow.Add(time.Hour)})
		if len(good)%4 == 0 {
			continue
		}
		last := strings.IndexByte(alphabet, good[len(good)-1])
		respelled := good[:len(good)-1] + string(alphabet[last|1])
		if respelled == good {
			respelled = good[:len(good)-1] + string(alphabet[last|2])
		}
		if respelled == good {
			continue
		}
		tried++
		if _, err := c.Open(respelled); !errors.Is(err, ErrNoSession) {
			t.Errorf("login %q: a second spelling of the cookie was accepted: %v", login, err)
		}
	}
	if tried == 0 {
		t.Fatal("no value had spare bits to set, so the test proved nothing")
	}
}

func TestTheCookieIsUnreadableToScriptsAndNotSentCrossSite(t *testing.T) {
	c := cookies(t, 1, true)
	rec := httptest.NewRecorder()
	session := Session{Login: "alice", Token: "t", Expires: testNow.Add(time.Hour)}
	if err := c.Set(rec, session); err != nil {
		t.Fatal(err)
	}
	set := rec.Result().Cookies()
	if len(set) != 1 {
		t.Fatalf("want one cookie, got %d", len(set))
	}
	got := set[0]
	if got.Name != CookieName || !got.HttpOnly || !got.Secure || got.SameSite != http.SameSiteLaxMode || got.Path != "/" {
		t.Errorf("cookie attributes: %+v", got)
	}

	// A request carrying it is read back as the same session.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(got)
	if read, err := c.Read(req); err != nil || read.Login != "alice" {
		t.Errorf("Read = %+v, %v", read, err)
	}
	if _, err := c.Read(httptest.NewRequest(http.MethodGet, "/", nil)); !errors.Is(err, ErrNoSession) {
		t.Errorf("a request with no cookie: want ErrNoSession, got %v", err)
	}

	cleared := httptest.NewRecorder()
	c.Clear(cleared)
	if gone := cleared.Result().Cookies(); len(gone) != 1 || gone[0].MaxAge >= 0 || gone[0].Value != "" {
		t.Errorf("Clear should expire the cookie, got %+v", gone)
	}
}

// Over plain http, which the configuration allows for the local machine only,
// a Secure cookie would never be sent back.
func TestTheCookieIsNotMarkedSecureOverLocalHTTP(t *testing.T) {
	c := cookies(t, 1, false)
	rec := httptest.NewRecorder()
	_ = c.Set(rec, Session{Login: "alice", Token: "t", Expires: testNow.Add(time.Hour)})
	if rec.Result().Cookies()[0].Secure {
		t.Error("want no Secure attribute when the service is reached over local http")
	}
}

func TestTheCookieKeyMustBe32Bytes(t *testing.T) {
	if _, err := NewCookies([]byte("short"), true); err == nil {
		t.Error("want a short key refused")
	}
}

// With a list of admitted accounts, an account that is not on it is refused at
// sign-in, in its own words, whatever its token can read.
func TestAnAccountThatIsNotListedIsRefusedAtSignIn(t *testing.T) {
	gh := testgithub.New(t)
	gh.SetRepo("example", "open", testgithub.Repo{Private: false})
	gh.SetRepo("example", "closed", testgithub.Repo{Private: true, Readers: []string{"alice", "mallory"}})
	checker := access.New([]wsconfig.Repository{
		{Owner: "example", Name: "open", AllowPublic: true}, {Owner: "example", Name: "closed"},
	}, time.Minute, nil)
	checker.Permit([]string{"alice"})
	m := &TokenMethod{NewClient: gh.Client, Access: checker, SessionLength: time.Hour, Now: func() time.Time { return testNow }}

	// mallory can read the public repository, as anyone can, and the private one too.
	if _, err := m.SignIn(context.Background(), gh.AddUser("mallory")); !errors.Is(err, ErrNotListed) {
		t.Errorf("an account that is not listed: %v, want ErrNotListed", err)
	}
	// Refused on the login alone: no repository was asked about.
	for _, call := range gh.Calls() {
		if strings.Contains(call, "/repos/") {
			t.Errorf("GitHub was asked %q for an account the workspace does not admit", call)
		}
	}

	session, err := m.SignIn(context.Background(), gh.AddUser("alice"))
	if err != nil {
		t.Fatalf("the listed account: %v", err)
	}
	// The public repository is served to her, because the operator allowed it.
	if ok, err := m.CanRead(context.Background(), session, "example", "open"); !ok || err != nil {
		t.Errorf("the allowed public repository: %v, %v", ok, err)
	}
}
