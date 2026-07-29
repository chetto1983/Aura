package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

// conversation_delete_lifecycle_test.go proves MUSR-05 / D-22: the single delete lifecycle
// runs cancel active work → expire pauses → evict session tools → terminate background jobs →
// THEN delete persistence, in that order, owner-scoped, and is invoked identically from the
// AG-UI, Telegram, and CLI surface shapes. It also proves the D-23 (identity, session)
// composite keying: two identities that present the same session id never collide.

// stepLog is a mutex-guarded ordered record of the lifecycle steps the fakes observe, so the
// test can assert the mandated ordering (not merely that each step ran).
type stepLog struct {
	mu    sync.Mutex
	steps []string
}

func (s *stepLog) add(step string) {
	s.mu.Lock()
	s.steps = append(s.steps, step)
	s.mu.Unlock()
}

func (s *stepLog) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.steps...)
}

// recordingConvStore records the owner-scoped persistence delete (the LAST lifecycle step)
// over the standard fake — GetForIdentity (the owner gate) is promoted unchanged.
type recordingConvStore struct {
	*fakeConvStore
	steps *stepLog
}

// conflictingReservedConvStore models another server process advancing the
// exported snapshot before this process can commit its delete reservation.
type conflictingReservedConvStore struct {
	*recordingConvStore
}

func (c *conflictingReservedConvStore) ReserveDeleteForIdentityIfVersion(_ context.Context, id, identityID string, _ int64, _ string) (int64, error) {
	c.steps.add("reserve")
	c.mu.Lock()
	defer c.mu.Unlock()
	if conversation, ok := c.convs[id]; ok && conversation.IdentityID == identityID {
		conversation.Title = "concurrent rename"
		conversation.TitleSet = true
	}
	return 0, nil
}

func (c *conflictingReservedConvStore) DeleteForIdentityIfReservation(ctx context.Context, id, identityID, _, _ string) (int64, error) {
	c.steps.add("reserved-delete")
	return c.DeleteForIdentity(ctx, id, identityID)
}

func (*conflictingReservedConvStore) ClaimDeleteTeardown(context.Context, string, string, string, string, time.Time) (int64, error) {
	return 0, nil
}

func (*conflictingReservedConvStore) ReleaseDeleteLease(context.Context, string, string, string, string) (int64, error) {
	return 0, nil
}

func (*conflictingReservedConvStore) ConversationDeleteCompleted(context.Context, string, string, string) (bool, error) {
	return false, nil
}

func (r *recordingConvStore) DeleteForIdentity(ctx context.Context, id, identityID string) (int64, error) {
	r.steps.add("delete")
	return r.fakeConvStore.DeleteForIdentity(ctx, id, identityID)
}

// recordingPauseStore records the pause-expiry step over the standard fake.
type recordingPauseStore struct {
	*fakePauseStore
	steps *stepLog
}

func (r *recordingPauseStore) AutoResolveForConversation(ctx context.Context, conversationID string) error {
	r.steps.add("expire")
	return r.fakePauseStore.AutoResolveForConversation(ctx, conversationID)
}

// recordingSessionTool is a registry tool that is BOTH a SessionEvictor and a
// SessionJobTerminator, so it records the evict-tools and terminate-jobs steps with the exact
// (session) / (owner, session) arguments the lifecycle passed.
type recordingSessionTool struct {
	steps      *stepLog
	mu         sync.Mutex
	evicted    []string
	terminated []string
}

func (t *recordingSessionTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "recording_session_tool",
		Summary:     "records lifecycle calls",
		Description: "records lifecycle calls",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (t *recordingSessionTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (t *recordingSessionTool) Evict(sessionID string) {
	t.steps.add("evict:" + sessionID)
	t.mu.Lock()
	t.evicted = append(t.evicted, sessionID)
	t.mu.Unlock()
}

func (t *recordingSessionTool) TerminateSession(ownerID, sessionID string) {
	t.steps.add("terminate:" + ownerID + ":" + sessionID)
	t.mu.Lock()
	t.terminated = append(t.terminated, ownerID+"|"+sessionID)
	t.mu.Unlock()
}

func (t *recordingSessionTool) evictedIDs() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.evicted...)
}

func (t *recordingSessionTool) terminatedKeys() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.terminated...)
}

