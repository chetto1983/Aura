// two_identity_concurrent_test.go proves ISO-05 (D-14/D-15): two LlmAgent runs
// driven concurrently under two identityctx values in ONE process, sharing the
// four process-wide surfaces D-15 enumerates, neither observe nor affect each
// other — including a deliberate collision (same tool, byte-identical
// arguments, overlapping in time). See two_identity_concurrent_harness_test.go
// for the shared construction; this file holds only assertions (01-05-PLAN.md).
package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
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

// TestTwoIdentityConcurrentSidecarPathsAreDisjoint is Task 2's sidecar
// surface: sidecarPath disjointness, cross-identity unreachability (with a
// positive control), and path-escape refusal — all through the real
// production tools.NewResult / tools.ReadToolOutput / tools.WithToolCallContext
// surface, never the unexported sidecarPath/validateID helpers directly.
func TestTwoIdentityConcurrentSidecarPathsAreDisjoint(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(tools.ReadToolOutput{})
	recA := &bigOutputRecorder{}
	recB := &bigOutputRecorder{}
	const payloadA = "A-PAYLOAD-CONTENT-"
	const payloadB = "B-PAYLOAD-CONTENT-"
	registry.Register(&bigOutputTool{name: "big_output_a", payload: strings.Repeat(payloadA, 400), rec: recA})
	registry.Register(&bigOutputTool{name: "big_output_b", payload: strings.Repeat(payloadB, 400), rec: recB})

	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()

	turnA := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("sA-1", "big_output_a", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("sA-2", "A done")),
	}
	turnB := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("sB-1", "big_output_b", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("sB-2", "B done")),
	}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 5, 5, harnessOptions{})
	_, errA, _, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	fullPathA, sessOfA := recA.snapshot()
	fullPathB, sessOfB := recB.snapshot()

	t.Run("paths_disjoint", func(t *testing.T) {
		if fullPathA == "" || fullPathB == "" {
			t.Fatalf("payload did not spill to a sidecar: A=%q B=%q (payload smaller than PreviewCap?)", fullPathA, fullPathB)
		}
		if fullPathA == fullPathB {
			t.Fatalf("both runs' sidecar spilled to the SAME path: %s", fullPathA)
		}
		if sessOfA != runA.sessionID || sessOfB != runB.sessionID {
			t.Fatalf("sidecar session attribution wrong: A recorded under %q (want %q), B under %q (want %q)", sessOfA, runA.sessionID, sessOfB, runB.sessionID)
		}
		const prefixSeg = "conversations"
		slashA, slashB := filepath.ToSlash(fullPathA), filepath.ToSlash(fullPathB)
		if !strings.Contains(slashA, prefixSeg) || !strings.Contains(slashB, prefixSeg) {
			t.Fatalf("sidecar paths do not carry the fixed %q prefix segment: A=%s B=%s", prefixSeg, slashA, slashB)
		}
		if !strings.Contains(slashA, runA.sessionID) {
			t.Fatalf("run A's sidecar path does not contain its own session id: %s", slashA)
		}
		if !strings.Contains(slashB, runB.sessionID) {
			t.Fatalf("run B's sidecar path does not contain its own session id: %s", slashB)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		aSpillID := strings.TrimSuffix(filepath.Base(fullPathA), ".result")

		// Positive control: A can read its own spill back in full.
		selfCtx := tools.WithToolCallContext(context.Background(), runA.sessionID, "probe-self", runDir, 64)
		selfArgs, _ := json.Marshal(map[string]any{"tool_call_id": aSpillID})
		selfRes, err := (tools.ReadToolOutput{}).Execute(selfCtx, selfArgs)
		if err != nil {
			t.Fatalf("A reading its own sidecar failed (positive control): %v", err)
		}
		if !strings.Contains(selfRes.Preview, payloadA) {
			t.Fatalf("A's own read did not return A's content, got: %.80q", selfRes.Preview)
		}

		// The actual assertion: B's tool surface, given A's spill id, cannot read
		// A's content — the read is scoped by B's OWN session id, so B resolves a
		// path under B's own session that does not exist.
		crossCtx := tools.WithToolCallContext(context.Background(), runB.sessionID, "probe-cross", runDir, 64)
		crossArgs, _ := json.Marshal(map[string]any{"tool_call_id": aSpillID})
		_, err = (tools.ReadToolOutput{}).Execute(crossCtx, crossArgs)
		if err == nil {
			t.Fatal("B's tool surface successfully read A's sidecar content using A's spill id — cross-identity read succeeded")
		}
		if !strings.Contains(err.Error(), "no output for tool_call_id") {
			t.Fatalf("unexpected error shape for a cross-identity read: %v", err)
		}
	})

	t.Run("path_escape_refused", func(t *testing.T) {
		for _, malicious := range []string{"../../etc/passwd", "..\\..\\windows", "a/b", "a\\b", ""} {
			ctx := tools.WithToolCallContext(context.Background(), runA.sessionID, "probe-escape", runDir, 64)
			args, _ := json.Marshal(map[string]any{"tool_call_id": malicious})
			_, err := (tools.ReadToolOutput{}).Execute(ctx, args)
			if err == nil {
				t.Fatalf("crafted spill id %q was NOT refused before any filesystem join", malicious)
			}
		}
	})
}

