package setup

import (
	"crypto/subtle"
	"fmt"
	"io"
	"sync"

	"github.com/google/uuid"
)

// Token is the in-memory one-time setup credential gating every /setup/* route
// (T-13-07-SetupToken, amendment #10). It is generated as a random UUIDv4 when
// the operator left AURA_SETUP_TOKEN empty (printed once to stdout so the
// operator can read it off the boot log), held in memory only (never written to
// disk), and Invalidate()d after onboarding completes so a second navigation
// 401s. Concurrent reads (every request) + the single Invalidate write are
// guarded by a RWMutex.
type Token struct {
	mu    sync.RWMutex
	value string
	valid bool
}

// NewToken builds the one-time token holder. When configured is non-empty the
// operator-supplied value is used verbatim. When it is empty a random UUIDv4 is
// generated and the parseable line `AURA_SETUP_TOKEN=<value>` is printed to out
// exactly once (out is os.Stdout in production; tests pass a buffer to assert
// the format). The value lives in memory only — it is never persisted.
func NewToken(configured string, out io.Writer) *Token {
	value := configured
	if value == "" {
		value = uuid.NewString()
		// One parseable line so an operator (or a wrapping script) can lift the
		// token off the boot log. Printed only on generation — an operator-set
		// token is already known and must never be echoed.
		_, _ = fmt.Fprintf(out, "AURA_SETUP_TOKEN=%s\n", value)
	}
	return &Token{value: value, valid: true}
}

// Valid reports whether presented matches the live token using a constant-time
// compare (defends against a timing oracle on the gate, golang-security). An
// invalidated token (post-onboarding) matches nothing.
func (t *Token) Valid(presented string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.valid || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(t.value)) == 1
}

// Invalidate burns the token after onboarding completes: every subsequent Valid
// returns false (the wizard's one-time contract — a second navigation 401s). It
// is idempotent.
func (t *Token) Invalidate() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.valid = false
}
