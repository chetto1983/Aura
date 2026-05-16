package chat

import (
	"context"
	"fmt"
	"time"

	"github.com/aura/aura/internal/identity"
)

// Result is the collected output of a completed subagent run. Returned by
// WaitForRun when the child run reaches a terminal state.
type Result struct {
	ReplyText      string
	ToolCallsCount int
	TokensTotal    int
	ElapsedMs      int64
}

// completedRunEntry is the in-memory record written by dispatch() when a swarm
// run reaches a terminal state. WaitForRun reads and consumes it.
type completedRunEntry struct {
	result Result
	status RunStatus
}

// WaitForRun blocks until the run identified by runID reaches a terminal state
// (completed, failed, or cancelled). On success it returns the run's Result
// and nil. For cancelled/failed runs it returns the Result and a non-nil
// error describing the final status.
//
// If ctx expires before the run completes, WaitForRun calls Stop to cancel
// the in-flight run and returns ctx.Err(). Poll interval is 200 ms (no
// busy-loop per mini-PC CPU budget constraint).
func (h *Hub) WaitForRun(ctx context.Context, runID string) (Result, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		if v, ok := h.completedRuns.LoadAndDelete(runID); ok {
			entry := v.(completedRunEntry)
			if entry.status != RunStatusCompleted {
				return entry.result, fmt.Errorf("chat: run %s ended with status %s", runID, entry.status)
			}
			return entry.result, nil
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			h.Stop(runID)
			return Result{}, ctx.Err()
		}
	}
}

// storeCompletedRun writes a completedRunEntry for run into completedRuns so
// WaitForRun can collect it. Called by dispatch via defer.
func (h *Hub) storeCompletedRun(run *Run) {
	if run == nil {
		return
	}
	h.completedRuns.Store(run.ID, completedRunEntry{
		result: resultFromRun(run),
		status: run.Status,
	})
}

// resultFromRun extracts the WaitForRun-visible fields from run.Metadata.
// AgentLoopAdapter writes final_text and stats there at turn end.
func resultFromRun(run *Run) Result {
	text, _ := run.Metadata["final_text"].(string)
	var toolCalls, tokensTotal int
	if stats, _ := run.Metadata["stats"].(map[string]any); stats != nil {
		if v, ok := stats["tool_calls"].(int); ok {
			toolCalls = v
		}
		if v, ok := stats["tokens_total"].(int); ok {
			tokensTotal = v
		}
	}
	var elapsedMs int64
	if !run.StartedAt.IsZero() && run.CompletedAt != nil {
		elapsedMs = run.CompletedAt.Sub(run.StartedAt).Milliseconds()
	}
	return Result{
		ReplyText:      text,
		ToolCallsCount: toolCalls,
		TokensTotal:    tokensTotal,
		ElapsedMs:      elapsedMs,
	}
}

// authorizeSwarmDispatch checks that the calling actor has CapabilityToolExecute
// and a per-tool grant for each name in msg.ChannelData["tool_allowlist"].
// Returns nil when no identity.Authorizer is installed in ctx (fail-open,
// matching the behaviour of all other channels).
func (h *Hub) authorizeSwarmDispatch(ctx context.Context, msg InboundMessage, actorID string) error {
	authz, ok := identity.AuthorizerFromContext(ctx)
	if !ok {
		return nil
	}
	decision, err := authz.Authorize(ctx, identity.AuthorizeParams{
		ActorID:    actorID,
		Capability: identity.CapabilityToolExecute,
	})
	if err != nil {
		return fmt.Errorf("chat: authorize swarm CapabilityToolExecute: %w", err)
	}
	genericAllowed := decision.Decision == identity.DecisionAllow
	if !genericAllowed {
		return fmt.Errorf("chat: swarm dispatch denied: missing %s capability", identity.CapabilityToolExecute)
	}
	// Per-tool granular check: only constrains delegated actors with restricted
	// per-tool grants. Owner-style principals carry the generic CapabilityToolExecute
	// (TelegramOwnerCapabilities at internal/identity/store.go:744), which already
	// covers every tool — the loop below short-circuits via genericAllowed. The
	// granular check is retained so a future Phase-8 delegated child actor with an
	// EXPLICIT per-tool grant (e.g. only tool.execute.web) is still verified, but
	// it does NOT block parents that hold the generic grant.
	toolAllowlist, _ := msg.ChannelData["tool_allowlist"].([]string)
	for _, toolName := range toolAllowlist {
		cap := identity.ToolExecuteCapability(toolName)
		d, err := authz.Authorize(ctx, identity.AuthorizeParams{
			ActorID:    actorID,
			Capability: cap,
		})
		if err != nil {
			return fmt.Errorf("chat: authorize swarm tool %q: %w", toolName, err)
		}
		if d.Decision == identity.DecisionAllow {
			continue
		}
		if genericAllowed {
			continue
		}
		return fmt.Errorf("chat: swarm dispatch denied: missing %s capability", cap)
	}
	return nil
}
