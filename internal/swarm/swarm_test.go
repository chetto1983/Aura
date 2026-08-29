package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"go.uber.org/goleak"
)

// outcome scripts one worker's behavior, keyed by a goal substring.
type outcome struct {
	kind     string // "ok" | "fail" | "pause" | "block" | "steps" | "pause_then_ok"
	text     string // ok summary
	question string // pause question
	options  []string
	steps    int // for "steps": number of distinct tool calls before terminating
	// delay simulates real worker latency before Stream answers, so a test can
	// prove Run() actually blocked for it (TestNestedDelegationSynchronous,
	// SWARM-04) rather than returning early via the background-enqueue branch.
	delay time.Duration
}

// routerClient is a goroutine-safe llm.Client that routes each Stream call to a
// scripted outcome by matching the goal substring carried in the request's user
// brief. It lets N concurrent workers run deterministically off ONE shared client
// (mirrors the production single-client fan-out). It records per-goal call counts
// so step-budget tests can assert consumption.
type routerClient struct {
	mu     sync.Mutex
	routes map[string]outcome // goal substring -> outcome
	calls  map[string]int     // goal substring -> Stream call count
}

func newRouter() *routerClient {
	return &routerClient{routes: map[string]outcome{}, calls: map[string]int{}}
}

func (r *routerClient) route(sub string, o outcome) *routerClient {
	r.routes[sub] = o
	return r
}

func (r *routerClient) matchGoal(req llm.Request) (string, outcome, bool) {
	// The prompt builder appends a tail <budget> user message, so scan ALL user
	// messages (not just the last) for the goal substring carried in the brief.
	var briefs strings.Builder
	for _, m := range req.Messages {
		if m.Role == llm.RoleUser {
			briefs.WriteString(m.Content)
			briefs.WriteByte('\n')
		}
	}
	all := briefs.String()
	for sub, o := range r.routes {
		if strings.Contains(all, sub) {
			return sub, o, true
		}
	}
	return "", outcome{}, false
}

