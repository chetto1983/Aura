//go:build db_integration

package runner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/messagedrafts"
	"github.com/chetto1983/aura/internal/toolapprovals"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newParkIntegrationRunner is newIntegrationRunner under single_user_hardened with the park
// switch on, the committer holding the record store, and the gateway and the runner writing one
// real ledger.
func newParkIntegrationRunner(t *testing.T, pool *pgxpool.Pool, extra []tools.Tool, calls ...llm.ToolCall) (*Runner, *conversations.Store, string) {
	t.Helper()
	r, convStore, pauseStore := newIntegrationRunner(t, pool, agenttest.NewFakeClient(agenttest.ToolCallTurn(calls...)))
	committer := NewPoolResumeCommitter(pool, convStore, pauseStore)
	committer.SetToolApprovals(toolapprovals.NewStore(pool))
	r.resumeCommitter = committer
	ledger := toolinvocations.New(pool)
	r.toolInvocations = ledger
	r.gateway = gateway.New(config.ProfileSingleUserHardened, ledger)
	r.gatewayOwnsToolStarts = r.gateway.OwnsToolStartRows()
	r.parkApprovals = true
	r.registry.Register(&parkFixtureTool{spec: parkFixtureSpec("skill_manage", true)})
	r.registry.Register(&parkFixtureTool{spec: parkFixtureSpec("read_notes", false)})
	r.registry.Register(&parkFixtureTool{spec: parkFixtureSpec("notes_write", true)})
	for _, tool := range extra {
		r.registry.Register(tool)
	}
	return r, convStore, newIntegrationConversation(t, pool, convStore)
}

func countRows(t *testing.T, pool *pgxpool.Pool, query, convID string) int {
	t.Helper()
	var n int
	asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ownerCtx(), query, convID).Scan(&n)
	})
	return n
}

// parkedRoundEvent is a park round for G alone, with the record a gateway park writes.
func parkedRoundEvent(fingerprint string) *agent.Event {
	call := agenttest.MakeToolCall("G", "skill_manage", gatedDeleteArgs)
	card := approvaltext.Presentation{Key: approvaltext.GatewayKey, Params: map[string]string{
		"tool": "skill_manage", "risk": "destructive", "args": "action, name",
	}}
	pause := &agent.AwaitingInput{
		Question: "Approve skill_manage?", Kind: "approval", ToolCallID: "G",
		OriginalToolName: "skill_manage", OriginalArguments: gatedDeleteArgs,
		Approval: &agent.ParkedApproval{
			Source: string(toolapprovals.SourceGateway), ToolName: "skill_manage",
			ExecuteArgs: json.RawMessage(gatedDeleteArgs), Fingerprint: fingerprint, Presentation: card,
			SubjectTool: "skill_manage", SubjectAction: "delete",
		},
	}
	return &agent.Event{Actions: agent.Actions{ParkedTurn: &agent.ParkedTurn{
		Calls: []llm.ToolCall{call}, Pauses: []*agent.AwaitingInput{pause},
	}}}
}

func TestParkRoundIsAtomicUntilTheSiblings(t *testing.T) {
	pool := migratedRunnerPool(t)
	const (
		assistantTurns = `SELECT count(*) FROM aura.conversation_turns WHERE conversation_id=$1 AND role='assistant'`
		pauseRows      = `SELECT count(*) FROM aura.paused_states WHERE conversation_id=$1`
		records        = `SELECT count(*) FROM aura.tool_approvals WHERE conversation_id=$1`
	)

	t.Run("a valid record commits with the turn's seq", func(t *testing.T) {
		r, _, convID := newParkIntegrationRunner(t, pool, nil)
		fingerprint := tools.CallFingerprint(json.RawMessage(gatedDeleteArgs))
		if err := r.persistEvent(ownerCtx(), &turnTracker{convID: convID}, parkedRoundEvent(fingerprint)); err != nil {
			t.Fatalf("persist ParkedTurn: %v", err)
		}
		var turnSeq, recordSeq int
		var token, pauseToken string
		asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
			if err := tx.QueryRow(ownerCtx(), `SELECT seq FROM aura.conversation_turns WHERE conversation_id=$1 AND role='assistant'`, convID).Scan(&turnSeq); err != nil {
				return err
			}
			if err := tx.QueryRow(ownerCtx(), `SELECT token FROM aura.paused_states WHERE conversation_id=$1`, convID).Scan(&pauseToken); err != nil {
				return err
			}
			return tx.QueryRow(ownerCtx(), `SELECT assistant_seq, pause_token FROM aura.tool_approvals WHERE conversation_id=$1`, convID).Scan(&recordSeq, &token)
		})
		if recordSeq != turnSeq || token != pauseToken {
			t.Fatalf("record (seq %d, token %s), want the parked turn's seq %d and its pause token %s", recordSeq, token, turnSeq, pauseToken)
		}
	})

	t.Run("a record the CHECK refuses rolls back the turn and the pause", func(t *testing.T) {
		r, _, convID := newParkIntegrationRunner(t, pool, nil)
		if err := r.persistEvent(ownerCtx(), &turnTracker{convID: convID}, parkedRoundEvent("not-a-sha256")); err == nil {
			t.Fatal("persist ParkedTurn accepted a record the CHECK refuses")
		}
		for query, name := range map[string]string{assistantTurns: "assistant turns", pauseRows: "pause rows", records: "records"} {
			if n := countRows(t, pool, query, convID); n != 0 {
				t.Fatalf("%s = %d after the rollback, want 0", name, n)
			}
		}
	})
}

