// SPDX-License-Identifier: MIT

// Package assistant turns a reader's question into a call on the model
// gateway: the instructions, the conversation so far, the selected code, and
// six read-only tools bound to the commit the question is about.
//
// The tools cannot write, post, or reach the network. They read the commit's
// files, which are already in memory, and the pull request the reader has
// open. Text inside what they return can contain instructions aimed at the
// model; nothing a tool can do changes anything, whatever the model is told.
package assistant

import (
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/adamtait/reviewer/internal/workspace/gateway"
	"github.com/adamtait/reviewer/internal/workspace/navigation"
	"github.com/adamtait/reviewer/internal/workspace/source"
)

//go:embed prompts/system.md
var systemPrompt string

// System is the instructions given to the model.
func System() string { return strings.TrimSpace(systemPrompt) }

// Bounds on what a reader may send in one question.
const (
	// MaxQuestion is the longest question, in characters.
	MaxQuestion = 4000
	// MaxHistory is the most earlier questions and answers sent along.
	MaxHistory = 40
	// MaxHistoryText is the most text in all of them together, in bytes.
	MaxHistoryText = 200_000
	// MaxSelection is the most lines of selected code.
	MaxSelection = 400
)

// Message is an earlier question or answer, as the browser holds it.
type Message struct {
	// Role is "user" or "assistant".
	Role string
	Text string
}

// Selection is the code the reader selected. Lines count from 0 and the end
// is exclusive, as everywhere in the API.
type Selection struct {
	Path               string
	StartLine, EndLine int
}

// Ask is one question.
type Ask struct {
	Text      string
	History   []Message
	Selection *Selection
}

// Pull is what the tools know of the pull request the reader has open.
type Pull struct {
	Number      int
	Title       string
	Description string
	// Files is each changed path with its status, in GitHub's order.
	Files []ChangedFile
}

// ChangedFile is one file the pull request changes.
type ChangedFile struct {
	Path, PreviousPath, Status string
}

// Subject is what a question is about: one commit of one repository, as the
// signed-in reader may see it.
type Subject struct {
	Owner, Repo, SHA string
	UserLogin        string
	// Commit is the commit's files.
	Commit *source.Commit
	// Index is the commit's navigation index. Nil when it has none, in which
	// case lookups are text searches.
	Index *navigation.Index
	// Pull is the pull request the reader has open. Nil when the question was
	// asked from a file with no pull request behind it.
	Pull *Pull
}

// ErrBadAsk means the question cannot be asked as sent. Its message says why,
// in words fit for the reader.
var ErrBadAsk = errors.New("the question cannot be asked")

func bad(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBadAsk, fmt.Sprintf(format, args...))
}

// Call builds the gateway call for a question.
func Call(subject Subject, ask Ask) (gateway.Call, error) {
	text := strings.TrimSpace(ask.Text)
	switch {
	case text == "":
		return gateway.Call{}, bad("it is empty")
	case utf8.RuneCountInString(text) > MaxQuestion:
		return gateway.Call{}, bad("it is longer than %d characters", MaxQuestion)
	case len(ask.History) > MaxHistory:
		return gateway.Call{}, bad("the conversation is longer than %d questions and answers; start a new one", MaxHistory)
	}

	call := gateway.Call{
		Owner: subject.Owner, Repo: subject.Repo, SHA: subject.SHA, UserLogin: subject.UserLogin,
		System:   System(),
		Question: text,
		File:     subject.Commit.Text,
	}
	total := 0
	for _, m := range ask.History {
		if m.Role != "user" && m.Role != "assistant" {
			return gateway.Call{}, bad("an earlier turn has the role %q", m.Role)
		}
		total += len(m.Text)
		if total > MaxHistoryText {
			return gateway.Call{}, bad("the conversation is too long to send; start a new one")
		}
		call.History = append(call.History, gateway.Message{Role: m.Role, Text: m.Text})
	}

	if s := ask.Selection; s != nil {
		content, ok := subject.Commit.Text(s.Path)
		if !ok {
			return gateway.Call{}, bad("the selected file, %s, is not a text file of this commit", s.Path)
		}
		lines := splitLines(content)
		switch {
		case s.StartLine < 0 || s.EndLine <= s.StartLine || s.EndLine > len(lines):
			return gateway.Call{}, bad("the selected lines are not in %s", s.Path)
		case s.EndLine-s.StartLine > MaxSelection:
			return gateway.Call{}, bad("the selection is longer than %d lines", MaxSelection)
		}
		call.Selection = &gateway.Segment{
			Path: s.Path, StartLine: s.StartLine + 1, EndLine: s.EndLine,
			Text: strings.Join(lines[s.StartLine:s.EndLine], "\n"),
		}
	}
	return call, nil
}

// splitLines splits a file's text into its lines. A final newline ends the
// last line and does not start an empty one.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}
