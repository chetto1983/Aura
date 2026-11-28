package agent

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolapprovals"
	"github.com/google/uuid"
)

// gatedDeleteArgs is a skill_manage delete, the Destructive call the gateway asks about.
const gatedDeleteArgs = `{"action":"delete","name":"obsolete-skill"}`

// parkClient replays one scripted model round per Stream call; past the script it answers
// "done". onStream runs before a round is answered, for a test that acts during a model call.
type parkClient struct {
	mu       sync.Mutex
	rounds   [][]llm.Chunk
	streams  int
	onStream func(round int)
}

func (c *parkClient) Stream(context.Context, llm.Request) (<-chan llm.Chunk, error) {
	c.mu.Lock()
	round := c.streams
	c.streams++
	c.mu.Unlock()
	if c.onStream != nil {
		c.onStream(round)
	}
	chunks := []llm.Chunk{{Text: "done"}, {FinishReason: "stop"}}
	if round < len(c.rounds) {
		chunks = c.rounds[round]
	}
	ch := make(chan llm.Chunk, len(chunks))
	for _, chunk := range chunks {
		ch <- chunk
	}
	close(ch)
	return ch, nil
}

func (c *parkClient) streamCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams
}

func callsRound(calls ...llm.ToolCall) []llm.Chunk {
	out := make([]llm.Chunk, 0, len(calls)+1)
	for i := range calls {
		call := calls[i]
		out = append(out, llm.Chunk{ToolCall: &call})
	}
	return append(out, llm.Chunk{FinishReason: "tool_calls"})
}

// execLog records, in order, every call a fixture tool actually executed.
type execLog struct {
	mu  sync.Mutex
	ran []string
}

func (l *execLog) add(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ran = append(l.ran, entry)
}

func (l *execLog) entries() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.ran)
}

// parkTool is a fixture tool that logs each execution as "<name> <args>". It describes
// itself for an approval card, recording the call context the describer was handed.
type parkTool struct {
	spec        tools.Spec
	log         *execLog
	describeErr error
	described   []string // "<session id>/<tool call id>" per describe
}

func (p *parkTool) Spec() tools.Spec { return p.spec }

func (p *parkTool) Execute(_ context.Context, raw json.RawMessage) (tools.ToolResult, error) {
	p.log.add(p.spec.Name + " " + string(raw))
	return tools.ToolResult{Preview: "ran " + p.spec.Name}, nil
}

func (p *parkTool) DescribeForApproval(ctx context.Context, _ json.RawMessage) (string, error) {
	p.described = append(p.described, tools.SessionIDFromContext(ctx)+"/"+tools.ToolCallIDFromContext(ctx))
	return "Skill obsoleta", p.describeErr
}

func fixtureSpec(name string, mutating bool) tools.Spec {
	spec := tools.Spec{Name: name, Summary: name, Description: name, Parameters: json.RawMessage(`{"type":"object"}`), Mutating: mutating}
	if mutating {
		spec.OperationScope, spec.OperationNormalizer, spec.ReplayPolicy = tools.OperationScopeAgent, tools.OperationNormalizerCanonical, tools.ReplayToolResult
	}
	return spec
}

// parkFixture is a strict-profile agent with a person on the turn: a gated skill_manage, a
// read and a write that need nobody, a trusted outbound send, a host-reviewed task and the
// terminal.
type parkFixture struct {
	agent  *LlmAgent
	client *parkClient
	gw     *gateway.Gateway
	log    *execLog
	gated  *parkTool
	review *reviewerTool
	send   *draftSpyTool
	drafts *draftSpyCreator
}

func newParkFixture(t *testing.T, parkApprovals bool, rounds ...[]llm.Chunk) *parkFixture {
	t.Helper()
	log := &execLog{}
	f := &parkFixture{
		client: &parkClient{rounds: rounds},
		gw:     gateway.New(config.ProfileSingleUserHardened, &fakeReserveStore{}),
		log:    log,
		gated:  &parkTool{spec: fixtureSpec("skill_manage", true), log: log},
		review: &reviewerTool{},
		send: &draftSpyTool{spec: tools.Spec{
			Name: "calendar__send_email", TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "send_email",
			Mutating: true, Destructive: true,
		}},
		drafts: &draftSpyCreator{},
	}
	registry := tools.NewRegistry()
	registry.Register(tools.TextResponse{})
	registry.Register(tools.AskUser{})
	registry.Register(f.gated)
	registry.Register(&parkTool{spec: fixtureSpec("read_notes", false), log: log})
	registry.Register(&parkTool{spec: fixtureSpec("notes_write", true), log: log})
	registry.Register(f.send)
	registry.Register(f.review)
	f.agent = NewLlmAgent(LlmAgentConfig{
		Client:        f.client,
		LLM:           llm.Config{Model: "m", Provider: "test", TotalTimeoutSec: 30},
		Registry:      registry,
		PreviewCap:    2048,
		RunDir:        t.TempDir(),
		SessionID:     uuid.Must(uuid.NewV7()).String(),
		UserTurns:     []llm.Message{{Role: llm.RoleUser, Content: "tidy up the skills"}},
		Gateway:       f.gw,
		MessageDrafts: f.drafts,
		ParkApprovals: parkApprovals,
	})
	return f
}