// A sibling's result is not in the pause transaction: it lands after it, through AppendTurn,
// which spills an over-cap result to the sidecar that AppendTurnTx cannot write.
func TestSiblingResultSpills(t *testing.T) {
	pool := migratedRunnerPool(t)
	page := pageProbeTool{page: strings.Repeat("a long page ", 70<<10/12)}
	r, _, convID := newParkIntegrationRunner(t, pool, []tools.Tool{page},
		agenttest.MakeToolCall("A", "page_probe", `{}`),
		agenttest.MakeToolCall("G", "skill_manage", gatedDeleteArgs),
	)
	if _, err := drain(r.Turn(ownerCtx(), convID, new("read and tidy"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	spilled := countRows(t, pool, `SELECT count(*) FROM aura.conversation_turns
		WHERE conversation_id=$1 AND role='tool' AND tool_call_id='A' AND content_sidecar_path IS NOT NULL`, convID)
	if spilled != 1 {
		t.Fatalf("spilled sibling results = %d, want 1", spilled)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM aura.tool_approvals WHERE conversation_id=$1 AND status='pending'`, convID); n != 1 {
		t.Fatalf("pending records = %d, want G's", n)
	}
}

// Under strict, the gateway's reservation writes a sibling's start and the runner its end, both
// under the park round's request id; the park round adds no second writer of either.
func TestStrictLedgerOfASiblingHasOneStartOneEnd(t *testing.T) {
	pool := migratedRunnerPool(t)
	r, _, convID := newParkIntegrationRunner(t, pool, nil,
		agenttest.MakeToolCall("W", "notes_write", `{"note":"x"}`),
		agenttest.MakeToolCall("G", "skill_manage", gatedDeleteArgs),
	)
	evs, err := drain(r.Turn(ownerCtx(), convID, new("note and tidy")))
	if err != nil {
		t.Fatalf("turn: %v", err)
	}
	var requestID string
	for _, ev := range evs {
		if ev.Actions.ParkedTurn != nil {
			requestID = ev.RequestID.String()
		}
	}
	rows, err := toolinvocations.New(pool).ListByConversation(ownerCtx(), convID)
	if err != nil {
		t.Fatalf("list ledger: %v", err)
	}
	var kinds []string
	for _, row := range rows {
		if row.ToolCallID != "W" {
			continue
		}
		if row.RequestID != requestID {
			t.Fatalf("ledger row %s under request %s, want the park round's %s", row.Event, row.RequestID, requestID)
		}
		kinds = append(kinds, row.Event)
	}
	if strings.Join(kinds, ",") != "start,end" {
		t.Fatalf("W's ledger rows = %v, want one start and one end", kinds)
	}
}

// T3-I2: a draft's pause row is committed with the parked turn, before the sibling runs, so a
// consumer that stops on the sibling still leaves the draft with its card.
func TestDraftParkRoundKeepsItsPauseWhenASiblingFails(t *testing.T) {
	pool := migratedRunnerPool(t)
	send := &parkFixtureTool{spec: tools.Spec{
		Name: "whatsapp__send_message", Summary: "send", Description: "send", Parameters: json.RawMessage(`{"type":"object"}`),
		TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_message", Mutating: true, Destructive: true,
	}}
	r, _, convID := newParkIntegrationRunner(t, pool, []tools.Tool{send},
		agenttest.MakeToolCall("S", "whatsapp__send_message", `{"recipient":"12345","message":"ciao"}`),
		agenttest.MakeToolCall("A", "read_notes", `{}`),
	)
	r.messageDrafts = messagedrafts.NewStore(pool)
	t.Cleanup(func() {
		asOwner(t, pool, localIdentityID, func(tx pgx.Tx) error {
			_, err := tx.Exec(context.Background(), `DELETE FROM aura.message_drafts WHERE conversation_id=$1`, convID)
			return err
		})
	})
	for ev, err := range r.Turn(ownerCtx(), convID, new("send it and read")) {
		if err != nil {
			t.Fatalf("turn: %v", err)
		}
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationStart && ti.ToolCallID == "A" {
			break
		}
	}

	if n := countRows(t, pool, `SELECT count(*) FROM aura.paused_states WHERE conversation_id=$1 AND resumed_at IS NULL`, convID); n != 1 {
		t.Fatalf("pending pause rows = %d, want the draft's", n)
	}
	drafts, err := r.ListMessageDrafts(ownerCtx(), convID)
	if err != nil || len(drafts) != 1 || drafts[0].ToolCallID != "S" {
		t.Fatalf("listed drafts = %+v, %v, want S's card", drafts, err)
	}
	if n := countRows(t, pool, `SELECT count(*) FROM aura.tool_approvals WHERE conversation_id=$1`, convID); n != 0 {
		t.Fatalf("records = %d, a draft gets none", n)
	}
}
