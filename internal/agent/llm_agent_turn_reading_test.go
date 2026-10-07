package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

type fakeClassifier struct {
	verdict prompt.ReasoningVerdict
	ok      bool
	calls   int
	texts   []string
}

func (c *fakeClassifier) Classify(_ context.Context, text string) (prompt.ReasoningVerdict, bool) {
	c.calls++
	c.texts = append(c.texts, text)
	return c.verdict, c.ok
}

// readingClient answers each Stream with the next scripted text; "" fails to open.
type readingClient struct {
	answers   []string
	requests  []llm.Request
	onRequest func()
}

func (c *readingClient) Stream(_ context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if c.onRequest != nil {
		c.onRequest()
	}
	c.requests = append(c.requests, req)
	answer := "Fatto."
	if index := len(c.requests) - 1; index < len(c.answers) {
		answer = c.answers[index]
	}
	if answer == "" {
		return nil, errors.New("teacher unavailable")
	}
	ch := make(chan llm.Chunk, 2)
	ch <- llm.Chunk{Text: answer}
	ch <- llm.Chunk{FinishReason: "stop"}
	close(ch)
	return ch, nil
}

type fakeRecaller struct {
	recall   TurnRecall
	err      error
	hang     bool
	requests []TurnRecallRequest
}

func (r *fakeRecaller) RecallTurns(ctx context.Context, request TurnRecallRequest) (TurnRecall, error) {
	r.requests = append(r.requests, request)
	if r.hang {
		<-ctx.Done()
		return TurnRecall{}, ctx.Err()
	}
	return r.recall, r.err
}

type readingTool struct {
	name     string
	deferred bool
}

func (t readingTool) Spec() tools.Spec {
	return tools.Spec{Name: t.name, Summary: t.name + " summary", Description: t.name + " does one thing.",
		Parameters: json.RawMessage(`{"type":"object"}`), Deferred: t.deferred}
}

func (readingTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

type readingSetup struct {
	text       string
	override   llm.ReasoningEffort
	classifier tierClassifier
	teacher    []string
	reading    TurnReading
	cfg        func(*llm.Config)
}

func newReadingAgent(t *testing.T, setup readingSetup) (*LlmAgent, *readingClient) {
	t.Helper()
	client := &readingClient{answers: setup.teacher}
	cfg := llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		TotalTimeoutSec: 30, AdaptiveReasoning: true, MaxTokens: 4096}
	if setup.cfg != nil {
		setup.cfg(&cfg)
	}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	for _, name := range []string{"calendar_add", "notes_add", "pdf_merge", "weather_lookup", "web_search"} {
		reg.Register(readingTool{name: name, deferred: true})
	}
	reg.Register(readingTool{name: "clock_now"})
	text := setup.text
	if text == "" {
		text = "che tempo fa domani a Cuneo?"
	}
	a := NewLlmAgent(LlmAgentConfig{
		Client: client, LLM: cfg, Registry: reg, SessionID: "reading-session", PreviewCap: 2048, RunDir: t.TempDir(),
		UserTurns:   []llm.Message{{Role: llm.RoleUser, Content: text}},
		TurnReading: setup.reading, ReasoningOverride: setup.override,
	})
	a.classifier = setup.classifier
	return a, client
}

func memoryReading(recaller TurnRecaller) TurnReading {
	return TurnReading{Recaller: recaller, ContextKey: "ctx1:k", SourceRef: "postgres://aura/conversations/now/turns/1"}
}

func recalledLabel(source, effort string, distance float64) RecalledTurn {
	return RecalledTurn{
		Distance: distance, SourceRef: "postgres://aura/conversations/past-" + source + "/turns/1",
		Effort: effort, RequestedEffort: effort, EffortSource: source,
		ContextKey: "ctx1:k", PolicyVersion: turnPolicyVersion,
	}
}

