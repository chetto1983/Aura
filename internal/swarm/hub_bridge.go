package swarm

import (
	"context"
	"errors"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/identity"
)

// HubBridge implements AgentRunner by dispatching agent tasks through a
// chat.Hub on the swarm channel. Each Run call creates a synthetic
// InboundMessage with ParentRunID set to the swarm run ID so Hub records
// the parent→child lineage in chat.Run.Metadata["parent_run_id"].
type HubBridge struct {
	hub         *chat.Hub
	parentRunID string
}

// NewHubBridge wraps hub as an AgentRunner. parentRunID propagates into
// every child chat.Run created by this bridge.
func NewHubBridge(hub *chat.Hub, parentRunID string) *HubBridge {
	return &HubBridge{hub: hub, parentRunID: parentRunID}
}

// WithParentRunID returns a shallow copy of the bridge with a different
// parentRunID. Used by Manager.Run to stamp child runs with the swarm
// run ID before dispatching.
func (b *HubBridge) WithParentRunID(id string) AgentRunner {
	return &HubBridge{hub: b.hub, parentRunID: id}
}

// Run satisfies AgentRunner. It normalizes task into an InboundMessage for
// the swarm channel and blocks until the hub completes the agent turn.
// The final assistant text is read from run.Metadata["final_text"] which
// the AgentLoopAdapter writes at turn end.
func (b *HubBridge) Run(ctx context.Context, task agent.Task) (agent.Result, error) {
	msg := chat.InboundMessage{
		Channel:     chat.ChannelSwarm,
		PrincipalID: task.UserID,
		Text:        task.Prompt,
		Mode:        chat.DeliveryModeSilent,
		ParentRunID: b.parentRunID,
	}
	run, err := b.hub.ReceiveMessage(ctx, msg)
	if run == nil {
		return agent.Result{}, err
	}
	text, _ := run.Metadata["final_text"].(string)
	return agent.Result{Content: text}, err
}

// Dispatch is the NodeSpec-based one-way dispatch for the Phase 8 read-only
// fanout slice. It validates the spec, builds a silent InboundMessage with
// ChannelData carrying all NodeSpec fields, calls hub.ReceiveMessage, and
// returns the child run ID immediately — it does NOT wait for completion
// (collect is a separate step via Router.WaitForRun in US-R02).
func (b *HubBridge) Dispatch(ctx context.Context, spec NodeSpec, principal identity.Principal) (string, error) {
	if err := spec.Validate(); err != nil {
		return "", err
	}
	msg := chat.InboundMessage{
		Channel:     chat.ChannelSwarm,
		PrincipalID: principal.ID,
		Text:        spec.Goal,
		Mode:        chat.DeliveryModeSilent,
		ParentRunID: spec.ParentRunID,
		ChannelData: map[string]any{
			"parent_run_id":  spec.ParentRunID,
			"assignment_id":  spec.AssignmentID,
			"instruction":    spec.Instruction,
			"tool_allowlist": spec.ToolAllowlist,
			"max_iterations": spec.MaxIterations,
			"max_tool_calls": spec.MaxToolCalls,
			"budget_secs":    spec.BudgetSecs,
		},
	}
	run, err := b.hub.ReceiveMessage(ctx, msg)
	if err != nil {
		return "", err
	}
	if run == nil {
		return "", errors.New("swarm: hub returned nil run for dispatch")
	}
	return run.ID, nil
}
