package agent

import (
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// artifactRun builds a toolRunResult whose ToolResult.Meta carries the given
// artifact descriptor (or none when art is nil), mirroring what send_file's
// Execute returns through runTool.
func artifactRun(t *testing.T, art map[string]any) toolRunResult {
	t.Helper()
	now := time.Now().UTC()
	run := toolRunResult{
		ToolCallID: "call-sf",
		ToolName:   "send_file",
		Arguments:  `{"path":"/abs/results.xlsx","caption":"results"}`,
		StartedAt:  now,
		EndedAt:    now.Add(2 * time.Millisecond),
		Preview:    "queued results.xlsx for delivery",
	}
	res := tools.ToolResult{Preview: run.Preview, Bytes: len(run.Preview)}
	if art != nil {
		meta := tools.ToolResultMeta{"artifact": art}
		res.Meta = &meta
	}
	run.Result = res
	return run
}

// TestToolResultEvent_LiftsArtifactMeta pins the emit seam: a run whose
// Result.Meta carries the `artifact` key has that descriptor lifted onto
// ev.Actions.ArtifactDelta by toolResultEvent (the named Meta→ArtifactDelta lift).
func TestToolResultEvent_LiftsArtifactMeta(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry())
	var span [8]byte
	descriptor := map[string]any{
		"path":     "/abs/results.xlsx",
		"filename": "results.xlsx",
		"caption":  "results",
	}
	ev := a.toolResultEvent(internalPauseIC(t), span, nil, artifactRun(t, descriptor))

	if ev.Actions.ArtifactDelta == nil {
		t.Fatal("toolResultEvent must lift the Meta artifact onto Actions.ArtifactDelta")
	}
	if got := ev.Actions.ArtifactDelta["filename"]; got != "results.xlsx" {
		t.Fatalf("ArtifactDelta.filename = %v, want results.xlsx", got)
	}
	if got := ev.Actions.ArtifactDelta["path"]; got != "/abs/results.xlsx" {
		t.Fatalf("ArtifactDelta.path = %v, want the descriptor path", got)
	}
	// The tool-result preview / tool-invocation projection still happens (the
	// artifact lift is purely additive, alongside the existing stamps).
	if ev.Actions.ToolInvocation == nil {
		t.Fatal("the existing ToolInvocation stamp must remain (artifact lift is additive)")
	}
}

// TestToolResultEvent_NoArtifactLeavesNil pins the additive contract: a run
// WITHOUT the artifact key leaves ev.Actions.ArtifactDelta nil — every existing
// (non-artifact) event is unchanged.
func TestToolResultEvent_NoArtifactLeavesNil(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry())
	var span [8]byte

	// No Meta at all.
	ev := a.toolResultEvent(internalPauseIC(t), span, nil, artifactRun(t, nil))
	if ev.Actions.ArtifactDelta != nil {
		t.Fatalf("a run without an artifact key must leave ArtifactDelta nil, got %v", ev.Actions.ArtifactDelta)
	}

	// Meta present but carrying a DIFFERENT key (e.g. shell_exec's exit_code) must
	// also leave ArtifactDelta nil — metaArtifact keys on the `artifact` field only.
	run := artifactRun(t, nil)
	meta := tools.ToolResultMeta{"exit_code": 0}
	run.Result.Meta = &meta
	ev2 := a.toolResultEvent(internalPauseIC(t), span, nil, run)
	if ev2.Actions.ArtifactDelta != nil {
		t.Fatalf("a non-artifact Meta key must leave ArtifactDelta nil, got %v", ev2.Actions.ArtifactDelta)
	}
}

// TestMetaArtifact_WrongTypeIsAbsent pins the metaArtifact type guard: an
// `artifact` key whose value is NOT a map[string]any is treated as absent
// (ok=false), so a malformed Meta never produces a bogus ArtifactDelta.
func TestMetaArtifact_WrongTypeIsAbsent(t *testing.T) {
	wrong := &tools.ToolResultMeta{"artifact": "not-a-map"}
	if art, ok := metaArtifact(wrong); ok {
		t.Fatalf("a non-map artifact value must report ok=false, got %v", art)
	}
	if _, ok := metaArtifact(nil); ok {
		t.Fatal("nil meta must report ok=false")
	}
	good := &tools.ToolResultMeta{"artifact": map[string]any{"path": "/x", "filename": "x"}}
	art, ok := metaArtifact(good)
	if !ok || art["filename"] != "x" {
		t.Fatalf("a valid artifact map must report ok=true with the descriptor, got %v ok=%v", art, ok)
	}
}
