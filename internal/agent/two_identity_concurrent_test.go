// two_identity_concurrent_test.go proves ISO-05 (D-14/D-15): two LlmAgent runs
// driven concurrently under two identityctx values in ONE process, sharing the
// four process-wide surfaces D-15 enumerates, neither observe nor affect each
// other — including a deliberate collision (same tool, byte-identical
// arguments, overlapping in time). See two_identity_concurrent_harness_test.go
// for the shared construction; this file holds only assertions (01-05-PLAN.md).
package agent_test

import (
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
)

// lastToolResultContent returns the LAST RoleTool message content found in
// the given recorded requests' final Messages slice — the tool-result text
// each run's own FakeClient actually saw threaded back into its history.
func lastToolResultContent(t *testing.T, reqs []llm.Request) string {
	t.Helper()
	if len(reqs) == 0 {
		t.Fatal("no requests recorded")
	}
	last := reqs[len(reqs)-1]
	for _, m := range slices.Backward(last.Messages) {
		if m.Role == llm.RoleTool {
			return m.Content
		}
	}
	t.Fatalf("no RoleTool message found in the final recorded request: %+v", last)
	return ""
}

// TestTwoIdentityConcurrentRunsDoNotCross is Task 1: two agents under two
// identities, sharing ONE tools.Registry, ONE gateway.Gateway, ONE llm.Client
// and ONE SteerInbox, raced into each other on purpose.
func TestTwoIdentityConcurrentRunsDoNotCross(t *testing.T) {
	t.Run("collision", func(t *testing.T) {
		testCollision(t)
	})
	t.Run("budget", func(t *testing.T) {
		testBudgetIsolation(t)
	})
	t.Run("steer", func(t *testing.T) {
		testSteerIsolation(t)
	})
	t.Run("registry", func(t *testing.T) {
		testRegistryUnmutated(t)
	})
	t.Run("result_attribution", func(t *testing.T) {
		testResultAttribution(t)
	})
}

// testCollision drives both runs into the SAME tool with byte-identical
// arguments, overlapping in time (collisionTool's WaitGroup rendezvous
// forces the overlap deterministically), and asserts the tool executed
// EXACTLY TWICE — once per run — never once with a result served to both.
func testCollision(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	rec := &collisionRecorder{}
	var barrier sync.WaitGroup
	barrier.Add(2)
	registry.Register(&collisionTool{rec: rec, wg: &barrier})

	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()

	const collisionArgs = `{"op":"do-the-thing"}`
	turnA := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("callA-1", "collision_probe", collisionArgs)),
		agenttest.ToolCallTurn(textResponseCall("callA-2", "A done")),
	}
	turnB := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("callB-1", "collision_probe", collisionArgs)),
		agenttest.ToolCallTurn(textResponseCall("callB-2", "B done")),
	}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 5, 5, harnessOptions{})

	_, errA, _, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	calls := rec.snapshot()
	if len(calls) != 2 {
		t.Fatalf("collision_probe executed %d times, want exactly 2 (once per run, never replayed/shared)", len(calls))
	}
	bySession := map[string]collisionCall{}
	for _, c := range calls {
		if c.args != collisionArgs {
			t.Fatalf("recorded call carries unexpected args %q, want %q", c.args, collisionArgs)
		}
		if _, dup := bySession[c.sessionID]; dup {
			t.Fatalf("collision_probe recorded TWO calls for the SAME session %s — one run dispatched it twice instead of once each", c.sessionID)
		}
		bySession[c.sessionID] = c
	}
	if _, ok := bySession[runA.sessionID]; !ok {
		t.Fatalf("collision_probe never recorded a call attributed to run A's session %s", runA.sessionID)
	}
	if _, ok := bySession[runB.sessionID]; !ok {
		t.Fatalf("collision_probe never recorded a call attributed to run B's session %s", runB.sessionID)
	}
}

