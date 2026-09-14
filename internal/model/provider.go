// SPDX-License-Identifier: MIT

// Package model is the boundary between this tool and whatever answers its
// questions.
//
// Six access paths are supported and they are not alike: four are HTTP APIs
// billed per token, two drive a CLI already signed in to somebody's subscription.
// The interface here is deliberately the smallest thing both shapes can implement
// — a prompt in, text out — because every capability beyond that (streaming, tool
// use, token accounting) is one more thing the two halves would have to agree on,
// and this tool needs none of them.
//
// Nothing in this package names an endpoint, a model, or an organisation
// (ADR-0004). Those are the destination repository's, supplied by config and the
// environment.
package model

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Role is who is speaking. Two are enough: the system prompt says what the job is,
// and the user turn carries the change under review. An assistant turn would only
// matter for a conversation, and a review is one question.
type Role string

const (
	// RoleSystem carries the instructions.
	RoleSystem Role = "system"
	// RoleUser carries the material.
	RoleUser Role = "user"
)

// Message is one turn.
type Message struct {
	Role    Role
	Content string
}

// Options are the per-call knobs every path can honour.
type Options struct {
	// Model is the identifier the destination chose. Never defaulted here: a model
	// name compiled into this repository goes stale faster than a release, and
	// picking one on someone's behalf spends their money.
	Model string
	// MaxTokens bounds the reply. Zero means the provider's own default.
	MaxTokens int
	// Temperature is the sampling temperature. A review wants the same answer
	// twice, so the caller sets this low and the adapters pass it through.
	Temperature float64
	// JSON asks for a JSON object rather than prose. Adapters that can enforce it
	// do; the rest ask in the prompt and the caller validates either way, because
	// an unenforced request is a request.
	JSON bool
	// Timeout bounds one call. Zero means the caller's context decides.
	Timeout time.Duration
}

// Provider answers one question.
//
// The single method is what lets a subscription CLI and an HTTP API sit behind the
// same type. A CLI cannot stream tokens, report usage, or accept a tool schema, so
// an interface that asked for any of those would admit four of the six paths and
// quietly exclude two — which is the failure this interface exists to prevent.
type Provider interface {
	// Name is the access path's id, for logs and warnings. Never a brand name in
	// the core's own text: it is whatever the destination configured.
	Name() string
	// Complete answers the messages. The returned string is the model's reply with
	// nothing added and nothing stripped.
	Complete(ctx context.Context, messages []Message, opts Options) (string, error)
}

// ErrDisabled means the model lane is switched off, or is not configured well
// enough to run. It is not a failure: the deterministic lane is the product and
// the model lane is the addition (ADR-0009), so every caller treats this as a skip
// and the run continues.
var ErrDisabled = errors.New("the model lane is not enabled")

// ErrRefused means the provider declined to answer — a safety filter, a content
// policy, a refusal. Distinct from a transport error because there is nothing to
// retry: the same prompt gets the same answer.
var ErrRefused = errors.New("the provider declined to answer")

// Unavailable wraps ErrDisabled with the specific reason, so a run can say which
// of the several ways to be switched off applies.
func Unavailable(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrDisabled, fmt.Sprintf(format, args...))
}

// Redact removes anything credential-shaped from text that is about to be logged
// or reported.
//
// Called on every provider error before it leaves this package. A 401 body
// frequently echoes the key that failed, and an error message is the least
// guarded thing in the system: it reaches stderr, the run log, and — through a
// warning — a public pull request comment.
func Redact(s string, secrets ...string) string {
	for _, secret := range secrets {
		if len(secret) < 8 {
			// Too short to be a credential and too likely to be a common
			// substring. Replacing it would mangle unrelated text.
			continue
		}
		s = strings.ReplaceAll(s, secret, "[redacted]")
	}
	return keyShaped.ReplaceAllString(s, "[redacted]")
}
