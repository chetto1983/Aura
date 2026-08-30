package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/llm"
	"go.uber.org/goleak"
)

// delegation_resume_observer_test.go is DelegationResumeObserver's own coverage --
// un-park-exactly-once, the fence/agent-identity refusal guards, and the end-to-end proof
// that a SECOND runChild call seeded from a FIRST call's returned history actually
// continues the same worker (including a promoted deferred tool surviving resume without
// a second tool_search). Split out of delegation_resume_test.go (CLAUDE.md's 600-LOC
// ceiling), which keeps the pause/park half: mintFreshUUID, openPauseAndPark and
// buildResumeTurns.

// fakeResumeStore is an in-memory double for the two DelegationJobStore methods
// DelegationResumeObserver needs: ListAnsweredAwaitingInput and UnparkIngestionJob. It
// does not implement the full DelegationJobStore surface -- ProcessOnce only calls
// these two -- but satisfies the interface via embedding.
type fakeResumeStore struct {
	DelegationJobStore // nil embed: any unimplemented method panics loudly if ever called
	jobs               []documents.AnsweredAwaitingInputJob
	listErr            error
	unparkRows         map[string]int64 // job id -> RowsAffected UnparkIngestionJob should report
	unparkErr          error
	unparked           []documents.UnparkIngestionJobRequest
	rejectErr          error
	rejected           []RejectAnsweredDelegationRequest
	resolved           map[string]bool
}

func (s *fakeResumeStore) ListAnsweredAwaitingInput(context.Context, string, int) ([]documents.AnsweredAwaitingInputJob, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	jobs := make([]documents.AnsweredAwaitingInputJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		if !s.resolved[job.JobID] {
			jobs = append(jobs, job)
		}
	}
	return jobs, nil
}

func (s *fakeResumeStore) UnparkIngestionJob(_ context.Context, req documents.UnparkIngestionJobRequest) (int64, error) {
	if s.unparkErr != nil {
		return 0, s.unparkErr
	}
	s.unparked = append(s.unparked, req)
	if s.unparkRows == nil {
		if s.resolved == nil {
			s.resolved = make(map[string]bool)
		}
		s.resolved[req.JobID] = true
		return 1, nil
	}
	rows := s.unparkRows[req.JobID]
	if rows > 0 {
		if s.resolved == nil {
			s.resolved = make(map[string]bool)
		}
		s.resolved[req.JobID] = true
	}
	return rows, nil
}

func (s *fakeResumeStore) RejectAnsweredDelegation(_ context.Context, req RejectAnsweredDelegationRequest) (bool, error) {
	s.rejected = append(s.rejected, req)
	if s.rejectErr != nil {
		return false, s.rejectErr
	}
	if s.resolved == nil {
		s.resolved = make(map[string]bool)
	}
	s.resolved[req.JobID] = true
	return true, nil
}

// answeredJobPayload builds a plausible AnsweredAwaitingInputJob payload map: a
// DelegationPayload carrying a DelegationResumeState, encoded the same way
// delegationPayloadMap does at park time.
func answeredJobPayload(t *testing.T, resume *DelegationResumeState) map[string]any {
	t.Helper()
	m, err := delegationPayloadMap(DelegationPayload{
		Goal: "g", ConversationID: "conv-1", Resume: resume,
	})
	if err != nil {
		t.Fatalf("delegationPayloadMap: %v", err)
	}
	return m
}

// TestDelegationResumeObserverUnparksExactlyOnce is the plan's own named acceptance
// test: an answered, parked job is un-parked with the operator's answer filled into
// AnswerContent, and reported as unparked (RowsAffected==1 -> a second concurrent pass
// would see RowsAffected==0, which is exercised separately below).
func TestDelegationResumeObserverUnparksExactlyOnce(t *testing.T) {
	resume := &DelegationResumeState{PendingActionID: "fence-1", PendingToolCallID: "call-1"}
	job := documents.AnsweredAwaitingInputJob{
		JobID: "job-1", IdentityID: "identity-1", PendingActionID: "fence-1",
		Payload:       answeredJobPayload(t, resume),
		ResumedAnswer: []byte(`{"content":"the operator answer"}`),
	}
	store := &fakeResumeStore{jobs: []documents.AnsweredAwaitingInputJob{job}}
	o := NewDelegationResumeObserver(store)

	n, err := o.ProcessOnce(context.Background(), "identity-1", 10)
	if err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if n != 1 {
		t.Fatalf("ProcessOnce unparked %d jobs, want 1", n)
	}
	if len(store.unparked) != 1 {
		t.Fatalf("UnparkIngestionJob called %d times, want 1", len(store.unparked))
	}
	req := store.unparked[0]
	if req.JobID != "job-1" || req.IdentityID != "identity-1" {
		t.Fatalf("unpark request = %+v, want job-1/identity-1", req)
	}
	payload, err := delegationPayloadFromMap(req.Payload)
	if err != nil {
		t.Fatalf("decode unparked payload: %v", err)
	}
	if payload.Resume == nil || payload.Resume.AnswerContent != "the operator answer" {
		t.Fatalf("unparked payload resume = %+v, want AnswerContent filled from the answered pause", payload.Resume)
	}
}

