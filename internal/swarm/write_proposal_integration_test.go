package swarm

import (
	"context"
	"database/sql"
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

// proposalInserter is a lightweight in-process fake for propose_patch persistence.
// It writes directly to the proposed_updates SQLite table so the integration
// test can assert on the ground-truth database state.
type proposalInserter struct {
	db *sql.DB
}

func (p *proposalInserter) insert(ctx context.Context, kind, fact, slug, category, sigHash string) error {
	now := time.Now().UTC().Format("2006-01-02 15:04:05")
	_, err := p.db.ExecContext(ctx, `
		INSERT INTO proposed_updates
		  (chat_id, fact, action, target_slug, similarity,
		   source_turn_ids, category, related_slugs, provenance_json,
		   status, kind, signature_hash, created_at)
		VALUES (0, ?, 'new', ?, 1.0, '[]', ?, '[]', '{}', 'pending', ?, ?, ?)`,
		fact, slug, category, kind, sigHash, now,
	)
	return err
}

// writeProposalChildFn simulates a write_proposal subagent that calls propose_patch.
// It inserts a pending proposal row and records the handle in run.Metadata.
func writeProposalChildFn(db *sql.DB) func(context.Context, *chat.Run, chat.InboundMessage, chat.EmitFn) error {
	pi := &proposalInserter{db: db}
	return func(ctx context.Context, run *chat.Run, msg chat.InboundMessage, _ chat.EmitFn) error {
		// Determine which proposal to insert based on the goal text.
		var kind, fact, slug, category, handle string
		switch {
		case strings.Contains(msg.Text, "wiki"):
			kind = "wiki"
			fact = "Test wiki page body about Go testing."
			slug = "go-testing-guide"
			category = ""
			handle = "proposal:wiki:go-testing-guide"
		case strings.Contains(msg.Text, "coffee"):
			kind = "user_memory"
			fact = "I like coffee"
			slug = "preference"
			category = "preference"
			handle = "proposal:user_memory:coffee-preference"
		default:
			run.Metadata["final_text"] = "no-op"
			return nil
		}
		sigHash := fmt.Sprintf("test-sig-%s", kind)
		if err := pi.insert(ctx, kind, fact, slug, category, sigHash); err != nil {
			return fmt.Errorf("write_proposal child: propose_patch: %w", err)
		}
		run.Metadata["final_text"] = fmt.Sprintf("Proposal submitted: %s", handle)
		return nil
	}
}

// TestParentSpawnsTwoWriteProposalSubagentsAndCollectsProposals is the Phase-S
// end-to-end integration test.
//
// Assertions:
//  1. spawn returns 2 non-empty child run IDs
//  2. collect returns aggregated markdown referencing both proposal handles
//  3. proposed_updates has 2 rows with status='pending', kinds 'wiki' + 'user_memory'
//  4. Dispatching a write_proposal NodeSpec containing 'wiki_page' in the allowlist
//     is BLOCKED by NodeSpec.Validate (ErrPermissionDenied / validation error)
func TestParentSpawnsTwoWriteProposalSubagentsAndCollectsProposals(t *testing.T) {
	ctx := context.Background()

	// 1 — SQLite lifecycle store + in-process DB (shares migrations with proposal table).
	db := testutil.OpenTestDB(t, migrations.Run)
	store, err := runstore.NewStore(db)
	if err != nil {
		t.Fatalf("runstore.NewStore: %v", err)
	}

	// 2 — Multi-channel loop: parent on ChannelWeb, write_proposal children on ChannelSwarm.
	childFn := writeProposalChildFn(db)
	loop := &multiChannelLoop{
		childFn: childFn,
	}

	// 3 — Hub with SQLite lifecycle store.
	hub, err := chat.New(chat.Config{Loop: loop, LifecycleStore: store})
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	hub.RegisterOutbound(silentadapter.NewSwarm(silentadapter.Config{}))

	// 4 — HubBridge.
	bridge := NewHubBridge(hub, "")

	var childRunIDs [2]string
	var childReplies [2]string

	loop.parentFn = func(ctx context.Context, run *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
		_ = emit(chat.OutboundEvent{
			Type: chat.EventToolStart,
			Payload: map[string]any{
				"tool":         "subagent_dispatch",
				"tool_call_id": "spawn-wp-1",
				"arg_keys":     []string{"action", "nodes"},
			},
		})

		// write_proposal allowlist: propose_patch + reads (no direct write tools).
		wpAllowlist := []string{"propose_patch", "search_memory", "web"}

		// Dispatch child A — wiki proposal.
		specA := NodeSpec{
			Goal:          "propose a wiki page about test",
			Instruction:   "use propose_patch with action=wiki",
			RiskTier:      "write_proposal",
			ToolAllowlist: wpAllowlist,
			MaxIterations: 2,
			BudgetSecs:    30,
			ParentRunID:   run.ID,
		}
		idA, dispErr := bridge.Dispatch(ctx, specA, identity.Principal{ID: msg.PrincipalID})
		if dispErr != nil {
			return fmt.Errorf("dispatch child A: %w", dispErr)
		}
		childRunIDs[0] = idA

		// Dispatch child B — user_memory proposal.
		specB := NodeSpec{
			Goal:          "surface user preference: I like coffee",
			Instruction:   "use propose_patch with action=user_memory",
			RiskTier:      "write_proposal",
			ToolAllowlist: wpAllowlist,
			MaxIterations: 2,
			BudgetSecs:    30,
			ParentRunID:   run.ID,
		}
		idB, dispErr := bridge.Dispatch(ctx, specB, identity.Principal{ID: msg.PrincipalID})
		if dispErr != nil {
			return fmt.Errorf("dispatch child B: %w", dispErr)
		}
		childRunIDs[1] = idB

		// Collect results.
		collectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		resA, err := hub.WaitForRun(collectCtx, idA)
		if err != nil {
			return fmt.Errorf("collect child A: %w", err)
		}
		childReplies[0] = resA.ReplyText
		resB, err := hub.WaitForRun(collectCtx, idB)
		if err != nil {
			return fmt.Errorf("collect child B: %w", err)
		}
		childReplies[1] = resB.ReplyText

		_ = emit(chat.OutboundEvent{
			Type: chat.EventToolEnd,
			Payload: map[string]any{
				"tool":         "subagent_dispatch",
				"tool_call_id": "spawn-wp-1",
				"success":      true,
				"elapsed_ms":   0,
			},
		})

		var sb strings.Builder
		fmt.Fprintf(&sb, "## Subagent A: %s\n\n%s\n\n", idA, resA.ReplyText)
		fmt.Fprintf(&sb, "## Subagent B: %s\n\n%s\n\n", idB, resB.ReplyText)
		run.Metadata["final_text"] = sb.String()
		return nil
	}

	// 5 — Dispatch the parent run.
	parentRun, err := hub.ReceiveMessage(ctx, chat.InboundMessage{
		Channel:     chat.ChannelWeb,
		PrincipalID: "user-wp-1",
		Text:        "orchestrate write proposal tasks",
		Mode:        chat.DeliveryModeSilent,
	})
	if err != nil {
		t.Fatalf("parent dispatch: %v", err)
	}
	if parentRun.Status != chat.RunStatusCompleted {
		t.Fatalf("parent run.Status = %s, want completed", parentRun.Status)
	}

	// ── Assertion A: 2 non-empty child run IDs ─────────────────────────────────
	for i, id := range childRunIDs {
		if id == "" {
			t.Errorf("childRunIDs[%d] is empty", i)
		}
	}

	// ── Assertion B: aggregated markdown references both proposal handles ───────
	aggregate, _ := parentRun.Metadata["final_text"].(string)
	if !strings.Contains(aggregate, "proposal:wiki:go-testing-guide") {
		t.Errorf("aggregated text missing wiki proposal handle: %q", aggregate)
	}
	if !strings.Contains(aggregate, "proposal:user_memory:coffee-preference") {
		t.Errorf("aggregated text missing user_memory proposal handle: %q", aggregate)
	}

	// ── Assertion C: proposed_updates has 2 pending rows ───────────────────────
	var pendingCount int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM proposed_updates WHERE status = 'pending'`,
	).Scan(&pendingCount); err != nil {
		t.Fatalf("count pending proposals: %v", err)
	}
	if pendingCount != 2 {
		t.Errorf("pending proposals = %d, want 2", pendingCount)
	}

	// ── Assertion D: correct kinds (wiki + user_memory) ───────────────────────
	rows, err := db.QueryContext(ctx, `SELECT kind FROM proposed_updates WHERE status='pending'`)
	if err != nil {
		t.Fatalf("query proposal kinds: %v", err)
	}
	defer rows.Close()
	kindSet := map[string]bool{}
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatalf("scan kind: %v", err)
		}
		kindSet[k] = true
	}
	if !kindSet["wiki"] {
		t.Error("no 'wiki' kind in proposed_updates")
	}
	if !kindSet["user_memory"] {
		t.Error("no 'user_memory' kind in proposed_updates")
	}

	// ── Assertion E: direct wiki_page in write_proposal allowlist is BLOCKED ───
	// NodeSpec.Validate must reject a write_proposal spec that contains 'wiki_page'.
	blockedSpec := NodeSpec{
		Goal:          "write directly to wiki",
		RiskTier:      "write_proposal",
		ToolAllowlist: []string{"propose_patch", "wiki_page"}, // wiki_page is forbidden
		MaxIterations: 1,
		BudgetSecs:    30,
		ParentRunID:   parentRun.ID,
	}
	_, dispErr := bridge.Dispatch(ctx, blockedSpec, identity.Principal{ID: "user-wp-1"})
	if dispErr == nil {
		t.Error("expected dispatch of write_proposal spec with wiki_page to be BLOCKED, got nil error")
	}
	if !strings.Contains(dispErr.Error(), "wiki_page") {
		t.Errorf("block error should mention 'wiki_page', got: %v", dispErr)
	}

	// ── Assertion F: run_events has tool_start for subagent_dispatch ──────────
	evRows, queryErr := db.QueryContext(ctx,
		`SELECT payload_json FROM run_events WHERE type = 'tool_start' AND run_id = ?`,
		parentRun.ID,
	)
	if queryErr != nil {
		t.Fatalf("query run_events: %v", queryErr)
	}
	defer evRows.Close()
	var foundDispatch bool
	for evRows.Next() {
		var payloadJSON string
		if err := evRows.Scan(&payloadJSON); err != nil {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(payloadJSON), &payload) == nil {
			if tool, _ := payload["tool"].(string); tool == "subagent_dispatch" {
				foundDispatch = true
				break
			}
		}
	}
	if !foundDispatch {
		t.Error("no tool_start event with tool=subagent_dispatch in run_events for parent run")
	}
}