func (r *routerClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	r.mu.Lock()
	sub, o, ok := r.matchGoal(req)
	if ok {
		r.calls[sub]++
	}
	call := r.calls[sub]
	r.mu.Unlock()

	if !ok {
		// Unknown goal: terminate with empty content so a stray worker never hangs.
		return closedChan(llm.Chunk{FinishReason: "stop"}), nil
	}

	if o.delay > 0 {
		select {
		case <-time.After(o.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	switch o.kind {
	case "fail":
		return nil, errors.New("scripted worker failure")
	case "panic":
		panic("scripted worker panic")
	case "block":
		<-ctx.Done() // hold until the per-child timeout cancels us
		return nil, ctx.Err()
	case "pause":
		args, _ := json.Marshal(map[string]any{
			"question": o.question,
			"kind":     "choice",
			"options":  o.options,
		})
		return toolChan(agenttest.MakeToolCall(fmt.Sprintf("call-%s", sub), "ask_user", string(args))), nil
	case "steps":
		// Emit distinct echo calls until the budget trips; each call consumes a step.
		if call <= o.steps {
			args, _ := json.Marshal(map[string]string{"v": fmt.Sprintf("%s-%d", sub, call)})
			return toolChan(agenttest.MakeToolCall(fmt.Sprintf("c-%s-%d", sub, call), "echo", string(args))), nil
		}
		return closedChan(llm.Chunk{Text: o.text}, llm.Chunk{FinishReason: "stop"}), nil
	case "pause_then_ok":
		// 51-06b Task 2: the FIRST Stream call against this goal pauses (same shape as
		// "pause"); every call after that -- i.e. the rebuilt worker's first call once
		// runChild is re-invoked with ResumeTurns seeded -- answers ok. Distinguishing by
		// r.calls[sub] (not request content) is deliberate: it proves the SECOND runChild
		// invocation is a genuinely NEW model call driven by the resumed history, not a
		// replay of the first.
		if call <= 1 {
			args, _ := json.Marshal(map[string]any{
				"question": o.question,
				"kind":     "choice",
				"options":  o.options,
			})
			return toolChan(agenttest.MakeToolCall(fmt.Sprintf("call-%s", sub), "ask_user", string(args))), nil
		}
		return closedChan(llm.Chunk{Text: o.text}, llm.Chunk{FinishReason: "stop"}), nil
	default: // "ok"
		return closedChan(llm.Chunk{Text: o.text}, llm.Chunk{FinishReason: "stop"}), nil
	}
}

func closedChan(chunks ...llm.Chunk) <-chan llm.Chunk {
	ch := make(chan llm.Chunk, len(chunks))
	for _, c := range chunks {
		ch <- c
	}
	close(ch)
	return ch
}

func toolChan(call llm.ToolCall) <-chan llm.Chunk {
	c := call
	return closedChan(llm.Chunk{ToolCall: &c}, llm.Chunk{FinishReason: "tool_calls"})
}

// echoTool is a non-terminal tool so "steps" workers can burn budget.
type echoTool struct{}

func (echoTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "echo",
		Summary:     "echo",
		Description: "echo",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"v":{"type":"string"}}}`),
	}
}
func (echoTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	return tools.NewResult(ctx, "echo:"+string(raw))
}

// stubWorkerRegistry holds the tools a worker may call in tests: text_response
// (terminal), ask_user (pause), echo (step burner), plus a swarm_spawn STUB so the
// Without filter is exercised. Named apart from the production workerRegistry
// (swarm_depth.go, plan 51-05) it stands in for -- that function derives the REAL
// depth-conditional registry a worker gets at runtime; this one is the flat,
// unconditional base a test's ParentRegistry starts from.
func stubWorkerRegistry() *tools.Registry {
	r := tools.NewRegistry()
	r.Register(tools.TextResponse{})
	r.Register(tools.AskUser{})
	r.Register(echoTool{})
	r.Register(stubTool{name: swarmSpawnTool})
	return r
}

// testLike is the slice of testing.TB that BOTH *testing.T and *rapid.T satisfy
// (rapid.T does not implement testing.TB — it lacks the unexported seal — but it
// exposes these). It lets the shared helpers run under the property test.
type testLike interface {
	Fatalf(format string, args ...any)
	Errorf(format string, args ...any)
	Cleanup(func())
}

// tempDir makes a self-cleaning temp dir without testing.TB.TempDir (absent on
// rapid.T).
func tempDir(t testLike) string {
	dir, err := os.MkdirTemp("", "swarm-test-")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func testRunConfig(t testLike, client llm.Client, maxSteps int) RunConfig {
	b, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &maxSteps})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	return RunConfig{
		ParentBudget:   b,
		ParentRegistry: stubWorkerRegistry(),
		Client:         client,
		LLM:            llm.Config{Model: "m", Provider: "openrouter", TotalTimeoutSec: 30},
		Cfg: config.Config{
			RunDir:             tempDir(t),
			ToolPreviewCap:     2048,
			MaxSwarmGoals:      8,
			SwarmChildIdleSec:  30,
			MaxSwarmConcurrent: 4,
		},
		ConvID: "conv1",
		Depth:  1,
	}
}

func parseReports(t testLike, raw string) []ChildReport {
	var reports []ChildReport
	if err := json.Unmarshal([]byte(raw), &reports); err != nil {
		t.Fatalf("unmarshal reports: %v (%q)", err, raw)
	}
	return reports
}

// TestSwarmParallelTiming (SC#1): 3 goals run concurrently and all return ok in
// goal order; goleak clean.
func TestSwarmParallelTiming(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done"}).
		route("bravo", outcome{kind: "ok", text: "B done"}).
		route("charlie", outcome{kind: "ok", text: "C done"})
	rc := testRunConfig(t, r, 25)

	out, err := Run(context.Background(), rc, []string{"alpha task", "bravo task", "charlie task"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, out)
	if len(reports) != 3 {
		t.Fatalf("want 3 reports, got %d", len(reports))
	}
	wantSummary := []string{"A done", "B done", "C done"}
	for i, rep := range reports {
		if rep.GoalIndex != i {
			t.Errorf("report[%d].GoalIndex = %d, want %d", i, rep.GoalIndex, i)
		}
		if rep.Status != StatusOK {
			t.Errorf("report[%d].Status = %q, want ok", i, rep.Status)
		}
		if rep.Summary != wantSummary[i] {
			t.Errorf("report[%d].Summary = %q, want %q", i, rep.Summary, wantSummary[i])
		}
		if rep.ChildID != fmt.Sprintf("w%d", i+1) {
			t.Errorf("report[%d].ChildID = %q, want w%d", i, rep.ChildID, i+1)
		}
	}
}

// TestSwarmFailureIsolation (D-02): one goal's worker errors; its slot is failed,
// siblings stay ok.
func TestSwarmFailureIsolation(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done"}).
		route("bravo", outcome{kind: "fail"}).
		route("charlie", outcome{kind: "ok", text: "C done"})
	rc := testRunConfig(t, r, 25)

	out, err := Run(context.Background(), rc, []string{"alpha task", "bravo task", "charlie task"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, out)
	if reports[0].Status != StatusOK || reports[2].Status != StatusOK {
		t.Errorf("siblings cancelled: [0]=%q [2]=%q, want both ok", reports[0].Status, reports[2].Status)
	}
	if reports[1].Status != StatusFailed {
		t.Errorf("report[1].Status = %q, want failed", reports[1].Status)
	}
	if reports[1].Error == "" {
		t.Error("failed report carries no error message")
	}
}

func TestSwarmPanicIsolation(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done"}).
		route("bravo", outcome{kind: "panic"}).
		route("charlie", outcome{kind: "ok", text: "C done"})
	rc := testRunConfig(t, r, 25)

	out, err := Run(context.Background(), rc, []string{"alpha task", "bravo task", "charlie task"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, out)
	if reports[0].Status != StatusOK || reports[2].Status != StatusOK {
		t.Errorf("siblings cancelled by panic: [0]=%q [2]=%q, want both ok", reports[0].Status, reports[2].Status)
	}
	if reports[1].Status != StatusFailed {
		t.Fatalf("panic report status = %q, want failed", reports[1].Status)
	}
	if !strings.Contains(reports[1].Error, "panic") || !strings.Contains(reports[1].Error, "scripted worker panic") {
		t.Fatalf("panic report error = %q, want recovered panic detail", reports[1].Error)
	}
}

// TestWorkerStalledReport (D-03, SWARM-03 edge: silence): a worker that emits
// nothing for AURA_SWARM_CHILD_IDLE_SEC has its context cancelled exactly once and
// is reported {failed, "stalled: ..."}; the sibling, unaffected, still completes ok.
func TestWorkerStalledReport(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A done"}).
		route("bravo", outcome{kind: "block"})
	rc := testRunConfig(t, r, 25)
	rc.Cfg.SwarmChildIdleSec = 1 // tight inactivity window for a fast test

	out, err := Run(context.Background(), rc, []string{"alpha task", "bravo task"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, out)
	if reports[0].Status != StatusOK {
		t.Errorf("sibling affected by reap: [0]=%q", reports[0].Status)
	}
	if reports[1].Status != StatusFailed || !strings.Contains(reports[1].Error, "stalled") {
		t.Errorf("report[1] = {%q,%q}, want {failed, contains \"stalled\"}", reports[1].Status, reports[1].Error)
	}
}

// TestStreamingWorkerNotReaped (D-03, SWARM-03 edge: N < idle): a worker emitting
// an event at intervals well under AURA_SWARM_CHILD_IDLE_SEC runs past what the OLD
// wall-clock-equivalent bound would have killed it at (each Stream call here carries
// a real delay, and their SUM exceeds the idle window) and still completes ok --
// proving the deadline resets on progress rather than accumulating age.
func TestStreamingWorkerNotReaped(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().route("alpha", outcome{kind: "steps", steps: 3, text: "A done", delay: 500 * time.Millisecond})
	rc := testRunConfig(t, r, 25)
	rc.Cfg.SwarmChildIdleSec = 2 // each step's 500ms gap is well under the 2s idle window

	out, err := Run(context.Background(), rc, []string{"alpha task"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, out)
	if reports[0].Status != StatusOK {
		t.Fatalf("report[0].Status = %q, want ok (total wall time exceeded the idle window, but no single gap did)", reports[0].Status)
	}
	if reports[0].Summary != "A done" {
		t.Errorf("report[0].Summary = %q, want %q", reports[0].Summary, "A done")
	}
}

// TestNormalizeStaleReportDoesNotClobberCompletedSuccess (WR-01): a worker that
// already produced a terminal success (StatusOK + a populated Summary) is NOT
// rewritten to {failed, "stalled: ..."} even when stalled=true -- the race window
// between a worker's last event and a coincidentally-firing staleness timer must
// never clobber a genuine completion. A pure-function test (not a real timer race)
// per the same reasoning the original wall-clock version of this test used: the
// GUARANTEE is deterministically testable without needing to win a real race.
func TestNormalizeStaleReportDoesNotClobberCompletedSuccess(t *testing.T) {
	rep := normalizeStaleReport(ChildReport{Status: StatusOK, Summary: "A done"}, true, time.Second)
	if rep.Status != StatusOK {
		t.Fatalf("a completed worker must keep StatusOK despite stalled=true, got {%q,%q}", rep.Status, rep.Error)
	}
	if rep.Summary != "A done" {
		t.Errorf("the streamed summary must survive, got %q", rep.Summary)
	}
}

// TestNormalizeStaleReportRelabelsNonTerminalOutcomes proves the OTHER two branches
// of the same guard: a worker that surfaced the cancellation as a stream error is
// re-labeled to the uniform stall message, and a worker with no terminal Summary at
// all is failed as stalled. Both only when stalled=true; stalled=false is always a
// no-op regardless of report shape.
func TestNormalizeStaleReportRelabelsNonTerminalOutcomes(t *testing.T) {
	failed := normalizeStaleReport(ChildReport{Status: StatusFailed, Error: "context canceled"}, true, 2*time.Second)
	if failed.Status != StatusFailed || !strings.Contains(failed.Error, "stalled") {
		t.Errorf("failed report = {%q,%q}, want relabeled to contain \"stalled\"", failed.Status, failed.Error)
	}

	empty := normalizeStaleReport(ChildReport{Status: StatusOK}, true, 2*time.Second)
	if empty.Status != StatusFailed || !strings.Contains(empty.Error, "stalled") {
		t.Errorf("empty-summary OK report = {%q,%q}, want failed/stalled", empty.Status, empty.Error)
	}

	untouched := normalizeStaleReport(ChildReport{Status: StatusFailed, Error: "boom"}, false, 2*time.Second)
	if untouched.Status != StatusFailed || untouched.Error != "boom" {
		t.Errorf("stalled=false must be a no-op, got {%q,%q}", untouched.Status, untouched.Error)
	}
}

// TestSwarmMultiPause (SC#3): 5 goals all emit ask_user; all 5 report
// needs_user_input with a tool_call_id in goal order; goleak clean.
func TestSwarmMultiPause(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter()
	goals := make([]string, 5)
	for i := range 5 {
		sub := fmt.Sprintf("g%d", i)
		goals[i] = sub + " task"
		r.route(sub, outcome{kind: "pause", question: "pick " + sub, options: []string{"a", "b"}})
	}
	rc := testRunConfig(t, r, 50)

	out, err := Run(context.Background(), rc, goals)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, out)
	if len(reports) != 5 {
		t.Fatalf("want 5 reports, got %d", len(reports))
	}
	for i, rep := range reports {
		if rep.Status != StatusNeedsUserInput {
			t.Errorf("report[%d].Status = %q, want needs_user_input", i, rep.Status)
		}
		if rep.ToolCallID == "" {
			t.Errorf("report[%d] carries no tool_call_id", i)
		}
		if rep.GoalIndex != i {
			t.Errorf("report[%d].GoalIndex = %d, want %d (order)", i, rep.GoalIndex, i)
		}
	}
}

// TestSwarmBudgetInheritance (SC#4): parent budget 20, 4 goals each looping echo
// until the shared *atomic.Int32 is drained — the TOTAL steps consumed across the
// tree is ≤ 20, proving the shared counter (not max_steps per child).
func TestSwarmBudgetInheritance(t *testing.T) {
	defer goleak.VerifyNone(t)
	const parentBudget = 20
	r := newRouter()
	goals := make([]string, 4)
	for i := range 4 {
		sub := fmt.Sprintf("b%d", i)
		goals[i] = sub + " task"
		r.route(sub, outcome{kind: "steps", steps: 100, text: "done"}) // greedy: each wants 100 steps
	}
	rc := testRunConfig(t, r, parentBudget)

	out, err := Run(context.Background(), rc, goals)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	_ = parseReports(t, out)
	if rem := rc.ParentBudget.Remaining(); rem < 0 {
		t.Errorf("shared budget went negative: %d", rem)
	}
	// AG-038: the synthesis reserve (budgetReserve=3) is now ATOMICALLY withheld
	// from the children and RELEASED after Run returns, so the greedy children drain
	// only the non-reserved pool (20-3=17) and Remaining is the released reserve (3),
	// never 0 — the parent's synthesis turn is genuinely protected. The shared-atomic
	// bound still holds (≤ parentBudget total, never < 0).
	if rem := rc.ParentBudget.Remaining(); rem != budgetReserve {
		t.Errorf("Remaining = %d, want %d (the synthesis reserve is protected and released after Run)", rem, budgetReserve)
	}
}

// TestSwarmBudgetPreflight (D-09): when Remaining() < len(goals)+reserve the spawn
// is rejected with a structured string and NO worker runs (goleak clean, and the
// router recorded zero calls).
func TestSwarmBudgetPreflight(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().route("x", outcome{kind: "ok", text: "should not run"})
	rc := testRunConfig(t, r, 4) // 4 remaining; 3 goals need 3+3=6 > 4

	out, err := Run(context.Background(), rc, []string{"x1", "x2", "x3"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out, "insufficient budget") {
		t.Errorf("preflight output = %q, want an insufficient-budget error string", out)
	}
	r.mu.Lock()
	total := 0
	for _, c := range r.calls {
		total += c
	}
	r.mu.Unlock()
	if total != 0 {
		t.Errorf("preflight rejected but %d worker Stream calls fired (want 0)", total)
	}
}

// TestSwarmTranscript (D-18, Pitfall 4): each child's transcript lands at
// <RunDir>/<convID>/swarm/w<i>.jsonl and the worker SessionID contains no '/'.
func TestSwarmTranscript(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRouter().
		route("alpha", outcome{kind: "ok", text: "A"}).
		route("bravo", outcome{kind: "ok", text: "B"})
	rc := testRunConfig(t, r, 25)

	if _, err := Run(context.Background(), rc, []string{"alpha task", "bravo task"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for i := 1; i <= 2; i++ {
		path := filepath.Join(rc.Cfg.RunDir, rc.ConvID, "swarm", fmt.Sprintf("w%d.jsonl", i))
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("transcript w%d missing at %s: %v", i, path, err)
			continue
		}
		if len(raw) == 0 {
			t.Errorf("transcript w%d is empty", i)
		}
		var ev agent.Event
		first := strings.SplitN(strings.TrimRight(string(raw), "\n"), "\n", 2)[0]
		if err := json.Unmarshal([]byte(first), &ev); err != nil {
			t.Errorf("transcript w%d first line not a valid Event: %v", i, err)
		}
	}
	// SessionID flat-id invariant: the suffix the runner builds has no slash.
	sid := fmt.Sprintf("%s-swarm-%s", rc.ConvID, "w1")
	if strings.ContainsAny(sid, `/\`) {
		t.Errorf("worker SessionID %q must be flat (no path separator)", sid)
	}
}

// runChild's 51-11 ChildID-fallback and terminal-marker coverage lives in
// swarm_childid_test.go (split out, CLAUDE.md's 600-LOC ceiling).
