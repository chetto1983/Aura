package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/adaptive/orderingcontrol"
	"github.com/chetto1983/aura/internal/adaptive/reasoningcontrol"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/jackc/pgx/v5/pgxpool"
)

type adaptiveControlSet struct {
	toolDiscovery     tools.ToolDiscoveryControl
	skillRouting      tools.SkillRoutingControl
	documentRetrieval tools.DocumentRetrievalControl
	memoryRecall      runner.DynamicRecallControl
	reasoning         agent.ReasoningControl
}

func newAdaptiveControlSet(
	pool *pgxpool.Pool,
	providerID string,
	modelID string,
) adaptiveControlSet {
	if pool == nil ||
		strings.TrimSpace(providerID) == "" ||
		strings.TrimSpace(modelID) == "" {
		return adaptiveControlSet{}
	}
	return adaptiveControlSet{
		toolDiscovery: orderingcontrol.NewToolDiscovery(
			pool, providerID, modelID,
		),
		skillRouting: orderingcontrol.NewSkillRouting(
			pool, providerID, modelID,
		),
		documentRetrieval: orderingcontrol.NewDocumentRetrieval(
			pool, providerID, modelID,
		),
		memoryRecall: orderingcontrol.NewDynamicRecall(
			pool, providerID, modelID,
		),
		reasoning: reasoningcontrol.New(
			pool, providerID, modelID,
		),
	}
}

func selectAdaptiveBootPool(
	ctx context.Context,
	pool *pgxpool.Pool,
	reader adaptive.PolicyReader,
	warnings io.Writer,
) *pgxpool.Pool {
	if reader == nil {
		writeAdaptiveBootWarning(warnings, "policy unavailable")
		return nil
	}
	policy, err := reader.CurrentPolicy(ctx)
	if err != nil {
		writeAdaptiveBootWarning(warnings, "policy unavailable")
		return nil
	}
	if !validAdaptiveBootPolicy(policy) {
		writeAdaptiveBootWarning(warnings, "invalid policy")
		return nil
	}
	switch policy.Mode {
	case adaptive.PolicyOff, adaptive.PolicyShadow, adaptive.PolicyRollback:
		return pool
	case adaptive.PolicyCanary, adaptive.PolicyActive:
		writeAdaptiveBootWarning(warnings, "unsupported policy mode")
		return nil
	default:
		writeAdaptiveBootWarning(warnings, "invalid policy")
		return nil
	}
}

func validAdaptiveBootPolicy(policy adaptive.Policy) bool {
	if policy.Epoch < 1 || strings.TrimSpace(policy.Version) == "" {
		return false
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(policy.Config, &config); err != nil || config == nil {
		return false
	}
	switch policy.Mode {
	case adaptive.PolicyOff, adaptive.PolicyShadow, adaptive.PolicyRollback:
		return policy.RolloutBPS == 0
	case adaptive.PolicyCanary:
		return policy.RolloutBPS > 0 && policy.RolloutBPS < 10_000
	case adaptive.PolicyActive:
		return policy.RolloutBPS == 10_000
	default:
		return false
	}
}

func writeAdaptiveBootWarning(warnings io.Writer, reason string) {
	if warnings == nil {
		return
	}
	_, _ = fmt.Fprintf(
		warnings,
		"warn: adaptive controls disabled at boot: %s\n",
		reason,
	)
}