// TestDelegationResumeObserverSkipsARaceLostUnpark covers the idempotency guard: when
// UnparkIngestionJob's conditional UPDATE matches zero rows (a concurrent pass already
// claimed this job), ProcessOnce reports it as NOT unparked this pass, with no error --
// exactly the "un-park exactly once" contract two racing observer passes need.
func TestDelegationResumeObserverSkipsARaceLostUnpark(t *testing.T) {
	resume := &DelegationResumeState{PendingActionID: "fence-1", PendingToolCallID: "call-1"}
	job := documents.AnsweredAwaitingInputJob{
		JobID: "job-1", IdentityID: "identity-1", PendingActionID: "fence-1",
		Payload:       answeredJobPayload(t, resume),
		ResumedAnswer: []byte(`{"content":"answer"}`),
	}
	store := &fakeResumeStore{
		jobs:       []documents.AnsweredAwaitingInputJob{job},
		unparkRows: map[string]int64{"job-1": 0},
	}
	o := NewDelegationResumeObserver(store)

	n, err := o.ProcessOnce(context.Background(), "identity-1", 10)
	if err != nil {
		t.Fatalf("ProcessOnce: %v", err)
	}
	if n != 0 {
		t.Fatalf("ProcessOnce unparked %d jobs, want 0 (race already claimed it)", n)
	}
}

// TestDelegationResumeObserverRefusesAFenceMismatch: the payload's own persisted
// PendingActionID must match the pause row's -- a mismatch means this job is NOT the
// pause that was actually answered (a stale resume state from an earlier pause/resume
// cycle on the same job), and un-parking it would rebuild the worker against the wrong
// answer. This must be a loud error, never a silent skip.
func TestDelegationResumeObserverRefusesAFenceMismatch(t *testing.T) {
	resume := &DelegationResumeState{PendingActionID: "stale-fence", PendingToolCallID: "call-1"}
	job := documents.AnsweredAwaitingInputJob{
		JobID: "job-1", IdentityID: "identity-1", PendingActionID: "current-fence",
		Payload:       answeredJobPayload(t, resume),
		ResumedAnswer: []byte(`{"content":"answer"}`),
	}
	store := &fakeResumeStore{jobs: []documents.AnsweredAwaitingInputJob{job}}
	o := NewDelegationResumeObserver(store)

	_, err := o.ProcessOnce(context.Background(), "identity-1", 10)
	if err == nil || !strings.Contains(err.Error(), "fence mismatch") {
		t.Fatalf("ProcessOnce = %v, want a fence mismatch error", err)
	}
	if len(store.unparked) != 0 {
		t.Fatal("a fence-mismatched job must not be un-parked")
	}
}

// TestDelegationResumeObserverRefusesAJobWithNoResumeState: structurally should be
// impossible (Task 1 always writes a Resume before parking), so a missing one is a
// loud error, not a silent no-op that would leave the row parked forever.
func TestDelegationResumeObserverRefusesAJobWithNoResumeState(t *testing.T) {
	job := documents.AnsweredAwaitingInputJob{
		JobID: "job-1", IdentityID: "identity-1",
		Payload:       answeredJobPayload(t, nil),
		ResumedAnswer: []byte(`{"content":"answer"}`),
	}
	store := &fakeResumeStore{jobs: []documents.AnsweredAwaitingInputJob{job}}
	o := NewDelegationResumeObserver(store)

	_, err := o.ProcessOnce(context.Background(), "identity-1", 10)
	if err == nil || !strings.Contains(err.Error(), "no resume state") {
		t.Fatalf("ProcessOnce = %v, want a no-resume-state error", err)
	}
}

