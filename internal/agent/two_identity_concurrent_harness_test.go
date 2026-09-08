// two_identity_concurrent_harness_test.go builds the ISO-05 concurrent-runner
// harness (D-14): two LlmAgents constructed EXACTLY the way runner.buildAgent
// constructs them (internal/runner/runner.go:375-419) — a fresh agent.NewBudget
// and a fresh agent.NewLlmAgent per run — sharing ONE *tools.Registry, ONE
// *gateway.Gateway, ONE SteerInbox and ONE llm.Client (the four D-15 surfaces
// this plan constrains), while carrying genuinely different identityctx
// principals and distinct conversation UUIDs. Assertions live in
// two_identity_concurrent_test.go; this file is construction + rendezvous only,
// so neither file approaches the 600-line ceiling (01-05-PLAN.md Task 1).
//
// Package agent_test (external, black-box) deliberately: it reuses
// agenttest.FakeClient, echoTool, textResponseCall and collect from
// llm_agent_test.go (CLAUDE.md REUSABLE CODE — no third set of fakes), and
// every assertion in this plan is provable through LlmAgent's EXPORTED surface
// (FakeClient.RecordedRequests, tools.ReadToolOutput, Budget.ConsumeStep,
// SteerInbox.Drain, tools.Registry.All) without reaching into unexported
// fields — which also keeps these files out of the internal/agent import-cycle
// trap agenttest's own doc comment names (agenttest imports internal/agent, so
// an INTERNAL `package agent` test file cannot import agenttest without a
// build cycle; no existing file in this package does).
package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// identityA/identityB are the two distinct identityctx principals every
// harness build carries. UUID-shaped but not real identities — this package
// never touches Postgres.
const (
	identityA = "a0000000-0000-4000-8000-00000000000a"
	identityB = "b0000000-0000-4000-8000-00000000000b"
)

// identityRoutedClient implements llm.Client and demultiplexes each Stream
// call to the correct underlying *agenttest.FakeClient by req.SessionID.
//
// It exists because agenttest.FakeClient's own script is a single global FIFO
// ordinal (Stream pops Turns[next] regardless of caller) — correct for one
// conversation, but unusable for TWO conversations racing into the SAME
// client object with deterministic, run-specific scripts: whichever goroutine
// happened to call Stream first would consume the other run's scripted turn.
// Routing on req.SessionID (LlmAgentConfig.SessionID, forwarded verbatim into
// every Request — confirmed by TestLlmAgent_ForwardsSessionID in
// llm_agent_test.go) rather than on anything identity-derived from ctx keeps
// the router itself inert with respect to what this plan is proving: it reads
// the SAME field a real OpenRouter sticky-routing client would key on, never
// req.Messages content, so it neither manufactures nor hides D-15 behavior.
// This is the ONE llm.Client both LlmAgents hold (D-15 surface 4) — a thin
// demultiplexer over two REAL agenttest.FakeClient instances, not a third
// competing fake.
type identityRoutedClient struct {
	bySessionID map[string]*agenttest.FakeClient
}

var _ llm.Client = (*identityRoutedClient)(nil)

func newIdentityRoutedClient(routes map[string]*agenttest.FakeClient) *identityRoutedClient {
	return &identityRoutedClient{bySessionID: routes}
}

func (c *identityRoutedClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	fc, ok := c.bySessionID[req.SessionID]
	if !ok {
		return nil, fmt.Errorf("identityRoutedClient: no fake client registered for session %q", req.SessionID)
	}
	return fc.Stream(ctx, req)
}

// twoIdentityRun bundles one identity's agent + its InvocationContext + its
// own dedicated FakeClient (reached through the shared router), mirroring
// what runner.buildAgent constructs per turn.
type twoIdentityRun struct {
	identity  string
	sessionID string
	agent     *agent.LlmAgent
	ic        agent.InvocationContext
	fc        *agenttest.FakeClient
}

// harnessOptions lets a subtest override the SHARED collaborators the D-15
// surfaces are actually about. The zero value builds two agents that are
// fully separated by construction; Task 3 deliberately breaks each field in
// turn to prove the corresponding assertion goes red (VALIDATION.md's Manual-
// Only recipe: same SessionID, then same *Budget, then cross-conversation
// steer drain, then an identity value in the system message).
type harnessOptions struct {
	// sameSessionID, when non-empty, is used as BOTH runs' SessionID instead
	// of two distinct UUIDs — Task 3's sidecar-surface break.
	sameSessionID string
}

