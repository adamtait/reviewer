// SPDX-License-Identifier: MIT

package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/adamtait/reviewer/internal/model"
	"github.com/adamtait/reviewer/internal/workspace/gateway"
	"github.com/adamtait/reviewer/internal/workspace/navigation"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

// What each tool returns at most. Line numbers in arguments and results count
// from 1, as a person reads a file.
const (
	maxSearchLines   = 50
	maxReadLines     = 400
	maxTargets       = 5
	targetContext    = 2 // lines shown either side of a target
	maxReferences    = 30
	maxDescription   = 20_000
	maxLineShown     = 400 // characters of one line
	descriptionLabel = "the pull request's title and description"
)

// Tools returns the six tools, each bound to the subject's commit.
func Tools(subject Subject) []gateway.Tool {
	return []gateway.Tool{
		{
			Spec: spec("search_text",
				"Search every text file of the commit for a literal string, ignoring case. Returns matching lines as path:line: text, at most "+
					fmt.Sprint(maxSearchLines)+" of them.",
				`{"type":"object","properties":{
					"query":{"type":"string","description":"The text to find, matched exactly. At least 2 characters."},
					"path_prefix":{"type":"string","description":"Only search paths that begin with this, such as src/."}
				},"required":["query"]}`),
			Run: subject.searchText,
		},
		{
			Spec: spec("read_file",
				"Read lines of one file at the commit, numbered. At most "+fmt.Sprint(maxReadLines)+" lines in one call; ask again for more.",
				`{"type":"object","properties":{
					"path":{"type":"string","description":"The file's path from the repository root."},
					"start_line":{"type":"integer","description":"The first line to read, counted from 1. Defaults to 1."},
					"end_line":{"type":"integer","description":"The last line to read. Defaults to as far as one call allows."}
				},"required":["path"]}`),
			Run: subject.readFile,
		},
		{
			Spec: spec("find_definition",
				"Find where a symbol is defined. Give the file and line where the symbol is used and the symbol's own text. "+
					"The result says where it came from: the CI-built index, a text search, or a package outside the repository.",
				positionSchema),
			Run: subject.findDefinition,
		},
		{
			Spec: spec("find_references",
				"Find where a symbol is used. Give a file and line where the symbol appears and the symbol's own text. "+
					"Returns the references the CI-built index lists and how many lines a text search finds.",
				positionSchema),
			Run: subject.findReferences,
		},
		{
			Spec: spec("list_changed_files", "List the files the pull request changes, each with how it changed.", `{"type":"object","properties":{}}`),
			Run:  subject.listChangedFiles,
		},
		{
			Spec: spec("read_description", "Read the pull request's title and description, as its author wrote them.", `{"type":"object","properties":{}}`),
			Run:  subject.readDescription,
		},
	}
}

const positionSchema = `{"type":"object","properties":{
	"path":{"type":"string","description":"The file's path from the repository root."},
	"line":{"type":"integer","description":"The line the symbol is on, counted from 1."},
	"name":{"type":"string","description":"The symbol's text, exactly as it appears on that line."}
},"required":["path","line","name"]}`

func spec(name, description, schema string) model.ToolSpec {
	return model.ToolSpec{Name: name, Description: description, SchemaJSON: schema}
}

// arguments reads a tool's arguments. An unreadable set is an error the model
// is told about, so it can ask again.
func arguments(raw string, into any) error {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	if err := json.Unmarshal([]byte(raw), into); err != nil {
		return errors.New("the arguments could not be read: they must be a JSON object matching the tool's schema")
	}
	return nil
}

// textOf returns a file's lines, or the error a tool reports for a path that
// is not a readable file of the commit.
func (s Subject) textOf(path string) ([]string, error) {
	if path == "" {
		return nil, errors.New("give the file's path")
	}
	kind, found := s.Commit.Kind(path)
	switch {
	case !found:
		return nil, fmt.Errorf("there is no file at %q in this commit; use search_text to find the right path", path)
	case kind != source.KindText:
		return nil, fmt.Errorf("%s is not readable text in this commit (%s)", path, kind)
	}
	text, _ := s.Commit.Text(path)
	return splitLines(text), nil
}

func shown(line string) string {
	line = strings.TrimRight(line, "\r")
	if len(line) <= maxLineShown {
		return line
	}
	cut := maxLineShown
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + "…"
}

