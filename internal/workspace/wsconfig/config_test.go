// SPDX-License-Identifier: MIT

package wsconfig

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const validKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=" // 32 zero bytes

func env(pairs ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(name string) string { return m[name] }
}

func mentions(problems []string, want string) bool {
	for _, p := range problems {
		if strings.Contains(p, want) {
			return true
		}
	}
	return false
}

// The example is what an operator copies, so it has to be a configuration the
// service would actually start with.
func TestTheExampleConfigurationIsValid(t *testing.T) {
	c, err := Load(filepath.Join("..", "..", "..", "examples", "workspace.config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if problems := c.Problems(); len(problems) != 0 {
		t.Fatalf("the example configuration has problems:\n%s", strings.Join(problems, "\n"))
	}
	if len(c.Repositories) != 2 || !c.Repositories[0].HasProvider() || c.Repositories[1].HasProvider() {
		t.Errorf("want one repository with an assistant and one without, got %+v", c.Repositories)
	}
	_, problems := LoadSecrets(c, env(EnvCookieKey, validKey, c.Repositories[0].APIKeyEnv, "a-provider-key"))
	if len(problems) != 0 {
		t.Errorf("want no secret problems with both variables set, got %v", problems)
	}
}

// Nothing that costs money or bounds memory has a default. An empty file must
// name every one of them, in one pass.
func TestAnEmptyConfigurationReportsEveryMissingValue(t *testing.T) {
	c, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	problems := c.Problems()
	for _, want := range []string{
		"listen", "publicUrl", "github.apiBaseUrl", "github.webBaseUrl",
		"indexWorkflowFile", "indexArtifactName", "sessionDays", "accessCacheSeconds",
		"limits.archiveBytes", "limits.fileBytes", "limits.indexBytes", "limits.cacheBytes",
		"limits.fetchSeconds", "limits.modelCalls", "limits.questionTokens",
		"limits.answerTokens", "limits.callSeconds", "limits.waitSeconds", "repositories is empty",
	} {
		if !mentions(problems, want) {
			t.Errorf("want a problem mentioning %q, got:\n%s", want, strings.Join(problems, "\n"))
		}
	}
}

func TestAMisspeltKeyIsAnError(t *testing.T) {
	if _, err := Parse([]byte("listen: \":8080\"\nlimitz:\n  cacheBytes: 1\n")); err == nil {
		t.Fatal("want an unknown key to be an error, so a misspelt limit is not silently zero")
	}
}

func valid() Config {
	return Config{
		Listen:            ":8080",
		PublicURL:         "https://workspace.example.com",
		GitHub:            GitHub{APIBaseURL: "https://api.example.com", WebBaseURL: "https://git.example.com"},
		IndexWorkflowFile: "review-index.yml", IndexArtifactName: "review-index",
		SessionDays: 30, AccessCacheSeconds: 300,
		Limits: Limits{ArchiveBytes: 100, FileBytes: 10, IndexBytes: 100, CacheBytes: 1000,
			FetchSeconds: 1, ModelCalls: 1, QuestionTokens: 1, AnswerTokens: 1, CallSeconds: 1, WaitSeconds: 1},
		Repositories: []Repository{{Owner: "o", Name: "r"}},
	}
}

func TestProblemsNamesEachMistake(t *testing.T) {
	if problems := valid().Problems(); len(problems) != 0 {
		t.Fatalf("the baseline should be valid, got %v", problems)
	}
	cases := []struct {
		name   string
		break_ func(*Config)
		want   string
	}{
		{"plain http to another host", func(c *Config) { c.PublicURL = "http://workspace.example.com" }, "must use https"},
		{"a workflow path, not a name", func(c *Config) { c.IndexWorkflowFile = ".github/workflows/x.yml" }, "indexWorkflowFile"},
		{"an access cache long enough to stop mirroring GitHub", func(c *Config) { c.AccessCacheSeconds = 86400 }, "accessCacheSeconds"},
		{"a session longer than a token lives", func(c *Config) { c.SessionDays = 1000 }, "sessionDays"},
		{"a file limit above the archive limit", func(c *Config) { c.Limits.FileBytes = 500 }, "limits.fileBytes"},
		{"an archive that cannot fit the cache", func(c *Config) { c.Limits.ArchiveBytes = 5000 }, "limits.archiveBytes"},
		{"an index that cannot fit the cache", func(c *Config) { c.Limits.IndexBytes = 5000 }, "limits.indexBytes"},
		{"a repository listed twice, differing only in case", func(c *Config) {
			c.Repositories = append(c.Repositories, Repository{Owner: "O", Name: "R"})
		}, "listed twice"},
		{"half a provider", func(c *Config) { c.Repositories[0].Provider = "gemini" }, "set all four"},
		{"a provider this version cannot call", func(c *Config) {
			c.Repositories[0] = Repository{Owner: "o", Name: "r", Provider: "other", Model: "m", BaseURL: "https://m.example.com", APIKeyEnv: "K"}
		}, "only provider"},
		{"a key written where a variable name belongs", func(c *Config) {
			c.Repositories[0] = Repository{Owner: "o", Name: "r", Provider: "gemini", Model: "m", BaseURL: "https://m.example.com", APIKeyEnv: "not-a-name"}
		}, "NAME of an environment variable"},
		{"the cookie key reused as a provider key", func(c *Config) {
			c.Repositories[0] = Repository{Owner: "o", Name: "r", Provider: "gemini", Model: "m", BaseURL: "https://m.example.com", APIKeyEnv: EnvCookieKey}
		}, "cookie key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := valid()
			c.Repositories = append([]Repository(nil), c.Repositories...)
			tc.break_(&c)
			if problems := c.Problems(); !mentions(problems, tc.want) {
				t.Errorf("want a problem mentioning %q, got %v", tc.want, problems)
			}
		})
	}
}