// testBudgetIsolation gives run A a MaxSteps=1 budget and run B a generous
// one, then asserts B completes its OWN full 3-step script — its step count
// is unaffected by A's tiny cap — while A, genuinely constrained by its own
// budget (one recovery grace turn, D-08), makes strictly fewer LLM calls than B.
func testBudgetIsolation(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(&echoTool{})
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()

	turnA := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("bA-1", "echo", `{"v":"x"}`)),
		agenttest.TextChunks("stop", "A recovered"),
	}
	turnB := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("bB-1", "echo", `{"v":"x"}`)),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("bB-2", "echo", `{"v":"y"}`)),
		agenttest.ToolCallTurn(textResponseCall("bB-3", "B finished all 3 steps")),
	}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 1, 5, harnessOptions{})

	_, errA, evsB, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	stepsA := runA.fc.CallCount()
	stepsB := runB.fc.CallCount()
	if stepsB != 3 {
		t.Fatalf("run B (MaxSteps=5) completed %d LLM calls, want exactly 3 — its own budget must not be shortened by run A's MaxSteps=1", stepsB)
	}
	lastB := lastFinal(evsB)
	if lastB == nil || lastB.LLMResponse.Content != "B finished all 3 steps" {
		t.Fatalf("run B did not complete its own full script: %+v", lastB)
	}
	if stepsA >= stepsB {
		t.Fatalf("run A (MaxSteps=1) made %d LLM calls, not fewer than run B's %d — its own cap had no effect on it", stepsA, stepsB)
	}
}

// testSteerIsolation pushes a steer message for run A's conversation ONLY,
// before either run starts, and asserts it is drained by A alone: A's Events
// carry a SteerDelta and A's recorded request history carries the
// <user_steer nonce="...">, while B's do not — SteerInbox.Drain is
// conversation-keyed, never identity-keyed, so this is the assertion that
// keying is enough.
func testSteerIsolation(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()

	turnA := []agenttest.FakeTurn{agenttest.TextChunks("stop", "A round 1")}
	turnB := []agenttest.FakeTurn{agenttest.TextChunks("stop", "B round 1")}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 5, 5, harnessOptions{})

	const steerText = "steer for A only"
	if err := steerInbox.Push(runA.sessionID, "operator", steerText); err != nil {
		t.Fatalf("Push: %v", err)
	}

	evsA, errA, evsB, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	var deltaA map[string]any
	for _, ev := range evsA {
		if ev.Actions.SteerDelta != nil {
			deltaA = ev.Actions.SteerDelta
			break
		}
	}
	if deltaA == nil {
		t.Fatal("run A never emitted a SteerDelta event for its own pushed steer message")
	}
	if deltaA["conversation_id"] != runA.sessionID {
		t.Fatalf("SteerDelta.conversation_id = %v, want run A's session %s", deltaA["conversation_id"], runA.sessionID)
	}
	for _, ev := range evsB {
		if ev.Actions.SteerDelta != nil {
			t.Fatalf("run B emitted a SteerDelta event, but no steer message was ever pushed for B's conversation: %+v", ev.Actions.SteerDelta)
		}
	}

	// Plain-text concatenation, not json.Marshal: json.Marshal HTML-escapes
	// '<'/'>' by default (encoding/json's SetEscapeHTML default), which would
	// turn the literal <user_steer nonce="..."> envelope into <user_steer
	// ...> and make a literal '<' search silently never match.
	blobA := concatMessageContents(runA.fc.RecordedRequests())
	blobB := concatMessageContents(runB.fc.RecordedRequests())
	// Hex-only capture: the byte-stable system prompt's own <steer_channel>
	// documentation contains the LITERAL example text
	// `<user_steer nonce="...">` (three dots, not a real nonce), which is
	// present in BOTH runs' messages[0] by construction (the stable-prefix
	// surface) — a permissive `[^"]+` capture would match that doc string
	// before the real hex nonce wrapUserSteer minted, in either run.
	nonceRe := regexp.MustCompile(`<user_steer nonce="([0-9a-f]+)">`)
	m := nonceRe.FindStringSubmatch(blobA)
	if m == nil {
		t.Fatalf("run A's recorded requests never carry the <user_steer nonce=...> envelope: %s", blobA)
	}
	if !strings.Contains(blobA, steerText) {
		t.Fatalf("run A's recorded requests do not carry the pushed steer text: %s", blobA)
	}
	if strings.Contains(blobB, steerText) {
		t.Fatalf("run B's recorded requests carry run A's steer text — cross-conversation delivery: %s", blobB)
	}
	if strings.Contains(blobB, m[1]) {
		t.Fatalf("run B's recorded requests carry run A's own steer nonce %q", m[1])
	}
}

