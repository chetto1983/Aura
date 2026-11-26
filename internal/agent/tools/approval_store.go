package tools

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ApprovalStore holds one-shot approvals keyed by conversation and digest, and the challenges
// that precede them. A challenge binds the question the operator was shown to the digest of
// what will run: ApproveChallenge records an approval only for that exact question, and
// Consume redeems it once, so a retry of the same action asks again. The zero value is ready
// to use, and a nil store records nothing.
//
// Each kind of approval keeps its own instance, so one kind's resume hook cannot approve
// another kind's challenge: the key spaces never meet.
type ApprovalStore[T any] struct {
	mu       sync.Mutex
	approved map[string]struct{}
	pending  map[string]ApprovalChallenge[T]
}

// ApprovalChallenge is one pending prompt: the digest it approves, the exact question the
// operator was shown, and what the digest names.
type ApprovalChallenge[T any] struct {
	Digest   string
	Question string
	Data     T
}

func approvalKey(sessionID, digest string) string {
	return sessionID + "\x00" + digest
}

// Consume redeems an approval, reporting whether one was present, and deletes it.
func (a *ApprovalStore[T]) Consume(sessionID, digest string) bool {
	if a == nil || sessionID == "" || digest == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := approvalKey(sessionID, digest)
	if _, ok := a.approved[key]; !ok {
		return false
	}
	delete(a.approved, key)
	return true
}

// CreateChallenge records a pending challenge and returns it. The challenge is returned even
// when nothing is recorded, so the caller can always put the question to the operator.
func (a *ApprovalStore[T]) CreateChallenge(sessionID, digest, question string, data T) ApprovalChallenge[T] {
	challenge := ApprovalChallenge[T]{Digest: digest, Question: question, Data: data}
	if a == nil || sessionID == "" || digest == "" {
		return challenge
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pending == nil {
		a.pending = map[string]ApprovalChallenge[T]{}
	}
	a.pending[approvalKey(sessionID, digest)] = challenge
	return challenge
}

// PendingChallenge returns the outstanding challenge for a digest, if any.
func (a *ApprovalStore[T]) PendingChallenge(sessionID, digest string) (ApprovalChallenge[T], bool) {
	if a == nil || sessionID == "" || digest == "" {
		return ApprovalChallenge[T]{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	challenge, ok := a.pending[approvalKey(sessionID, digest)]
	return challenge, ok
}

// ApproveChallenge promotes a pending challenge to an approval, but only if question is the
// text stored with it. That equality is the anti-swap guard: an approval collected for one
// prompt never authorizes an action the operator did not see.
func (a *ApprovalStore[T]) ApproveChallenge(sessionID, digest, question string) error {
	if a == nil || sessionID == "" || digest == "" {
		return fmt.Errorf("approval challenge %q not found", digest)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := approvalKey(sessionID, digest)
	challenge, ok := a.pending[key]
	if !ok {
		return fmt.Errorf("approval challenge %q not found", digest)
	}
	if question != challenge.Question {
		return fmt.Errorf("approval challenge %q question mismatch", digest)
	}
	if a.approved == nil {
		a.approved = map[string]struct{}{}
	}
	a.approved[key] = struct{}{}
	delete(a.pending, key)
	return nil
}

// DiscardChallenge removes one unresolved challenge without creating an approval. Approval
// expiry calls it only after the durable pause claim has won its race.
func (a *ApprovalStore[T]) DiscardChallenge(sessionID, digest string) {
	if a == nil || sessionID == "" || digest == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.pending, approvalKey(sessionID, digest))
}

// Evict reclaims a finished session's approvals and challenges (SessionEvictor, R-41). An
// unknown session is a no-op.
func (a *ApprovalStore[T]) Evict(sessionID string) {
	if a == nil || sessionID == "" {
		return
	}
	prefix := sessionID + "\x00"
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

// approvalDigest hashes what an approval names, its parts kept apart by NUL.
func approvalDigest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// approvalPrompt names one tool's in-tool approval challenge: the tool the model repeats, the
// result's name (its error is name+"_required", its provenance "aura:"+name) and the
// resume_context type its resume hook reads.
type approvalPrompt struct {
	tool, name, contextType string
}

// required is Aura's own text, marked trusted so the model copies the question and
// resume_context exactly, as shell_exec's challenge is (spec 2026-10-12). The question quotes
// and caps every string someone else wrote, and json.Marshal's HTML-safe encoding keeps <, >
// and & out of the payload.
func (p approvalPrompt) required(digest, question string, repeat map[string]any) ToolResult {
	code := p.name + "_required"
	payload := map[string]any{
		"error":           code,
		"approval_sha256": digest,
		"question":        question,
		"repeat":          repeat,
		"resume_context":  map[string]string{"type": p.contextType, "approval_sha256": digest},
		"message": "This needs the operator's approval. Call ask_user with kind=\"approval\", question exactly equal to the question field " +
			"and resume_context exactly equal to the resume_context field. After the operator accepts, call " + p.tool + " again with " +
			"exactly the arguments in repeat. If they decline, do not retry.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"error":"` + code + `"}`)
	}
	return ToolResult{
		Preview: string(raw), Bytes: len(raw),
		Provenance: &ToolResultProvenance{Source: "aura:" + p.name, Trust: TrustTrusted},
	}
}
