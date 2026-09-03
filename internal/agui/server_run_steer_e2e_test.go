package agui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/steer/steertest"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

// server_run_steer_e2e_test.go drives a REAL *runner.Runner (not agui's own
// read-only scriptedRunner/fakeConvStore) through the agui HTTP handler end
// to end (TestSteerEndToEndRedirectsNextRound), closing the tracer's own
// proof obligation. internal/runner's OWN fakes_test.go fakes are unexported
// and not importable across packages, so this file carries a minimal,
// purpose-built set that implements exactly what this test path exercises;
// every agui.ConversationStore/runner store method NOT on that path is a
// harmless stub — the SAME convention server_convstore_fake_test.go's own
// fakeConvStore already uses for reads its own tests never needed.

type steerE2EConvStore struct {
	mu    sync.Mutex
	convs map[string]*conversations.Conversation
	turns map[string][]conversations.AppendTurnParams
}

func newSteerE2EConvStore() *steerE2EConvStore {
	return &steerE2EConvStore{convs: map[string]*conversations.Conversation{}, turns: map[string][]conversations.AppendTurnParams{}}
}

func (f *steerE2EConvStore) Create(_ context.Context, p conversations.CreateParams) (conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := conversations.Conversation{ID: p.ID, IdentityID: p.IdentityID, Status: conversations.StatusActive, Model: p.Model}
	f.convs[p.ID] = &c
	return c, nil
}

func (f *steerE2EConvStore) Get(_ context.Context, id string) (conversations.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.convs[id]
	if !ok {
		return conversations.Conversation{}, conversations.ErrConversationNotFound
	}
	return *c, nil
}

func (f *steerE2EConvStore) GetForIdentity(ctx context.Context, id, _ string) (conversations.Conversation, error) {
	return f.Get(ctx, id)
}

func (f *steerE2EConvStore) CountTurns(_ context.Context, id string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.turns[id]), nil
}

func (f *steerE2EConvStore) appendLocked(p conversations.AppendTurnParams) {
	if p.Seq <= 0 {
		p.Seq = len(f.turns[p.ConversationID]) + 1
	}
	f.turns[p.ConversationID] = append(f.turns[p.ConversationID], p)
}

func (f *steerE2EConvStore) AppendTurn(_ context.Context, p conversations.AppendTurnParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendLocked(p)
	return nil
}

func (f *steerE2EConvStore) AppendAssistantTurnWithCacheMetric(_ context.Context, p conversations.AppendTurnParams, _ sqlc.InsertCacheMetricParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appendLocked(p)
	return nil
}

// messagesLocked reconstructs []llm.Message from the raw persisted turns —
// the REAL loader shape (mirrors conversations.Store's own rebuild), never a
// hand-built fixture standing in for a reload.
func (f *steerE2EConvStore) messagesLocked(id string) []llm.Message {
	turns := f.turns[id]
	out := make([]llm.Message, 0, len(turns))
	for _, t := range turns {
		msg := llm.Message{Role: t.Role, Content: t.Content, ToolCallID: t.ToolCallID}
		if len(t.ToolCalls) > 0 {
			var calls []llm.ToolCall
			if err := json.Unmarshal(t.ToolCalls, &calls); err == nil {
				msg.ToolCalls = calls
			}
		}
		out = append(out, msg)
	}
	return out
}

func (f *steerE2EConvStore) LoadHistory(_ context.Context, id string) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.messagesLocked(id), nil
}

func (f *steerE2EConvStore) LoadManagedHistory(_ context.Context, id string, _ conversations.ContextConfig) ([]llm.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.messagesLocked(id), nil
}

func (f *steerE2EConvStore) LoadManagedHistoryForBranch(ctx context.Context, id string, _ int, cfg conversations.ContextConfig) ([]llm.Message, error) {
	return f.LoadManagedHistory(ctx, id, cfg)
}

func (f *steerE2EConvStore) Compact(context.Context, string, conversations.ContextConfig) (conversations.CompactionResult, error) {
	return conversations.CompactionResult{}, nil
}