// personCtx is an interactive turn: an identity and a person who can answer.
func personCtx() context.Context { return gateway.WithResponder(ownerCtx()) }

func (f *parkFixture) run(t *testing.T, ctx context.Context) []*Event {
	t.Helper()
	evs, err := collectInternal(f.agent.Run(InvocationContext{Ctx: ctx, RequestID: uuid.Must(uuid.NewV7()), Budget: testBudget(t)}))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return evs
}

func pausesIn(evs []*Event) []*AwaitingInput {
	var out []*AwaitingInput
	for _, ev := range evs {
		if ev.Actions.AwaitingInput != nil {
			out = append(out, ev.Actions.AwaitingInput)
		}
	}
	return out
}

// parkedTurnIn returns the round's ParkedTurn and fails unless there is exactly one.
func parkedTurnIn(t *testing.T, evs []*Event) *ParkedTurn {
	t.Helper()
	var found []*ParkedTurn
	for _, ev := range evs {
		if ev.Actions.ParkedTurn != nil {
			found = append(found, ev.Actions.ParkedTurn)
		}
	}
	if len(found) != 1 {
		t.Fatalf("ParkedTurn events = %d, want 1", len(found))
	}
	return found[0]
}

func hasParkedTurn(evs []*Event) bool {
	return slices.ContainsFunc(evs, func(ev *Event) bool { return ev.Actions.ParkedTurn != nil })
}

func hasFinalAnswer(evs []*Event) bool {
	return slices.ContainsFunc(evs, func(ev *Event) bool { return ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" })
}

func callIDs(calls []llm.ToolCall) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		out[i] = c.ID
	}
	return out
}

// toolAnswers lists the call ids the history answers with a tool message, in order.
func toolAnswers(history []llm.Message) []string {
	var out []string
	for _, m := range history {
		if m.Role == llm.RoleTool {
			out = append(out, m.ToolCallID)
		}
	}
	return out
}

func lastAssistantCalls(history []llm.Message) []llm.ToolCall {
	for _, m := range slices.Backward(history) {
		if m.Role == llm.RoleAssistant && len(m.ToolCalls) > 0 {
			return m.ToolCalls
		}
	}
	return nil
}

// The source the agent stamps is the record's own value, which the runner stores as is.
func TestApprovalSourceIsTheRecordsGatewaySource(t *testing.T) {
	if approvalSourceGateway != string(toolapprovals.SourceGateway) {
		t.Fatalf("approvalSourceGateway = %q, want %q", approvalSourceGateway, toolapprovals.SourceGateway)
	}
}

func TestGatedCallParksInsteadOfExecuting(t *testing.T) {
	f := newParkFixture(t, true, callsRound(toolCall("G", "skill_manage", gatedDeleteArgs)))
	evs := f.run(t, personCtx())

	pauses := pausesIn(evs)
	if len(pauses) != 1 || pauses[0].ToolCallID != "G" || pauses[0].Approval == nil {
		t.Fatalf("pauses = %+v, want one gateway approval for G", pauses)
	}
	p, approval := pauses[0], pauses[0].Approval
	if approval.Fingerprint != tools.CallFingerprint(json.RawMessage(gatedDeleteArgs)) || string(approval.ExecuteArgs) != gatedDeleteArgs {
		t.Fatalf("approval = %+v, want the model's arguments and their fingerprint", approval)
	}
	if approval.Source != approvalSourceGateway || approval.ToolName != "skill_manage" || approval.CallIndex != 0 ||
		approval.SubjectTool != "skill_manage" || approval.SubjectAction != "delete" || approval.Presentation.Key != approvaltext.GatewayKey {
		t.Fatalf("approval = %+v, want the gateway's skill_manage delete card at index 0", approval)
	}
	if p.Kind != tools.KindApproval || p.Priority != 80 || len(p.Options) != 3 || len(p.ResumeContext) != 0 || p.Question == "" {
		t.Fatalf("pause = %+v, want the host's card with three scopes and no streamed context", p)
	}
	if got := f.log.entries(); len(got) != 0 {
		t.Fatalf("the parked call ran: %v", got)
	}
	if hasFinalAnswer(evs) || f.client.streamCount() != 1 {
		t.Fatalf("after the park: final answer %v, model calls %d; want none and 1", hasFinalAnswer(evs), f.client.streamCount())
	}
	if answers := toolAnswers(f.agent.HistoryForTest()); len(answers) != 0 {
		t.Fatalf("the parked call was answered in history: %v", answers)
	}
}