// TestTwoIdentityConcurrentStablePrefixIsByteIdentical is Task 2's KV-prefix
// surface: messages[0] — the byte-stable system prompt — is byte-identical
// between the two runs, and carries no identity or session value.
func TestTwoIdentityConcurrentStablePrefixIsByteIdentical(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()
	turnA := []agenttest.FakeTurn{agenttest.TextChunks("stop", "A answer")}
	turnB := []agenttest.FakeTurn{agenttest.TextChunks("stop", "B answer")}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 5, 5, harnessOptions{})

	_, errA, _, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	reqsA := runA.fc.RecordedRequests()
	reqsB := runB.fc.RecordedRequests()
	if len(reqsA) == 0 || len(reqsB) == 0 {
		t.Fatal("no requests recorded for one of the two runs")
	}
	if len(reqsA[0].Messages) == 0 || len(reqsB[0].Messages) == 0 {
		t.Fatal("first recorded request carries no messages")
	}
	msg0A, err := json.Marshal(reqsA[0].Messages[0])
	if err != nil {
		t.Fatal(err)
	}
	msg0B, err := json.Marshal(reqsB[0].Messages[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(msg0A, msg0B) {
		t.Fatalf("messages[0] differs between the two identities' runs:\nA=%s\nB=%s", msg0A, msg0B)
	}
	for _, needle := range []string{identityA, identityB, runA.sessionID, runB.sessionID} {
		if bytes.Contains(msg0A, []byte(needle)) {
			t.Fatalf("messages[0] carries an identity-derived value %q: %s", needle, msg0A)
		}
	}
}

// TestTwoIdentityConcurrentHistoriesDoNotInterleave is Task 2's no-
// interleaving assertion: after both runs complete, each run's OWN recorded
// history contains only its own turns and its own tool results — never a
// message from the other run's list.
func TestTwoIdentityConcurrentHistoriesDoNotInterleave(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(&echoTool{})
	gw := gateway.New(config.ProfileSingleUserHardened, nil)
	steerInbox := steertest.New(steer.Config{})
	runDir := t.TempDir()
	turnA := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("hA-1", "echo", `{"v":"only-in-A"}`)),
		agenttest.ToolCallTurn(textResponseCall("hA-2", "A final")),
	}
	turnB := []agenttest.FakeTurn{
		agenttest.ToolCallTurn(agenttest.MakeToolCall("hB-1", "echo", `{"v":"only-in-B"}`)),
		agenttest.ToolCallTurn(textResponseCall("hB-2", "B final")),
	}
	runA, runB := newTwoIdentityHarness(t, registry, gw, steerInbox, runDir, turnA, turnB, 5, 5, harnessOptions{})
	_, errA, _, errB := runBothConcurrently(t, runA, runB)
	if errA != nil {
		t.Fatalf("run A errored: %v", errA)
	}
	if errB != nil {
		t.Fatalf("run B errored: %v", errB)
	}

	lastA := runA.fc.LastRequest()
	lastB := runB.fc.LastRequest()
	blobA, _ := json.Marshal(lastA.Messages)
	blobB, _ := json.Marshal(lastB.Messages)
	if bytes.Contains(blobA, []byte("only-in-B")) {
		t.Fatalf("run A's own history contains run B's content: %s", blobA)
	}
	if bytes.Contains(blobB, []byte("only-in-A")) {
		t.Fatalf("run B's own history contains run A's content: %s", blobB)
	}
	if bytes.Contains(blobA, []byte(runB.sessionID)) {
		t.Fatalf("run A's history contains run B's session id: %s", blobA)
	}
	if bytes.Contains(blobB, []byte(runA.sessionID)) {
		t.Fatalf("run B's history contains run A's session id: %s", blobB)
	}
}
