// SPDX-License-Identifier: MIT

package model

import "regexp"

// keyShaped matches the prefixes the supported paths issue keys under, plus a
// bearer token in a header echo.
//
// A denylist of prefixes cannot be complete, which is why Redact also replaces the
// configured key verbatim. This catches the other case: a key that is not ours
// appearing in a proxied error body, and a key the caller did not think to pass.
var keyShaped = regexp.MustCompile(
	`(?i)(sk-[A-Za-z0-9_-]{8,}` +
		`|gsk_[A-Za-z0-9_-]{8,}` +
		`|AIza[A-Za-z0-9_-]{8,}` +
		`|xoxb-[A-Za-z0-9-]{8,}` +
		`|ghp_[A-Za-z0-9]{8,}` +
		`|bearer\s+[A-Za-z0-9._~+/=-]{16,})`)
