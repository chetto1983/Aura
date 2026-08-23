package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// TestReserveAcquire proves the rows==1 mapping: the reservation is acquired, the verdict
// is Allow with NO replay, and exactly one reservation start row is written.
func TestReserveAcquire(t *testing.T) {
	store := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.reserve(context.Background(), gatedSpec(), nil, testKey(), scoring.Risky, "", "")
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

	v, err := g.reserve(context.Background(), gatedSpec(), nil, testKey(), scoring.Destructive, "op-9", "")
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

	v, err := g.reserve(context.Background(), gatedSpec(), nil, testKey(), scoring.Risky, "", "")
	if err != nil {
		t.Fatalf("reserve err: %v", err)
	}
	if v.Decision != Allow || v.Replay == nil {
		t.Fatalf("verdict = %+v, want allow with a replay", v)
	}
	// D-10: replayResult now appends replayedMarker, so the preview contains the
	// recorded text plus the marker rather than equaling it verbatim.
	if !strings.Contains(v.Replay.Preview, "recorded-output") || v.Replay.Bytes != 15 {
		t.Fatalf("replay = %+v, want the recorded end preview", v.Replay)
	}
}

// TestReserveFailClosed proves the INSERT-error mapping: a reservation that cannot be
// durably taken denies (GATE-03 fail-closed), so Execute is never reached upstream.
func TestReserveFailClosed(t *testing.T) {
	store := &fakeStore{reserveErr: errors.New("insert boom")}
	g := New(config.ProfileSingleUserHardened, store)

	v, err := g.reserve(context.Background(), gatedSpec(), nil, testKey(), scoring.Risky, "op-1", "")
	if err != nil {
		t.Fatalf("reserve must map the store error to a Deny verdict, not return err: %v", err)
	}
	if v.Decision != Deny || v.Reason != "reservation failed" {
		t.Fatalf("verdict = %+v, want deny/reservation failed", v)
	}
}

// TestReplayResultMissingSidecar proves Pitfall 6 AND D-10's composition rule: a
// recorded end whose sidecar is gone replays the capped preview plus BOTH the
// result-expired marker and the replayed marker, with no error and no FullPath.
// The two markers answer different questions (the body is gone / this result was
// not produced by this call) and neither replaces the other.
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
	if !strings.Contains(res.Preview, "replayed") {
		t.Fatalf("preview = %q, want it to ALSO carry the replayed marker (D-10 composition)", res.Preview)
	}
}

// TestReplayResultAppendsReplayedMarkerOnDuplicate proves D-10's core Layer A case:
// a normal (sidecar-present) reservation duplicate carries the recorded preview
// text AND the replayed marker, so the model can tell this result apart from a
// fresh execution.
func TestReplayResultAppendsReplayedMarkerOnDuplicate(t *testing.T) {
	res := replayResult(&toolinvocations.Event{
		ResultPreview: "recorded-output",
		ResultBytes:   15,
	})
	if !strings.Contains(res.Preview, "recorded-output") {
		t.Fatalf("preview = %q, want the recorded output preserved", res.Preview)
	}
	if !strings.Contains(res.Preview, "replayed") {
		t.Fatalf("preview = %q, want the replayed marker appended", res.Preview)
	}
}

// TestReserveDeniesAnUnaccountedPriorDispatch pins the rows==0-with-no-recorded-end case at
// the level that matters. It used to return Allow with a Replay whose Preview was the string
// "[reservation held: result not yet available]" and a nil error — so the caller returned a
// SUCCESSFUL tool result for a tool that had never executed. The operator hit it as a write
// that reported fine and left no file; the agent's own report was "fs_write non ha salvato il
// file — la scrittura è finita in reservation held".
//
// The at-most-once guarantee is unchanged: the effect still does not re-run. What changes is
// that a call with no recorded outcome is now DENIED rather than reported as done. A denial
// the model can see and act on beats an invented success it cannot.
func TestReserveDeniesAnUnaccountedPriorDispatch(t *testing.T) {
	g := New(config.ProfileSingleUserHardened, &fakeStore{notAcquired: true}) // replayEnd nil
	v, err := g.reserve(context.Background(), gatedSpec(), nil, testKey(), scoring.Normal, "", "")
	if err != nil {
		t.Fatalf("reserve returned err=%v, want a verdict", err)
	}
	if v.Decision != Deny {
		t.Fatalf("decision = %q, want deny for a prior dispatch with no recorded end", v.Decision)
	}
	if v.Replay != nil {
		t.Fatal("a denial must carry no replay: there is no outcome to replay")
	}
	if !strings.Contains(v.Reason, "not re-run") {
		t.Errorf("reason = %q, want it to say the tool was not re-run", v.Reason)
	}
}