func (f *steerE2EConvStore) List(context.Context, bool) ([]conversations.Conversation, error) {
	return nil, nil
}
func (f *steerE2EConvStore) ListForIdentity(context.Context, string, bool) ([]conversations.Conversation, error) {
	return nil, nil
}
func (f *steerE2EConvStore) SearchConversationTurns(context.Context, string, int) ([]conversations.SearchResult, error) {
	return nil, nil
}
func (f *steerE2EConvStore) SearchConversationTurnsForIdentity(context.Context, string, string, int) ([]conversations.SearchResult, error) {
	return nil, nil
}
func (f *steerE2EConvStore) UpdateStatus(context.Context, string, string) error { return nil }
func (f *steerE2EConvStore) UpdateStatusForIdentity(context.Context, string, string, string) (int64, error) {
	return 1, nil
}
func (f *steerE2EConvStore) Rename(context.Context, string, string) error { return nil }
func (f *steerE2EConvStore) RenameForIdentity(context.Context, string, string, string) (int64, error) {
	return 1, nil
}
func (f *steerE2EConvStore) SetTitleIfNull(context.Context, string, string) error { return nil }
func (f *steerE2EConvStore) Delete(context.Context, string) error                 { return nil }
func (f *steerE2EConvStore) DeleteForIdentity(context.Context, string, string) (int64, error) {
	return 1, nil
}
func (f *steerE2EConvStore) ListContextRotEvents(context.Context, string) ([]conversations.RotEvent, error) {
	return nil, nil
}
func (f *steerE2EConvStore) ListTurnAttachments(context.Context, string) ([]conversations.TurnAttachments, error) {
	return nil, nil
}

func (f *steerE2EConvStore) ListTurnReasoning(context.Context, string) ([]conversations.TurnReasoning, error) {
	return nil, nil
}
func (f *steerE2EConvStore) UpdateReasoningEffortForIdentity(context.Context, string, string, string) (int64, error) {
	return 1, nil
}
func (f *steerE2EConvStore) ListBranches(context.Context, string) ([]conversations.Branch, error) {
	return nil, nil
}
func (f *steerE2EConvStore) ForkBranch(context.Context, string, int, string, string) (int, uuid.UUID, error) {
	return 0, uuid.UUID{}, nil
}
func (f *steerE2EConvStore) CanonicalBranchLeaf(context.Context, string) (int, error) { return 0, nil }

type steerE2EPauseStore struct{}

func (steerE2EPauseStore) Insert(context.Context, askuser.InsertParams) error { return nil }
func (steerE2EPauseStore) GetByToken(context.Context, string) (askuser.Pending, error) {
	return askuser.Pending{}, askuser.ErrPauseNotFound
}
func (steerE2EPauseStore) ListPending(context.Context, string) ([]askuser.Pending, error) {
	return nil, nil
}
func (steerE2EPauseStore) MarkResumed(context.Context, string, askuser.ResumeAnswer) error {
	return nil
}
func (steerE2EPauseStore) MarkResumedBatch(context.Context, map[string]askuser.ResumeAnswer) error {
	return nil
}
func (steerE2EPauseStore) AutoResolveForConversation(context.Context, string) error { return nil }
func (steerE2EPauseStore) ListExpiredPendingApprovals(context.Context, time.Time, int) ([]askuser.Pending, error) {
	return nil, nil
}

type steerE2EIdentityStore struct{}

func (steerE2EIdentityStore) ListIdentities(context.Context) ([]identity.Identity, error) {
	return []identity.Identity{{ID: localIdentityID, Name: "local", Kind: "system"}}, nil
}

func (steerE2EIdentityStore) GetIdentityByName(_ context.Context, name string) (identity.Identity, error) {
	if name == "local" {
		return identity.Identity{ID: localIdentityID, Name: "local", Kind: "system"}, nil
	}
	return identity.Identity{}, identity.ErrIdentityNotFound
}

func (steerE2EIdentityStore) GetIdentityByID(_ context.Context, id string) (identity.Identity, error) {
	if id == localIdentityID {
		return identity.Identity{ID: localIdentityID, Name: "local", Kind: "system"}, nil
	}
	return identity.Identity{}, identity.ErrIdentityNotFound
}

type steerE2ECacheMetricStore struct{}

func (steerE2ECacheMetricStore) Insert(context.Context, sqlc.InsertCacheMetricParams) error {
	return nil
}

type steerE2EToolInvocationStore struct{}

func (steerE2EToolInvocationStore) Insert(context.Context, toolinvocations.Event) error { return nil }