func TestReadTurnFixedEffortIsTheUsersAndStillPreloadsTools(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.5}, ok: true}
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.02, SourceRef: "postgres://aura/conversations/past/turns/1", Tools: []string{"web_search"}}}}}
	a, client := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortHigh, classifier: classifier, reading: memoryReading(recaller)})

	decision, read := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceUser || decision.EffortRequested != llm.ReasoningEffortHigh || decision.Effort != llm.ReasoningEffortHigh {
		t.Fatalf("decision = %+v, want the composer's high", decision)
	}
	if len(recaller.requests) != 1 || recaller.requests[0].IncludeLabels {
		t.Fatalf("recall requests = %+v, want one tool-only read", recaller.requests)
	}
	if _, ok := a.activated["web_search"]; !ok || !slices.Equal(read.preloaded, []string{"web_search"}) {
		t.Fatalf("preloaded = %v, activated = %v; want web_search", read.preloaded, a.activated)
	}
	if classifier.calls != 0 || len(client.requests) != 0 {
		t.Fatalf("a fixed effort embedded (%d) or asked the teacher (%d)", classifier.calls, len(client.requests))
	}
}

func TestReadTurnStandaloneGreetingSkipsMemoryAndEmbedding(t *testing.T) {
	recaller := &fakeRecaller{}
	classifier := &fakeClassifier{ok: true}
	reading := memoryReading(recaller)
	reading.Standalone = true
	a, _ := newReadingAgent(t, readingSetup{text: "Buongiorno!", classifier: classifier, reading: reading})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceGreeting || decision.EffortRequested != llm.ReasoningEffortNone {
		t.Fatalf("decision = %+v, want greeting/none", decision)
	}
	if len(recaller.requests) != 0 || classifier.calls != 0 {
		t.Fatalf("a standalone greeting read memory (%d) or embedded (%d)", len(recaller.requests), classifier.calls)
	}

	fixed, _ := newReadingAgent(t, readingSetup{text: "Buongiorno!", override: llm.ReasoningEffortLow, reading: reading})
	if decision, _ := fixed.readTurn(context.Background()); decision.EffortSource != EffortSourceUser || decision.EffortRequested != llm.ReasoningEffortLow {
		t.Fatalf("decision = %+v, want the composer's choice to beat the greeting path", decision)
	}
}

// composedCatalog is what the cockpit puts before the typed message when the identity has an
// indexed document (assets.BuildTurnContext). Memory stores the typed text, and the seed
// gates were measured on typed text.
const composedCatalog = "<knowledge_base trust=\"operator_pinned_context\">\n- [1] document_id=d1 filename=a.pdf\n</knowledge_base>\n\nUser message:\n"

func TestReadTurnDecidesFromTheTypedMessage(t *testing.T) {
	const typed = "che tempo fa domani a Cuneo?"
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.01}, ok: true}
	recaller := &fakeRecaller{}
	reading := memoryReading(recaller)
	reading.Text = typed
	a, client := newReadingAgent(t, readingSetup{text: composedCatalog + typed, classifier: classifier,
		teacher: []string{`{"tier":"high"}`}, reading: reading})

	a.readTurn(context.Background())
	if !slices.Equal(classifier.texts, []string{typed}) {
		t.Fatalf("the classifier read %q, want the typed %q", classifier.texts, typed)
	}
	if len(recaller.requests) != 1 || recaller.requests[0].Text != typed {
		t.Fatalf("recall requests = %+v, want one reading the typed text", recaller.requests)
	}
	if len(client.requests) != 1 || client.requests[0].Messages[1].Content != typed {
		t.Fatalf("teacher requests = %+v, want one asking about the typed text", client.requests)
	}
}

func TestReadTurnTypedGreetingTakesTheGreetingPath(t *testing.T) {
	recaller := &fakeRecaller{}
	classifier := &fakeClassifier{ok: true}
	reading := memoryReading(recaller)
	reading.Text, reading.Standalone = "ciao", true
	a, _ := newReadingAgent(t, readingSetup{text: composedCatalog + "ciao", classifier: classifier, reading: reading})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceGreeting || len(recaller.requests) != 0 || classifier.calls != 0 {
		t.Fatalf("decision = %+v after %d recalls and %d embeddings, want the greeting path", decision, len(recaller.requests), classifier.calls)
	}
}

// "ok" after a request acknowledges that request: without Standalone it is read like any turn.
func TestReadTurnContextualAcknowledgementIsNotAGreeting(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{text: "ok", classifier: classifier, reading: memoryReading(&fakeRecaller{})})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceSeeds || decision.EffortRequested != llm.ReasoningEffortHigh {
		t.Fatalf("decision = %+v, want the seed verdict for a contextual ok", decision)
	}
}

