package runner

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

type fakeEmbedder struct {
	mu  sync.Mutex
	err error
	n   int
}

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float64, len(texts))
	for i, text := range texts {
		seed := float64(len(text)%7) + 1
		out[i] = []float64{seed, seed / 2, 1}
	}
	return out, nil
}

func (f *fakeEmbedder) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.n
}

// --- TryLockThread + Turn(threadLockHeld) ---

func TestTryLockThread(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	convID := newConvID(t)

	unlock, ok := r.TryLockThread(context.Background(), convID)
	if !ok || unlock == nil {
		t.Fatal("first TryLockThread must succeed")
	}
	// A second attempt while held must fail without blocking.
	if _, ok2 := r.TryLockThread(context.Background(), convID); ok2 {
		t.Fatal("a second TryLockThread on a held thread must report ok=false")
	}
	unlock()
	// After release the lock is acquirable again.
	unlock2, ok3 := r.TryLockThread(context.Background(), convID)
	if !ok3 {
		t.Fatal("TryLockThread must succeed after release")
	}
	unlock2()
}

// TestTurn_WithThreadLockHeld_SkipsReLock proves the gateway path: when the caller
// already owns the per-thread lock (WithThreadLockHeld), Turn dispatches directly to
// turnLocked without re-acquiring (no deadlock against the held lock).
func TestTurn_WithThreadLockHeld_SkipsReLock(t *testing.T) {
	client := agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "done")))
	r, conv, _ := newTestRunner(t, client)
	convID := newConvID(t)
	mustCreate(t, r, convID)

	unlock, ok := r.TryLockThread(context.Background(), convID)
	if !ok {
		t.Fatal("could not acquire the thread lock")
	}
	defer unlock()

	ctx := WithThreadLockHeld(context.Background())
	if _, err := drain(r.Turn(ctx, convID, new("hi"))); err != nil {
		t.Fatalf("turn under a held lock must not deadlock/error: %v", err)
	}
	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil || len(hist) == 0 {
		t.Fatalf("turn produced no history: %v (%v)", hist, err)
	}
}

// --- wireToolSearchEmbedder embedder paths ---

func TestWireToolSearchEmbedder(t *testing.T) {
	t.Run("nil registry is a no-op", func(t *testing.T) {
		wireToolSearchEmbedder(nil, &fakeEmbedder{}) // must not panic
	})
	t.Run("registry without tool_search is a no-op", func(t *testing.T) {
		wireToolSearchEmbedder(tools.NewRegistry(), &fakeEmbedder{})
	})
	t.Run("nil embedder logs and leaves Embed nil", func(t *testing.T) {
		reg := tools.NewRegistry()
		ts := &tools.ToolSearch{Registry: reg}
		reg.Register(ts)
		wireToolSearchEmbedder(reg, nil)
		if ts.Embed != nil {
			t.Fatal("a nil embedder must leave tool_search.Embed nil")
		}
	})
	t.Run("a wired embedder is set and health-checked", func(t *testing.T) {
		reg := tools.NewRegistry()
		ts := &tools.ToolSearch{Registry: reg}
		reg.Register(ts)
		embed := &fakeEmbedder{}
		wireToolSearchEmbedder(reg, embed)
		if ts.Embed == nil {
			t.Fatal("a wired embedder must be set on tool_search")
		}
		if embed.calls() == 0 {
			t.Fatal("the boot health-check must probe the embedder once")
		}
	})
	t.Run("an unreachable embedder logs but does not panic", func(t *testing.T) {
		reg := tools.NewRegistry()
		ts := &tools.ToolSearch{Registry: reg}
		reg.Register(ts)
		embed := &fakeEmbedder{err: errors.New("sidecar down")}
		wireToolSearchEmbedder(reg, embed) // health-check error path: log-only, never fatal
		if ts.Embed == nil {
			t.Fatal("an unreachable embedder must still be wired (recovers later)")
		}
	})
}

type turnStartProbeHook struct {
	starts int
}

func (h *turnStartProbeHook) OnTurnStart(context.Context, agent.HookTurn) error {
	h.starts++
	return nil
}

func (*turnStartProbeHook) BeforeModel(context.Context, *llm.Request) (*agent.ModelHookResult, error) {
	return nil, nil
}

func (*turnStartProbeHook) BeforeTool(context.Context, llm.ToolCall) (*agent.ToolHookResult, error) {
	return nil, nil
}

func (*turnStartProbeHook) AfterTool(context.Context, llm.ToolCall, tools.ToolResult) (*agent.ToolResultHookResult, error) {
	return nil, nil
}

func (*turnStartProbeHook) OnTurnEnd(context.Context, agent.HookTurn) error {
	return nil
}

