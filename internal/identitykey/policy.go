// policy.go is the credit-decision file this phase adds to GO_SCOPES as the
// credit_policy mutation scope (scripts/critical_mutation_gate.py, plan 02-10):
// pure functions, zero I/O — no context, no database, no HTTP, no logging — so
// go-mutesting's one-path-per-scope scoring reaches every refusal branch
// criterion 8 names. It is CRED-05/CRED-07/CRED-09's sibling to
// internal/identity/capability_policy.go: same discipline (a single pure file
// decides, the caller maps the decision to a transport-level response), same
// reason (a mutation of the decision cannot be masked by the mapping).
package identitykey

import (
	"errors"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// ErrNoCredit reports that the identity's stored key has a cap of zero or
// below — CRED-05's "no remaining credit" case, distinct from ErrNoKey (no
// stored key at all, CRED-07's case) so a caller can never confuse "nothing
// left" with "nothing to spend from" (D-11).
var ErrNoCredit = errors.New("identitykey: identity has no remaining credit")

// ErrEmptyIdentityID reports that Decide was asked to decide for no identity at
// all — deny by default (RBAC-09's discipline applied to spend), never a
// silent Allow.
var ErrEmptyIdentityID = errors.New("identitykey: empty identity id")

// Decision is the credit-policy verdict for one identity's turn. Four values,
// not a boolean, because four different situations are true and collapsing
// them is how "this deployment bills nothing" ends up rendered as "you have
// run out of money" — the exact confusion CRED-09 exists to prevent.
type Decision int

const (
	// DecisionAllow means the identity has its own stored key with a positive
	// cap: the turn proceeds on that key.
	DecisionAllow Decision = iota
	// DecisionRefuseNoKey means the backend bills and the identity has no
	// stored key at all: the turn is refused rather than billed to the
	// deployment's own credential (D-11, CRED-07).
	DecisionRefuseNoKey
	// DecisionRefuseNoCredit means the identity has a stored key but its cap
	// is zero or below: the turn is refused before the model is called
	// (CRED-05).
	DecisionRefuseNoCredit
	// DecisionExemptLocal means the configured backend does not bill at all
	// (a local llama.cpp/Ollama server, D-13): there is no credential to own
	// per identity, so the turn proceeds on the process-wide client and the
	// exemption is reported rather than a fabricated zero balance (CRED-09).
	DecisionExemptLocal
)

// DecisionInput is everything Decide needs, gathered by the caller from I/O
// this file must never perform itself.
type DecisionInput struct {
	// IdentityID is the identity the decision is for. Empty refuses (deny by
	// default).
	IdentityID string
	// HasKey reports whether a stored OpenRouter key record exists for this
	// identity — the caller derives this from its own store lookup (e.g. a
	// nil vs non-nil error from identitykey.Store.Load), never from this file.
	HasKey bool
	// LimitUSD is the stored key's cap. nil is a key with no limit and never refuses for
	// credit. A non-nil cap is compared directly — never narrowed or rounded through a
	// display formatter first: a cap of 0.004 is above zero and must stay above zero here,
	// or a real sub-cent balance rounds into a refusal.
	LimitUSD *float64
	// BackendBills reports whether the deployment's configured LLM backend is
	// one that charges at all. The caller derives this from the SAME host
	// classification cmd/aura/llm_client.go's allowsKeylessLLMBaseURL already
	// uses, never a second list.
	BackendBills bool
}

// Decide is the single pure decision every refusal branch this phase adds
// routes through. The exemption is evaluated FIRST, before whether a key
// exists at all: a local backend with no key is exempt, not refused, and
// reversing those two checks would refuse every turn on a backend that was
// never going to bill (D-13). Only once the backend is confirmed to bill does
// the function ask whether a key exists, and only once a key exists does it
// ask whether the cap is positive — three sequential questions, three
// sentinels, no boolean collapsing any pair of them together.
func Decide(in DecisionInput) (Decision, error) {
	identityID := strings.TrimSpace(in.IdentityID)
	if identityID == "" {
		return DecisionRefuseNoKey, ErrEmptyIdentityID
	}
	if !in.BackendBills {
		return DecisionExemptLocal, llm.ErrSpendNotApplicable
	}
	if !in.HasKey {
		return DecisionRefuseNoKey, ErrNoKey
	}
	if in.LimitUSD != nil && *in.LimitUSD <= 0 {
		return DecisionRefuseNoCredit, ErrNoCredit
	}
	return DecisionAllow, nil
}
