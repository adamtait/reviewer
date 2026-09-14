// SPDX-License-Identifier: MIT

package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/config"
)

// shape is what differs between the HTTP providers: where to post, what headers to
// set, what the body looks like, and where the text is in the reply.
//
// Everything else — retries, timeouts, status handling, redaction — is the same
// for all of them and lives in httpProvider. Four adapters' worth of that logic,
// written four times, is four places for a 401 to print a key.
type shape interface {
	// path is appended to the configured base URL.
	path(model string) string
	// header sets whatever this provider calls its credential.
	header(h http.Header, apiKey string)
	// body builds the request payload.
	body(messages []Message, opts Options) any
	// text pulls the reply out, or reports that there was not one.
	text(raw []byte) (string, error)
}

// httpProvider is every HTTP access path.
type httpProvider struct {
	name    string
	baseURL string
	apiKey  string
	shape   shape
	client  *http.Client
}

// Timeouts and retry policy.
//
// A review is a batch job nobody is watching, so the timeout is generous — and
// bounded, because the alternative is a wedged poller. Retries cover the two
// failures that are genuinely transient and nothing else: a rate limit and a
// server error. A 400 retried three times is three times the wrong answer.
const (
	defaultRequestTimeout = 3 * time.Minute
	maxAttempts           = 3
	baseBackoff           = time.Second
)

func newHTTP(cfg config.Config, secrets config.Secrets) (Provider, error) {
	// The environment wins over the file, so one machine can point elsewhere
	// without editing a tracked file.
	baseURL := secrets.ModelBaseURL
	if baseURL == "" {
		baseURL = cfg.LaneB.BaseURL
	}
	if baseURL == "" {
		return nil, Unavailable(
			"no endpoint for %q; set laneB.baseUrl or %s",
			cfg.LaneB.Provider, config.EnvModelBaseURL)
	}
	if secrets.ModelAPIKey == "" {
		return nil, Unavailable("no credential; set %s", config.EnvModelAPIKey)
	}

	var s shape
	switch cfg.LaneB.Provider {
	case "openai", "openai-compatible":
		s = openAIShape{}
	case "anthropic":
		s = anthropicShape{}
	case "gemini":
		s = geminiShape{}
	default:
		return nil, Unavailable("no HTTP adapter for %q", cfg.LaneB.Provider)
	}

	return &httpProvider{
		name:    cfg.LaneB.Provider,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  secrets.ModelAPIKey,
		shape:   s,
		client:  &http.Client{Timeout: defaultRequestTimeout},
	}, nil
}

func (p *httpProvider) Name() string { return p.name }

func (p *httpProvider) Complete(ctx context.Context, messages []Message, opts Options) (string, error) {
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	payload, err := json.Marshal(p.shape.body(messages, opts))
	if err != nil {
		return "", err
	}
	url := p.baseURL + p.shape.path(opts.Model)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return "", err
			}
		}

		text, retryable, err := p.once(ctx, url, payload)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if !retryable {
			return "", err
		}
	}
	return "", fmt.Errorf("%s: gave up after %d attempts: %w", p.name, maxAttempts, lastErr)
}

// once performs one request. The bool says whether trying again could help.
func (p *httpProvider) once(ctx context.Context, url string, payload []byte) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		// This error carries the raw URL, which is the one place a gateway's
		// path-embedded token would appear verbatim.
		return "", false, fmt.Errorf("%s: %s", p.name, p.redact(err.Error()))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	p.shape.header(req.Header, p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		// A transport error is worth one more try, and its text can contain the URL
		// — which, for a provider that puts the key in the query string, contains
		// the key.
		return "", true, fmt.Errorf("%s: %s", p.name, p.redact(err.Error()))
	}
	defer resp.Body.Close()

	// Bounded: a provider that streams an unbounded body should not be able to
	// exhaust this process's memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", true, fmt.Errorf("%s: reading the response: %s", p.name, firstLine(p.redact(err.Error())))
	}

	if resp.StatusCode != http.StatusOK {
		retryable := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
		// The body is the most likely place for a credential to appear: a 401
		// frequently echoes the key that failed.
		return "", retryable, fmt.Errorf("%s: provider error %d: %s",
			p.name, resp.StatusCode, p.redact(firstLine(string(raw))))
	}

	text, err := p.shape.text(raw)
	if err != nil {
		if errors.Is(err, ErrRefused) {
			return "", false, err
		}
		return "", false, fmt.Errorf("%s: %s", p.name, firstLine(p.redact(err.Error())))
	}
	return text, false, nil
}

// redact removes the key and the endpoint from anything about to be reported.
//
// The endpoint as well as the key, because a gateway routinely carries its token
// in the path, as a path segment before the version prefix, and a transport error
// prints the whole URL. config.Secrets already treats the base URL as a secret and
// hides it from its own String(); this is the other place it escapes.
func (p *httpProvider) redact(s string) string { return Redact(s, p.apiKey, p.baseURL) }

func backoff(attempt int) time.Duration {
	return time.Duration(math.Pow(2, float64(attempt-2))) * baseBackoff
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	return s
}