// steerViaHTTPTool simulates "the operator typed a steer while this run was
// live": its Execute runs SYNCHRONOUSLY inside the agent's tool dispatch
// (itself running on runProducer's own goroutine, detached from the request
// goroutine draining the SSE response — genuinely concurrent with the
// stream), looks up the run's OWN live session by (identity, thread), and
// POSTs to the SAME httptest.Server's real /steer route — the identical path
// 52-07's cockpit will drive. baseURL/runs are set after the server exists,
// before the turn that calls this tool is triggered.
type steerViaHTTPTool struct {
	mu      sync.Mutex
	baseURL string
	runs    *RunRegistry
	convID  string
	text    string
	status  int
	postErr error
}

func (s *steerViaHTTPTool) Spec() tools.Spec {
	return tools.Spec{
		Name:        "steer_via_http",
		Summary:     "Test-only: POSTs a steer to this run's own /steer route mid-dispatch.",
		Description: "Test-only: POSTs a steer to this run's own /steer route mid-dispatch.",
		Parameters:  json.RawMessage(`{"type":"object"}`),
	}
}

func (s *steerViaHTTPTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	sess, ok := s.runs.LiveForThread(localIdentityID, s.convID)
	if !ok {
		return tools.ToolResult{}, fmt.Errorf("steerViaHTTPTool: no live run for thread %s", s.convID)
	}
	body := `{"text":` + strconv.Quote(s.text) + `}`
	resp, err := http.Post(s.baseURL+"/agent/runs/"+sess.RunID+"/steer", "application/json", strings.NewReader(body))
	if err != nil {
		s.mu.Lock()
		s.postErr = err
		s.mu.Unlock()
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()
	s.mu.Lock()
	s.status = resp.StatusCode
	s.mu.Unlock()
	return tools.NewResult(ctx, "steer posted")
}

// readFullBody drains a response to EOF (unlike server_run_steer_test.go's
// readAllClose, which reads one chunk — fine for a short error body, wrong
// here: the test must block until the detached run's SSE stream naturally
// closes at turn end, so the concurrent tool.Execute has already run and the
// full frame set, including the aura.steer echo, is captured).
func readFullBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(raw)
}

// joinMessageContents concatenates a recorded request's message contents so
// the test can substring-search the round's full text for the marked steer
// envelope, regardless of which message carries it.
func joinMessageContents(msgs []llm.Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
	}
	return b.String()
}

// steerRunPayload mirrors server_test.go's runPayload but avoids "ciao" —
// runner.fastReplyFor intercepts trivial greetings BEFORE the LLM client is
// ever called (a real Runner behavior runPayload's fixture text collides
// with), which would silently skip both the scripted client and every tool
// call this test drives.
func steerRunPayload(threadID string) string {
	return `{"threadId":"` + threadID + `","messages":[{"id":"m1","role":"user","content":"please handle this task for me"}]}`
}

func (s *steerViaHTTPTool) result() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status, s.postErr
}

// newRealSteerRunner builds a REAL *runner.Runner + steerE2EConvStore pair
// over the minimal e2e fakes, with the shared steer inbox already wired the
// SAME way cmd/aura/chat_boot.go wires it in production (Steer field on
// Deps) — the only difference from production is the store implementations.
func newRealSteerRunner(t *testing.T, client llm.Client, inbox *steertest.Fake, extraTools ...tools.Tool) (*runner.Runner, *steerE2EConvStore) {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	for _, tool := range extraTools {
		reg.Register(tool)
	}
	conv := newSteerE2EConvStore()
	r := runner.New(runner.Deps{
		Conv:            conv,
		Pause:           steerE2EPauseStore{},
		ApprovalExpiry:  steerE2EPauseStore{},
		Identity:        steerE2EIdentityStore{},
		CacheMetrics:    steerE2ECacheMetricStore{},
		ToolInvocations: steerE2EToolInvocationStore{},
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
		Steer:           inbox,
	})
	return r, conv
}

