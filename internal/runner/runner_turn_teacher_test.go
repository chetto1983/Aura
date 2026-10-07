package runner

import (
	"bytes"
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
)

// routerTeacher answers the teacher's router requests and sends every other request to the
// turn's own scripted client. With release set it answers only once release is closed, and
// it reports the context it then sees.
type routerTeacher struct {
	turns   llm.Client
	answer  string
	hang    bool
	release chan struct{}
	started chan struct{}
	once    sync.Once

	mu       sync.Mutex
	requests []llm.Request
}

func (c *routerTeacher) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if len(req.Messages) == 0 || req.Messages[0].Content != prompt.ReasoningRouterSystemPrompt {
		return c.turns.Stream(ctx, req)
	}
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	if c.started != nil {
		c.once.Do(func() { close(c.started) })
	}
	if c.release != nil {
		<-c.release
	}
	if c.hang {
		<-ctx.Done()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ch := make(chan llm.Chunk, 2)
	ch <- llm.Chunk{Text: c.answer}
	ch <- llm.Chunk{FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func (c *routerTeacher) snapshot() []llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.requests)
}

// lockedBuffer collects log output written from the turn and from its workers.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	logs := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

// mandatoryRoute takes low and high only, and cannot turn reasoning off, so the clamp turns
// a teacher none into low. The label must store none, the effort before the clamp
// (migration 0137); a reuse clamps it again. A label stored as low means something clamped.
func mandatoryRoute() llm.Config {
	cfg := reasoningRoute()
	cfg.SupportedReasoningEfforts = []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh}
	cfg.ReasoningMandatory = true
	return cfg
}

// teacherRunner runs turns whose seed verdict is uncertain (agenttest.UniformEmbedder), so
// each dispatched turn without a composer effort is decided by seeds and asks the teacher.
func teacherRunner(t *testing.T, teacher *routerTeacher, turns ...agenttest.FakeTurn) (*Runner, *recordingDecisionStore) {
	t.Helper()
	teacher.turns = agenttest.TitleClient{
		Main:  agenttest.NewFakeClient(turns...),
		Title: agenttest.NewFakeClient(agenttest.TextChunks("stop", "Test conversation title")),
	}
	r, _, _ := newTestRunnerCfg(t, teacher, mandatoryRoute())
	r.classifier = prompt.NewReasoningClassifier(&agenttest.UniformEmbedder{})
	decisions := &recordingDecisionStore{}
	r.turnDecisions = decisions
	return r, decisions
}

func awaitTeacher(t *testing.T, teacher *routerTeacher) {
	t.Helper()
	select {
	case <-teacher.started:
	case <-time.After(5 * time.Second):
		t.Fatal("the turn ended without asking the teacher")
	}
}