// newTwoIdentityHarness builds two LlmAgents EXACTLY the way runner.buildAgent
// does (internal/runner/runner.go:375-419): a fresh agent.NewBudget + a fresh
// agent.NewLlmAgent per run, sharing ONE *tools.Registry, ONE *gateway.Gateway,
// ONE SteerInbox and ONE llm.Client — the four D-15 surfaces this plan
// constrains — while identityA and identityB carry genuinely different
// identityctx principals and (by default) distinct conversation UUIDs.
func newTwoIdentityHarness(
	t *testing.T,
	registry *tools.Registry,
	gw *gateway.Gateway,
	steerInbox agent.SteerInbox,
	runDir string,
	turnsA, turnsB []agenttest.FakeTurn,
	maxStepsA, maxStepsB int,
	opts harnessOptions,
) (runA, runB *twoIdentityRun) {
	t.Helper()
	sessA := uuid.Must(uuid.NewV7()).String()
	sessB := uuid.Must(uuid.NewV7()).String()
	if opts.sameSessionID != "" {
		sessA = opts.sameSessionID
		sessB = opts.sameSessionID
	}
	fcA := agenttest.NewFakeClient(turnsA...)
	fcB := agenttest.NewFakeClient(turnsB...)
	routes := map[string]*agenttest.FakeClient{sessA: fcA}
	// A same-session break (Task 3) collapses both routes onto one client —
	// the router degrades exactly the way a real OpenRouter sticky key would,
	// which is the honest failure shape for that break.
	if sessA != sessB {
		routes[sessB] = fcB
	}
	client := newIdentityRoutedClient(routes)

	build := func(identity, sessionID string, fc *agenttest.FakeClient, label string, maxSteps int) *twoIdentityRun {
		steps := maxSteps
		bud, err := agent.NewBudget(agent.BudgetOptions{MaxSteps: &steps})
		if err != nil {
			t.Fatalf("NewBudget(%s): %v", identity, err)
		}
		ctx := identityctx.WithIdentityID(context.Background(), identity)
		ctx = gateway.WithResponder(ctx) // mirrors runner.buildAgent's interactive composition root
		la := agent.NewLlmAgent(agent.LlmAgentConfig{
			Client:               client,
			LLM:                  llm.Config{Model: "test-model", Provider: "test-provider", TotalTimeoutSec: 30},
			Registry:             registry,
			PreviewCap:           2048,
			RunDir:               runDir,
			SessionID:            sessionID,
			UserTurns:            []llm.Message{{Role: llm.RoleUser, Content: "identity " + identity + " turn: " + label}},
			Gateway:              gw,
			Steer:                steerInbox,
			LedgerConversationID: sessionID,
		})
		ic := agent.InvocationContext{
			Ctx:       ctx,
			Agent:     la,
			RequestID: uuid.Must(uuid.NewV7()),
			Branch:    "root",
			Budget:    bud,
		}
		return &twoIdentityRun{identity: identity, sessionID: sessionID, agent: la, ic: ic, fc: fc}
	}

	runA = build(identityA, sessA, fcA, "A", maxStepsA)
	runB = build(identityB, sessB, fcB, "B", maxStepsB)
	return runA, runB
}

// runBothConcurrently races runA.agent.Run and runB.agent.Run in real
// goroutines, released by one closed-channel start barrier so both are
// genuinely in flight before either proceeds — never a sequential for loop
// over two agents. internal/arcadedb/concurrent_fact_write_test.go's own
// header names that failure mode ("a sequential `for` loop ... passes green
// while leaving the actual race ... completely unexercised") as exactly what
// this harness exists to avoid.
func runBothConcurrently(t *testing.T, runA, runB *twoIdentityRun) (evsA []*agent.Event, errA error, evsB []*agent.Event, errB error) {
	t.Helper()
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		evsA, errA = collect(runA.agent.Run(runA.ic))
	}()
	go func() {
		defer wg.Done()
		<-start
		evsB, errB = collect(runB.agent.Run(runB.ic))
	}()
	close(start)
	wg.Wait()
	return evsA, errA, evsB, errB
}

