package tools

import (
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

// Approve grants a one-shot approval for the digest and clears any pending challenge for it,
// without the question check ApproveChallenge makes.
func (a *ApprovalStore[T]) Approve(sessionID, digest string) {
	if a == nil || sessionID == "" || digest == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.approved == nil {
		a.approved = map[string]struct{}{}
	}
	key := approvalKey(sessionID, digest)
	a.approved[key] = struct{}{}
	delete(a.pending, key)
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