// The defensive branch of replayResult is unreachable through reserve, but if a future caller
// reintroduces it the wording must not imply the effect happened. It must also NOT gain the
// replayed marker (D-10 probe edge, empty case): this is not a replay of a result, and
// labelling it as one would tell the model a recorded result exists where none does.
func TestReplayResultNilEndSaysTheToolDidNotRun(t *testing.T) {
	res := replayResult(nil)
	if !strings.Contains(res.Preview, "did NOT run") {
		t.Fatalf("preview = %q, want it to state the tool did not run", res.Preview)
	}
	if strings.Contains(res.Preview, "replayed") {
		t.Fatalf("preview = %q, must NOT carry the replayed marker (no result was recorded)", res.Preview)
	}
}

// TestDecodeOperationReplayAppendsReplayedMarkerFromBody proves D-10's Layer B case
// on the JSON-Body branch: decodeOperationReplay's decoded result carries the
// replayed marker in its Preview.
func TestDecodeOperationReplayAppendsReplayedMarkerFromBody(t *testing.T) {
	body, err := json.Marshal(tools.ToolResult{Preview: "layer-b-body-output", Bytes: 20})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	res, err := decodeOperationReplay(&idempotency.ReplayResult{Body: body, ExpiresAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("decodeOperationReplay err: %v", err)
	}
	if !strings.Contains(res.Preview, "layer-b-body-output") {
		t.Fatalf("preview = %q, want the decoded body preview preserved", res.Preview)
	}
	if !strings.Contains(res.Preview, "replayed") {
		t.Fatalf("preview = %q, want the replayed marker appended", res.Preview)
	}
}

// TestDecodeOperationReplayAppendsReplayedMarkerFromPreviewSidecar proves D-10's
// Layer B case on the preview/sidecar branch (no Body): the marker is appended
// here too — this is genuinely new code, not a mirror of an existing append site
// (PATTERNS.md §1).
func TestDecodeOperationReplayAppendsReplayedMarkerFromPreviewSidecar(t *testing.T) {
	res, err := decodeOperationReplay(&idempotency.ReplayResult{
		Preview: "layer-b-sidecar-output", SidecarRef: "/some/sidecar.result", ExpiresAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("decodeOperationReplay err: %v", err)
	}
	if !strings.Contains(res.Preview, "layer-b-sidecar-output") {
		t.Fatalf("preview = %q, want the sidecar-branch preview preserved", res.Preview)
	}
	if !strings.Contains(res.Preview, "replayed") {
		t.Fatalf("preview = %q, want the replayed marker appended", res.Preview)
	}
	if res.FullPath != "/some/sidecar.result" {
		t.Fatalf("FullPath = %q, want the sidecar ref preserved", res.FullPath)
	}
}

// TestDecodeOperationReplayNilOrEmptyReturnsErrorNoMarker proves D-10's probe edge
// (empty case) for Layer B: a nil replay, and a fully-empty replay, both keep their
// existing error and gain no marker — there is no result to label as replayed.
func TestDecodeOperationReplayNilOrEmptyReturnsErrorNoMarker(t *testing.T) {
	if _, err := decodeOperationReplay(nil); err == nil {
		t.Fatal("decodeOperationReplay(nil) must return an error")
	}
	if _, err := decodeOperationReplay(&idempotency.ReplayResult{}); err == nil {
		t.Fatal("decodeOperationReplay(empty) must return an error")
	}
}