// collisionCall is one recorded invocation of collisionTool.Execute.
type collisionCall struct {
	sessionID string
	identity  string
	args      string
	seq       int
}

// collisionRecorder is the mutex-protected call log collisionTool writes to.
// -race is what proves this needs the lock; goleak proves nothing leaks
// holding it.
type collisionRecorder struct {
	mu    sync.Mutex
	calls []collisionCall
	seq   int
}

func (r *collisionRecorder) record(ctx context.Context, args string) collisionCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	c := collisionCall{
		sessionID: tools.SessionIDFromContext(ctx),
		identity:  identityctx.IdentityID(ctx),
		args:      args,
		seq:       r.seq,
	}
	r.calls = append(r.calls, c)
	return c
}

func (r *collisionRecorder) snapshot() []collisionCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]collisionCall(nil), r.calls...)
}

// collisionTool is the tool BOTH runs call with byte-identical arguments,
// overlapping in time ON PURPOSE (D-14). wg is a shared 2-count rendezvous
// barrier: each Execute calls Done() then Wait(), so BOTH calls are
// deterministically IN Execute together before EITHER can return — the
// "overlapping in time" half of the collision is forced by construction, not
// left to goroutine-scheduling luck (which is what would make this a
// randomized-interleaving harness, the shape D-14 explicitly rejects).
type collisionTool struct {
	rec *collisionRecorder
	wg  *sync.WaitGroup
}

func (t *collisionTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "collision_probe",
		Summary:     "Test-only tool that rendezvous-barriers with its concurrent twin before returning.",
		Description: "Test-only tool for the ISO-05 concurrent-runner harness.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"op":{"type":"string"}}}`),
		Deferred:    false,
	}
}

func (t *collisionTool) Execute(ctx context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	call := t.rec.record(ctx, string(raw))
	t.wg.Done()
	t.wg.Wait()
	return tools.NewResult(ctx, fmt.Sprintf("executed-for-session:%s-seq:%d", call.sessionID, call.seq))
}

// bigOutputRecorder captures the ToolResult.FullPath and session id a
// bigOutputTool call actually spilled to, so Task 2's sidecar assertions can
// read the REAL production sidecarPath output without needing to call the
// unexported sidecarPath/validateID helpers directly (they live in package
// tools, not agent_test).
type bigOutputRecorder struct {
	mu        sync.Mutex
	fullPath  string
	sessionID string
}

func (r *bigOutputRecorder) record(ctx context.Context, res tools.ToolResult) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fullPath = res.FullPath
	r.sessionID = tools.SessionIDFromContext(ctx)
}

func (r *bigOutputRecorder) snapshot() (fullPath, sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fullPath, r.sessionID
}

// bigOutputTool returns content larger than the harness's PreviewCap (2048
// bytes) so tools.NewResult spills it to the per-session sidecar
// (internal/agent/tools/result.go), letting Task 2 assert on the REAL
// sidecarPath output via the returned ToolResult.FullPath.
type bigOutputTool struct {
	name    string
	payload string
	rec     *bigOutputRecorder
}

func (t *bigOutputTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        t.name,
		Summary:     "Test-only large-output tool that forces sidecar spillover.",
		Description: "Test-only tool for the ISO-05 concurrent-runner harness.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Deferred:    false,
	}
}

func (t *bigOutputTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	res, err := tools.NewResult(ctx, t.payload)
	if err != nil {
		return tools.ToolResult{}, err
	}
	t.rec.record(ctx, res)
	return res, nil
}

// registryToolNames returns the SORTED tool names currently in r — a cheap,
// exported-surface snapshot the "registry unmutated by concurrent runs"
// assertion diffs before/after (tools.Registry.All is exported; the
// per-run deferred-tool activation state it must NOT touch,
// LlmAgent.activated/everLoaded, lives on the agent instead — see
// llm_agent.go's own field comments). Registry.All's own doc comment states
// its order is map-iteration order and "deliberately not stable" — sorting
// here is required, not cosmetic, or two calls to this helper over the SAME
// unchanged registry could spuriously differ and fail the assertion for a
// reason that has nothing to do with what this test is proving.
func registryToolNames(r *tools.Registry) []string {
	all := r.All()
	names := make([]string, 0, len(all))
	for _, tool := range all {
		names = append(names, tool.Spec().Name)
	}
	sort.Strings(names)
	return names
}