func TestDelegationResumeObserverContinuesAndQuarantinesPoisonRows(t *testing.T) {
	missingResume := documents.AnsweredAwaitingInputJob{
		JobID: "poison-no-resume", IdentityID: "identity-1", PendingActionID: "fence-1",
		Payload: answeredJobPayload(t, nil), ResumedAnswer: []byte(`{"content":"answer"}`),
	}
	badAnswer := documents.AnsweredAwaitingInputJob{
		JobID: "poison-bad-answer", IdentityID: "identity-1", PendingActionID: "fence-2",
		Payload:       answeredJobPayload(t, &DelegationResumeState{PendingActionID: "fence-2"}),
		ResumedAnswer: []byte(`{"content":`),
	}
	valid := documents.AnsweredAwaitingInputJob{
		JobID: "valid", IdentityID: "identity-1", PendingActionID: "fence-3",
		Payload:       answeredJobPayload(t, &DelegationResumeState{PendingActionID: "fence-3"}),
		ResumedAnswer: []byte(`{"content":"resume me"}`),
	}
	store := &fakeResumeStore{jobs: []documents.AnsweredAwaitingInputJob{missingResume, badAnswer, valid}}
	observer := NewDelegationResumeObserver(store)

	unparked, err := observer.ProcessOnce(context.Background(), "identity-1", 10)
	if unparked != 1 {
		t.Fatalf("ProcessOnce unparked %d jobs, want the valid later row", unparked)
	}
	if err == nil || !strings.Contains(err.Error(), "no resume state") || !strings.Contains(err.Error(), "decode answer") {
		t.Fatalf("ProcessOnce error = %v, want both poison-row errors aggregated", err)
	}
	if len(store.unparked) != 1 || store.unparked[0].JobID != "valid" {
		t.Fatalf("unparked requests = %+v, want only the valid row", store.unparked)
	}
	if len(store.rejected) != 2 {
		t.Fatalf("rejected requests = %+v, want both poison rows quarantined", store.rejected)
	}
	for _, req := range store.rejected {
		if req.ErrorCode != invalidDelegationResumeCode || req.PendingActionID == "" {
			t.Fatalf("reject request lacks terminal reason/fence: %+v", req)
		}
	}

	unparked, err = observer.ProcessOnce(context.Background(), "identity-1", 10)
	if err != nil || unparked != 0 {
		t.Fatalf("second ProcessOnce = %d, %v; quarantined poison rows must not recur", unparked, err)
	}
}

// TestDelegationResumeObserverSurfacesAListFailure and
// TestDelegationResumeObserverGuardsAgainstMisconfiguration pin the same two wiring
// guards DelegationClaimLoop.ProcessOnce already pins (no store, no identity) plus the
// underlying list error passthrough.
func TestDelegationResumeObserverSurfacesAListFailure(t *testing.T) {
	store := &fakeResumeStore{listErr: errors.New("queue unreachable")}
	o := NewDelegationResumeObserver(store)
	_, err := o.ProcessOnce(context.Background(), "identity-1", 10)
	if err == nil || !strings.Contains(err.Error(), "queue unreachable") {
		t.Fatalf("ProcessOnce = %v, want the list failure surfaced", err)
	}
}

func TestDelegationResumeObserverGuardsAgainstMisconfiguration(t *testing.T) {
	if _, err := (&DelegationResumeObserver{}).ProcessOnce(context.Background(), "identity-1", 10); err == nil {
		t.Fatal("ProcessOnce on an observer with no store = nil error, want a wiring error")
	}
	o := NewDelegationResumeObserver(&fakeResumeStore{})
	if _, err := o.ProcessOnce(context.Background(), "", 10); err == nil {
		t.Fatal("ProcessOnce with no identity = nil error, want a wiring error")
	}
}

