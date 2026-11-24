// SPDX-License-Identifier: MIT

package scan

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/adamtait/reviewer/internal/testfixture"
)

// credential reads the fixture credential: a made-up token in a real format,
// kept under testdata so that this file holds nothing that looks like one.
func credential(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "tiny-ts-repo", "head", "src", "config.ts"))
	if err != nil {
		t.Fatal(err)
	}
	found := regexp.MustCompile(`apiKey: "([^"]+)"`).FindSubmatch(raw)
	if found == nil {
		t.Fatal("the fixture no longer holds its credential")
	}
	return string(found[1])
}

func scanner(t *testing.T) *Scanner {
	t.Helper()
	s, err := New(testfixture.RequireTool(t, "gitleaks"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func check(t *testing.T, s *Scanner, items ...string) []Result {
	t.Helper()
	results, err := s.Check(context.Background(), items)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(results) != len(items) {
		t.Fatalf("%d results for %d items", len(results), len(items))
	}
	return results
}

func TestACredentialIsFoundAndCleanTextIsNot(t *testing.T) {
	s := scanner(t)
	secret := credential(t)
	results := check(t, s,
		"export function sum(a: number, b: number) { return a + b; }\n",
		"const config = {\n  apiKey: \""+secret+"\",\n};\n",
		"",
		"Nothing to see here.\n",
	)
	for i, want := range []bool{false, true, false, false} {
		if results[i].Flagged != want {
			t.Errorf("item %d: flagged %v, want %v", i, results[i].Flagged, want)
		}
	}
	// The result says what kind of credential, and never what it was.
	if len(results[1].Rules) == 0 {
		t.Error("a flagged item names no rule")
	}
	for _, rule := range results[1].Rules {
		if strings.Contains(rule, secret) || strings.Contains(rule, secret[:12]) {
			t.Errorf("a rule name carries the credential: %q", rule)
		}
	}
}

// The four ways content can try to switch the scanner off, each tested against
// the real scanner with the fixture credential planted. Each is reported.
func TestContentCannotSwitchTheScannerOff(t *testing.T) {
	s := scanner(t)
	secret := credential(t)
	line := `const apiKey = "` + secret + `";`

	for name, item := range map[string]string{
		"an exemption comment on the credential's line": line + " // gitleaks:allow\n",
		"the credential wrapped in base64":              "blob = \"" + base64.StdEncoding.EncodeToString([]byte(line)) + "\"\n",
		"base64 inside base64": "blob = \"" + base64.StdEncoding.EncodeToString(
			[]byte("inner = \""+base64.StdEncoding.EncodeToString([]byte(line))+"\"")) + "\"\n",
	} {
		if got := check(t, s, item); !got[0].Flagged {
			t.Errorf("%s: the credential was not reported", name)
		}
	}

	// An ignore file beside the content, and a rule file, as items of their
	// own. Neither is at the scan root or in the working directory, and
	// neither has its own name, so neither is read as configuration.
	ignoreEverything := "*\n**/*\nitem\n1/item\n"
	allowEverything := "[allowlist]\nregexes = ['''.*''']\npaths = ['''.*''']\n"
	results := check(t, s, ignoreEverything, line+"\n", allowEverything)
	if !results[1].Flagged {
		t.Error("an ignore file and a rule file among the items hid the credential")
	}

	// A file name the scanner's default rules exempt. The name an item had in
	// its repository never reaches the scanner, so there is no name to exempt:
	// the same content under any name is one item with one generated name.
	lockfile := "{\n  \"name\": \"x\",\n  \"lockfileVersion\": 3,\n  \"token\": \"" + secret + "\"\n}\n"
	if got := check(t, s, lockfile); !got[0].Flagged {
		t.Error("a credential in content shaped like a lockfile was not reported")
	}
}

func TestEachItemGetsItsOwnResult(t *testing.T) {
	s := scanner(t)
	secret := credential(t)
	items := make([]string, 25)
	for i := range items {
		items[i] = "line of code number " + strings.Repeat("x", i) + "\n"
	}
	flagged := map[int]bool{3: true, 11: true, 24: true}
	for i := range flagged {
		items[i] = "token = \"" + secret + "\" // " + items[i]
	}
	for i, result := range check(t, s, items...) {
		if result.Flagged != flagged[i] {
			t.Errorf("item %d: flagged %v, want %v", i, result.Flagged, flagged[i])
		}
	}
}

func TestAResultIsRememberedByContent(t *testing.T) {
	s := scanner(t)
	secret := credential(t)
	clean, dirty := "const a = 1;\n", "const k = \""+secret+"\";\n"
	first := check(t, s, clean, dirty)

	// Break the scanner. Content already judged needs no scan.
	working := s.binary
	s.binary = filepath.Join(t.TempDir(), "gone")
	again := check(t, s, dirty, clean, clean)
	if !again[0].Flagged || again[1].Flagged || again[2].Flagged || first[1].Flagged != again[0].Flagged {
		t.Errorf("remembered results = %+v", again)
	}
	// Content not seen before does need one, and without a scanner that is an
	// error, not a clean result.
	if _, err := s.Check(context.Background(), []string{clean, "something new\n"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("new content with no scanner: %v, want ErrUnavailable", err)
	}
	s.binary = working

	// The same content under a different scanner version is judged again.
	s.version = "another version"
	if got := check(t, s, dirty); !got[0].Flagged {
		t.Error("a new scanner version did not judge the content again")
	}
}

func TestAScannerThatCannotRunIsAnErrorNotACleanResult(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "absent")); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a scanner that is not installed: %v, want ErrUnavailable", err)
	}

	// A scanner that runs and writes no report.
	dir := t.TempDir()
	broken := filepath.Join(dir, "gitleaks")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\n[ \"$1\" = version ] && { echo 0.0.0; exit 0; }\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := New(broken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(context.Background(), []string{"anything"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a scanner that wrote no report: %v, want ErrUnavailable", err)
	}

	// A scanner that reports a file nobody asked it to scan.
	lying := filepath.Join(dir, "lying")
	script := "#!/bin/sh\n[ \"$1\" = version ] && { echo 0.0.0; exit 0; }\n" +
		"while [ $# -gt 0 ]; do case \"$1\" in --report-path) shift; out=\"$1\";; esac; shift; done\n" +
		"echo '[{\"RuleID\":\"x\",\"File\":\"/etc/passwd\"}]' > \"$out\"\nexit 1\n"
	if err := os.WriteFile(lying, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err = New(lying)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Check(context.Background(), []string{"anything"}); !errors.Is(err, ErrUnavailable) {
		t.Errorf("a report naming a file that was not scanned: %v, want ErrUnavailable", err)
	}
}

func TestNothingIsLeftBehind(t *testing.T) {
	s := scanner(t)
	scratch := t.TempDir()
	t.Setenv("TMPDIR", scratch)
	check(t, s, "const a = 1;\n", "const k = \""+credential(t)+"\";\n")
	left, err := os.ReadDir(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Errorf("%d entries left in the scratch directory after a scan: content that was scanned must not stay on disk", len(left))
	}
}

func TestTheRuleFileIsTheDefaultRulesAndNothingElse(t *testing.T) {
	lines := strings.Split(strings.TrimSpace(string(rules)), "\n")
	if len(lines) != 2 || lines[0] != "[extend]" || lines[1] != "useDefault = true" {
		t.Errorf("rules.toml = %q; it must extend the scanner's default rules and add nothing, least of all an allowlist", lines)
	}
}

func TestItemOf(t *testing.T) {
	for file, want := range map[string]int{
		"/scratch/content/7/item": 7,
		"7/item":                  7,
		"content/12/item":         12,
		"./0/item":                0,
	} {
		if got, ok := itemOf("/scratch/content", file); !ok || got != want {
			t.Errorf("itemOf(%q) = %d, %v; want %d", file, got, ok, want)
		}
	}
	for _, file := range []string{"/etc/passwd", "item", "x/item", "7/other", "7/item/extra", "-1/item", ""} {
		if _, ok := itemOf("/scratch/content", file); ok {
			t.Errorf("itemOf(%q) named an item", file)
		}
	}
}

func TestTheSameContentTwiceInOneCallIsScannedOnce(t *testing.T) {
	// A scanner that counts the items it is shown, by counting the files it finds.
	dir := t.TempDir()
	counting := filepath.Join(dir, "gitleaks")
	script := "#!/bin/sh\n[ \"$1\" = version ] && { echo 0.0.0; exit 0; }\n" +
		"for last; do :; done\nfind \"$last\" -name item | wc -l > " + filepath.Join(dir, "count") + "\n" +
		"while [ $# -gt 0 ]; do case \"$1\" in --report-path) shift; out=\"$1\";; esac; shift; done\n" +
		"echo '[]' > \"$out\"\nexit 0\n"
	if err := os.WriteFile(counting, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := New(counting)
	if err != nil {
		t.Fatal(err)
	}
	results := check(t, s, "a file", "one line", "a file", "another line", "a file")
	if len(results) != 5 {
		t.Fatalf("%d results", len(results))
	}
	raw, err := os.ReadFile(filepath.Join(dir, "count"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(raw)); got != "3" {
		t.Errorf("the scanner was shown %s items for three different contents", got)
	}
}