func TestReadTurnReusesACompatibleLabelUserBeforeTeacher(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.5}, ok: true}
	user := recalledLabel(EffortSourceUser, "high", 0.06)
	teacher := recalledLabel(EffortSourceTeacher, "low", 0.02)
	recaller := &fakeRecaller{recall: TurnRecall{UserLabels: []RecalledTurn{user}, TeacherLabels: []RecalledTurn{teacher}}}
	a, client := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(recaller)})

	decision, read := a.readTurn(context.Background())
	if decision.EffortSource != EffortSourceMemory || decision.EffortRequested != llm.ReasoningEffortHigh || decision.OriginRef != user.SourceRef {
		t.Fatalf("decision = %+v, want the user label's high from %s", decision, user.SourceRef)
	}
	if read.label.SourceRef != user.SourceRef || classifier.calls != 0 || len(client.requests) != 0 {
		t.Fatalf("label %+v, classifier calls %d, teacher calls %d", read.label, classifier.calls, len(client.requests))
	}

	onlyTeacher := &fakeRecaller{recall: TurnRecall{TeacherLabels: []RecalledTurn{teacher}}}
	b, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(onlyTeacher)})
	if decision, _ := b.readTurn(context.Background()); decision.EffortSource != EffortSourceMemory || decision.EffortRequested != llm.ReasoningEffortLow {
		t.Fatalf("decision = %+v, want the teacher label's low", decision)
	}
}

func TestReadTurnReclampsALabelAndRefusesAnUnknownEffort(t *testing.T) {
	label := recalledLabel(EffortSourceUser, "xhigh", 0.03)
	recaller := &fakeRecaller{recall: TurnRecall{UserLabels: []RecalledTurn{label}}}
	a, _ := newReadingAgent(t, readingSetup{reading: memoryReading(recaller), cfg: func(c *llm.Config) {
		c.SupportedReasoningEfforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}
	}})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortRequested != llm.ReasoningEffortXHigh || decision.Effort != llm.ReasoningEffortHigh {
		t.Fatalf("decision = %+v, want xhigh requested and high applied on this route", decision)
	}

	unknown := &fakeRecaller{recall: TurnRecall{UserLabels: []RecalledTurn{recalledLabel(EffortSourceUser, "turbo", 0.01)}}}
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.4}, ok: true}
	b, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(unknown)})
	if decision, read := b.readTurn(context.Background()); decision.EffortSource != EffortSourceSeeds || read.recallMiss != "no_compatible_label" {
		t.Fatalf("decision = %+v, miss %q; want an unknown remembered effort to be a named miss", decision, read.recallMiss)
	}
}

func TestReadTurnSeedsAndTeacherFollowTheMargin(t *testing.T) {
	for _, test := range []struct {
		name       string
		classifier *fakeClassifier
		teacher    []string
		source     string
		effort     llm.ReasoningEffort
		asked      int
	}{
		{name: "seeds above the margin", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.075}, ok: true}, source: EffortSourceSeeds, effort: llm.ReasoningEffortHigh},
		{name: "teacher below the margin", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.05}, ok: true}, teacher: []string{`{"tier":"high"}`}, source: EffortSourceTeacher, effort: llm.ReasoningEffortHigh, asked: 1},
		{name: "seeds when the teacher fails", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.05}, ok: true}, teacher: []string{""}, source: EffortSourceSeeds, effort: llm.ReasoningEffortLow, asked: 1},
		{name: "seeds when the teacher answers nonsense", classifier: &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.01}, ok: true}, teacher: []string{"maybe"}, source: EffortSourceSeeds, effort: llm.ReasoningEffortNone, asked: 1},
		{name: "static low when the embedding fails", classifier: &fakeClassifier{ok: false}, source: EffortSourceFallback, effort: llm.ReasoningEffortLow},
	} {
		t.Run(test.name, func(t *testing.T) {
			dispatched := TurnReading{SourceRef: "postgres://aura/conversations/now/turns/1"}
			a, client := newReadingAgent(t, readingSetup{classifier: test.classifier, teacher: test.teacher, reading: dispatched})
			decision, _ := a.readTurn(context.Background())
			if decision.EffortSource != test.source || decision.EffortRequested != test.effort {
				t.Fatalf("decision = %+v, want %s/%s", decision, test.source, test.effort)
			}
			if len(client.requests) != test.asked {
				t.Fatalf("teacher asked %d times, want %d", len(client.requests), test.asked)
			}
		})
	}
}

