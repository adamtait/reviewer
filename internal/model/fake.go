// SPDX-License-Identifier: MIT

package model

import (
	"context"
	"fmt"
	"sync"
)

// Fake is a Provider that answers from a script.
//
// It exists so that everything above this package — the assembler, the review
// pass, the invalidation pass — can be tested against an exact reply rather than
// against a model's mood. Every test of lane B's *logic* uses this; the adapters
// are the only things that talk to anything real.
//
// It also records what it was asked, which is how the tests assert that a prompt
// carries what it should and, more importantly, that it does not carry what it
// should not.
type Fake struct {
	// Replies are returned in order. The last one repeats once they run out, so a
	// test that does not care how many calls happen does not have to count them.
	Replies []string
	// Err, when set, is returned instead of a reply.
	Err error

	mu     sync.Mutex
	calls  []Call
	served int
}

// Call is one recorded request.
type Call struct {
	Messages []Message
	Options  Options
}

// NewFake returns a Fake that answers with the given replies in order.
func NewFake(replies ...string) *Fake { return &Fake{Replies: replies} }

func (f *Fake) Name() string { return "fake" }

func (f *Fake) Complete(ctx context.Context, messages []Message, opts Options) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Honoured even by the fake: a caller that forgets to plumb cancellation
	// through would otherwise pass every test and hang in production.
	if err := ctx.Err(); err != nil {
		return "", err
	}

	f.calls = append(f.calls, Call{Messages: append([]Message(nil), messages...), Options: opts})
	if f.Err != nil {
		return "", f.Err
	}
	if len(f.Replies) == 0 {
		return "", fmt.Errorf("fake provider has no reply for call %d", len(f.calls))
	}
	i := f.served
	if i >= len(f.Replies) {
		i = len(f.Replies) - 1
	}
	f.served++
	return f.Replies[i], nil
}

// Calls returns every request made so far.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Call(nil), f.calls...)
}

// Prompt returns the concatenated content of one call, for assertions about what
// crossed the boundary.
func (f *Fake) Prompt(i int) string {
	calls := f.Calls()
	if i < 0 || i >= len(calls) {
		return ""
	}
	var b []byte
	for _, m := range calls[i].Messages {
		b = append(b, m.Content...)
		b = append(b, '\n')
	}
	return string(b)
}