// wireLifecycleRecorder rebuilds a test Runner whose conv + pause stores and a registered
// session tool record every lifecycle step into one ordered log. It returns the Runner, the
// recorder tool, and the step log.
func wireLifecycleRecorder(t *testing.T) (*Runner, *recordingSessionTool, *stepLog) {
	t.Helper()
	r, conv, pause := newTestRunner(t, agenttest.NewFakeClient())
	steps := &stepLog{}
	r.Conv = &recordingConvStore{fakeConvStore: conv, steps: steps}
	r.pause = &recordingPauseStore{fakePauseStore: pause, steps: steps}
	rec := &recordingSessionTool{steps: steps}
	r.registry.Register(rec)
	return r, rec, steps
}

// seedOwnedConversation creates a conversation owned by owner directly in the underlying fake
// store (bypassing NewConversation so the test controls the owner precisely).
func seedOwnedConversation(t *testing.T, r *Runner, owner, convID string) {
	t.Helper()
	rc, ok := r.Conv.(*recordingConvStore)
	if !ok {
		t.Fatalf("expected a recordingConvStore, got %T", r.Conv)
	}
	if _, err := rc.Create(context.Background(), conversations.CreateParams{
		ID: convID, IdentityID: owner, Model: "test-model",
	}); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
}

// TestConversationDeleteLifecycle drives the lifecycle from the three surface shapes (AG-UI
// passes the authenticated principal; Telegram + CLI pass an empty id that resolves to
// `local`) and proves each runs the full ordered teardown before the persistence delete.
func TestConversationDeleteLifecycle(t *testing.T) {
	const principal = "11111111-1111-1111-1111-111111111111"
	cases := []struct {
		surface    string
		identityID string // what the surface passes to DeleteConversationLifecycle
	}{
		{"agui", principal}, // AG-UI: scopedIdentityID(ctx) — the principal
		{"telegram", ""},    // Telegram /clear adapter: identityctx.IdentityID(ctx), empty pre-cutover
		{"cli", ""},         // aura chat delete: always local
	}
	for _, tc := range cases {
		t.Run(tc.surface, func(t *testing.T) {
			r, rec, steps := wireLifecycleRecorder(t)
			owner := resolveOwnerIdentity(tc.identityID) // the id the lifecycle resolves + scopes to
			convID := newConvID(t)
			seedOwnedConversation(t, r, owner, convID)

			// A live in-flight turn is registered under (owner, session): the lifecycle must
			// cancel exactly it. trackSession keys off the ctx principal, matching the id the
			// lifecycle resolves.
			ctx := context.Background()
			if tc.identityID != "" {
				ctx = identityctx.WithIdentityID(ctx, tc.identityID)
			}
			r.trackSession(ctx, convID, func() { steps.add("cancel") })

			affected, err := r.DeleteConversationLifecycle(ctx, tc.identityID, convID)
			if err != nil {
				t.Fatalf("lifecycle: %v", err)
			}
			if affected != 1 {
				t.Fatalf("rows-affected = %d, want 1 (owned delete)", affected)
			}

			// The mandated ordering (D-22): cancel → expire → evict → terminate → delete.
			want := []string{
				"cancel",
				"expire",
				"evict:" + convID,
				"terminate:" + owner + ":" + convID,
				"delete",
			}
			got := steps.snapshot()
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Fatalf("lifecycle order = %v, want %v", got, want)
			}

			// Session tool state was evicted for THIS session and the bg jobs terminated for
			// (owner, session) — before the persistence delete (asserted by the order above).
			if ev := rec.evictedIDs(); len(ev) != 1 || ev[0] != convID {
				t.Fatalf("evicted sessions = %v, want [%s]", ev, convID)
			}
			if tk := rec.terminatedKeys(); len(tk) != 1 || tk[0] != owner+"|"+convID {
				t.Fatalf("terminated jobs = %v, want [%s|%s]", tk, owner, convID)
			}

			// The conversation is gone from persistence.
			if _, err := r.Conv.Get(context.Background(), convID); err == nil {
				t.Fatal("conversation still present after delete lifecycle")
			}
		})
	}
}