// TestRunChildResumeContinuesFromPersistedHistory is the end-to-end proof of Task 2's
// central claim: a SECOND runChild call, seeded with RunConfig.ResumeTurns built from
// the FIRST call's returned history plus a synthetic answer, produces the SAME worker
// continuing (the model sees its own prior tool_search call/result and the fresh
// answer, then completes) rather than a worker re-asking the same question or starting
// over. No mock/stub of internal/agent -- this drives the REAL agent.NewLlmAgent
// construction runChild always uses, through a scripted llm.Client double, exactly the
// way the claim loop's runWithHeartbeat does in production.
func TestRunChildResumeContinuesFromPersistedHistory(t *testing.T) {
	defer goleak.VerifyNone(t)
	const goal = "summarise the inbox"
	r := newRouter().route(goal, outcome{kind: "pause_then_ok", question: "which inbox?", text: "done after resume"})
	rc := testRunConfig(t, r, 25)

	// First call: pauses. history1 carries the seeded brief plus the synthesized
	// ask_user assistant call (runChild's own doc comment: "on a fresh pause" append).
	report1, history1 := runChild(context.Background(), rc, rc.ParentBudget, 0, goal)
	if report1.Status != StatusNeedsUserInput {
		t.Fatalf("first runChild status = %q, want needs_user_input", report1.Status)
	}
	if report1.ToolCallID == "" {
		t.Fatal("first runChild report has no ToolCallID -- resume cannot answer it")
	}
	if len(history1) == 0 {
		t.Fatal("first runChild returned no history to resume from")
	}

	// Build the resume state exactly the way openPauseAndPark/unparkOne compose it in
	// production, then seed the SECOND runChild call with it.
	state := &DelegationResumeState{
		PendingToolCallID: report1.ToolCallID,
		AnswerContent:     "the inbox is support@example.com",
		History:           history1,
	}
	rc.ResumeTurns = buildResumeTurns(state)

	report2, history2 := runChild(context.Background(), rc, rc.ParentBudget, 0, goal)
	if report2.Status != StatusOK {
		t.Fatalf("resumed runChild status = %q (%s), want ok", report2.Status, report2.Error)
	}
	if report2.Summary != "done after resume" {
		t.Fatalf("resumed runChild summary = %q, want the post-resume completion", report2.Summary)
	}
	// Exactly 2 Stream calls total for this goal: the original pause + the resumed
	// completion -- never a third (which would mean a re-ask or a duplicate dispatch).
	r.mu.Lock()
	calls := r.calls[goal]
	r.mu.Unlock()
	if calls != 2 {
		t.Fatalf("Stream was called %d times for the goal, want exactly 2 (pause + resume)", calls)
	}
	// history2's seed must be rc.ResumeTurns verbatim (runChild's own contract: "the
	// reconstruction's own seed is always exactly what was passed to NewLlmAgent this
	// time") -- so the answer turn from state survives into the SECOND report's own
	// reconstructed history too.
	found := false
	for _, m := range history2 {
		if m.Role == llm.RoleTool && m.ToolCallID == report1.ToolCallID && m.Content == state.AnswerContent {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("resumed history does not carry the answer turn that unblocked the worker")
	}
}

// TestDelegationResumeStateHistoryRoundTripsTheToolSearchPair pins T-51-48's own named
// regression: the tool_search assistant ToolCalls entry AND its paired RoleTool result
// (matched by ToolCallID) must both survive a payload jsonb marshal/unmarshal
// (delegationPayloadMap -> delegationPayloadFromJob), byte-identical, because that pair
// -- and nothing else -- is what deriveActivated/deriveEverLoaded re-grant a deferred
// tool from at rebuild. Losing either half silently un-grants every deferred tool the
// worker had loaded.
func TestDelegationResumeStateHistoryRoundTripsTheToolSearchPair(t *testing.T) {
	searchArgs, _ := json.Marshal(map[string]string{"query": "select:special_tool"})
	searchResult := "## special_tool\nParameters:\n  {\"type\":\"object\"}\n\n"
	history := []llm.Message{
		{Role: llm.RoleUser, Content: "brief"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
			agenttest.MakeToolCall("call-search", "tool_search", string(searchArgs)),
		}},
		{Role: llm.RoleTool, ToolCallID: "call-search", Content: searchResult},
	}
	state := &DelegationResumeState{
		WorkerID: "job-1", Goal: "g", ConversationID: "conv-1",
		PendingToolCallID: "call-pause", PendingActionID: "fence-1", PauseToken: "token-1",
		AgentIdentity: "identity-1", History: history,
	}
	payload := DelegationPayload{Goal: "g", ConversationID: "conv-1", FanoutKey: "f-test", Resume: state}

	m, err := delegationPayloadMap(payload)
	if err != nil {
		t.Fatalf("delegationPayloadMap: %v", err)
	}
	back, err := delegationPayloadFromJob(documents.IngestionJob{Payload: m})
	if err != nil {
		t.Fatalf("delegationPayloadFromJob: %v", err)
	}
	if back.Resume == nil {
		t.Fatal("round trip lost the Resume state entirely")
	}
	if len(back.Resume.History) != len(history) {
		t.Fatalf("round-tripped history has %d turns, want %d", len(back.Resume.History), len(history))
	}
	assistantTurn := back.Resume.History[1]
	if len(assistantTurn.ToolCalls) != 1 || assistantTurn.ToolCalls[0].Function.Name != "tool_search" {
		t.Fatalf("round-tripped assistant turn = %+v, want the tool_search ToolCalls entry preserved", assistantTurn)
	}
	searchCallID := assistantTurn.ToolCalls[0].ID
	toolTurn := back.Resume.History[2]
	if toolTurn.Role != llm.RoleTool || toolTurn.ToolCallID != searchCallID {
		t.Fatalf("round-tripped tool turn ToolCallID = %q, want it paired with the search call %q", toolTurn.ToolCallID, searchCallID)
	}
	if toolTurn.Content != searchResult {
		t.Fatalf("round-tripped tool_search result content = %q, want it byte-identical to the original", toolTurn.Content)
	}
}

