package swarm

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
)

type swarmReasoningControlProbe struct {
	mu     sync.Mutex
	inputs []agent.ReasoningControlInput
}

func (probe *swarmReasoningControlProbe) DecideAndDeliverReasoning(
	ctx context.Context,
	input agent.ReasoningControlInput,
	learned agent.ReasoningRequestBuilder,
	_ agent.StaticReasoningRequestBuilder,
) (agent.PreparedReasoningRequest, error) {
	probe.mu.Lock()
	probe.inputs = append(probe.inputs, input)
	probe.mu.Unlock()
	return learned(ctx, agent.ReasoningActionStatic)
}

func (probe *swarmReasoningControlProbe) snapshot() []agent.ReasoningControlInput {
	probe.mu.Lock()
	defer probe.mu.Unlock()
	return slices.Clone(probe.inputs)
}

func TestRunnerAdapterThreadsParentReasoningControlIntoChildModelRound(t *testing.T) {
	control := &swarmReasoningControlProbe{}
	client := newRouter().
		route("alpha", outcome{kind: "ok", text: "done"}).
		route("panic", outcome{kind: "panic"})
	rc := testRunConfig(t, client, 25)
	rc.LLM.MaxTokens = 256
	ctx := identityctx.WithIdentityID(
		withToolCtx(context.Background(), t),
		identityctx.LocalOperatorIdentity,
	)
	ctx = agent.WithSwarmContext(
		ctx,
		rc.ParentBudget,
		rc.ParentRegistry,
		rc.Client,
		rc.LLM,
		rc.ConvID,
		rc.Gateway,
		control,
	)

	result, err := NewRunnerAdapter(rc.Cfg).Run(
		ctx,
		[]string{"alpha task", "panic task"},
	)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	reports := parseReports(t, result.Preview)
	if len(reports) != 2 ||
		reports[0].Status != StatusOK ||
		reports[1].Status != StatusFailed {
		t.Fatalf("reports = %+v, want one success and one isolated panic", reports)
	}

	inputs := control.snapshot()
	if len(inputs) != 2 {
		t.Fatalf("reasoning decisions = %d, want 2", len(inputs))
	}
	for _, input := range inputs {
		if input.OwnerID.String() != identityctx.LocalOperatorIdentity ||
			input.RequestID.Version() != 7 ||
			input.PointOrdinal != 1 ||
			input.MessageCount != 2 ||
			input.ProviderID != rc.LLM.Provider ||
			input.ModelID != rc.LLM.Model {
			t.Fatalf("child reasoning input = %+v", input)
		}
	}
}

func TestPanicChildReportPreservesWorkerIdentity(t *testing.T) {
	report := panicChildReport(2, "boom")
	if report.GoalIndex != 2 ||
		report.ChildID != "w3" ||
		report.Status != StatusFailed ||
		report.Error != "panic: boom" {
		t.Fatalf("panic report = %+v", report)
	}
}

func TestPreflightRejectsInvalidGoalSets(t *testing.T) {
	runConfig := testRunConfig(t, newRouter(), 25)
	runConfig.Cfg.MaxSwarmGoals = 1
	tests := []struct {
		name    string
		goals   []string
		message string
	}{
		{name: "empty", message: "no goals provided"},
		{name: "over cap", goals: []string{"one", "two"}, message: "too many goals"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			message, ok := preflight(runConfig, test.goals)
			if ok || !strings.Contains(message, test.message) {
				t.Fatalf("preflight = (%q, %t), want rejection containing %q", message, ok, test.message)
			}
		})
	}
}

var _ agent.ReasoningControl = (*swarmReasoningControlProbe)(nil)