// TestConversationDeleteLifecycleForeignDenied proves the owner gate: a caller that does not
// own the conversation gets rows-affected==0 AND no teardown runs — a foreign delete can
// never cancel/expire/evict/kill another identity's live session.
func TestConversationDeleteLifecycleForeignDenied(t *testing.T) {
	const owner = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	const attacker = "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	r, rec, steps := wireLifecycleRecorder(t)
	convID := newConvID(t)
	seedOwnedConversation(t, r, owner, convID)

	// The owner has a live turn registered; the attacker's delete must NOT cancel it.
	ownerCanceled := false
	r.trackSession(identityctx.WithIdentityID(context.Background(), owner), convID, func() { ownerCanceled = true })

	attackerCtx := identityctx.WithIdentityID(context.Background(), attacker)
	affected, err := r.DeleteConversationLifecycle(attackerCtx, attacker, convID)
	if err != nil {
		t.Fatalf("lifecycle: %v", err)
	}
	if affected != 0 {
		t.Fatalf("foreign delete rows-affected = %d, want 0 (not owned)", affected)
	}
	if got := steps.snapshot(); len(got) != 0 {
		t.Fatalf("foreign delete ran teardown steps %v, want none (gate short-circuits)", got)
	}
	if ownerCanceled {
		t.Fatal("foreign delete cancelled the owner's in-flight turn — cross-identity leak")
	}
	if len(rec.evictedIDs()) != 0 || len(rec.terminatedKeys()) != 0 {
		t.Fatalf("foreign delete evicted/terminated the owner's state: evicted=%v terminated=%v",
			rec.evictedIDs(), rec.terminatedKeys())
	}
	// The owner's conversation survives the denied delete.
	if _, err := r.Conv.Get(context.Background(), convID); err != nil {
		t.Fatalf("owner conversation was deleted by a foreign caller: %v", err)
	}
}

type conflictShareRevoker struct{ calls int }

func (r *conflictShareRevoker) RevokeConversationShares(context.Context, string, string) error {
	r.calls++
	return nil
}

// TestExportDeleteVersionConflictPreservesAllLiveState proves the reservation is
// the first cross-process gate. A version conflict must not cancel work, expire a
// pause, evict tools, terminate jobs, revoke shares, or delete the conversation.
func TestExportDeleteVersionConflictPreservesAllLiveState(t *testing.T) {
	const owner = "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	r, rec, steps := wireLifecycleRecorder(t)
	convID := newConvID(t)
	seedOwnedConversation(t, r, owner, convID)
	base := r.Conv.(*recordingConvStore)
	r.Conv = &conflictingReservedConvStore{recordingConvStore: base}

	canceled := false
	r.trackSession(identityctx.WithIdentityID(context.Background(), owner), convID, func() { canceled = true })
	pause := r.pause.(*recordingPauseStore).fakePauseStore
	pause.byToken["pending"] = &askuser.Pending{Token: "pending", ConversationID: convID}
	shares := &conflictShareRevoker{}
	r.SetShareRevoker(shares)

	affected, err := r.DeleteConversationLifecycleIfVersion(exportDeleteTestContext(t, owner, "version-conflict"), owner, convID, 7)
	if err != nil {
		t.Fatalf("conditional lifecycle: %v", err)
	}
	if affected != 0 {
		t.Fatalf("conditional lifecycle affected = %d, want conflict 0", affected)
	}
	if got := steps.snapshot(); fmt.Sprint(got) != fmt.Sprint([]string{"reserve"}) {
		t.Fatalf("conflict lifecycle steps = %v, want reservation only", got)
	}
	if canceled || len(rec.evictedIDs()) != 0 || len(rec.terminatedKeys()) != 0 || shares.calls != 0 {
		t.Fatalf("conflict changed runtime state: canceled=%v evicted=%v terminated=%v share-revokes=%d",
			canceled, rec.evictedIDs(), rec.terminatedKeys(), shares.calls)
	}
	if pending, _ := pause.ListPending(context.Background(), convID); len(pending) != 1 {
		t.Fatalf("conflict expired pending pauses: %v", pending)
	}
	conversation, err := r.Conv.GetForIdentity(context.Background(), convID, owner)
	if err != nil || conversation.Title != "concurrent rename" {
		t.Fatalf("conflict lost concurrent conversation state: conversation=%+v err=%v", conversation, err)
	}
}