func TestSeveralParksInOneTurn(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("G1", "skill_manage", `{"action":"delete","name":"first"}`),
		toolCall("G2", "skill_manage", `{"action":"delete","name":"second"}`),
	))
	evs := f.run(t, personCtx())

	pauses := pausesIn(evs)
	if len(pauses) != 2 || pauses[0].ToolCallID != "G1" || pauses[1].ToolCallID != "G2" {
		t.Fatalf("pauses = %+v, want G1 then G2", pauses)
	}
	if pauses[0].Approval.CallIndex != 0 || pauses[1].Approval.CallIndex != 1 {
		t.Fatalf("call indexes = %d, %d; want 0, 1", pauses[0].Approval.CallIndex, pauses[1].Approval.CallIndex)
	}
	if turn := parkedTurnIn(t, evs); len(turn.Pauses) != 2 || turn.Pauses[0] != pauses[0] || turn.Pauses[1] != pauses[1] {
		t.Fatalf("ParkedTurn pauses = %+v, want the two pause payloads", turn.Pauses)
	}
}

// One call, one card (spec decision 2): a send the draft owns and a task the host reviews
// are never also the gateway's, even when a policy would ask about them.
func TestDraftsAndHostReviewParkFirst(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("S", "calendar__send_email", `{"to":["anna@example.com"],"subject":"Fattura","body":"In allegato."}`),
		toolCall("T", "task", reviewedArgs),
	))
	f.gw.SetPolicyStore(askPolicies{})
	evs := f.run(t, personCtx())

	pauses := pausesIn(evs)
	if len(pauses) != 2 || pauses[0].ToolCallID != "S" || pauses[1].ToolCallID != "T" {
		t.Fatalf("pauses = %+v, want the draft then the host review", pauses)
	}
	for _, p := range pauses {
		if p.Approval != nil {
			t.Fatalf("%s carries a gateway approval as well: %+v", p.ToolCallID, p.Approval)
		}
	}
	if !strings.Contains(string(pauses[0].ResumeContext), "message_draft") || pauses[1].Question != "Review" {
		t.Fatalf("pauses = %+v, want the draft's card and the host review's", pauses)
	}
	if f.send.calls != 0 || f.review.executions != 0 {
		t.Fatalf("a parked call ran: send %d, task %d", f.send.calls, f.review.executions)
	}
}

// askPolicies asks about every subject.
type askPolicies struct{}

func (askPolicies) Get(context.Context, string, string, string) (gateway.Policy, bool, error) {
	return gateway.PolicyAsk, true, nil
}

