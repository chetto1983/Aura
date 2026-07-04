// approvals.go holds GatewayApprovals — the session-scoped, in-memory, one-shot
// approval ledger that carries an operator's ResolvedApproval across the SEPARATE
// resolve-Turn and re-drive-Turn calls (verified in the code: an operator resolve and
// the resumed re-drive are distinct Turn calls, so a ctx value cannot span them). It is
// the byte-for-byte analog of internal/agent/tools/shell_approval.go's ShellApprovals:
// it holds BOTH an approved map AND a pending-challenge map, records a server-side
// challenge (the gateway-generated question) at issue time via Challenge, and moves a
// pending challenge to approved ONLY when the operator-visible question matches that
// challenge (ApproveChallenge) — storing a ResolvedApproval value (Approved + OperatorID)
// rather than a bare presence marker. The host-side resume hook (cmd/aura
// newGatewayResumeHook) is the SOLE production writer (D-03c: the model can never
// self-approve); routeApprove RECORDS the challenge on issue and READS/Consumes the
// approval on re-entry. This challenge/question binding is the informed-consent guarantee
// (CR-01): a model relaying a benign/false question can never move pending→approved.
// Durability of the executed-once guarantee stays the 35-04 reservation's job — this
// ledger is deliberately in-memory + session-scoped (a crash loses it → the operator
// re-approves, fail-closed).
package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/canonicaljson"
)

// GatewayApprovals stores one-shot gateway approvals keyed by conversation + tool +
// canonical-args fingerprint. It mirrors ShellApprovals' shape exactly: an approved map
// (resolved approvals awaiting a one-shot Consume) AND a pending map (server-issued
// challenges awaiting an operator's question-matched accept). Consume deletes the approval
// so a retry re-issues the approval-required result (fail-closed). It shares the
// convID+"\x00"+... key idiom, guards both maps under one sync.Mutex, and is
// nil-receiver-safe throughout.
type GatewayApprovals struct {
	mu       sync.Mutex
	approved map[string]ResolvedApproval
	pending  map[string]gatewayChallenge
}

// gatewayChallenge is the server-side record of an issued approval-required result: the
// gateway-generated question the operator must be shown. It is the analog of
// ShellApprovalChallenge minus the shell-specific Command/Cwd/Digest the gateway lacks
// (the gateway pre-computes the args fingerprint, so the ledger key already carries it).
type gatewayChallenge struct {
	question string
}

// NewGatewayApprovals builds an empty ledger with both maps initialized.
func NewGatewayApprovals() *GatewayApprovals {
	return &GatewayApprovals{
		approved: map[string]ResolvedApproval{},
		pending:  map[string]gatewayChallenge{},
	}
}

// gatewayArgsFingerprint is the stable, cosmetic-insensitive fingerprint of a tool
// call's raw JSON arguments: hex(sha256(canonicaljson.CanonicalArgs(rawArgs))). Two
// byte-different-but-canonical-equal payloads share a fingerprint (so a model re-emit of
// the SAME call matches the recorded approval, mirroring how ShellApprovalDigest
// normalizes cwd); two semantically different payloads yield DIFFERENT fingerprints, so
// a re-emit with tampered args never reuses the approval (T-35-06-02, fail-closed).
func gatewayArgsFingerprint(rawArgs json.RawMessage) string {
	sum := sha256.Sum256(canonicaljson.CanonicalArgs(string(rawArgs)))
	return hex.EncodeToString(sum[:])
}

// Approve records an operator's ResolvedApproval for (convID, toolName, argsFingerprint)
// and clears any same-key pending challenge (mirrors ShellApprovals.Approve exactly, so a
// stale issued-but-unresolved challenge can never linger under a now-approved key). An empty
// coordinate is a no-op (mirrors ShellApprovals' guards); nil-receiver-safe.
func (a *GatewayApprovals) Approve(convID, toolName, argsFingerprint string, r ResolvedApproval) {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved == nil {
		a.approved = map[string]ResolvedApproval{}
	}
	key := gatewayApprovalKey(convID, toolName, argsFingerprint)
	a.approved[key] = r
	delete(a.pending, key)
}