// TestSteerEndToEndRedirectsNextRound is the tracer's proof (STEER-01): a
// steer reaches the model at the next round boundary, is echoed on the wire,
// and is persisted where it landed — exercising BOTH delivery branches (FA-1).
func TestSteerEndToEndRedirectsNextRound(t *testing.T) {
	t.Run("tool-result-append branch (tail IS a tool result)", func(t *testing.T) {
		inbox := steertest.New(steer.Config{Max: 8, MaxBytes: 16384})
		client := agenttest.NewFakeClient(
			agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "steer_via_http", "{}")),
			agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
		)
		tool := &steerViaHTTPTool{convID: "", text: "switch to plan B"}
		r, conv := newRealSteerRunner(t, client, inbox, tool)

		convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
		if err != nil {
			t.Fatalf("NewConversationWithID: %v", err)
		}
		tool.convID = convID

		s, srv := newDetachTestServer(t, r, conv, ServerConfig{})
		s.SetSteerInbox(inbox)
		tool.baseURL = srv.URL
		tool.runs = s.runs

		resp := postRun(t, srv, steerRunPayload(convID))
		defer resp.Body.Close()
		raw := readFullBody(t, resp)

		status, postErr := tool.result()
		if postErr != nil {
			t.Fatalf("steer POST from tool: %v", postErr)
		}
		if status != http.StatusAccepted {
			t.Fatalf("steer POST status = %d, want 202 (a steer POSTed while the run is live)", status)
		}

		if !strings.Contains(raw, SteerEventName) {
			t.Fatalf("SSE stream missing the %s CUSTOM frame: %s", SteerEventName, raw)
		}
		if !strings.Contains(raw, "switch to plan B") {
			t.Fatalf("SSE stream missing the raw steer text in the echo frame: %s", raw)
		}

		reqs := client.RecordedRequests()
		if len(reqs) < 2 {
			t.Fatalf("recorded %d requests, want at least 2 (round 1 + round 2)", len(reqs))
		}
		round2Content := joinMessageContents(reqs[1].Messages)
		if !strings.Contains(round2Content, "<user_steer nonce=\"") {
			t.Fatalf("round-2 request missing the marked steer envelope: %+v", reqs[1].Messages)
		}
		if !strings.Contains(round2Content, "switch to plan B") {
			t.Fatalf("round-2 request missing the operator's text: %+v", reqs[1].Messages)
		}

		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		var persistedRaw, persistedMarked bool
		for _, m := range hist {
			if m.Role == llm.RoleUser && m.Content == "switch to plan B" {
				persistedRaw = true
			}
			if strings.Contains(m.Content, "<user_steer") {
				persistedMarked = true
			}
		}
		if !persistedRaw {
			t.Fatalf("no persisted RoleUser turn carrying the RAW steer text: %+v", hist)
		}
		if persistedMarked {
			t.Fatalf("a persisted turn carries the marker/nonce envelope -- D-07 forbids this: %+v", hist)
		}
	})

	t.Run("user-message fallback branch (tail is NOT a tool result)", func(t *testing.T) {
		inbox := steertest.New(steer.Config{Max: 8, MaxBytes: 16384})
		client := agenttest.NewFakeClient(
			agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "text_response", `{"text":"done"}`)),
		)
		r, conv := newRealSteerRunner(t, client, inbox)

		convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
		if err != nil {
			t.Fatalf("NewConversationWithID: %v", err)
		}
		// Queued BEFORE the turn starts: drain point A (before round 1's API
		// call) sees the history tail as the initial RoleUser message — NOT a
		// tool result — so the fallback branch (a NEW RoleUser message) fires
		// on round 1 itself, proving the fallback without needing HTTP
		// concurrency (already exhaustively proven for the primary branch
		// above and for the route itself in server_run_steer_test.go).
		if err := inbox.Push(convID, "cockpit", "actually, stop and summarize"); err != nil {
			t.Fatalf("Push: %v", err)
		}

		s, srv := newDetachTestServer(t, r, conv, ServerConfig{})
		s.SetSteerInbox(inbox)

		resp := postRun(t, srv, steerRunPayload(convID))
		defer resp.Body.Close()
		raw := readFullBody(t, resp)

		if !strings.Contains(raw, SteerEventName) {
			t.Fatalf("SSE stream missing the %s CUSTOM frame: %s", SteerEventName, raw)
		}

		reqs := client.RecordedRequests()
		if len(reqs) < 1 {
			t.Fatal("no recorded requests")
		}
		round1Content := joinMessageContents(reqs[0].Messages)
		if !strings.Contains(round1Content, "<user_steer nonce=\"") {
			t.Fatalf("round-1 request missing the marked steer envelope: %+v", reqs[0].Messages)
		}

		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		found := false
		for _, m := range hist {
			if m.Role == llm.RoleUser && m.Content == "actually, stop and summarize" {
				found = true
			}
		}
		if !found {
			t.Fatalf("no persisted RoleUser turn carrying the RAW steer text: %+v", hist)
		}
	})
}
