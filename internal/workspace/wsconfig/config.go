// SPDX-License-Identifier: MIT

// Package wsconfig loads the workspace service's configuration and secrets.
//
// Nothing about a deployment is compiled in: every host, repository and model
// name arrives through the file or the environment (ADR-0004). And nothing that
// costs money or bounds memory has a default — a zero is an error, so the
// service never chooses such a value on an operator's behalf.
package wsconfig

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// EnvCookieKey names the variable holding the key that seals session cookies:
// 32 random bytes, base64-encoded.
const EnvCookieKey = "WORKSPACE_COOKIE_KEY"

// ProviderGemini is the one model provider the first version can call.
const ProviderGemini = "gemini"

// maxAccessCacheSeconds bounds how long a "yes, this user can read this
// repository" answer is believed. Access removed on GitHub takes effect in the
// workspace within this period, so an unbounded value would quietly stop the
// workspace mirroring GitHub at all.
const maxAccessCacheSeconds = 3600

// maxSessionDays is GitHub's own default ceiling on a fine-grained token's life.
// A session cannot usefully outlive the token sealed inside it.
const maxSessionDays = 366

// Config is the whole configuration file.
type Config struct {
	Listen             string       `yaml:"listen"`
	PublicURL          string       `yaml:"publicUrl"`
	GitHub             GitHub       `yaml:"github"`
	IndexWorkflowFile  string       `yaml:"indexWorkflowFile"`
	IndexArtifactName  string       `yaml:"indexArtifactName"`
	SessionDays        int          `yaml:"sessionDays"`
	AccessCacheSeconds int          `yaml:"accessCacheSeconds"`
	Limits             Limits       `yaml:"limits"`
	Repositories       []Repository `yaml:"repositories"`
}

// GitHub says where GitHub is. Supplied rather than compiled in, so no host
// appears in this repository and GitHub Enterprise works without a code change.
type GitHub struct {
	APIBaseURL string `yaml:"apiBaseUrl"`
	WebBaseURL string `yaml:"webBaseUrl"`
}

// Limits are the bounds on memory, time and model use.
type Limits struct {
	// ArchiveBytes is the most text kept from one commit's archive.
	ArchiveBytes int64 `yaml:"archiveBytes"`
	// FileBytes is the most text kept from one file.
	FileBytes int64 `yaml:"fileBytes"`
	// IndexBytes is the largest decoded navigation index accepted.
	IndexBytes int64 `yaml:"indexBytes"`
	// CacheBytes bounds every archive and index held in memory together.
	CacheBytes int64 `yaml:"cacheBytes"`
	// FetchSeconds bounds one archive or artifact fetch from GitHub.
	FetchSeconds int `yaml:"fetchSeconds"`
	// ModelCalls is the most model calls one question may make.
	ModelCalls int `yaml:"modelCalls"`
	// QuestionTokens is the most input tokens, as the provider reports them,
	// summed over one question.
	QuestionTokens int `yaml:"questionTokens"`
	// AnswerTokens is the output ceiling for one model call, reasoning included.
	AnswerTokens int `yaml:"answerTokens"`
	// CallSeconds bounds one model call.
	CallSeconds int `yaml:"callSeconds"`
	// WaitSeconds is the longest provider-named wait the assistant sits out.
	WaitSeconds int `yaml:"waitSeconds"`
}

// Repository is one connected repository. The four model fields are all set or
// all empty: a repository with no provider has navigation and no assistant.
type Repository struct {
	Owner    string `yaml:"owner"`
	Name     string `yaml:"name"`
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	BaseURL  string `yaml:"baseUrl"`
	// APIKeyEnv is the NAME of the environment variable holding the key. The key
	// itself is never written to a file.
	APIKeyEnv string `yaml:"apiKeyEnv"`
}

// HasProvider reports whether the assistant is configured for this repository.
func (r Repository) HasProvider() bool { return r.Provider != "" }

// Slug is the repository's owner/name, lower-cased: GitHub treats both parts
// case-insensitively, so this is the form to compare and key by.
func (r Repository) Slug() string {
	return strings.ToLower(r.Owner) + "/" + strings.ToLower(r.Name)
}

// Load reads and parses the file at path. It does not validate: call Problems.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("reading the configuration: %w", err)
	}
	return Parse(raw)
}