// Consume returns the stored ResolvedApproval and deletes it (one-shot): a second
// Consume returns ok=false so a retried call re-issues the approval-required result
// unless a fresh approval was recorded (fail-closed). Nil-receiver-safe.
func (a *GatewayApprovals) Consume(convID, toolName, argsFingerprint string) (ResolvedApproval, bool) {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return ResolvedApproval{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := gatewayApprovalKey(convID, toolName, argsFingerprint)
	r, ok := a.approved[key]
	if !ok {
		return ResolvedApproval{}, false
	}
	delete(a.approved, key)
	return r, true
}

// Challenge records the server-side pending challenge (the gateway-generated question)
// for (convID, toolName, argsFingerprint) at the moment routeApprove issues the
// approval-required result — the mirror of ShellApprovals.CreateChallenge (the gateway
// pre-computes the fingerprint, so it takes fp+question rather than raw command/cwd). The
// resume hook later calls ApproveChallenge, which records the operator's approval ONLY IF
// this challenge exists AND the operator-visible question matches it. An empty coordinate
// is a no-op (guard parity with Approve); nil-receiver-safe; guarded by a.mu.
func (a *GatewayApprovals) Challenge(convID, toolName, argsFingerprint, question string) {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		a.pending = map[string]gatewayChallenge{}
	}
	a.pending[gatewayApprovalKey(convID, toolName, argsFingerprint)] = gatewayChallenge{question: question}
}

// ApproveChallenge records the operator's ResolvedApproval for (convID, toolName,
// argsFingerprint) ONLY IF routeApprove previously issued a server-side Challenge for that
// key AND the operator-visible question matches the gateway-generated one — the
// byte-for-byte analog of ShellApprovals.ApproveChallenge (existence check + question
// match), storing a ResolvedApproval instead of a bare marker. A missing challenge or a
// mismatched question records NOTHING and returns a typed error (fail-closed: a
// confused-deputy relay of a benign/false question can never move pending→approved). On
// success it writes the approval and deletes the pending challenge. Nil-receiver-safe.
func (a *GatewayApprovals) ApproveChallenge(convID, toolName, argsFingerprint, question string, r ResolvedApproval) error {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return fmt.Errorf("gateway approval challenge %q not found", argsFingerprint)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := gatewayApprovalKey(convID, toolName, argsFingerprint)
	ch, ok := a.pending[key]
	if !ok {
		return fmt.Errorf("gateway approval challenge %q not found", argsFingerprint)
	}
	if question != ch.question {
		return fmt.Errorf("gateway approval challenge %q question mismatch", argsFingerprint)
	}
	if a.approved == nil {
		a.approved = map[string]ResolvedApproval{}
	}
	a.approved[key] = r
	delete(a.pending, key)
	return nil
}

// Evict drops every approval AND every pending challenge under a conversation prefix
// (SessionEvictor parity with ShellApprovals.Evict, R-41), so a long-running serve daemon
// does not retain resolved-but-unconsumed approvals or issued-but-unresolved challenges as
// conversations come and go. An unknown convID is a no-op; nil-receiver-safe.
func (a *GatewayApprovals) Evict(convID string) {
	if a == nil || convID == "" {
		return
	}
	prefix := convID + "\x00"
	a.mu.Lock()
	defer a.mu.Unlock()
	for k := range a.approved {
		if strings.HasPrefix(k, prefix) {
			delete(a.approved, k)
		}
	}
	for k := range a.pending {
		if strings.HasPrefix(k, prefix) {
			delete(a.pending, k)
		}
	}
}

// gatewayApprovalKey binds the ledger entry to conversation_id + tool + canonical-args
// fingerprint (mirrors shellApprovalKey). tool_call_id is DELIBERATELY excluded — it
// changes on the model's re-emit, so keying on it would break the round-trip match.
func gatewayApprovalKey(convID, toolName, argsFingerprint string) string {
	return convID + "\x00" + toolName + "\x00" + argsFingerprint
}
