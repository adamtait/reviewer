// SPDX-License-Identifier: MIT

package gateway

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/adamtait/reviewer/internal/model"
)

// The ways a question ends. Exactly one applies.
const (
	// OutcomeOK: the model answered.
	OutcomeOK = "ok"
	// OutcomeRateLimited: the provider is limiting requests, and named a wait
	// longer than the service will sit out, up to five minutes.
	OutcomeRateLimited = "rate_limited"
	// OutcomeQuotaExhausted: the provider named a wait over five minutes.
	OutcomeQuotaExhausted = "quota_exhausted"
	// OutcomeLimitReached: one of the service's own limits on a question.
	OutcomeLimitReached = "limit_reached"
	// OutcomeSecretBlocked: a credential in the question, the history or the
	// selection. Nothing was sent.
	OutcomeSecretBlocked = "secret_blocked"
	// OutcomeScanFailed: the scanner could not run. Nothing was sent.
	OutcomeScanFailed = "scan_failed"
	// OutcomeRefused: the provider declined to answer.
	OutcomeRefused = "refused"
	// OutcomeProviderError: anything else from the provider.
	OutcomeProviderError = "provider_error"
	// OutcomeAbandoned: the reader went away before the answer ended. It is
	// written to the log and never sent, there being nobody to send it to.
	OutcomeAbandoned = "abandoned"
)

// quotaLine separates a rate limit from an exhausted quota. The provider
// answers both with the same status and the same code, and only the length of
// the wait tells them apart: a per-minute limit names seconds, a daily quota
// names hours.
const quotaLine = 5 * time.Minute

// demandWait is how long a call waits before its one retry when the provider
// reports heavy demand.
const demandWait = 5 * time.Second

// kind is what a provider failure is, for deciding what to do about it.
type kind int

const (
	kindOther kind = iota
	kindLimited
	kindDemand
)

// classify reads a provider failure.
//
// A rate limit is status 429. Heavy demand is status 503. Both were observed
// from the provider in the two shapes its error bodies take, and the status is
// the same in both, which is why the status is what is read.
func classify(failure *model.ProviderError) kind {
	switch failure.Status {
	case http.StatusTooManyRequests:
		return kindLimited
	case http.StatusServiceUnavailable:
		return kindDemand
	}
	return kindOther
}

// limited decides the outcome of a rate limit the service will not sit out.
func limited(failure *model.ProviderError, now time.Time) Result {
	if !failure.HasRetryAfter {
		return Result{Outcome: OutcomeRateLimited, Message: "The model provider is limiting requests. Try again shortly."}
	}
	seconds := int(failure.RetryAfter.Round(time.Second) / time.Second)
	if failure.RetryAfter > quotaLine {
		at := now.Add(failure.RetryAfter).UTC()
		return Result{
			Outcome: OutcomeQuotaExhausted, RetryAfterSeconds: seconds, ResetAt: at.Format(time.RFC3339),
			Message: "The model provider's quota is exhausted. It resets at " + at.Format("15:04 UTC on 2 January") + ".",
		}
	}
	return Result{
		Outcome: OutcomeRateLimited, RetryAfterSeconds: seconds,
		Message: fmt.Sprintf("The model provider is limiting requests. Try again in %s.", spoken(failure.RetryAfter)),
	}
}

// spoken writes a wait the way a person would say it.
func spoken(d time.Duration) string {
	seconds := int(d.Round(time.Second) / time.Second)
	switch {
	case seconds < 60:
		return fmt.Sprintf("%d seconds", seconds)
	case seconds < 120:
		return "about a minute"
	default:
		return fmt.Sprintf("about %d minutes", (seconds+30)/60)
	}
}

// failed is the outcome for any provider failure that is not a limit.
func failed(failure *model.ProviderError) Result {
	message := "The model provider could not be reached."
	if failure.Status != 0 {
		message = fmt.Sprintf("The model provider answered with an error (%d %s): %s",
			failure.Status, failure.Code, strings.TrimSpace(failure.Message))
	}
	return Result{Outcome: OutcomeProviderError, Message: message}
}