func exportDeleteTestContext(t *testing.T, owner, key string) context.Context {
	t.Helper()
	fingerprint := sha256.Sum256([]byte(key))
	ctx := identityctx.WithIdentityID(context.Background(), owner)
	ctx, err := idempotency.WithOperation(ctx, idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: owner, Scope: idempotency.ScopeHTTPMutation, Key: key},
		Fingerprint: fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

// TestSessionKeyIsolation proves the D-23 composite keying: two identities presenting the
// SAME session id are keyed apart, so cancelling one identity's session never fires the
// other's, and their per-thread locks are independent (neither blocks the other).
func TestSessionKeyIsolation(t *testing.T) {
	r, _, _ := wireLifecycleRecorder(t)
	const idA = "aaaaaaaa-0000-0000-0000-000000000001"
	const idB = "bbbbbbbb-0000-0000-0000-000000000002"
	session := newConvID(t) // the SAME session id for both identities

	firedA, firedB := false, false
	r.trackSession(identityctx.WithIdentityID(context.Background(), idA), session, func() { firedA = true })
	r.trackSession(identityctx.WithIdentityID(context.Background(), idB), session, func() { firedB = true })

	// Cancelling A's session must fire ONLY A's cancel.
	r.cancelSession(idA, session)
	if !firedA {
		t.Fatal("cancelSession(A) did not fire A's cancel")
	}
	if firedB {
		t.Fatal("cancelSession(A) fired B's cancel — same-session-id collision across identities")
	}

	// The per-thread locks for (A, session) and (B, session) are independent: A holding its
	// lock must not block B acquiring its own.
	unlockA, okA := r.TryLockThread(identityctx.WithIdentityID(context.Background(), idA), session)
	if !okA {
		t.Fatal("A could not acquire its thread lock")
	}
	defer unlockA()
	unlockB, okB := r.TryLockThread(identityctx.WithIdentityID(context.Background(), idB), session)
	if !okB {
		t.Fatal("B could not acquire its own thread lock while A holds (A, session) — composite key collision")
	}
	defer unlockB()
}

// TestSessionKeyIsolationConcurrent hammers trackSession/cancelSession from many goroutines,
// half under identity A and half under identity B, all on the SAME session id. Each identity
// cancels only its own registrations; run under -race it also proves the shared registry is
// race-free (sync.Map). It is the concurrent same-session-id-different-identity proof.
func TestSessionKeyIsolationConcurrent(t *testing.T) {
	r, _, _ := wireLifecycleRecorder(t)
	const idA = "aaaaaaaa-0000-0000-0000-00000000000a"
	const idB = "bbbbbbbb-0000-0000-0000-00000000000b"

	const n = 64
	var wg sync.WaitGroup
	var aFired, bFired sync.Map // session -> struct{} the cancel recorded
	for i := range n {
		wg.Go(func() {
			session := fmt.Sprintf("session-%d", i%8) // deliberate reuse across both identities
			id, fired := idA, &aFired
			if i%2 == 1 {
				id, fired = idB, &bFired
			}
			ctx := identityctx.WithIdentityID(context.Background(), id)
			r.trackSession(ctx, session, func() { fired.Store(id+session, struct{}{}) })
			r.cancelSession(id, session)
		})
	}
	wg.Wait()

	// Every A-cancel recorded under idA+session; a B key must never appear in A's map (and
	// vice-versa) — no cross-identity fire despite the shared session ids.
	aFired.Range(func(k, _ any) bool {
		if key, _ := k.(string); len(key) >= len(idB) && key[:len(idB)] == idB {
			t.Fatalf("A's fired-map contains a B key %q — cross-identity collision", key)
		}
		return true
	})
	bFired.Range(func(k, _ any) bool {
		if key, _ := k.(string); len(key) >= len(idA) && key[:len(idA)] == idA {
			t.Fatalf("B's fired-map contains an A key %q — cross-identity collision", key)
		}
		return true
	})
}

// fakeShareRevoker is a minimal ShareRevoker double for TestSetShareRevoker — it only needs to
// be a distinguishable value, never actually invoked here (that firing behavior is already
// covered by the db_integration TestDeleteLifecycleRevokesShares/-Nil/-FailureDoesNotBlock trio
// in runner_delete_share_test.go).
type fakeShareRevoker struct{}

func (fakeShareRevoker) RevokeConversationShares(context.Context, string, string) error { return nil }

// TestSetShareRevoker proves the D-15 late-bound setter (runner_delete.go, added because
// chat.run is assembled before objectStore/chat.assets exist — share_service_wiring.go) actually
// wires r.shareRevoker: nil by default (Deps never set it), non-nil and equal to what was passed
// after SetShareRevoker. Untagged/no DB — SetShareRevoker itself is a pure field assignment.
func TestSetShareRevoker(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	if r.shareRevoker != nil {
		t.Fatal("sanity: shareRevoker must be nil by default (Deps never set it)")
	}

	sr := fakeShareRevoker{}
	r.SetShareRevoker(sr)

	if r.shareRevoker != ShareRevoker(sr) {
		t.Fatalf("shareRevoker after SetShareRevoker = %#v, want %#v", r.shareRevoker, sr)
	}
}