// Parse decodes a configuration. An unknown key is an error rather than
// ignored: a misspelt limit that silently stayed zero would otherwise be
// reported as "limits.x must be greater than zero" with no hint why.
func Parse(raw []byte) (Config, error) {
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parsing the configuration: %w", err)
	}
	return c, nil
}

var (
	workflowFileRe = regexp.MustCompile(`^[A-Za-z0-9._-]+\.ya?ml$`)
	artifactNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	repoPartRe     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	envNameRe      = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)
)

// Problems returns everything wrong with the configuration, not just the first
// thing: an operator fixing a file one error per restart is an operator who
// stops reading the errors.
func (c Config) Problems() []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	if c.Listen == "" {
		add("listen is empty: set the address to serve on, for example \":8080\"")
	}
	if problem := checkURL(c.PublicURL); problem != "" {
		add("publicUrl %s", problem)
	}
	if problem := checkURL(c.GitHub.APIBaseURL); problem != "" {
		add("github.apiBaseUrl %s", problem)
	}
	if problem := checkURL(c.GitHub.WebBaseURL); problem != "" {
		add("github.webBaseUrl %s", problem)
	}
	if !workflowFileRe.MatchString(c.IndexWorkflowFile) {
		add("indexWorkflowFile must be a workflow's file name, such as \"review-index.yml\", not %q", c.IndexWorkflowFile)
	}
	if !artifactNameRe.MatchString(c.IndexArtifactName) {
		add("indexArtifactName must be an artifact name, such as \"review-index\", not %q", c.IndexArtifactName)
	}
	if c.SessionDays <= 0 || c.SessionDays > maxSessionDays {
		add("sessionDays must be between 1 and %d, not %d", maxSessionDays, c.SessionDays)
	}
	if c.AccessCacheSeconds <= 0 || c.AccessCacheSeconds > maxAccessCacheSeconds {
		add("accessCacheSeconds must be between 1 and %d, not %d: access removed on GitHub takes effect within this period",
			maxAccessCacheSeconds, c.AccessCacheSeconds)
	}

	l := c.Limits
	for _, limit := range []struct {
		name  string
		value int64
	}{
		{"archiveBytes", l.ArchiveBytes}, {"fileBytes", l.FileBytes},
		{"indexBytes", l.IndexBytes}, {"cacheBytes", l.CacheBytes},
		{"fetchSeconds", int64(l.FetchSeconds)}, {"modelCalls", int64(l.ModelCalls)},
		{"questionTokens", int64(l.QuestionTokens)}, {"answerTokens", int64(l.AnswerTokens)},
		{"callSeconds", int64(l.CallSeconds)}, {"waitSeconds", int64(l.WaitSeconds)},
	} {
		if limit.value <= 0 {
			add("limits.%s must be greater than zero: it has no default, because the service does not choose a value that costs money or bounds memory", limit.name)
		}
	}
	if l.FileBytes > 0 && l.ArchiveBytes > 0 && l.FileBytes > l.ArchiveBytes {
		add("limits.fileBytes (%d) is larger than limits.archiveBytes (%d)", l.FileBytes, l.ArchiveBytes)
	}
	if l.CacheBytes > 0 && l.ArchiveBytes > l.CacheBytes {
		add("limits.archiveBytes (%d) is larger than limits.cacheBytes (%d), so no archive could be held", l.ArchiveBytes, l.CacheBytes)
	}
	if l.CacheBytes > 0 && l.IndexBytes > l.CacheBytes {
		add("limits.indexBytes (%d) is larger than limits.cacheBytes (%d), so no index could be held", l.IndexBytes, l.CacheBytes)
	}

	if len(c.Repositories) == 0 {
		add("repositories is empty: list at least one private repository to connect")
	}
	seen := map[string]bool{}
	for i, r := range c.Repositories {
		where := fmt.Sprintf("repositories[%d]", i)
		if !repoPartRe.MatchString(r.Owner) || !repoPartRe.MatchString(r.Name) {
			add("%s needs an owner and a name, not %q and %q", where, r.Owner, r.Name)
			continue
		}
		where = r.Owner + "/" + r.Name
		if seen[r.Slug()] {
			add("%s is listed twice", where)
		}
		seen[r.Slug()] = true

		set := 0
		for _, v := range []string{r.Provider, r.Model, r.BaseURL, r.APIKeyEnv} {
			if v != "" {
				set++
			}
		}
		switch {
		case set == 0:
			// No assistant for this repository. Navigation needs no model.
		case set < 4:
			add("%s sets some of provider, model, baseUrl and apiKeyEnv: set all four for an assistant, or none for navigation alone", where)
		default:
			if r.Provider != ProviderGemini {
				add("%s names provider %q: the only provider this version can call is %q", where, r.Provider, ProviderGemini)
			}
			if problem := checkURL(r.BaseURL); problem != "" {
				add("%s baseUrl %s", where, problem)
			}
			if !envNameRe.MatchString(r.APIKeyEnv) {
				add("%s apiKeyEnv must be the NAME of an environment variable, such as WORKSPACE_MODEL_KEY, not %q", where, r.APIKeyEnv)
			} else if r.APIKeyEnv == EnvCookieKey {
				add("%s apiKeyEnv is %s, which holds the cookie key", where, EnvCookieKey)
			}
		}
	}
	return out
}

