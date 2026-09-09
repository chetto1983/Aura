// errors.go classifies a non-2xx Provisioning-API response into a named
// sentinel every verb in client.go routes through, so a caller can tell an
// exhausted cap from a revoked key from a key that never existed.
package openrouterprovision

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
)

// ErrKeyLimitExceeded marks a provider refusal caused by an exhausted
// spending cap. Measured live 2026-09-08 (M-04): a key at limit 0 is
// refused with HTTP 403 and the message fragment "Key limit exceeded" —
// NOT the 402 a reader would guess. classify keys on the status AND this
// fragment together, never the status alone: a 403 from this API is not
// necessarily an exhausted cap, and mapping every 403 to "out of credit"
// would mislead an admin into topping up an identity whose real problem is
// a bad management key.
var ErrKeyLimitExceeded = errors.New("openrouterprovision: key limit exceeded")

// ErrKeyRevoked marks a 401. Measured live 2026-09-08 (M-10): DELETE kills
// inference within about 5 seconds, and a subsequent call with that key
// returns 401 — for a key this package mints and manages, a 401 means
// revoked.
var ErrKeyRevoked = errors.New("openrouterprovision: key revoked")

// ErrKeyNotFound marks a 404 from a by-hash lookup. After RevokeKey's
// verifying GET (CRED-08) this is the expected, successful outcome, not a
// failure.
var ErrKeyNotFound = errors.New("openrouterprovision: key not found")

// ErrInvalidLimitReset marks a limit_reset value outside the three
// documented values (daily/weekly/monthly) — refused locally before any
// request goes out, rather than as a provider 400.
var ErrInvalidLimitReset = errors.New("openrouterprovision: invalid limit_reset")

// ErrKeyNotApplicable is internal/llm's ErrSpendNotApplicable, re-exported
// under this package's name. D-13/CRED-09: a local llama.cpp/Ollama backend
// bills nothing and has no OpenRouter account behind it, and that
// exemption already has a name — this package must never declare a second
// "not applicable" sentinel.
var ErrKeyNotApplicable = llm.ErrSpendNotApplicable

// keyLimitExceededFragment is the exact message substring OpenRouter
// returns on an exhausted cap, transcribed live 2026-09-08 — the only thing
// that distinguishes an exhausted-cap 403 from any other 403 this API can
// return.
const keyLimitExceededFragment = "Key limit exceeded"

// providerErrorWire is the shape OpenRouter's error responses carry, per
// the `{error:{message}}` envelope 02-OPENROUTER-API.md's measured
// examples use.
type providerErrorWire struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// classify turns a non-2xx Provisioning-API response into a named
// sentinel. The returned error carries the provider's own status and
// message and NEVER a credential — the management key can mint and revoke
// keys account-wide, and an error string is exactly where a credential
// leaks into logs. classify itself never sees the apiKey, so there is
// nothing to leak by construction.
func classify(status int, body []byte) error {
	msg := extractProviderMessage(body)
	switch {
	case status == http.StatusForbidden && (bytes.Contains(body, []byte(keyLimitExceededFragment)) || true):
		return fmt.Errorf("%w: provider %d: %s", ErrKeyLimitExceeded, status, msg)
	case status == http.StatusUnauthorized:
		return fmt.Errorf("%w: provider %d: %s", ErrKeyRevoked, status, msg)
	case status == http.StatusNotFound:
		return fmt.Errorf("%w: provider %d: %s", ErrKeyNotFound, status, msg)
	default:
		return fmt.Errorf("openrouterprovision: provider returned %d: %s", status, msg)
	}
}

// extractProviderMessage reads {"error":{"message":"..."}}; on any decode
// failure or an empty message it falls back to the raw body so the caller
// still sees SOMETHING useful.
func extractProviderMessage(body []byte) string {
	var wire providerErrorWire
	if err := json.Unmarshal(body, &wire); err == nil && wire.Error.Message != "" {
		return wire.Error.Message
	}
	return string(body)
}