// Loopback http is how a developer runs the service and how the tests stand
// GitHub up. It is the only plain-http address accepted.
func TestLoopbackHTTPIsAccepted(t *testing.T) {
	c := valid()
	c.PublicURL = "http://localhost:8080"
	c.GitHub.APIBaseURL = "http://127.0.0.1:9000"
	if problems := c.Problems(); len(problems) != 0 {
		t.Errorf("want loopback http accepted, got %v", problems)
	}
}

func TestSecretsAreValidatedAndNeverPrinted(t *testing.T) {
	c := valid()
	c.Repositories[0] = Repository{Owner: "Example", Name: "Repo", Provider: "gemini", Model: "m", BaseURL: "https://m.example.com", APIKeyEnv: "MODEL_KEY"}

	_, problems := LoadSecrets(c, env())
	if !mentions(problems, EnvCookieKey+" is not set") || !mentions(problems, "MODEL_KEY is not set") {
		t.Errorf("want both missing variables named, got %v", problems)
	}
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	if _, problems := LoadSecrets(c, env(EnvCookieKey, short, "MODEL_KEY", "k")); !mentions(problems, "must decode to 32") {
		t.Errorf("want a short cookie key refused, got %v", problems)
	}
	if _, problems := LoadSecrets(c, env(EnvCookieKey, "not base64!!", "MODEL_KEY", "k")); !mentions(problems, "not valid base64") {
		t.Errorf("want a malformed cookie key refused, got %v", problems)
	}

	const providerKey = "provider-key-that-must-not-be-printed"
	s, problems := LoadSecrets(c, env(EnvCookieKey, validKey, "MODEL_KEY", providerKey))
	if len(problems) != 0 {
		t.Fatalf("want no problems, got %v", problems)
	}
	for _, printed := range []string{s.String(), fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s)} {
		if strings.Contains(printed, providerKey) || strings.Contains(printed, validKey) {
			t.Errorf("a secret was printed: %s", printed)
		}
	}
	keys := s.Reveal()
	if len(keys.Cookie) != 32 || keys.Provider["example/repo"] != providerKey {
		t.Errorf("want the keys revealed under the repository's lower-cased slug, got %d cookie bytes and %d provider keys",
			len(keys.Cookie), len(keys.Provider))
	}
}

func TestAPublicRepositoryNeedsAListOfWhoMaySignIn(t *testing.T) {
	c := valid()
	c.Repositories[0].AllowPublic = true
	if problems := c.Problems(); !mentions(problems, "allowPublic") || !mentions(problems, "allowedLogins") {
		t.Errorf("allowPublic with no allowedLogins should be refused, got %v", problems)
	}
	c.AllowedLogins = []string{"octocat"}
	if problems := c.Problems(); len(problems) != 0 {
		t.Errorf("allowPublic with a list: %v", problems)
	}

	// The list is optional when every repository is private, and checked when given.
	c = valid()
	c.AllowedLogins = []string{"octocat", "Mona-Lisa", "a"}
	if problems := c.Problems(); len(problems) != 0 {
		t.Errorf("a list with private repositories: %v", problems)
	}
	for name, logins := range map[string][]string{
		"an empty login":              {""},
		"a login with a space":        {"octo cat"},
		"an address":                  {"octocat@example.com"},
		"a login listed twice":        {"octocat", "Octocat"},
		"a login far too long":        {strings.Repeat("a", 40)},
		"a login with a slash":        {"example/octocat"},
		"a login with a hyphen first": {"-octocat"},
	} {
		c := valid()
		c.AllowedLogins = logins
		if problems := c.Problems(); !mentions(problems, "allowedLogins") {
			t.Errorf("%s: want a problem naming allowedLogins, got %v", name, problems)
		}
	}
}

func TestTheTwoSettingsAreReadFromTheFile(t *testing.T) {
	c, err := Parse([]byte("allowedLogins: [octocat, hubot]\nrepositories:\n  - owner: example\n    name: open\n    allowPublic: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.AllowedLogins) != 2 || !c.Repositories[0].AllowPublic {
		t.Errorf("config = %+v", c)
	}
}
