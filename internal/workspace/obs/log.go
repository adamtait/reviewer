// SPDX-License-Identifier: MIT

// Package obs writes the service's log.
//
// The log is the service's only record: there is no database, so the measures
// the design's feedback weeks rest on are counts over these lines. Each request
// writes exactly one. A line says who did what to which pull request and how it
// ended — and never carries source code, a description, conversation text,
// search text, a token or a key.
package obs

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

// Fields are what a handler knows about its request that the middleware does
// not: who is asking, and about what. A handler fills them in as it learns them,
// and the middleware writes them out once, when the request ends.
type Fields struct {
	mu      sync.Mutex
	user    string
	owner   string
	repo    string
	pull    int
	sha     string
	label   string
	outcome string
}

type fieldsKey struct{}

// WithFields attaches an empty Fields to ctx.
func WithFields(ctx context.Context) (context.Context, *Fields) {
	f := &Fields{}
	return context.WithValue(ctx, fieldsKey{}, f), f
}

// From returns the request's Fields. Outside a request it returns a scratch
// value, so a handler under test can call the setters without a nil check.
func From(ctx context.Context) *Fields {
	if f, ok := ctx.Value(fieldsKey{}).(*Fields); ok {
		return f
	}
	return &Fields{}
}

// SetUser records the signed-in user's login.
func (f *Fields) SetUser(login string) { f.mu.Lock(); f.user = login; f.mu.Unlock() }

// SetRepo records the repository the request is about.
func (f *Fields) SetRepo(owner, repo string) {
	f.mu.Lock()
	f.owner, f.repo = owner, repo
	f.mu.Unlock()
}

// SetPull records the pull request number.
func (f *Fields) SetPull(number int) { f.mu.Lock(); f.pull = number; f.mu.Unlock() }

// SetSHA records the commit.
func (f *Fields) SetSHA(sha string) { f.mu.Lock(); f.sha = sha; f.mu.Unlock() }

// SetLabel records where a navigation result came from.
func (f *Fields) SetLabel(label string) { f.mu.Lock(); f.label = label; f.mu.Unlock() }

// SetOutcome records how the request ended, where a status code does not say.
func (f *Fields) SetOutcome(outcome string) { f.mu.Lock(); f.outcome = outcome; f.mu.Unlock() }

func (f *Fields) attrs() []slog.Attr {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []slog.Attr
	add := func(key, value string) {
		if value != "" {
			out = append(out, slog.String(key, value))
		}
	}
	add("user", f.user)
	add("owner", f.owner)
	add("repo", f.repo)
	if f.pull != 0 {
		out = append(out, slog.Int("pull", f.pull))
	}
	add("sha", f.sha)
	add("label", f.label)
	add("outcome", f.outcome)
	return out
}

// Logger writes structured lines, one JSON object each.
type Logger struct {
	slog *slog.Logger
}

// New returns a Logger writing to w. Every string it writes passes through the
// Redactor first — as a property of the logger, not a habit of its callers, so a
// new log line cannot forget.
func New(w io.Writer, redactor *Redactor) *Logger {
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Value.Kind() == slog.KindString {
				a.Value = slog.StringValue(redactor.Redact(a.Value.String()))
			}
			return a
		},
	})
	return &Logger{slog: slog.New(handler)}
}

// Request writes the one line a request gets.
func (l *Logger) Request(ctx context.Context, requestID, route, method string, status int, took time.Duration) {
	attrs := []slog.Attr{
		slog.String("request_id", requestID),
		slog.String("route", route),
		slog.String("method", method),
		slog.Int("status", status),
		slog.Int64("duration_ms", took.Milliseconds()),
	}
	attrs = append(attrs, From(ctx).attrs()...)
	l.slog.LogAttrs(ctx, slog.LevelInfo, "request", attrs...)
}

// Event writes a line that is not a request: the service starting, or a
// configuration error found while serving.
func (l *Logger) Event(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	l.slog.LogAttrs(ctx, level, message, attrs...)
}
