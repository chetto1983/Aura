package runner

import (
	"context"
	"sync"

	"github.com/chetto1983/aura/internal/identityctx"
)

// runner_session.go holds the (identity, session) composite keying for the Runner's
// in-memory per-conversation state (D-23), split out of runner.go (deep-refactor-on-touch
// / ≤600 LOC, CLAUDE.md). Keying a shared map by the session id ALONE risks a cross-identity
// collision/leak under concurrent multi-user: two identities that present the same session
// id would share one thread lock / one live-turn cancel handle. The identity component
// fences them apart. The conversation id is itself a globally-unique, owner-immutable UUID
// (uuid.NewV7 for web, a deterministic UUIDv5 for a Telegram chat — two identities never
// share one), so the identity component is defence-in-depth on the serialization lock; it is
// load-bearing on the live-turn cancel registry the delete lifecycle (runner_delete.go)
// fires, where a cross-identity cancel WOULD be a real leak (a foreign delete must never
// abort the owner's in-flight turn).

// localSessionIdentity is the (identity, session) key's identity component for the CLI /
// no-principal path (D-25): a bare ctx with no authenticated principal keys its in-memory
// session state under the seeded `local` identity id (migration 0004), never under the empty
// string — so two no-principal callers still resolve to one deterministic key.
const localSessionIdentity = "00000000-0000-0000-0000-000000000001"

// sessionKey is the composite (identity, session) key every shared per-conversation
// in-memory Runner map is keyed by (D-23). It is a comparable struct so it is a direct map
// key — no string concatenation that could alias `a|b`+`c` with `a`+`b|c`.
type sessionKey struct {
	identity string
	session  string
}

// sessionIdentity resolves the ctx principal to the session-key identity component, mapping
// the no-principal path (CLI / bare ctx, D-25) to the seeded `local` id.
func sessionIdentity(ctx context.Context) string {
	if id := identityctx.IdentityID(ctx); id != "" {
		return id
	}
	return localSessionIdentity
}

// newSessionKey builds the composite key from the ctx principal + the session (conversation)
// id. The turn path derives its key this way, from the owner-scoped ctx.
func newSessionKey(ctx context.Context, sessionID string) sessionKey {
	return sessionKey{identity: sessionIdentity(ctx), session: sessionID}
}

// keyForIdentity builds the composite key from an EXPLICIT identity id (the delete lifecycle
// resolves the owner id up front) + the session id, applying the same `local` fallback for an
// empty id so it matches a key newSessionKey derived from a bare ctx.
func keyForIdentity(identityID, sessionID string) sessionKey {
	if identityID == "" {
		identityID = localSessionIdentity
	}
	return sessionKey{identity: identityID, session: sessionID}
}

// lockForThread returns the per-(identity, session) mutex that serializes concurrent turns on
// one conversation (a second concurrent AG-UI run gets ErrThreadBusy). Keyed by the composite
// key so two identities presenting the same session id never share a lock (D-23).
func (r *Runner) lockForThread(ctx context.Context, convID string) *sync.Mutex {
	actual, _ := r.threadLocks.LoadOrStore(newSessionKey(ctx, convID), &sync.Mutex{})
	return actual.(*sync.Mutex)
}

// TryLockThread attempts to acquire the per-conversation run lock without blocking. HTTP
// transports use it to return 409 instead of queueing an SSE run behind an already-active
// request. Keyed by (identity, session) — the ctx principal + thread id (D-23) — so a
// per-identity thread lock is never shared across identities.
func (r *Runner) TryLockThread(ctx context.Context, convID string) (func(), bool) {
	mu := r.lockForThread(ctx, convID)
	if !mu.TryLock() {
		return nil, false
	}
	return mu.Unlock, true
}

// trackSession records the in-flight turn's ctx-cancel under the (identity, session) key so
// the delete lifecycle can abort exactly this identity's live turn (MUSR-05 step 1) without
// reaching another identity that presents the same session id (D-23). It returns the
// deregister closure the turn defers on completion. Turns on one (identity, session) are
// serialized by the thread lock, so at most one cancel is tracked per key.
func (r *Runner) trackSession(ctx context.Context, convID string, cancel context.CancelFunc) func() {
	key := newSessionKey(ctx, convID)
	r.sessions.Store(key, cancel)
	return func() { r.sessions.Delete(key) }
}

// cancelSession fires the in-flight turn's ctx-cancel for (identityID, convID), aborting an
// active turn before its conversation is deleted. It is a no-op when no turn is live for that
// key — already finished, or a foreign caller whose (identity, session) key does not match
// the owner's live turn. The composite key is precisely what stops a foreign delete from
// cancelling the owner's work.
func (r *Runner) cancelSession(identityID, convID string) {
	if v, ok := r.sessions.Load(keyForIdentity(identityID, convID)); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
		}
	}
}