func TestRunnerThreadsHookManagerIntoPerTurnAgent(t *testing.T) {
	hook := &turnStartProbeHook{}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	r := New(Deps{
		Conv:            newFakeConvStore(),
		Pause:           newFakePauseStore(),
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "ok"))),
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		HookManager:     agent.NewHookManager(hook),
		TitleTimeout:    time.Second,
		StopTimeout:     time.Second,
	})
	convID := newConvID(t)
	mustCreate(t, r, convID)

	if _, err := drain(r.Turn(context.Background(), convID, new("hi"))); err != nil {
		t.Fatalf("turn with hook manager: %v", err)
	}
	if hook.starts != 1 {
		t.Fatalf("hook OnTurnStart calls = %d, want 1", hook.starts)
	}
}

// --- contextConfig + renderContextBlock error paths ---

// errConvStore wraps fakeConvStore to inject a Get error so renderContextBlock's
// load-conversation-identity branch surfaces.
type errConvStore struct {
	*fakeConvStore
	getErr error
}

func (e *errConvStore) Get(ctx context.Context, id string) (conversations.Conversation, error) {
	if e.getErr != nil {
		return conversations.Conversation{}, e.getErr
	}
	return e.fakeConvStore.Get(ctx, id)
}

func newCtxBlockRunner(t *testing.T, conv ConversationStore, id IdentityStore) *Runner {
	t.Helper()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	return New(Deps{
		Conv:            conv,
		Pause:           newFakePauseStore(),
		Identity:        id,
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "ok"))),
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    time.Second,
		StopTimeout:     time.Second,
	})
}

// TestRenderContextBlock_AlwaysBlockComposed proves the alwaysBlock leg of
// renderContextBlock (a non-empty always-block is appended).
func TestRenderContextBlock_AlwaysBlockComposed(t *testing.T) {
	conv := newFakeConvStore()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	r := New(Deps{
		Conv:            conv,
		Pause:           newFakePauseStore(),
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "ok"))),
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		AlwaysBlock:     func() string { return "always-on skill block" },
		HistoryCap:      7,
		TitleTimeout:    time.Second,
		StopTimeout:     time.Second,
	})
	convID := newConvID(t)
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(context.Background(), convID, new("hi"))); err != nil {
		t.Fatalf("turn with an always-block: %v", err)
	}
	if got := conv.lastCfg.AlwaysBlock; got != "always-on skill block" {
		t.Fatalf("LoadManagedHistory cfg.AlwaysBlock = %q; want the composed always-block", got)
	}
	if got := conv.lastCfg.HistoryHardCapTurns; got != 7 {
		t.Fatalf("LoadManagedHistory cfg.HistoryHardCapTurns = %d; want 7", got)
	}
}

// --- buildAgent budget-config error ---

// TestTurn_BuildAgentBudgetError forces NewBudget to fail via an invalid AURA_LOOP_*
// env so the buildAgent error branch in turnLocked surfaces on the turn.
func TestTurn_BuildAgentBudgetError(t *testing.T) {
	t.Setenv("AURA_LOOP_MAX_STEPS", "not-a-number")
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("c1", "x"))))
	convID := newConvID(t)
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(context.Background(), convID, new("hi"))); err == nil {
		t.Fatal("expected the budget-config error (invalid AURA_LOOP_MAX_STEPS) to surface")
	}
}

// --- NewConversation happy path (covers the mint + delegate) ---

func TestNewConversation_DelegatesToWithID(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient())
	id, err := r.NewConversation(context.Background())
	if err != nil {
		t.Fatalf("NewConversation: %v", err)
	}
	if _, err := conv.Get(context.Background(), id); err != nil {
		t.Fatalf("created conversation not found: %v", err)
	}
}

// --- maybeAutoTitle Get-error guard ---

// TestMaybeAutoTitle_GetErrorIsNoOp proves a conversation Get failure inside the
// auto-title gate is a silent no-op (the worker never fires).
func TestMaybeAutoTitle_GetErrorIsNoOp(t *testing.T) {
	conv := &errConvStore{fakeConvStore: newFakeConvStore(), getErr: errFake}
	r := newCtxBlockRunner(t, conv, newFakeIdentityStore())
	// Direct call: a Get error returns before any CountTurns/worker spawn.
	r.maybeAutoTitle(context.Background(), newConvID(t), nil)
	// Nothing to assert beyond "did not panic / did not spawn a worker"; Stop joins
	// the (empty) WaitGroup cleanly.
	if err := r.Stop(context.Background(), newConvID(t)); err != nil {
		t.Fatalf("stop: %v", err)
	}
}