func (s Subject) searchText(_ context.Context, raw string) ([]gateway.Segment, error) {
	var args struct {
		Query      string `json:"query"`
		PathPrefix string `json:"path_prefix"`
	}
	if err := arguments(raw, &args); err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(args.Query) < 2 {
		return nil, errors.New("the query must be at least 2 characters")
	}
	matches, truncated := s.Commit.Search(source.Query{Text: args.Query, PathPrefix: args.PathPrefix})
	if len(matches) == 0 {
		return []gateway.Segment{{Name: "a search result", Text: fmt.Sprintf("No line contains %q.", args.Query)}}, nil
	}
	if len(matches) > maxSearchLines {
		matches, truncated = matches[:maxSearchLines], true
	}
	// One segment for each line, so that each is tied to its file: a file the
	// secrets scan flags is then withheld, and the rest are not.
	out := make([]gateway.Segment, 0, len(matches)+1)
	for _, m := range matches {
		out = append(out, gateway.Segment{
			Path: m.Path, StartLine: m.Line + 1, EndLine: m.Line + 1,
			Text: fmt.Sprintf("%s:%d: %s", m.Path, m.Line+1, shown(m.Text)),
		})
	}
	if truncated {
		out = append(out, gateway.Segment{Name: "a search result",
			Text: fmt.Sprintf("(Only the first %d matching lines are shown. Narrow the search with path_prefix or a longer query.)", len(matches))})
	}
	return out, nil
}

func (s Subject) readFile(_ context.Context, raw string) ([]gateway.Segment, error) {
	var args struct {
		Path      string `json:"path"`
		StartLine int    `json:"start_line"`
		EndLine   int    `json:"end_line"`
	}
	if err := arguments(raw, &args); err != nil {
		return nil, err
	}
	lines, err := s.textOf(args.Path)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return []gateway.Segment{{Path: args.Path, Text: args.Path + " is empty."}}, nil
	}
	start, end := args.StartLine, args.EndLine
	if start < 1 {
		start = 1
	}
	if start > len(lines) {
		return nil, fmt.Errorf("%s has %d lines; line %d is past its end", args.Path, len(lines), start)
	}
	if end < start || end > len(lines) {
		end = len(lines)
	}
	note := ""
	if end-start+1 > maxReadLines {
		end = start + maxReadLines - 1
		note = fmt.Sprintf("\n(%s has %d lines. Lines %d to %d are shown; ask for the rest with start_line.)", args.Path, len(lines), start, end)
	}
	return []gateway.Segment{{
		Path: args.Path, StartLine: start, EndLine: end,
		Text: fmt.Sprintf("%s, lines %d to %d of %d:\n%s%s", args.Path, start, end, len(lines), numbered(lines, start, end), note),
	}}, nil
}

// numbered writes lines start to end, each with its number.
func numbered(lines []string, start, end int) string {
	var out strings.Builder
	for n := start; n <= end; n++ {
		fmt.Fprintf(&out, "%5d  %s\n", n, shown(lines[n-1]))
	}
	return strings.TrimRight(out.String(), "\n")
}

// position finds the character a named symbol starts at on a line, counted in
// UTF-16 units as the index counts them.
func (s Subject) position(raw string) (path string, line, character int, name string, err error) {
	var args struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Name string `json:"name"`
	}
	if err := arguments(raw, &args); err != nil {
		return "", 0, 0, "", err
	}
	lines, err := s.textOf(args.Path)
	if err != nil {
		return "", 0, 0, "", err
	}
	if args.Line < 1 || args.Line > len(lines) {
		return "", 0, 0, "", fmt.Errorf("%s has %d lines; there is no line %d", args.Path, len(lines), args.Line)
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", 0, 0, "", errors.New("give the symbol's text as name")
	}
	text := lines[args.Line-1]
	at := strings.Index(text, args.Name)
	if at < 0 {
		return "", 0, 0, "", fmt.Errorf("line %d of %s does not contain %q; read the file to find the right line", args.Line, args.Path, args.Name)
	}
	units := 0
	for _, r := range text[:at] {
		if r >= 0x10000 {
			units += 2
		} else {
			units++
		}
	}
	return args.Path, args.Line - 1, units, args.Name, nil
}

// around returns the lines either side of a location, numbered.
func (s Subject) around(at navigation.Location) (gateway.Segment, bool) {
	lines, err := s.textOf(at.Path)
	if err != nil || len(lines) == 0 {
		return gateway.Segment{}, false
	}
	start, end := at.Line+1-targetContext, at.Line+1+targetContext
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if start > end {
		return gateway.Segment{}, false
	}
	return gateway.Segment{
		Path: at.Path, StartLine: start, EndLine: end,
		Text: fmt.Sprintf("%s, line %d:\n%s", at.Path, at.Line+1, numbered(lines, start, end)),
	}, true
}

