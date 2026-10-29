//go:build db_integration

// Acceptance 8 of spec 2026-10-27: conversation branches end to end over HTTP. Real Postgres,
// the real Runner and a scripted model, through NewServer(...).Mux(): a run, an edit, and a run
// that names the first answer as aura.parent_seq. Every request the model receives holds its
// own path, each stream reports the seqs it started from and ended on, and the history
// snapshot returns the tree. Run via:
//
//	go test -tags db_integration -race ./internal/agui -run TestBranchHTTP -count=1
package agui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

func answerWith(id, text string) agenttest.FakeTurn {
	return agenttest.ToolCallTurn(agenttest.MakeToolCall(id, "text_response", `{"text":"`+text+`"}`))
}

// branchTurns is the user and assistant text of one model request, without the system head
// and the per-round envelope the agent appends after the user message.
func branchTurns(req llm.Request) string {
	var out []string
	for _, m := range req.Messages {
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant {
			continue
		}
		if m.Role == llm.RoleUser && strings.HasPrefix(m.Content, "<") {
			continue
		}
		out = append(out, m.Content)
	}
	return strings.Join(out, " | ")
}

// streamSeqs posts body to path and returns the seqs the stream reported.
func streamSeqs(t *testing.T, srv *httptest.Server, path, body string) string {
	t.Helper()
	resp, err := http.Post(srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s = %d (%v): %s", path, resp.StatusCode, err, raw)
	}
	_, data := sseFrames(t, string(raw))
	if strings.Contains(data, "RUN_ERROR") {
		t.Fatalf("POST %s failed: %s", path, data)
	}
	var seqs []string
	for _, key := range []string{"head_seq", "leaf_seq"} {
		for seq := 1; seq < 20; seq++ {
			if strings.Contains(data, fmt.Sprintf(`"path":"/%s","value":%d`, key, seq)) {
				seqs = append(seqs, fmt.Sprintf("%s=%d", key, seq))
			}
		}
	}
	return strings.Join(seqs, " ")
}

func TestBranchHTTP_EveryRunHoldsItsOwnPathAndTheSnapshotIsTheTree(t *testing.T) {
	pool := migratedPool(t)
	store := conversations.New(pool, conversations.Config{RunDir: t.TempDir(), TurnCapBytes: 65536})
	tid := seedConversation(t, store, pool, nil)
	client := agenttest.NewFakeClient(answerWith("c1", "A1"), answerWith("c2", "A1 edited"), answerWith("c3", "A3"))
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	pauses := askuser.New(pool)
	run := runner.New(runner.Deps{
		Conv:            store,
		Pause:           pauses,
		ApprovalExpiry:  pauses,
		Identity:        steerE2EIdentityStore{},
		CacheMetrics:    steerE2ECacheMetricStore{},
		ToolInvocations: steerE2EToolInvocationStore{},
		Client:          agenttest.TitleClient{Main: client, Title: agenttest.NewFakeClient(agenttest.TextChunks("stop", "Branches"))},
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
	})
	// RequireAuth puts the owner on every request in production; the runner's own store
	// reads are scoped by it.
	mux := NewServer(run, store, ServerConfig{}).Mux()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(identityctx.WithIdentityID(r.Context(), localIdentityID)))
	}))
	t.Cleanup(srv.Close)
	send := func(text, aura string) string {
		return `{"threadId":"` + tid + `","messages":[{"id":"m","role":"user","content":"` + text + `"}]` + aura + `}`
	}

	if got := streamSeqs(t, srv, "/agent/run", send("U1", "")); got != "head_seq=1 leaf_seq=2" {
		t.Errorf("the first run reported %q, want U1 then A1", got)
	}
	edit := `{"diverge_seq":1,"role":"user","content":"U1 edited"}`
	if got := streamSeqs(t, srv, "/api/conversations/"+tid+"/edit", edit); got != "head_seq=3 leaf_seq=4" {
		t.Errorf("the edit reported %q, want the rewrite then its answer", got)
	}
	if got := streamSeqs(t, srv, "/agent/run", send("U3", `,"aura":{"parent_seq":2}`)); got != "head_seq=5 leaf_seq=6" {
		t.Errorf("the run on the first branch reported %q, want U3 then A3", got)
	}

	want := []string{"U1", "U1 edited", "U1 | A1 | U3"}
	if len(client.Requests) != len(want) {
		t.Fatalf("the model received %d requests, want %d", len(client.Requests), len(want))
	}
	for i, w := range want {
		if got := branchTurns(client.Requests[i]); got != w {
			t.Errorf("request %d = %q, want %q", i, got, w)
		}
	}

	resp, err := http.Get(srv.URL + "/threads/" + tid + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	var snapshot struct {
		Messages []struct {
			ID       string  `json:"id"`
			ParentID *string `json:"parentId"`
			Content  string  `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	var tree []string
	for _, m := range snapshot.Messages {
		parent := "root"
		if m.ParentID != nil {
			parent = *m.ParentID
		}
		tree = append(tree, m.ID+"<"+parent+":"+m.Content)
	}
	wantTree := []string{
		"msg-1<root:U1", "msg-2<msg-1:A1",
		"msg-3<root:U1 edited", "msg-4<msg-3:A1 edited",
		"msg-5<msg-2:U3", "msg-6<msg-5:A3",
	}
	if strings.Join(tree, "\n") != strings.Join(wantTree, "\n") {
		t.Fatalf("snapshot tree:\n%s\nwant:\n%s", strings.Join(tree, "\n"), strings.Join(wantTree, "\n"))
	}
}
