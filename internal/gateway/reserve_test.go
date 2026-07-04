package gateway

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// TestReserveAcquire proves the rows==1 mapping: the reservation is acquired, the verdict
// is Allow with NO replay, and exactly one reservation start row is written.
func TestReserveAcquire(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.reserve(context.Background(), mutatingRiskySpec(), nil, testKey(), scoring.Risky, "")
	if err != nil {
		t.Fatalf("reserve returned err=%v, want nil", err)
	}
	if v.Decision != Allow || v.Replay != nil {
		t.Fatalf("verdict = %+v, want allow with no replay", v)
	}
	reserved := store.reserves()
	if len(reserved) != 1 || reserved[0].Event != toolinvocations.EventStart {
		t.Fatalf("want 1 reservation start, got %+v", reserved)
	}
	if reserved[0].Meta["reservation"] != true {
		t.Fatalf("start meta = %v, want reservation:true", reserved[0].Meta)
	}
}

// TestReserveFoldsOperatorID proves the approved-resume origin folds operator_id into the
// SAME single start Meta (the executed marker) — never a separate row (D-03 point 2).
func TestReserveFoldsOperatorID(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.reserve(context.Background(), mutatingRiskySpec(), nil, testKey(), scoring.Destructive, "op-9")
	if err != nil {
		t.Fatalf("reserve err: %v", err)
	}
	if v.OperatorID != "op-9" {
		t.Fatalf("verdict operator id = %q, want op-9", v.OperatorID)
	}
	reserved := store.reserves()
	if len(reserved) != 1 {
		t.Fatalf("approved reserve wrote %d start rows, want exactly 1", len(reserved))
	}
	m := reserved[0].Meta
	if m["operator_id"] != "op-9" || m["approved"] != true {
		t.Fatalf("start meta = %v, want operator_id/op-9 + approved:true", m)
	}
	if got := len(store.calls()); got != 0 {
		t.Fatalf("approved reserve wrote %d competing Inserts, want 0", got)
	}
}

// TestReserveReplayOnConflict proves the rows==0 mapping: the slot is already held, so the
// verdict is Allow carrying the recorded end as a replay (Execute is skipped upstream).
func TestReserveReplayOnConflict(t *testing.T) {
	store := &fakeStore{
		notAcquired: true,
		replayEnd:   &toolinvocations.Event{ResultPreview: "recorded-output", ResultBytes: 15},
	}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.reserve(context.Background(), mutatingRiskySpec(), nil, testKey(), scoring.Risky, "")
	if err != nil {
		t.Fatalf("reserve err: %v", err)
	}
	if v.Decision != Allow || v.Replay == nil {
		t.Fatalf("verdict = %+v, want allow with a replay", v)
	}
	if v.Replay.Preview != "recorded-output" || v.Replay.Bytes != 15 {
		t.Fatalf("replay = %+v, want the recorded end preview", v.Replay)
	}
}

// TestReserveFailClosed proves the INSERT-error mapping: a reservation that cannot be
// durably taken denies (GATE-03 fail-closed), so Execute is never reached upstream.
func TestReserveFailClosed(t *testing.T) {
	store := &fakeStore{reserveErr: errors.New("insert boom")}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.reserve(context.Background(), mutatingRiskySpec(), nil, testKey(), scoring.Risky, "op-1")
	if err != nil {
		t.Fatalf("reserve must map the store error to a Deny verdict, not return err: %v", err)
	}
	if v.Decision != Deny || v.Reason != "reservation failed" {
		t.Fatalf("verdict = %+v, want deny/reservation failed", v)
	}
}

// TestReplayResultMissingSidecar proves Pitfall 6: a recorded end whose sidecar is gone
// replays the capped preview plus a result-expired marker with no error and no FullPath.
func TestReplayResultMissingSidecar(t *testing.T) {
	res := replayResult(&toolinvocations.Event{
		ResultPreview:     "partial preview",
		ResultSidecarPath: "/nonexistent/definitely-gc-ed.result",
		ResultTruncated:   true,
	})
	if res.FullPath != "" {
		t.Fatalf("missing sidecar must clear FullPath, got %q", res.FullPath)
	}
	if !strings.Contains(res.Preview, "partial preview") || !strings.Contains(res.Preview, "result expired") {
		t.Fatalf("preview = %q, want the partial preview + a result-expired marker", res.Preview)
	}
}

// TestReplayResultInFlight proves a rows==0 with NO recorded end (a prior reservation still
// in-flight / crash-orphaned before its end) returns an in-flight marker, NOT a re-execute.
func TestReplayResultInFlight(t *testing.T) {
	res := replayResult(nil)
	if !strings.Contains(res.Preview, "not yet available") {
		t.Fatalf("preview = %q, want an in-flight marker", res.Preview)
	}
}