func (s Subject) findDefinition(_ context.Context, raw string) ([]gateway.Segment, error) {
	path, line, character, name, err := s.position(raw)
	if err != nil {
		return nil, err
	}
	found := navigation.FindDefinition(s.Index, s.Commit, path, line, character)
	note := func(text string) gateway.Segment { return gateway.Segment{Name: "a lookup result", Text: text} }

	switch found.Label {
	case navigation.LabelIndex:
		out := []gateway.Segment{note(fmt.Sprintf(
			"Definition of %q, from the CI-built index (%d found). The index is built by the repository's CI and is not checked.",
			name, len(found.Targets)))}
		for i, target := range found.Targets {
			if i == maxTargets {
				out = append(out, note(fmt.Sprintf("(Only the first %d are shown.)", maxTargets)))
				break
			}
			if segment, ok := s.around(target); ok {
				out = append(out, segment)
			}
		}
		return out, nil
	case navigation.LabelExternal:
		return []gateway.Segment{note(fmt.Sprintf(
			"The CI-built index places %q in the package %s, version %s, which is not part of this repository. There is nothing to read here.",
			name, found.Package, orUnknown(found.Version)))}, nil
	case navigation.LabelTextSearch:
		out := []gateway.Segment{note(fmt.Sprintf(
			"The index has no definition for %q. From text search: these lines contain the word. They are not known to define it.", name))}
		for i, m := range found.Matches {
			if i == maxSearchLines {
				break
			}
			out = append(out, gateway.Segment{
				Path: m.Path, StartLine: m.Line + 1, EndLine: m.Line + 1,
				Text: fmt.Sprintf("%s:%d: %s", m.Path, m.Line+1, shown(m.Text)),
			})
		}
		return out, nil
	default:
		return []gateway.Segment{note(fmt.Sprintf("Nothing was found for %q, in the index or by text search.", name))}, nil
	}
}

func orUnknown(text string) string {
	if text == "" {
		return "unknown"
	}
	return text
}

func (s Subject) findReferences(_ context.Context, raw string) ([]gateway.Segment, error) {
	path, line, character, name, err := s.position(raw)
	if err != nil {
		return nil, err
	}
	found := navigation.FindReferences(s.Index, s.Commit, path, line, character)
	note := func(text string) gateway.Segment { return gateway.Segment{Name: "a lookup result", Text: text} }

	var out []gateway.Segment
	if len(found.References) == 0 {
		out = append(out, note(fmt.Sprintf(
			"The CI-built index lists no references to %q. An index can cover only part of a repository, so this does not mean there are none.", name)))
	} else {
		out = append(out, note(fmt.Sprintf(
			"References to %q from the CI-built index (%d%s). The index is built by the repository's CI and is not checked.",
			name, len(found.References), more(found.Truncated))))
		for i, ref := range found.References {
			if i == maxReferences {
				out = append(out, note(fmt.Sprintf("(Only the first %d are shown.)", maxReferences)))
				break
			}
			text := ""
			if lines, err := s.textOf(ref.Path); err == nil && ref.Line < len(lines) {
				text = shown(lines[ref.Line])
			}
			out = append(out, gateway.Segment{
				Path: ref.Path, StartLine: ref.Line + 1, EndLine: ref.Line + 1,
				Text: fmt.Sprintf("%s:%d: %s", ref.Path, ref.Line+1, text),
			})
		}
	}
	out = append(out, note(fmt.Sprintf(
		"A text search finds %d%s lines that contain the word %q. Use search_text to see them.",
		len(found.Matches), more(found.MatchesTruncated), name)))
	return out, nil
}

func more(truncated bool) string {
	if truncated {
		return " or more"
	}
	return ""
}

var errNoPull = errors.New("this question was not asked from a pull request, so there is none to read")

func (s Subject) listChangedFiles(context.Context, string) ([]gateway.Segment, error) {
	if s.Pull == nil {
		return nil, errNoPull
	}
	if len(s.Pull.Files) == 0 {
		return []gateway.Segment{{Name: "the list of changed files", Text: "The pull request changes no files."}}, nil
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Pull request #%d changes %d files:\n", s.Pull.Number, len(s.Pull.Files))
	for _, f := range s.Pull.Files {
		if f.Status == "renamed" && f.PreviousPath != "" {
			fmt.Fprintf(&out, "%s  %s (was %s)\n", f.Status, f.Path, f.PreviousPath)
			continue
		}
		fmt.Fprintf(&out, "%s  %s\n", f.Status, f.Path)
	}
	// Paths are not content from a file, and so are not withheld by file.
	return []gateway.Segment{{Name: "the list of changed files", Text: strings.TrimRight(out.String(), "\n")}}, nil
}

func (s Subject) readDescription(context.Context, string) ([]gateway.Segment, error) {
	if s.Pull == nil {
		return nil, errNoPull
	}
	description := s.Pull.Description
	note := ""
	if len(description) > maxDescription {
		cut := maxDescription
		for cut > 0 && !utf8.RuneStart(description[cut]) {
			cut--
		}
		description, note = description[:cut], "\n(The description is longer than this and was cut here.)"
	}
	if strings.TrimSpace(description) == "" {
		description = "(The pull request has no description.)"
	}
	return []gateway.Segment{{
		Name: descriptionLabel,
		Text: fmt.Sprintf("Pull request #%d\nTitle: %s\n\nDescription:\n%s%s", s.Pull.Number, s.Pull.Title, description, note),
	}}, nil
}
