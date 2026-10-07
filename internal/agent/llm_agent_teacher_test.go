package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/llm"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// teacherClient answers the router prompt with text, fails to open, waits for the deadline,
// or opens a stream the deadline closes with no error and no text.
type teacherClient struct {
	text           string
	openErr        error
	hang           bool
	silentDeadline bool
}

func (c *teacherClient) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	if c.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.openErr != nil {
		return nil, c.openErr
	}
	if c.silentDeadline {
		ch := make(chan llm.Chunk)
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	}
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Text: c.text}
	close(ch)
	return ch, nil
}

// teacherBound stands in for the runner's teacher timeout: AskTeacher takes its bound from
// the caller's context.
func teacherBound(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

// Every attempt is counted with its outcome, whatever the caller then does with it (spec,
// "Teacher usage").
func TestAskTeacherNamesEveryOutcomeAndCountsIt(t *testing.T) {
	recorded, reader := newTestAgentMetrics(t)
	previous := metrics
	metrics = recorded
	t.Cleanup(func() { metrics = previous })

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	cases := []struct {
		name    string
		ctx     context.Context
		client  llm.Client
		tier    prompt.ReasoningTier
		outcome string
	}{
		{name: "success", ctx: teacherBound(t), client: &teacherClient{text: `{"tier":"high"}`}, tier: prompt.ReasoningTierHigh, outcome: TeacherSuccess},
		{name: "invalid", ctx: teacherBound(t), client: &teacherClient{text: "probably high"}, outcome: TeacherInvalid},
		{name: "error", ctx: teacherBound(t), client: &teacherClient{openErr: errors.New("401 unauthorized")}, outcome: TeacherError},
		{name: "no client", ctx: teacherBound(t), outcome: TeacherError},
		{name: "timeout", ctx: teacherBound(t), client: &teacherClient{hang: true}, outcome: TeacherTimeout},
		{name: "silent deadline", ctx: teacherBound(t), client: &teacherClient{silentDeadline: true}, outcome: TeacherTimeout},
		{name: "canceled", ctx: canceled, client: &teacherClient{hang: true}, outcome: TeacherCanceled},
	}
	want := map[string]int64{}
	for _, test := range cases {
		want[test.outcome]++
		t.Run(test.name, func(t *testing.T) {
			tier, outcome := AskTeacher(test.ctx, test.client, "m", "che tempo fa domani a Cuneo?")
			if tier != test.tier || outcome != test.outcome {
				t.Fatalf("AskTeacher = %q, %q; want %q, %q", tier, outcome, test.tier, test.outcome)
			}
		})
	}

	sum, ok := findOTelMetric(t, reader, "aura.agent.teacher.attempt").Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatal("aura.agent.teacher.attempt is not an int64 sum")
	}
	seen := map[string]int64{}
	for _, point := range sum.DataPoints {
		for outcome := range want {
			if hasOTelLabel(point.Attributes.ToSlice(), "outcome", outcome) {
				seen[outcome] += point.Value
			}
		}
	}
	for outcome, count := range want {
		if seen[outcome] != count {
			t.Errorf("teacher attempts with outcome %s = %d, want %d (all: %v)", outcome, seen[outcome], count, seen)
		}
	}
}

// The teacher is the router prompt over the typed message, on the caller's model, with no
// tools and no reasoning.
func TestAskTeacherSendsTheToolFreeRouterRequest(t *testing.T) {
	client := &readingClient{}
	AskTeacher(teacherBound(t), client, "route-model", "che tempo fa domani a Cuneo?")
	if len(client.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(client.requests))
	}
	req := client.requests[0]
	if req.Model != "route-model" || req.ToolChoice != "none" || len(req.Tools) != 0 || req.MaxTokens != 32 ||
		req.Reasoning.Enabled == nil || *req.Reasoning.Enabled || len(req.Messages) != 2 ||
		req.Messages[0].Content != prompt.ReasoningRouterSystemPrompt || req.Messages[1].Content != "che tempo fa domani a Cuneo?" {
		t.Fatalf("teacher request = %+v, want the tool-free router request over the typed message", req)
	}
}