// A resumed, headless or sub-agent run has no dispatched user turn: its decision lands on no
// row and teaches nothing, so it keeps the seed verdict rather than pay the teacher, as it
// did before turn recall. Only with no classifier at all does it still ask the teacher.
func TestReadTurnWithoutADispatchedTurnKeepsTheSeedVerdict(t *testing.T) {
	uncertain := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.01}, ok: true}
	a, client := newReadingAgent(t, readingSetup{classifier: uncertain, teacher: []string{`{"tier":"none"}`}})
	if decision, read := a.readTurn(context.Background()); decision.EffortSource != EffortSourceSeeds ||
		decision.EffortRequested != llm.ReasoningEffortHigh || len(client.requests) != 0 || read.teacher != "" {
		t.Fatalf("decision = %+v after %d teacher requests, want the seed verdict and no teacher", decision, len(client.requests))
	}

	b, teacher := newReadingAgent(t, readingSetup{teacher: []string{`{"tier":"low"}`}})
	if decision, _ := b.readTurn(context.Background()); decision.EffortSource != EffortSourceTeacher || len(teacher.requests) != 1 {
		t.Fatalf("decision = %+v after %d teacher requests, want the teacher when no classifier is wired", decision, len(teacher.requests))
	}

	fixed, _ := newReadingAgent(t, readingSetup{classifier: uncertain, override: llm.ReasoningEffortLow})
	if decision, _ := fixed.readTurn(context.Background()); decision.EffortSource != EffortSourceUser || decision.EffortRequested != llm.ReasoningEffortLow {
		t.Fatalf("decision = %+v, want the composer's low", decision)
	}
}

func TestReadTurnWithoutAClassifierUsesTheSameTeacher(t *testing.T) {
	a, client := newReadingAgent(t, readingSetup{teacher: []string{`{"tier":"none"}`}})
	if decision, read := a.readTurn(context.Background()); decision.EffortSource != EffortSourceTeacher ||
		decision.EffortRequested != llm.ReasoningEffortNone || read.teacher != teacherSuccess {
		t.Fatalf("decision = %+v (teacher %q), want teacher/none", decision, read.teacher)
	}
	if client.requests[0].ToolChoice != "none" || len(client.requests[0].Tools) != 0 {
		t.Fatal("the teacher request is not the tool-free router request")
	}

	b, _ := newReadingAgent(t, readingSetup{teacher: []string{""}})
	if decision, read := b.readTurn(context.Background()); decision.EffortSource != EffortSourceFallback ||
		decision.EffortRequested != llm.ReasoningEffortLow || read.teacher != teacherError {
		t.Fatalf("decision = %+v (teacher %q), want fallback/low with the failure recorded", decision, read.teacher)
	}
}

// Requested and applied are kept apart: a mandatory-reasoning model turns a decided none into
// low on the wire, and the decision still says none.
func TestReadTurnClampsEveryEffortAndKeepsTheRequest(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierNone, Margin: 0.5}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, cfg: func(c *llm.Config) {
		c.SupportedReasoningEfforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}
		c.ReasoningMandatory = true
	}})
	decision, _ := a.readTurn(context.Background())
	if decision.EffortRequested != llm.ReasoningEffortNone || decision.Effort != llm.ReasoningEffortLow {
		t.Fatalf("decision = %+v, want none requested and low applied", decision)
	}
}

func TestReadTurnReadsWithoutMemoryWhenRecallHangs(t *testing.T) {
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(&fakeRecaller{hang: true})})
	started := time.Now()
	decision, read := a.readTurn(context.Background())
	if elapsed := time.Since(started); elapsed > recallTimeout+time.Second {
		t.Fatalf("readTurn took %v with a hung memory, want about recallTimeout (%v)", elapsed, recallTimeout)
	}
	if decision.EffortSource != EffortSourceSeeds || read.recallMiss != "recall_error" {
		t.Fatalf("decision = %+v, miss %q; want the seeds and a recorded recall error", decision, read.recallMiss)
	}
}

