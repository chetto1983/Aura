package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// teacherClient answers the router prompt with text, fails to open, or waits for the
// deadline.
type teacherClient struct {
	text    string
	openErr error
	hang    bool
	calls   int
}

func (c *teacherClient) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	c.calls++
	if c.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if c.openErr != nil {
		return nil, c.openErr
	}
	ch := make(chan llm.Chunk, 1)
	ch <- llm.Chunk{Text: c.text}
	close(ch)
	return ch, nil
}

func teacherAgent(client llm.Client) *LlmAgent {
	return NewLlmAgent(LlmAgentConfig{
		Client:    client,
		LLM:       llm.Config{Model: "m", Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", TotalTimeoutSec: 1},
		Registry:  tools.NewRegistry(),
		SessionID: "teacher-session",
	})
}

// Every attempt is counted with its outcome, whatever the caller then decides (spec,
// "Teacher usage"): a timeout followed by a seed decision is still a teacher attempt.
func TestAskTeacherNamesEveryOutcomeAndCountsIt(t *testing.T) {
	recorded, reader := newTestAgentMetrics(t)
	previous := metrics
	metrics = recorded
	t.Cleanup(func() { metrics = previous })

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, test := range []struct {
		name    string
		ctx     context.Context
		client  *teacherClient
		tier    prompt.ReasoningTier
		outcome teacherOutcome
	}{
		{name: "success", ctx: context.Background(), client: &teacherClient{text: `{"tier":"high"}`}, tier: prompt.ReasoningTierHigh, outcome: teacherSuccess},
		{name: "invalid", ctx: context.Background(), client: &teacherClient{text: "probably high"}, outcome: teacherInvalid},
		{name: "error", ctx: context.Background(), client: &teacherClient{openErr: errors.New("401 unauthorized")}, outcome: teacherError},
		{name: "timeout", ctx: context.Background(), client: &teacherClient{hang: true}, outcome: teacherTimeout},
		{name: "canceled", ctx: canceled, client: &teacherClient{hang: true}, outcome: teacherCanceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			tier, outcome := teacherAgent(test.client).askTeacher(test.ctx, "che tempo fa domani a Cuneo?")
			if tier != test.tier || outcome != test.outcome {
				t.Fatalf("askTeacher = %q, %q; want %q, %q", tier, outcome, test.tier, test.outcome)
			}
		})
	}

	sum, ok := findOTelMetric(t, reader, "aura.agent.teacher.attempt").Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatal("aura.agent.teacher.attempt is not an int64 sum")
	}
	seen := map[string]int64{}
	for _, point := range sum.DataPoints {
		for _, outcome := range []string{"success", "invalid", "error", "timeout", "canceled"} {
			if hasOTelLabel(point.Attributes.ToSlice(), "outcome", outcome) {
				seen[outcome] += point.Value
			}
		}
	}
	for _, outcome := range []string{"success", "invalid", "error", "timeout", "canceled"} {
		if seen[outcome] != 1 {
			t.Errorf("teacher attempts with outcome %s = %d, want 1 (all: %v)", outcome, seen[outcome], seen)
		}
	}
}