func stopRunner(t *testing.T, r *Runner, convID string) {
	t.Helper()
	if err := r.Stop(context.Background(), convID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestUncertainTurnIsLabelledByTheTeacherAfterItsDecision(t *testing.T) {
	logs := captureLogs(t)
	teacher := &routerTeacher{answer: `{"tier":"none"}`, release: make(chan struct{}), started: make(chan struct{})}
	r, decisions := teacherRunner(t, teacher, agenttest.ToolCallTurn(textResponseCall("call-1", "Ecco il riassunto.")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	const typed = "riassumi il rapporto trimestrale"
	if _, err := drain(r.Turn(context.Background(), convID, new(typed))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	awaitTeacher(t, teacher)

	stopped := make(chan error, 1)
	go func() { stopped <- r.Stop(context.Background(), convID) }()
	select {
	case err := <-stopped:
		t.Fatalf("Stop returned (%v) while the teacher was still answering; it must join the worker", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(teacher.release)
	if err := <-stopped; err != nil {
		t.Fatalf("Stop: %v", err)
	}

	records := decisions.snapshot()
	if len(records) != 1 || records[0].decision.EffortSource != agent.EffortSourceSeeds || records[0].decision.EffortRequested != "high" {
		t.Fatalf("decisions = %+v, want one uncertain seeds decision", records)
	}
	seq := records[0].seq
	labels, writes := decisions.labelSnapshot()
	if !slices.Equal(labels, []recordedLabel{{convID: convID, seq: seq, requested: "none"}}) || !slices.Equal(writes, []string{"decision", "label"}) {
		t.Fatalf("labels %+v after writes %v; want the teacher's none, before the clamp, on the decided row after its decision", labels, writes)
	}
	requests := teacher.snapshot()
	if len(requests) != 1 || requests[0].Messages[1].Content != typed || requests[0].Model != mandatoryRoute().Model {
		t.Fatalf("teacher requests = %+v, want one on the turn's model about the typed text", requests)
	}
	for _, want := range []string{`msg="adaptive reasoning: teacher label"`, "source_ref=" + reasoningSourceRef(convID, seq),
		"outcome=success", "tier=none", "requested=none", "effort=low"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs lack %q:\n%s", want, logs.String())
		}
	}
}

// The worker outlives the turn the way the auto-title worker does: its context is detached
// from the turn's, so a client that hangs up after the answer cancels nothing.
func TestCancellingAFinishedTurnDoesNotCancelItsTeacher(t *testing.T) {
	teacher := &routerTeacher{answer: `{"tier":"high"}`, release: make(chan struct{}), started: make(chan struct{})}
	r, decisions := teacherRunner(t, teacher, agenttest.ToolCallTurn(textResponseCall("call-1", "Fatto.")))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := drain(r.Turn(ctx, convID, new("confronta le due offerte del fornitore"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	awaitTeacher(t, teacher)
	cancel()
	close(teacher.release)
	stopRunner(t, r, convID)
	if labels, _ := decisions.labelSnapshot(); len(labels) != 1 || labels[0].requested != "high" {
		t.Fatalf("labels = %+v, want the teacher's high despite the cancelled turn", labels)
	}
}

func TestNoTeacherWorkerWithoutAWrittenDecisionThatAsks(t *testing.T) {
	for _, test := range []struct {
		name     string
		turn     agenttest.FakeTurn
		override llm.ReasoningEffort
		arrange  func(*Runner, *recordingDecisionStore)
	}{
		{name: "a failed round", turn: agenttest.FakeTurn{Err: errFake}},
		{name: "a decision that does not ask", turn: agenttest.ToolCallTurn(textResponseCall("call-1", "Fatto.")), override: llm.ReasoningEffortLow},
		{name: "a failed decision write", turn: agenttest.ToolCallTurn(textResponseCall("call-1", "Fatto.")),
			arrange: func(_ *Runner, d *recordingDecisionStore) { d.err = errFake }},
		{name: "an open breaker", turn: agenttest.ToolCallTurn(textResponseCall("call-1", "Fatto.")),
			arrange: func(r *Runner, d *recordingDecisionStore) {
				r.breaker = llm.NewBreaker(1, time.Hour)
				d.onDecision = func() { r.breaker.Failure(errFake) }
			}},
	} {
		t.Run(test.name, func(t *testing.T) {
			teacher := &routerTeacher{answer: `{"tier":"high"}`}
			r, decisions := teacherRunner(t, teacher, test.turn)
			if test.arrange != nil {
				test.arrange(r, decisions)
			}
			convID := newConvID(t)
			mustCreate(t, r, convID)
			ctx := context.Background()
			if test.override != "" {
				ctx = WithReasoningOverride(ctx, test.override)
			}
			_, _ = drain(r.Turn(ctx, convID, new("pianifica la migrazione del database")))
			stopRunner(t, r, convID)
			labels, _ := decisions.labelSnapshot()
			if requests := teacher.snapshot(); len(requests) != 0 || len(labels) != 0 {
				t.Fatalf("teacher requests %d, labels %+v; want no worker", len(requests), labels)
			}
		})
	}
}

// A teacher that gives no tier labels nothing, and a label that cannot be written is one
// warning; the turn's decision stands either way.
func TestTeacherWithoutAWrittenLabelLeavesTheDecision(t *testing.T) {
	for _, test := range []struct {
		name     string
		teacher  *routerTeacher
		labelErr error
		outcome  string
		labels   int
		warning  bool
	}{
		{name: "timeout", teacher: &routerTeacher{hang: true}, outcome: agent.TeacherTimeout},
		{name: "invalid answer", teacher: &routerTeacher{answer: "probabilmente alto"}, outcome: agent.TeacherInvalid},
		{name: "failed label write", teacher: &routerTeacher{answer: `{"tier":"low"}`}, labelErr: errFake, outcome: agent.TeacherSuccess, labels: 1, warning: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			logs := captureLogs(t)
			r, decisions := teacherRunner(t, test.teacher, agenttest.ToolCallTurn(textResponseCall("call-1", "Fatto.")))
			r.teacherTimeout = 50 * time.Millisecond
			decisions.labelErr = test.labelErr
			convID := newConvID(t)
			mustCreate(t, r, convID)
			if _, err := drain(r.Turn(context.Background(), convID, new("valuta i rischi del nuovo contratto"))); err != nil {
				t.Fatalf("turn: %v", err)
			}
			stopRunner(t, r, convID)
			labels, _ := decisions.labelSnapshot()
			if records := decisions.snapshot(); len(records) != 1 || records[0].decision.EffortSource != agent.EffortSourceSeeds || len(labels) != test.labels {
				t.Fatalf("decisions %+v, labels %+v; want the seeds decision and %d label write(s)", records, labels, test.labels)
			}
			if !strings.Contains(logs.String(), "outcome="+test.outcome) ||
				strings.Contains(logs.String(), "teacher label write failed") != test.warning {
				t.Fatalf("logs = %s, want outcome=%s and a write warning: %v", logs.String(), test.outcome, test.warning)
			}
		})
	}
}
