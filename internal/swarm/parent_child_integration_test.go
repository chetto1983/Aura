package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/chat"
	silentadapter "github.com/aura/aura/internal/channels/silent"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
	runstore "github.com/aura/aura/internal/storage/runs"
	"github.com/aura/aura/internal/testutil"
)

// multiChannelLoop routes to parentFn for non-swarm channels and childFn for
// ChannelSwarm. Both fields must be set before the first Run call.
type multiChannelLoop struct {
	parentFn func(context.Context, *chat.Run, chat.InboundMessage, chat.EmitFn) error
	childFn  func(context.Context, *chat.Run, chat.InboundMessage, chat.EmitFn) error
}

func (l *multiChannelLoop) Run(ctx context.Context, run *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
	if msg.Channel == chat.ChannelSwarm {
		return l.childFn(ctx, run, msg, emit)
	}
	return l.parentFn(ctx, run, msg, emit)
}

// TestParentSpawnsTwoReadOnlyChildrenAndCollectsResults is the Phase-R
// end-to-end integration test. It uses an in-memory hub backed by an in-process
// SQLite lifecycle store and a fake LLM (multiChannelLoop) that returns
// deterministic short replies without any live network calls.
//
// Assertions locked by this test:
//  1. spawn produces 2 child run IDs (non-empty)
//  2. collect returns aggregated markdown containing both child replies
//  3. SQLite runs table has exactly 3 rows (parent + 2 children)
//  4. Both child rows have parent_run_id = parentRun.ID and channel = "swarm"
//  5. run_events table has a tool_start event with tool="subagent_dispatch"
//     for the parent run
func TestParentSpawnsTwoReadOnlyChildrenAndCollectsResults(t *testing.T) {
	// 1 — SQLite lifecycle store (in-process, cleaned up by t.Cleanup).
	db := testutil.OpenTestDB(t, migrations.Run)
	store, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("runstore.NewStore: %v", err)
	}

	// 2 — Build the multi-channel loop; parentFn is set after hub+bridge are ready.
	loop := &multiChannelLoop{
		childFn: func(_ context.Context, run *chat.Run, msg chat.InboundMessage, _ chat.EmitFn) error {
			// Deterministic child reply based on the goal text.
			switch msg.Text {
			case "count to 3":
				run.Metadata["final_text"] = "1 2 3"
			case "say hello in 2 languages":
				run.Metadata["final_text"] = "Hello Ciao"
			default:
				run.Metadata["final_text"] = "ok"
			}
			return nil
		},
	}

	// 3 — Hub with SQLite lifecycle store.
	hub, err := chat.New(chat.Config{Loop: loop, LifecycleStore: store})
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	// ChannelSwarm outbound adapter (silent, no user delivery).
	hub.RegisterOutbound(silentadapter.NewSwarm(silentadapter.Config{}))

	// 4 — HubBridge (parentRunID is filled per-dispatch via NodeSpec).
	bridge := NewHubBridge(hub, "")

	// 5 — Collected outputs populated by parentFn for post-dispatch assertions.
	var childRunIDs [2]string
	var childReplies [2]string

	loop.parentFn = func(ctx context.Context, run *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
		// Emit EventToolStart to simulate what SubagentDispatchTool does;
		// this creates a durable tool_start row in run_events.
		_ = emit(chat.OutboundEvent{
			Type: chat.EventToolStart,
			Payload: map[string]any{
				"tool":         "subagent_dispatch",
				"tool_call_id": "spawn-1",
				"arg_keys":     []string{"action", "nodes"},
			},
		})

		// Dispatch child A (sequential to avoid concurrent SQLite writes).
		specA := NodeSpec{
			Goal:          "count to 3",
			Instruction:   "reply with just numbers",
			RiskTier:      "read_only",
			MaxIterations: 2,
			BudgetSecs:    30,
			ParentRunID:   run.ID,
		}
		idA, dispErr := bridge.Dispatch(ctx, specA, identity.Principal{ID: msg.PrincipalID})
		if dispErr != nil {
			return fmt.Errorf("dispatch child A: %w", dispErr)
		}
		childRunIDs[0] = idA

		// Dispatch child B.
		specB := NodeSpec{
			Goal:          "say hello in 2 languages",
			Instruction:   "no markdown",
			RiskTier:      "read_only",
			MaxIterations: 2,
			BudgetSecs:    30,
			ParentRunID:   run.ID,
		}
		idB, dispErr := bridge.Dispatch(ctx, specB, identity.Principal{ID: msg.PrincipalID})
		if dispErr != nil {
			return fmt.Errorf("dispatch child B: %w", dispErr)
		}
		childRunIDs[1] = idB

		// Collect: both children complete synchronously inside Dispatch (fake loop),
		// so WaitForRun returns immediately from completedRuns sync.Map.
		collectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		resA, waitErr := hub.WaitForRun(collectCtx, idA)
		if waitErr != nil {
			return fmt.Errorf("collect child A: %w", waitErr)
		}
		childReplies[0] = resA.ReplyText

		resB, waitErr := hub.WaitForRun(collectCtx, idB)
		if waitErr != nil {
			return fmt.Errorf("collect child B: %w", waitErr)
		}
		childReplies[1] = resB.ReplyText

		// Emit EventToolEnd.
		_ = emit(chat.OutboundEvent{
			Type: chat.EventToolEnd,
			Payload: map[string]any{
				"tool":         "subagent_dispatch",
				"tool_call_id": "spawn-1",
				"success":      true,
				"elapsed_ms":   0,
			},
		})

		// Build aggregated markdown and store as final_text on the parent run.
		var sb strings.Builder
		fmt.Fprintf(&sb, "## Subagent 1: %s\n\n%s\n\n", idA, resA.ReplyText)
		fmt.Fprintf(&sb, "## Subagent 2: %s\n\n%s\n\n", idB, resB.ReplyText)
		run.Metadata["final_text"] = sb.String()
		return nil
	}

	// 6 — Dispatch the parent run on ChannelWeb (no inbound adapter needed;
	// ReceiveMessage bypasses the adapter step).
	parentRun, err := hub.ReceiveMessage(context.Background(), chat.InboundMessage{
		Channel:     chat.ChannelWeb,
		PrincipalID: "user-1",
		Text:        "orchestrate two tasks",
		Mode:        chat.DeliveryModeSilent,
	})
	if err != nil {
		t.Fatalf("parent dispatch: %v", err)
	}
	if parentRun.Status != chat.RunStatusCompleted {
		t.Fatalf("parent run.Status = %s, want completed", parentRun.Status)
	}

	// ── Assertion A: spawn returned 2 non-empty child run IDs ─────────────────
	for i, id := range childRunIDs {
		if id == "" {
			t.Errorf("childRunIDs[%d] is empty", i)
		}
	}

	// ── Assertion B: aggregated markdown contains both child replies ───────────
	aggregate, _ := parentRun.Metadata["final_text"].(string)
	if !strings.Contains(aggregate, "1 2 3") {
		t.Errorf("aggregated text missing '1 2 3': %q", aggregate)
	}
	if !strings.Contains(aggregate, "Hello Ciao") {
		t.Errorf("aggregated text missing 'Hello Ciao': %q", aggregate)
	}

	// ── Assertion C: SQLite runs table — 3 rows total ─────────────────────────
	bgCtx := context.Background()
	var totalRuns int
	if err := db.QueryRowContext(bgCtx, `SELECT COUNT(*) FROM runs`).Scan(&totalRuns); err != nil {
		t.Fatalf("count runs: %v", err)
	}
	if totalRuns != 3 {
		t.Fatalf("expected 3 runs (1 parent + 2 children), got %d", totalRuns)
	}

	// ── Assertion D: both children have correct parent_run_id linkage ─────────
	var childCount int
	if err := db.QueryRowContext(bgCtx,
		`SELECT COUNT(*) FROM runs WHERE parent_run_id = ? AND channel = 'swarm'`,
		parentRun.ID,
	).Scan(&childCount); err != nil {
		t.Fatalf("count swarm children: %v", err)
	}
	if childCount != 2 {
		t.Fatalf("expected 2 swarm children with parent_run_id=%q, got %d", parentRun.ID, childCount)
	}

	// ── Assertion E: run_events has EventToolStart for subagent_dispatch ──────
	rows, queryErr := db.QueryContext(bgCtx,
		`SELECT payload_json FROM run_events WHERE type = 'tool_start' AND run_id = ?`,
		parentRun.ID,
	)
	if queryErr != nil {
		t.Fatalf("query run_events: %v", queryErr)
	}
	defer rows.Close()

	var foundSubagentDispatch bool
	for rows.Next() {
		var payloadJSON string
		if scanErr := rows.Scan(&payloadJSON); scanErr != nil {
			t.Fatalf("scan run_events: %v", scanErr)
		}
		var payload map[string]any
		if json.Unmarshal([]byte(payloadJSON), &payload) == nil {
			if tool, _ := payload["tool"].(string); tool == "subagent_dispatch" {
				foundSubagentDispatch = true
				break
			}
		}
	}
	if !foundSubagentDispatch {
		t.Fatal("no tool_start event with tool=subagent_dispatch in run_events for parent run")
	}
}