// Every case dispatches as today: nothing parks, the call is answered in history, and the
// gated tool does not run.
func TestNoParkWithout(t *testing.T) {
	gated := toolCall("G", "skill_manage", gatedDeleteArgs)
	cases := []struct {
		name   string
		park   bool
		ctx    func() context.Context
		batch  []llm.ToolCall
		adjust func(*parkFixture)
	}{
		{name: "switch off", park: false, ctx: personCtx},
		{name: "no responder", park: true, ctx: ownerCtx},
		{name: "delegated", park: true, ctx: func() context.Context { return tools.WithDelegatedDispatch(personCtx()) }},
		{name: "no identity", park: true, ctx: func() context.Context { return gateway.WithResponder(context.Background()) }},
		{name: "deferred and unloaded", park: true, ctx: personCtx, adjust: func(f *parkFixture) { f.gated.spec.Deferred = true }},
		{name: "deny policy", park: true, ctx: personCtx, adjust: func(f *parkFixture) { f.gw.SetPolicyStore(denyPolicies{}) }},
		{name: "review error", park: true, ctx: personCtx, adjust: func(f *parkFixture) { f.gated.describeErr = errors.New("card lookup failed") }},
		{name: "text_response in the batch", park: true, ctx: personCtx,
			batch: []llm.ToolCall{gated, toolCall("F", terminalTool, `{"text":"Fatto."}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batch := tc.batch
			if batch == nil {
				batch = []llm.ToolCall{gated}
			}
			f := newParkFixture(t, tc.park, callsRound(batch...))
			if tc.adjust != nil {
				tc.adjust(f)
			}
			evs := f.run(t, tc.ctx())
			if pauses := pausesIn(evs); len(pauses) != 0 || hasParkedTurn(evs) {
				t.Fatalf("parked: %+v", pauses)
			}
			if !slices.Contains(toolAnswers(f.agent.HistoryForTest()), "G") {
				t.Fatal("the call was not dispatched: no tool message answers it")
			}
			if got := f.log.entries(); len(got) != 0 {
				t.Fatalf("the gated call ran: %v", got)
			}
		})
	}
}

// AM-4: the review runs with the call's own context, so a session grant recorded for this
// conversation answers the gateway's need and the describer sees this conversation's call.
func TestReviewSeesTheCallContext(t *testing.T) {
	f := newParkFixture(t, true)
	call := toolCall("G", "skill_manage", gatedDeleteArgs)
	ctx := personCtx()
	if pauses := f.agent.pauseCalls(ctx, []llm.ToolCall{call}); len(pauses) != 1 {
		t.Fatalf("pauses = %+v, want the gated call parked", pauses)
	}
	if want := []string{f.agent.sessionID + "/G"}; !slices.Equal(f.gated.described, want) {
		t.Fatalf("described = %v, want %v", f.gated.described, want)
	}
	f.gw.RecordGrant(ctx, gateway.GrantRequest{
		ConversationID: f.agent.ledgerConvID, OperatorID: reviewOwner,
		Tool: "skill_manage", Action: "delete", Scope: gateway.ScopeSession,
	})
	if pauses := f.agent.pauseCalls(ctx, []llm.ToolCall{call}); len(pauses) != 0 {
		t.Fatalf("a session grant for the conversation still parked: %+v", pauses)
	}
}

// D18: the dedupe runs before pause detection, so one call written twice is one card.
func TestIdenticalGatedCallsInOneTurnParkOnce(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("G1", "skill_manage", gatedDeleteArgs),
		toolCall("G2", "skill_manage", `{"name":"obsolete-skill","action":"delete"}`),
	))
	evs := f.run(t, personCtx())

	if pauses := pausesIn(evs); len(pauses) != 1 || pauses[0].ToolCallID != "G1" {
		t.Fatalf("pauses = %+v, want one card for G1", pauses)
	}
	if ids := callIDs(parkedTurnIn(t, evs).Calls); !slices.Equal(ids, []string{"G1"}) {
		t.Fatalf("parked calls = %v, want [G1]", ids)
	}
	if ids := callIDs(lastAssistantCalls(f.agent.HistoryForTest())); !slices.Equal(ids, []string{"G1"}) {
		t.Fatalf("assistant calls = %v, want [G1]", ids)
	}
}

// Pass 1 keeps today's precedence (map R4): the ask_user pause owns the round, and the gated
// sibling is dropped unrun, to be emitted again next round.
func TestAskUserWithAGatedCallKeepsAskUserPrecedence(t *testing.T) {
	f := newParkFixture(t, true, callsRound(
		toolCall("Q", askUserToolName, `{"question":"Quale skill?","kind":"clarification"}`),
		toolCall("G", "skill_manage", gatedDeleteArgs),
	))
	evs := f.run(t, personCtx())

	pauses := pausesIn(evs)
	if len(pauses) != 1 || pauses[0].ToolCallID != "Q" || pauses[0].Approval != nil || hasParkedTurn(evs) {
		t.Fatalf("pauses = %+v, want the ask_user pause alone", pauses)
	}
	if ids := callIDs(lastAssistantCalls(f.agent.HistoryForTest())); !slices.Equal(ids, []string{"Q"}) {
		t.Fatalf("assistant calls = %v, want [Q]", ids)
	}
	if got := f.log.entries(); len(got) != 0 || len(toolAnswers(f.agent.HistoryForTest())) != 0 {
		t.Fatalf("the dropped sibling ran or was answered: ran %v", got)
	}
}