func TestReadTurnPreloadsOnlyRegisteredDeferredTools(t *testing.T) {
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{
		{Distance: 0.01, SourceRef: "postgres://aura/conversations/a/turns/1", Tools: []string{"clock_now", "tool_search", "unmounted_tool"}},
		{Distance: 0.03, SourceRef: "postgres://aura/conversations/b/turns/1", Tools: []string{"web_search", "calendar_add", "web_search"}},
		{Distance: 0.05, SourceRef: "postgres://aura/conversations/c/turns/1", Tools: []string{"notes_add"}},
	}}}
	a, _ := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortLow, reading: memoryReading(recaller)})
	_, read := a.readTurn(context.Background())
	if !slices.Equal(read.preloaded, []string{"calendar_add", "web_search"}) || read.toolTurn.SourceRef != "postgres://aura/conversations/b/turns/1" {
		t.Fatalf("preloaded %v from %s, want the first candidate with eligible tools, sorted", read.preloaded, read.toolTurn.SourceRef)
	}
	for _, name := range []string{"clock_now", "tool_search", "unmounted_tool", "notes_add"} {
		if _, ok := a.activated[name]; ok {
			t.Errorf("%s was activated", name)
		}
	}

	many := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.01, SourceRef: "postgres://aura/conversations/d/turns/1",
		Tools: []string{"web_search", "weather_lookup", "pdf_merge", "notes_add", "calendar_add"}}}}}
	b, _ := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortLow, reading: memoryReading(many)})
	if _, read := b.readTurn(context.Background()); len(read.preloaded) != preloadMax {
		t.Fatalf("preloaded %v, want at most %d", read.preloaded, preloadMax)
	}
}

func TestReadTurnRecallRequestCarriesTheTurnsKeys(t *testing.T) {
	recaller := &fakeRecaller{}
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: memoryReading(recaller)})
	a.readTurn(context.Background())
	if len(recaller.requests) != 1 {
		t.Fatalf("recall requests = %d, want 1", len(recaller.requests))
	}
	got := recaller.requests[0]
	want := TurnRecallRequest{
		Text: "che tempo fa domani a Cuneo?", ContextKey: "ctx1:k", RouteKey: routeKey(a.cfg),
		PolicyVersion: turnPolicyVersion, SourceRef: "postgres://aura/conversations/now/turns/1",
		DeferredTools: []string{"calendar_add", "notes_add", "pdf_merge", "weather_lookup", "web_search"}, IncludeLabels: true,
	}
	if got.Text != want.Text || got.ContextKey != want.ContextKey || got.RouteKey != want.RouteKey ||
		got.PolicyVersion != want.PolicyVersion || got.SourceRef != want.SourceRef ||
		!slices.Equal(got.DeferredTools, want.DeferredTools) || got.IncludeLabels != want.IncludeLabels {
		t.Fatalf("recall request = %+v, want %+v", got, want)
	}
}

func TestReadTurnWithoutAContextKeyAsksNoMemory(t *testing.T) {
	recaller := &fakeRecaller{}
	reading := memoryReading(recaller)
	reading.ContextKey = ""
	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierLow, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier, reading: reading})
	if _, read := a.readTurn(context.Background()); len(recaller.requests) != 0 || read.recallMiss != "context_ineligible" {
		t.Fatalf("recall requests = %d, miss %q; want none and context_ineligible", len(recaller.requests), read.recallMiss)
	}
}

// A route that takes no adaptive effort decides nothing but still preloads.
func TestReadTurnOnARouteWithoutEffortStillPreloads(t *testing.T) {
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.01, SourceRef: "postgres://aura/conversations/e/turns/1", Tools: []string{"web_search"}}}}}
	a, _ := newReadingAgent(t, readingSetup{reading: memoryReading(recaller), cfg: func(c *llm.Config) { c.AdaptiveReasoning = false }})
	decision, read := a.readTurn(context.Background())
	if decision.EffortSource != "" || decision.EffortRequested != "" || decision.RouteKey == "" {
		t.Fatalf("decision = %+v, want no effort but the route named", decision)
	}
	if !slices.Equal(read.preloaded, []string{"web_search"}) || len(recaller.requests) != 1 || recaller.requests[0].IncludeLabels {
		t.Fatalf("preloaded %v after requests %+v, want a tool-only read", read.preloaded, recaller.requests)
	}
}