// TestProcessJobRefusesAnAgentIdentityMismatch is the plan's own named acceptance test
// (T-51-36, LibreChat's agent_id rule): a resume state whose AgentIdentity does not
// match the identity the job is claimed under must refuse loudly and dispatch NOTHING
// -- never a silent rebuild under someone else's registry, gateway or budget. The
// scripted client here panics if Stream is ever called, so a passing test proves no
// worker was constructed at all.
func TestProcessJobRefusesAnAgentIdentityMismatch(t *testing.T) {
	defer goleak.VerifyNone(t)
	panicIfCalledClient := panicClient{t: t}
	store := &fakeDelegationStore{claimJobs: []documents.IngestionJob{{
		ID: "j1", IdentityID: "identity-claimed", JobType: JobTypeSwarmDelegation,
		Payload: map[string]any{
			"goal": "g", "conversation_id": "conv-1", "fanout_key": "f-test",
			"resume": map[string]any{
				"agent_identity":       "identity-that-paused",
				"pending_tool_call_id": "call-1",
				"pending_action_id":    "fence-1",
			},
		},
		MaxAttempts: 3,
	}}}
	l := &DelegationClaimLoop{
		Store: store, IdentityID: "identity-claimed",
		Worker: testRunConfig(t, panicIfCalledClient, 5),
	}
	n, err := l.ProcessOnce(withToolCtx(context.Background(), t))
	if err != nil {
		t.Fatalf("ProcessOnce = %v, want the mismatch handled (not propagated) via recordFailure", err)
	}
	l.Wait()
	if n != 1 {
		t.Fatalf("processed = %d, want 1 (the mismatched row still counts as handled)", n)
	}
	if len(store.retriesSnapshot()) != 1 || !strings.Contains(store.retriesSnapshot()[0].ErrorMessage, "agent identity mismatch") {
		t.Fatalf("retries = %+v, want one naming the agent identity mismatch", store.retriesSnapshot())
	}
}

// panicClient is an llm.Client that fails the test the instant Stream is called --
// the negative-space proof that a refused resume never constructs a worker.
type panicClient struct{ t *testing.T }

func (c panicClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	c.t.Helper()
	c.t.Fatal("Stream called on a resume that should have been refused before any worker construction")
	return nil, nil
}

// deferredPromotionTool is a fixture deferred tool that counts real dispatches --
// distinguishing "the dispatch gate bounced the call" (count stays 0) from "the call
// actually reached Execute" (count becomes 1), which a bounce's own RoleTool error
// result cannot distinguish from the outside.
type deferredPromotionTool struct {
	mu    *sync.Mutex
	calls *int
}

func (d deferredPromotionTool) Spec() tools.Spec {
	return tools.Spec{
		Name: "special_tool", Summary: "fixture", Description: "fixture",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: true,
	}
}