// checkURL returns what is wrong with an address, or "".
//
// Plain http is accepted only for the local machine: a developer's own run, and
// the tests, which stand GitHub up on a loopback port. Anywhere else it would
// send a user's token in the clear.
func checkURL(raw string) string {
	if raw == "" {
		return "is empty"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Sprintf("is not an address: %q", raw)
	}
	switch u.Scheme {
	case "https":
		return ""
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return ""
		}
		return fmt.Sprintf("must use https, because it carries users' tokens: %q", raw)
	default:
		return fmt.Sprintf("must use https, not %q", u.Scheme)
	}
}

// Secrets holds every secret the service is given. Its zero value is empty.
//
// The fields are unexported and String never prints them, so a Secrets can be
// logged or embedded in an error without leaking. The values leave through one
// method, Reveal, so that "who can see a key" is answered by "who calls Reveal".
type Secrets struct {
	cookieKey []byte
	provider  map[string]string
}

// Keys is what Reveal hands over.
type Keys struct {
	// Cookie is the 32-byte key that seals session cookies.
	Cookie []byte
	// Provider maps a repository's Slug to its model provider key.
	Provider map[string]string
}

// LoadSecrets reads every secret the configuration calls for from the
// environment, and returns everything missing or malformed.
func LoadSecrets(c Config, getenv func(string) string) (Secrets, []string) {
	var problems []string
	s := Secrets{provider: map[string]string{}}

	raw := getenv(EnvCookieKey)
	switch key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(raw)); {
	case raw == "":
		problems = append(problems, EnvCookieKey+" is not set: it must hold 32 random bytes, base64-encoded")
	case err != nil:
		problems = append(problems, EnvCookieKey+" is not valid base64")
	case len(key) != 32:
		problems = append(problems, fmt.Sprintf("%s decodes to %d bytes, and must decode to 32", EnvCookieKey, len(key)))
	default:
		s.cookieKey = key
	}

	for _, r := range c.Repositories {
		if !r.HasProvider() || !envNameRe.MatchString(r.APIKeyEnv) {
			continue
		}
		value := getenv(r.APIKeyEnv)
		if value == "" {
			problems = append(problems, fmt.Sprintf("%s is not set: it must hold the model provider key for %s/%s", r.APIKeyEnv, r.Owner, r.Name))
			continue
		}
		s.provider[r.Slug()] = value
	}
	return s, problems
}

// Reveal hands the secrets over. Only cmd/workspace calls it, once, at start: it
// gives the cookie key to the identity package and the provider keys to the
// model gateway, and nothing else in the service ever holds either.
func (s Secrets) Reveal() Keys {
	provider := make(map[string]string, len(s.provider))
	for k, v := range s.provider {
		provider[k] = v
	}
	return Keys{Cookie: append([]byte(nil), s.cookieKey...), Provider: provider}
}

// String says which secrets are set and never what they are.
func (s Secrets) String() string {
	cookie := "unset"
	if len(s.cookieKey) > 0 {
		cookie = "set"
	}
	slugs := make([]string, 0, len(s.provider))
	for slug := range s.provider {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return fmt.Sprintf("cookie key %s; provider keys set for %d repositories %v", cookie, len(slugs), slugs)
}

// GoString keeps %#v as quiet as %v.
func (s Secrets) GoString() string { return s.String() }
