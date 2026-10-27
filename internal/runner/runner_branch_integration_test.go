//go:build db_integration

// The 2026-10-27 branch probe, with its assertions (spec acceptance 1 and 2). Real Postgres,
// the real Runner and a scripted model: every request the model receives must hold its own
// path and no other.
//
//	go test -tags db_integration -race ./internal/runner/ -run TestBranchRun -count=1
package runner

import (
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// conversationText is the request's user and assistant contents, without the system head
// and without the per-round envelope the agent appends after the user message.
func conversationText(req llm.Request) []string {
	var out []string
	for _, m := range req.Messages {
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant {
			continue
		}
		if m.Role == llm.RoleUser && len(m.Content) > 0 && m.Content[0] == '<' {
			continue
		}
		out = append(out, m.Content)
	}
	return out
}

func wantRequest(t *testing.T, client *agenttest.FakeClient, i int, want ...string) {
	t.Helper()
	if len(client.Requests) <= i {
		t.Fatalf("the model received %d requests, want at least %d", len(client.Requests), i+1)
	}
	if got := conversationText(client.Requests[i]); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("request %d = %q, want %q", i, got, want)
	}
}

func TestBranchRun_EveryRequestHoldsItsOwnPath(t *testing.T) {
	pool := migratedRunnerPool(t)
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(textResponseCall("c1", "A1")),
		agenttest.ToolCallTurn(textResponseCall("c2", "A1 edited")),
		agenttest.ToolCallTurn(textResponseCall("c3", "A2")),
		agenttest.ToolCallTurn(textResponseCall("c4", "A2 again")),
		agenttest.ToolCallTurn(textResponseCall("c5", "A3")),
	)
	r, store, _ := newIntegrationRunner(t, pool, client)
	convID := newIntegrationConversation(t, pool, store)
	ctx := ownerCtx()

	if _, err := drain(r.Turn(ctx, convID, new("U1"))); err != nil {
		t.Fatalf("U1: %v", err)
	}
	edited, err := store.ForkBranch(ctx, convID, 1, llm.RoleUser, "U1 edited")
	if err != nil {
		t.Fatalf("edit U1: %v", err)
	}
	if _, err := drain(r.TurnBranch(ctx, convID, edited)); err != nil {
		t.Fatalf("re-run the edit: %v", err)
	}
	if _, err := drain(r.Turn(ctx, convID, new("U2"))); err != nil {
		t.Fatalf("U2: %v", err)
	}
	latest, err := store.LatestTurnSeq(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	regenerated, err := store.ForkBranch(ctx, convID, latest, llm.RoleAssistant, "")
	if err != nil {
		t.Fatalf("regenerate A2: %v", err)
	}
	if _, err := drain(r.TurnBranch(ctx, convID, regenerated)); err != nil {
		t.Fatalf("re-run the regenerate: %v", err)
	}
	// Back to the first branch: U3 continues A1 (seq 2).
	if _, err := drain(r.Turn(WithBranchLeaf(ctx, 2), convID, new("U3"))); err != nil {
		t.Fatalf("U3 on the first branch: %v", err)
	}

	wantRequest(t, client, 0, "U1")
	wantRequest(t, client, 1, "U1 edited")
	wantRequest(t, client, 2, "U1 edited", "A1 edited", "U2")
	wantRequest(t, client, 3, "U1 edited", "A1 edited", "U2")
	wantRequest(t, client, 4, "U1", "A1", "U3")

	u3, err := store.LatestTurnSeq(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	branches, err := store.ListBranches(ctx, convID)
	if err != nil {
		t.Fatal(err)
	}
	leaves := map[int]conversations.Branch{}
	for _, b := range branches {
		leaves[b.LeafSeq] = b
	}
	if len(leaves) != 3 {
		t.Fatalf("the tree has leaves %v, want 3: A2, A2 again, A3", branches)
	}
	if a3, ok := leaves[u3]; !ok || a3.BranchID != conversations.CanonicalBranchID {
		t.Errorf("A3 (seq %d) = %+v, want a leaf on the canonical branch it continued", u3, a3)
	}
}