// concatMessageContents joins every recorded request's message Content
// fields with newlines, for plain substring/regex search — unlike
// json.Marshal it never HTML-escapes '<'/'>', so a literal
// <user_steer nonce="..."> envelope search actually matches.
func concatMessageContents(reqs []llm.Request) string {
	var b strings.Builder
	for _, req := range reqs {
		for _, m := range req.Messages {
			b.WriteString(m.Content)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// testRegistryUnmutated asserts the SHARED tools.Registry holds no per-run
// state that differs from its pre-run state after both runs complete — the
// deferred-tool activation each run performs lives on the AGENT
// (activated/everLoaded, llm_agent.go), never on the registry.
func testRegistryUnmutated(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(&echoTool{})
	before := registryToolNames(registry)

	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()
	turnA := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("rA-1", "echo", `{"v":"x"}`)),
		agenttest.ToolCallTurn(textResponseCall("rA-2", "A done")),
	}
	turnB := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("rB-1", "echo", `{"v":"y"}`)),
		agenttest.ToolCallTurn(textResponseCall("rB-2", "B done")),
	}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 5, 5, harnessOptions{})
	_, errA, _, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	after := registryToolNames(registry)
	if !slices.Equal(before, after) {
		t.Fatalf("shared tools.Registry mutated by concurrent runs: before=%v after=%v", before, after)
	}
}

// testResultAttribution re-runs the collision scenario and asserts each
// run's OWN final answer and OWN recorded tool-result content are its own —
// never the other's — proving no cross-attribution alongside testCollision's
// call-count proof.
func testResultAttribution(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	rec := &collisionRecorder{}
	var barrier sync.WaitGroup
	barrier.Add(2)
	registry.Register(&collisionTool{rec: rec, wg: &barrier})
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()

	const collisionArgs = `{"op":"do-the-thing"}`
	turnA := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("caA-1", "collision_probe", collisionArgs)),
		agenttest.ToolCallTurn(textResponseCall("caA-2", "A done")),
	}
	turnB := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("caB-1", "collision_probe", collisionArgs)),
		agenttest.ToolCallTurn(textResponseCall("caB-2", "B done")),
	}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 5, 5, harnessOptions{})
	evsA, errA, evsB, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	lastA := lastFinal(evsA)
	lastB := lastFinal(evsB)
	if lastA == nil || lastA.LLMResponse.Content != "A done" {
		t.Fatalf("run A final content = %+v, want %q", lastA, "A done")
	}
	if lastB == nil || lastB.LLMResponse.Content != "B done" {
		t.Fatalf("run B final content = %+v, want %q", lastB, "B done")
	}

	toolMsgA := lastToolResultContent(t, runA.fc.RecordedRequests())
	toolMsgB := lastToolResultContent(t, runB.fc.RecordedRequests())
	if !strings.Contains(toolMsgA, "executed-for-session:"+runA.sessionID) {
		t.Fatalf("run A's own recorded tool result does not carry its own session id: %q", toolMsgA)
	}
	if !strings.Contains(toolMsgB, "executed-for-session:"+runB.sessionID) {
		t.Fatalf("run B's own recorded tool result does not carry its own session id: %q", toolMsgB)
	}
	if strings.Contains(toolMsgA, runB.sessionID) {
		t.Fatalf("run A's history carries run B's session id — a result crossed over: %q", toolMsgA)
	}
	if strings.Contains(toolMsgB, runA.sessionID) {
		t.Fatalf("run B's history carries run A's session id — a result crossed over: %q", toolMsgB)
	}
}