func (d deferredPromotionTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	d.mu.Lock()
	*d.calls++
	d.mu.Unlock()
	return tools.NewResult(ctx, "special tool executed")
}

// sequentialClient replays one llm.Client response per Stream call, in order, then
// idles with a bare "stop" -- the shape a hand-scripted multi-turn worker conversation
// needs that routerClient's single-outcome-per-goal shape cannot express.
type sequentialClient struct {
	mu    sync.Mutex
	call  int
	steps []func() (<-chan llm.Chunk, error)
}

func (c *sequentialClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	c.mu.Lock()
	i := c.call
	c.call++
	c.mu.Unlock()
	if i >= len(c.steps) {
		return closedChan(llm.Chunk{FinishReason: "stop"}), nil
	}
	return c.steps[i]()
}

func (c *sequentialClient) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.call
}

// TestDeferredToolPromotionSurvivesResume is the plan's own named acceptance test: a
// worker that promotes a deferred tool via tool_search, then pauses, must be able to
// dispatch that SAME tool after resume WITHOUT a second tool_search call -- because
// deriveActivated/deriveEverLoaded re-read the persisted tool_search assistant/RoleTool
// pair from the seeded ResumeTurns at construction (internal/agent stays untouched).
func TestDeferredToolPromotionSurvivesResume(t *testing.T) {
	defer goleak.VerifyNone(t)
	var mu sync.Mutex
	calls := 0
	deferredTool := deferredPromotionTool{mu: &mu, calls: &calls}

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	reg.Register(deferredTool)
	reg.Register(&tools.ToolSearch{Registry: reg})

	searchArgs, _ := json.Marshal(map[string]string{"query": "select:special_tool"})
	pauseArgs, _ := json.Marshal(map[string]any{"question": "which target?", "kind": "choice"})
	specialArgs, _ := json.Marshal(map[string]string{})

	client := &sequentialClient{steps: []func() (<-chan llm.Chunk, error){
		// 1: tool_search promotes special_tool.
		func() (<-chan llm.Chunk, error) {
			return toolChan(agenttest.MakeToolCall("call-search", "tool_search", string(searchArgs))), nil
		},
		// 2: ask_user pauses the worker.
		func() (<-chan llm.Chunk, error) {
			return toolChan(agenttest.MakeToolCall("call-pause", "ask_user", string(pauseArgs))), nil
		},
		// 3 (post-resume): special_tool dispatched -- NO second tool_search preceding it.
		func() (<-chan llm.Chunk, error) {
			return toolChan(agenttest.MakeToolCall("call-special", "special_tool", string(specialArgs))), nil
		},
		// 4: the worker finishes.
		func() (<-chan llm.Chunk, error) {
			return closedChan(llm.Chunk{Text: "resumed and used the tool"}, llm.Chunk{FinishReason: "stop"}), nil
		},
	}}

	rc := testRunConfig(t, client, 25)
	rc.ParentRegistry = reg

	report1, history1 := runChild(context.Background(), rc, rc.ParentBudget, 0, "promote then pause")
	if report1.Status != StatusNeedsUserInput {
		t.Fatalf("first runChild status = %q (%s), want needs_user_input", report1.Status, report1.Error)
	}
	if calls != 0 {
		t.Fatalf("special_tool dispatched %d times before the pause, want 0", calls)
	}

	state := &DelegationResumeState{PendingToolCallID: report1.ToolCallID, AnswerContent: "target-x", History: history1}
	rc.ResumeTurns = buildResumeTurns(state)

	report2, _ := runChild(context.Background(), rc, rc.ParentBudget, 0, "promote then pause")
	if report2.Status != StatusOK {
		t.Fatalf("resumed runChild status = %q (%s), want ok", report2.Status, report2.Error)
	}
	if report2.Summary != "resumed and used the tool" {
		t.Fatalf("resumed runChild summary = %q, want the post-tool completion", report2.Summary)
	}
	if calls != 1 {
		t.Fatalf("special_tool dispatched %d times total, want exactly 1 -- a 0 means the dispatch gate bounced it (the promotion was lost), a 2+ means it was called twice", calls)
	}
	if got := client.callCount(); got != 4 {
		t.Fatalf("Stream called %d times total, want exactly 4 (search, pause, special_tool, finish) -- a second tool_search would show up as a 5th", got)
	}
}
