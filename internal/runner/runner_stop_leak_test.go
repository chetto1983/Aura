package runner

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

// hangingTitleClient blocks in Stream until unblock is closed OR the caller ctx is
// cancelled — it HONORS the titleTimeout-bounded auto-title worker ctx (runner_resume.go:
// context.WithTimeout(context.WithoutCancel(turnCtx), titleTimeout)), so it can never
// become a bare permanently-blocked goroutine. It signals `entered` on the first Stream
// entry so the test can wait until the title worker is actually hung before measuring.
type hangingTitleClient struct {
	unblock chan struct{}
	entered chan struct{}
	once    sync.Once
}

func (h *hangingTitleClient) Stream(ctx context.Context, _ llm.Request) (<-chan llm.Chunk, error) {
	h.once.Do(func() { close(h.entered) })
	select {
	case <-h.unblock:
	case <-ctx.Done():
	}
	// Return a closed empty channel so GenerateTitle drains immediately (empty → error) and
	// the worker returns, calling wg.Done().
	ch := make(chan llm.Chunk)
	close(ch)
	return ch, nil
}

// newLeakTestRunner builds a Runner with caller-chosen title/stop timeouts and a custom
// client, so the leak test can set titleTimeout >> stopTimeout (the worker stays hung
// across the whole measurement window while each Stop times out fast).
func newLeakTestRunner(t *testing.T, client llm.Client, titleTimeout, stopTimeout time.Duration) (*Runner, *fakeConvStore) {
	t.Helper()
	conv := newFakeConvStore()
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	r := New(Deps{
		Conv:            conv,
		Pause:           newFakePauseStore(),
		Identity:        newFakeIdentityStore(),
		CacheMetrics:    newFakeCacheMetricStore(),
		ToolInvocations: newFakeToolInvocationStore(),
		Client:          client,
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768},
		TitleTimeout:    titleTimeout,
		StopTimeout:     stopTimeout,
	})
	return r, conv
}

// TestStop_HungWorkerDoesNotLeakWaiterGoroutines is the LOOP-11/F-045 regression: repeated
// Stop on a HUNG auto-title worker must not grow runtime.NumGoroutine. The single
// lifecycle-owned sync.Once + stopDone spawns exactly ONE wg-drain waiter; the pre-fix
// per-call waiter grew the count by one per Stop. The DELTA is measured via NumGoroutine
// (not goleak) because ONE waiter is legitimately blocked WHILE the worker is hung.
//
// The worker is spawned via maybeAutoTitle directly (not a full Turn): the property under
// test is waitWorkers/Stop's waiter lifecycle, and the worker is identical either way —
// driving a full Turn would additionally require the same client to serve the agent's
// round without hanging, an unrelated complication. A t.Cleanup deterministically unblocks
// the client and joins the worker so ZERO goroutines remain at return — the package-wide
// goleak.VerifyTestMain stays green WITHOUT any goleak-ignore.
func TestStop_HungWorkerDoesNotLeakWaiterGoroutines(t *testing.T) {
	client := &hangingTitleClient{unblock: make(chan struct{}), entered: make(chan struct{})}
	// titleTimeout >> stopTimeout: the worker stays hung across the measurement window;
	// stopTimeout is tiny so each Stop times out fast.
	r, conv := newLeakTestRunner(t, client, 5*time.Minute, 20*time.Millisecond)
	ctx := context.Background()
	convID := newConvID(t)

	// Reach the seq>=3 auto-title threshold on an untitled conversation.
	if _, err := conv.Create(ctx, conversations.CreateParams{ID: convID, IdentityID: "00000000-0000-0000-0000-000000000001"}); err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	for seq := 1; seq <= autoTitleMinSeq; seq++ {
		if err := conv.AppendTurn(ctx, conversations.AppendTurnParams{
			ConversationID: convID, Seq: seq, Role: llm.RoleUser, Content: "x",
		}); err != nil {
			t.Fatalf("seed turn %d: %v", seq, err)
		}
	}
	history := []llm.Message{{Role: llm.RoleUser, Content: "x"}}

	// Deterministic teardown registered BEFORE the hung window opens: unblock the client and
	// join the worker so ZERO goroutines remain at return (keeps goleak.VerifyTestMain green).
	t.Cleanup(func() {
		close(client.unblock)
		if !r.waitWorkers(5 * time.Second) {
			t.Error("title worker did not drain after unblock — a goroutine leaked")
		}
	})

	// Spawn the (soon-to-hang) title worker and wait until it enters the blocked stream.
	r.maybeAutoTitle(ctx, convID, history)
	select {
	case <-client.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("title worker never entered the blocking stream")
	}

	// First Stop spawns the SINGLE wg-drain waiter (the fix); measure the baseline AFTER it
	// so the one legitimate waiter is already counted.
	_ = r.Stop(ctx, convID) // times out (worker hung); auto-resolve still runs
	runtime.Gosched()
	base := runtime.NumGoroutine()

	// Repeated Stop on the still-hung worker MUST NOT grow the goroutine count: sync.Once +
	// one stopDone → no fresh waiter per call. Pre-fix this grew by ~extraStops.
	const extraStops = 20
	for i := 0; i < extraStops; i++ {
		_ = r.Stop(ctx, convID)
	}
	runtime.Gosched()
	after := runtime.NumGoroutine()

	if grew := after - base; grew > 2 {
		t.Fatalf("repeated Stop on a hung worker leaked ~%d waiter goroutines (want ~0; base=%d after=%d over %d extra Stops)",
			grew, base, after, extraStops)
	}
}