// The preload must reach round 1 (spec, "How they load"), and the decision is handed over
// once, before that request.
func TestRunPreloadsBeforeTheFirstRequestAndReportsTheDecisionOnce(t *testing.T) {
	var order []string
	recaller := &fakeRecaller{recall: TurnRecall{ToolTurns: []RecalledTurn{{Distance: 0.01, SourceRef: "postgres://aura/conversations/f/turns/1", Tools: []string{"weather_lookup"}}}}}
	reading := memoryReading(recaller)
	var decisions []TurnDecision
	reading.OnDecision = func(d TurnDecision) {
		decisions = append(decisions, d)
		order = append(order, "decision")
	}
	a, client := newReadingAgent(t, readingSetup{override: llm.ReasoningEffortLow, reading: reading})
	client.onRequest = func() { order = append(order, "request") }
	budget, err := NewBudget(BudgetOptions{})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	if _, err := collectInternal(a.Run(InvocationContext{Ctx: context.Background(), RequestID: uuid.Must(uuid.NewV7()), Budget: budget})); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(decisions) != 1 || decisions[0].EffortSource != EffortSourceUser {
		t.Fatalf("decisions = %+v, want exactly one", decisions)
	}
	if len(client.requests) == 0 {
		t.Fatal("no request was sent")
	}
	names := make([]string, 0, len(client.requests[0].Tools))
	for _, def := range client.requests[0].Tools {
		names = append(names, def.Function.Name)
	}
	if !slices.Contains(names, "weather_lookup") {
		t.Fatalf("round-1 tools = %v, want the preloaded weather_lookup", names)
	}
	for _, m := range client.requests[0].Messages {
		if strings.Contains(m.Content, "weather_lookup") {
			t.Fatalf("a round-1 %s message lists weather_lookup, which round 1 already holds", m.Role)
		}
	}
	if len(order) < 2 || order[0] != "decision" || order[1] != "request" {
		t.Fatalf("order = %v, want the decision reported before the first request", order)
	}
}

// The VM checks compare the logged origins with the turns they expect to be recalled.
func TestTurnReadLogShowsTheOriginsVerbatim(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	label := recalledLabel(EffortSourceTeacher, "high", 0.04)
	tool := RecalledTurn{Distance: 0.02, SourceRef: "postgres://aura/conversations/past-tool/turns/3", Tools: []string{"web_search"}}
	recaller := &fakeRecaller{recall: TurnRecall{TeacherLabels: []RecalledTurn{label}, ToolTurns: []RecalledTurn{tool}}}
	a, _ := newReadingAgent(t, readingSetup{reading: memoryReading(recaller)})
	a.readTurn(context.Background())
	for _, want := range []string{"label_origin=" + label.SourceRef, "tool_turn_origin=" + tool.SourceRef} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("turn-read log lacks %q:\n%s", want, logs.String())
		}
	}
}

func TestReadTurnCountsItsDecisionSource(t *testing.T) {
	recorded, reader := newTestAgentMetrics(t)
	previous := metrics
	metrics = recorded
	t.Cleanup(func() { metrics = previous })

	classifier := &fakeClassifier{verdict: prompt.ReasoningVerdict{Tier: prompt.ReasoningTierHigh, Margin: 0.3}, ok: true}
	a, _ := newReadingAgent(t, readingSetup{classifier: classifier})
	a.readTurn(context.Background())

	sum, ok := findOTelMetric(t, reader, "aura.agent.turn.decision").Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 || !hasOTelLabel(sum.DataPoints[0].Attributes.ToSlice(), "outcome", "seeds") || sum.DataPoints[0].Value != 1 {
		t.Fatalf("turn decisions = %+v, want one seeds decision", sum.DataPoints)
	}
}
