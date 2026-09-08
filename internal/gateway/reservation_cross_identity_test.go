// reservation_cross_identity_test.go proves the D-15 surface 2 half of ISO-05
// that belongs in this package: ReservationKey values built from two
// concurrent identities' calls never merge, even when the tool name and the
// argument fingerprint are byte-identical, because ConversationID differs —
// the exact machinery a prior replay defect (F-1) lived in. Package gateway
// (internal, white-box) like every other test file here, so it can assert on
// gatewayApprovalContext directly — what a resume actually reads back — not
// only on ReservationKey struct equality (01-05-PLAN.md Task 2).
package gateway

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// uniqueKeyStore mimics the production store's UNIQUE(conversation_id,
// request_id, tool_call_id, event_kind) constraint (see reserve.go's own
// "rows==1 (acquired) / rows==0 (already held)" doc comment) with an
// in-memory map keyed on the full ReservationKey triple.
//
// decide_test.go's fakeStore (reused by every other test in this package)
// always reports acquired=true regardless of key collisions — correct for
// what those tests assert, but it would make THIS test vacuous: reserving
// the SAME key twice would "succeed" twice, so a bug that merged two
// identities' reservations under one key would never be caught. This store
// enforces the real uniqueness rule instead.
type uniqueKeyStore struct {
	mu   sync.Mutex
	seen map[ReservationKey]toolinvocations.Event
}

func newUniqueKeyStore() *uniqueKeyStore {
	return &uniqueKeyStore{seen: make(map[ReservationKey]toolinvocations.Event)}
}

func (s *uniqueKeyStore) Insert(_ context.Context, _ toolinvocations.Event) error { return nil }

func (s *uniqueKeyStore) Reserve(_ context.Context, start toolinvocations.Event) (bool, *toolinvocations.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := ReservationKey{ConversationID: start.ConversationID, RequestID: start.RequestID, ToolCallID: start.ToolCallID}
	if prior, held := s.seen[key]; held {
		return false, &prior, nil
	}
	s.seen[key] = start
	return true, nil, nil
}

func (s *uniqueKeyStore) GetEnd(_ context.Context, _, _, _ string) (*toolinvocations.Event, error) {
	return nil, nil
}

// TestReservationKeyCrossIdentityDoesNotMerge is Task 2: the same tool name
// and the same byte-identical arguments, dispatched under two different
// conversation UUIDs — the shape of a genuine cross-identity collision — must
// never resolve to one merged reservation.
func TestReservationKeyCrossIdentityDoesNotMerge(t *testing.T) {
	spec := ordinaryWriteSpec() // reused from decide_test.go: mutating, non-gated
	args := json.RawMessage(`{"path":"/workspace/shared.txt","content":"byte-identical"}`)
	fp := gatewayArgsFingerprint(args)

	keyA := ReservationKey{
		ConversationID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		RequestID:      "req-shared-across-identities",
		ToolCallID:     "call-shared-across-identities",
	}
	keyB := ReservationKey{
		ConversationID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
		RequestID:      "req-shared-across-identities",  // deliberately identical to keyA
		ToolCallID:     "call-shared-across-identities", // deliberately identical to keyA
	}

	t.Run("struct_disjoint", func(t *testing.T) {
		if keyA == keyB {
			t.Fatal("ReservationKey equal despite different ConversationID")
		}
		if keyA.RequestID != keyB.RequestID || keyA.ToolCallID != keyB.ToolCallID {
			t.Fatal("test setup invalid: RequestID/ToolCallID must be identical so ConversationID is the ONLY differing field")
		}
	})

	t.Run("approval_context_disjoint", func(t *testing.T) {
		ctxA := gatewayApprovalContext(spec, scoring.Normal, keyA, fp)
		ctxB := gatewayApprovalContext(spec, scoring.Normal, keyB, fp)
		if string(ctxA) == string(ctxB) {
			t.Fatalf("gatewayApprovalContext byte-identical for two different conversations: %s", ctxA)
		}
		var mA, mB map[string]any
		if err := json.Unmarshal(ctxA, &mA); err != nil {
			t.Fatalf("unmarshal ctxA: %v", err)
		}
		if err := json.Unmarshal(ctxB, &mB); err != nil {
			t.Fatalf("unmarshal ctxB: %v", err)
		}
		if mA["conversation_id"] == mB["conversation_id"] {
			t.Fatal("marshalled conversation_id matches across identities")
		}
		if mA["request_id"] != mB["request_id"] || mA["tool_call_id"] != mB["tool_call_id"] {
			t.Fatal("test setup invalid: request_id/tool_call_id must match in the marshalled context too")
		}
		if mA["args_sha256"] != mB["args_sha256"] {
			t.Fatalf("args_sha256 differs (%v vs %v) despite byte-identical arguments — the fingerprint is not purely argument-derived", mA["args_sha256"], mB["args_sha256"])
		}
	})

	t.Run("ledger_non_merge", func(t *testing.T) {
		store := newUniqueKeyStore()
		g := New(config.ProfileSingleUserHardened, store)
		startA := g.reservationStart(spec, args, keyA, scoring.Normal, "", "")
		startB := g.reservationStart(spec, args, keyB, scoring.Normal, "", "")

		var wg sync.WaitGroup
		var accA, accB bool
		wg.Add(2)
		go func() { defer wg.Done(); accA, _, _ = store.Reserve(context.Background(), startA) }()
		go func() { defer wg.Done(); accB, _, _ = store.Reserve(context.Background(), startB) }()
		wg.Wait()

		if !accA {
			t.Fatal("run A's reservation was not acquired")
		}
		if !accB {
			t.Fatal("run B's reservation was denied/replayed despite a DIFFERENT conversation_id — the same request_id+tool_call_id collided across identities and merged into one ledger row")
		}

		// Positive control: the store DOES enforce uniqueness for a genuine
		// repeat of the exact same key — without this, accA/accB both being
		// true would prove nothing (a store that never denies anything would
		// pass the assertion above vacuously).
		reacquired, replay, _ := store.Reserve(context.Background(), startA)
		if reacquired {
			t.Fatal("positive control failed: a genuine repeat of the SAME key was re-acquired — this store's uniqueness check exercises nothing")
		}
		if replay == nil {
			t.Fatal("positive control: a denied repeat should return the prior held reservation")
		}
	})
}
