// SPDX-License-Identifier: MIT

// Package scan checks text for credentials before it is sent to a model
// provider.
//
// The text is not trusted to leave the scanner alone. A file in a repository
// can carry a comment that exempts its own line, sit beside an ignore file,
// hide a credential in base64, or have a name the scanner's rules skip. So the
// scanner is run one fixed way, and nothing in the content can change it:
//
//   - each item is written as <n>/item under a scratch directory, so its name
//     is generated and nothing is ever at the scan root, where an ignore file
//     would be read;
//   - the scanner runs from an empty working directory, with no environment,
//     with this package's own rule file, which is the scanner's default rules
//     and nothing else;
//   - the inline exemption comment is disabled, and encoded text is decoded.
//
// Each of the four was tested against the pinned scanner with a planted
// credential; the tests here plant it the same four ways.
package scan

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/adamtait/reviewer/internal/analyzers/gitleaks"
)

//go:embed rules.toml
var rules []byte

// decodeDepth is how many layers of encoding the scanner looks through.
const decodeDepth = 2

// maxRemembered bounds how many results are kept. A result is a hash and a
// few words, so this is a few megabytes at most.
const maxRemembered = 50_000

// ErrUnavailable means the scan did not happen. It is never the same as a
// clean scan: a caller must send nothing.
var ErrUnavailable = errors.New("the secrets scanner could not be run")

// Result is the scan of one item.
type Result struct {
	// Flagged reports that the scanner found a credential.
	Flagged bool
	// Rules names the scanner's rules that matched, sorted. A rule's name says
	// what kind of credential it is, and never what the credential was.
	Rules []string
}

// Scanner runs the secrets scanner and remembers what it found.
type Scanner struct {
	binary  string
	version string

	mu         sync.Mutex
	remembered map[[sha256.Size]byte]Result
}

// New returns a Scanner that runs the named binary. It fails if the binary
// cannot be run, so that a service which cannot scan learns it at start and
// not at the first question.
func New(binary string) (*Scanner, error) {
	if binary == "" {
		binary = gitleaks.Binary
	}
	path, version, err := gitleaks.Probe(binary)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return &Scanner{binary: path, version: version, remembered: map[[sha256.Size]byte]Result{}}, nil
}

// Version is what the scanner says it is, for the log.
func (s *Scanner) Version() string { return s.version }

// key identifies a result: the content, the scanner that judged it, and the
// rules it judged by. A new scanner or new rules is a new judgement.
func (s *Scanner) key(text string) [sha256.Size]byte {
	h := sha256.New()
	h.Write([]byte(s.version))
	h.Write([]byte{0})
	h.Write(rules)
	h.Write([]byte{0})
	h.Write([]byte(text))
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Check scans each item and returns one result for each, in order.
//
// An error means nothing was learned about any item not already known, and
// the caller must treat every one as unchecked.
func (s *Scanner) Check(ctx context.Context, items []string) ([]Result, error) {
	results := make([]Result, len(items))
	keys := make([][sha256.Size]byte, len(items))
	var pending []int

	s.mu.Lock()
	for i, item := range items {
		keys[i] = s.key(item)
		if known, ok := s.remembered[keys[i]]; ok {
			results[i] = known
			continue
		}
		pending = append(pending, i)
	}
	s.mu.Unlock()
	if len(pending) == 0 {
		return results, nil
	}

	found, err := s.run(ctx, items, pending)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.remembered)+len(pending) > maxRemembered {
		// Forgetting costs a scan and nothing else.
		s.remembered = map[[sha256.Size]byte]Result{}
	}
	for _, i := range pending {
		results[i] = found[i]
		s.remembered[keys[i]] = found[i]
	}
	return results, nil
}

// run writes the pending items to a scratch directory, scans it once, and
// returns a result for each.
func (s *Scanner) run(ctx context.Context, items []string, pending []int) (map[int]Result, error) {
	scratch, err := os.MkdirTemp("", "workspace-scan-*")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() { _ = os.RemoveAll(scratch) }()

	// Three directories, apart from each other: the content, the rule file,
	// and an empty one to run from. The scanner looks for configuration in its
	// working directory and at the scan root, and finds content in neither.
	content := filepath.Join(scratch, "content")
	config := filepath.Join(scratch, "rules")
	empty := filepath.Join(scratch, "empty")
	for _, dir := range []string{content, config, empty} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}
	rulePath := filepath.Join(config, "rules.toml")
	if err := os.WriteFile(rulePath, rules, 0o600); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	for _, i := range pending {
		dir := filepath.Join(content, strconv.Itoa(i))
		if err := os.Mkdir(dir, 0o700); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		// One generated name for every item. What the file was called in its
		// repository never reaches the scanner, so no rule keyed on a path can
		// exempt it.
		if err := os.WriteFile(filepath.Join(dir, "item"), []byte(items[i]), 0o600); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
	}

	reports, err := gitleaks.Scan(ctx, s.binary, gitleaks.Options{
		Dir: empty, Target: content, Config: rulePath,
		IgnoreAllowComments: true, MaxDecodeDepth: decodeDepth, EmptyEnvironment: true,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	out := map[int]Result{}
	for _, i := range pending {
		out[i] = Result{}
	}
	matched := map[int]map[string]bool{}
	for _, report := range reports {
		i, ok := itemOf(content, report.File)
		if !ok {
			// A report that names no item this scan wrote. Whatever it is, it
			// is not a clean result, and it cannot be pinned on one item.
			return nil, fmt.Errorf("%w: the scanner reported a file that was not scanned", ErrUnavailable)
		}
		if matched[i] == nil {
			matched[i] = map[string]bool{}
		}
		matched[i][report.RuleID] = true
	}
	for i, names := range matched {
		result := Result{Flagged: true}
		for name := range names {
			result.Rules = append(result.Rules, name)
		}
		sort.Strings(result.Rules)
		out[i] = result
	}
	return out, nil
}

// itemOf reads the item's number back out of a path the scanner reported.
func itemOf(content, file string) (int, bool) {
	rel, err := filepath.Rel(content, file)
	if err != nil {
		// The scanner may report the path relative to its target.
		rel = file
	}
	rel = filepath.ToSlash(rel)
	rel = strings.TrimPrefix(rel, "./")
	// "<n>/item", possibly with the target's own name in front.
	parts := strings.Split(rel, "/")
	for at := 0; at+1 < len(parts); at++ {
		if parts[at+1] != "item" || at+2 != len(parts) {
			continue
		}
		if n, err := strconv.Atoi(parts[at]); err == nil && n >= 0 {
			return n, true
		}
	}
	return 0, false
}
