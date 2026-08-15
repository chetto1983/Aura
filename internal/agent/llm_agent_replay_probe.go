package agent

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"sync"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// A verification seam for HARN-03, and nothing else.
//
// The replay path — replayedMarker on a recorded result, plus aura.tool.replayed /
// aura.tool.replay_layer on the span — is the fix that stops Aura acting on a result
// she did not produce. Phase 45 could prove it only with unit tests, because on a live
// stack it turned out to be unreachable: a crash repudiates rather than re-runs
// (recoverOrphans marks unknown_recovery), a scheduler re-fire mints a new run id and
// therefore a new operation key (proved by TestSchedulerReclaimCannotReplayAcrossRuns),
// and an HTTP retry with a duplicate Idempotency-Key is refused before the agent loop
// starts. That leaves exactly ONE reachable trigger: a genuine same-round retry, which
// only happens when a provider or transport actually fails mid-round.
//
// Waiting for a real provider failure is not a verification strategy, so this makes
// that one trigger inducible on demand. It is OFF unless AURA_TEST_FORCE_REPLAY_PROBE
// is truthy, and when on it fires exactly ONCE for the whole process.
//
// It is deliberately a re-DISPATCH, not a fake: the second call goes through the real
// execTool -> Decide -> reserve path with the same parent operation, the same round
// ordinal and the same canonical arguments, so it derives the identical child key and
// the registry answers DecisionReplay on its own. Nothing here constructs a replay
// result or sets a marker; if the production path did not replay, the probe observes
// that instead, which is the whole point of using it as evidence.
var (
	replayProbeOnce    sync.Once
	replayProbeEnabled bool
)

// replayProbeArmed reports whether the seam is on. Parsed once: an operator flipping
// the env mid-process must not change behaviour halfway through a turn.
func replayProbeArmed() bool {
	replayProbeOnce.Do(func() {
		raw := os.Getenv("AURA_TEST_FORCE_REPLAY_PROBE")
		if raw == "" {
			return
		}
		on, err := strconv.ParseBool(raw)
		replayProbeEnabled = err == nil && on
	})
	return replayProbeEnabled
}

// replayProbeFired guards the single shot.
var replayProbeFired sync.Once

// forceSameRoundReplay re-dispatches one already-completed mutating call so the
// same-round retry path executes for real. It returns the replayed result and true
// when it fired; the caller substitutes that result so the marker reaches the model
// exactly as it would after a genuine transport retry.
//
// Returns false — leaving the original result untouched — when the seam is disarmed,
// when it has already fired, when the call was not mutating (no reservation, so
// nothing to replay), or when the re-dispatch errors. A probe that cannot observe the
// path must never damage the turn it was observing.
func (a *LlmAgent) forceSameRoundReplay(ctx context.Context, tool tools.Tool, mutating bool, args json.RawMessage) (tools.ToolResult, bool) {
	if !replayProbeArmed() || !mutating {
		return tools.ToolResult{}, false
	}
	var (
		res   tools.ToolResult
		err   error
		fired bool
	)
	replayProbeFired.Do(func() {
		res, err = a.execTool(ctx, tool, mutating, args)
		fired = true
	})
	if !fired || err != nil {
		return tools.ToolResult{}, false
	}
	return res, true
}
