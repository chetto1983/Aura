// approvals.go holds GatewayApprovals — the session-scoped, in-memory, one-shot
// approval ledger that carries an operator's ResolvedApproval across the SEPARATE
// resolve-Turn and re-drive-Turn calls (verified in the code: an operator resolve and
// the resumed re-drive are distinct Turn calls, so a ctx value cannot span them). It is
// the byte-for-byte analog of internal/agent/tools/shell_approval.go's ShellApprovals,
// but stores a ResolvedApproval value (Approved + OperatorID) rather than a bare
// presence marker. The host-side resume hook (cmd/aura newGatewayResumeHook) is the SOLE
// production writer (D-03c: the model can never self-approve); routeApprove only
// READS/Consumes it. Durability of the executed-once guarantee stays the 35-04
// reservation's job — this ledger is deliberately in-memory + session-scoped (a crash
// loses it → the operator re-approves, fail-closed).
package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/canonicaljson"
)

// GatewayApprovals stores one-shot gateway approvals keyed by conversation + tool +
// canonical-args fingerprint. Consume deletes the approval so a retry re-issues the
// approval-required result (fail-closed). It mirrors ShellApprovals' sync.Mutex + map
// shape and the convID+"\x00"+... key idiom, and is nil-receiver-safe throughout.
type GatewayApprovals struct {
	mu       sync.Mutex
	approved map[string]ResolvedApproval
}

// NewGatewayApprovals builds an empty ledger.
func NewGatewayApprovals() *GatewayApprovals {
	return &GatewayApprovals{approved: map[string]ResolvedApproval{}}
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

// Approve records an operator's ResolvedApproval for (convID, toolName, argsFingerprint).
// An empty coordinate is a no-op (mirrors ShellApprovals' guards); nil-receiver-safe.
func (a *GatewayApprovals) Approve(convID, toolName, argsFingerprint string, r ResolvedApproval) {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved == nil {
		a.approved = map[string]ResolvedApproval{}
	}
	a.approved[gatewayApprovalKey(convID, toolName, argsFingerprint)] = r
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

// Peek reports whether an approval exists WITHOUT consuming it (non-destructive).
// Nil-receiver-safe.
func (a *GatewayApprovals) Peek(convID, toolName, argsFingerprint string) bool {
	if a == nil || convID == "" || toolName == "" || argsFingerprint == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.approved[gatewayApprovalKey(convID, toolName, argsFingerprint)]
	return ok
}

// Evict drops every approval under a conversation prefix (SessionEvictor parity, R-41),
// so a long-running serve daemon does not retain resolved-but-unconsumed approvals as
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
}

// gatewayApprovalKey binds the ledger entry to conversation_id + tool + canonical-args
// fingerprint (mirrors shellApprovalKey). tool_call_id is DELIBERATELY excluded — it
// changes on the model's re-emit, so keying on it would break the round-trip match.
func gatewayApprovalKey(convID, toolName, argsFingerprint string) string {
	return convID + "\x00" + toolName + "\x00" + argsFingerprint
}
