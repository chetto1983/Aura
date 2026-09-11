package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

type titleLifecycleClient struct {
	titleStarted chan struct{}
	releaseTitle chan struct{}
	releaseReply chan struct{}
	titleCalls   atomic.Int32
	titleOnce    sync.Once
	titleError   bool
}

func (c *titleLifecycleClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Chunk, error) {
	if strings.Contains(req.Messages[0].Content, "title summarizing a conversation") {
		c.titleCalls.Add(1)
		c.titleOnce.Do(func() { close(c.titleStarted) })
		if c.releaseTitle != nil {
			select {
			case <-c.releaseTitle:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if c.titleError {
			return nil, errors.New("title provider unavailable")
		}
		ch := make(chan llm.Chunk, 1)
		ch <- llm.Chunk{Text: "Titolo della conversazione", FinishReason: "stop"}
		close(ch)
		return ch, nil
	}
	ch := make(chan llm.Chunk, 1)
	go func() {
		defer close(ch)
		select {
		case <-c.releaseReply:
			ch <- llm.Chunk{Text: "Risposta completata.", FinishReason: "stop"}
		case <-ctx.Done():
		}
	}()
	return ch, nil
}

func TestAutoTitleStartsFromFirstUserWhileReplyRuns(t *testing.T) {
	client := &titleLifecycleClient{titleStarted: make(chan struct{}), releaseReply: make(chan struct{})}
	r, store, _ := newTestRunner(t, client)
	id := newConvID(t)
	mustCreate(t, r, id)
	ctx := t.Context()
	done := make(chan error, 1)
	go func() { _, err := drain(r.Turn(ctx, id, new("Organizziamo il rilascio di Aura"))); done <- err }()
	defer func() { close(client.releaseReply); <-done; _ = r.Stop(context.Background(), id) }()
	select {
	case <-client.titleStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("title waited for the answer instead of starting from the first user message")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conv, _ := store.Get(ctx, id)
		if conv.Title == "Titolo della conversazione" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("title was not persisted while the reply was still running")
}

func TestAutoTitleDeduplicatesAndPreservesManualRename(t *testing.T) {
	client := &titleLifecycleClient{titleStarted: make(chan struct{}), releaseTitle: make(chan struct{})}
	r, store, _ := newTestRunner(t, client)
	id := newConvID(t)
	mustCreate(t, r, id)
	ctx := context.Background()
	const userMsg = "Pianifica il rilascio"
	if err := store.AppendTurn(ctx, conversations.AppendTurnParams{ConversationID: id, Seq: 1, Role: llm.RoleUser, Content: userMsg}); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		r.maybeAutoTitle(ctx, id, userMsg)
	}
	select {
	case <-client.titleStarted:
	case <-time.After(time.Second):
		close(client.releaseTitle)
		t.Fatal("title did not start")
	}
	if err := store.SetTitleIfNull(ctx, id, "Titolo scelto a mano"); err != nil {
		t.Fatal(err)
	}
	close(client.releaseTitle)
	if err := r.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	conv, _ := store.Get(ctx, id)
	if client.titleCalls.Load() != 1 || conv.Title != "Titolo scelto a mano" {
		t.Fatalf("calls=%d title=%q", client.titleCalls.Load(), conv.Title)
	}
}

func TestAutoTitleFailureUsesFirstMessageFallback(t *testing.T) {
	client := &titleLifecycleClient{titleStarted: make(chan struct{}), titleError: true}
	r, store, _ := newTestRunner(t, client)
	id := newConvID(t)
	mustCreate(t, r, id)
	ctx := context.Background()
	const userMsg = "  Pianifica   il rilascio  "
	if err := store.AppendTurn(ctx, conversations.AppendTurnParams{ConversationID: id, Seq: 1, Role: llm.RoleUser, Content: userMsg}); err != nil {
		t.Fatal(err)
	}
	r.maybeAutoTitle(ctx, id, userMsg)
	if err := r.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	conv, _ := store.Get(ctx, id)
	if conv.Title != "Pianifica il rilascio" {
		t.Fatalf("fallback title=%q", conv.Title)
	}
}

// An always:true skill reaches the history as a user-role turn ahead of the person's
// message (conversations.injectAlwaysBlock). Titling from the history named a member's
// conversation "Active skill instruc…" (measured 2026-09-11 on the reinstalled stack).
const alwaysOnSkillBlock = "Active skill instructions (always-on):\n\nSKILL-BODY"

func alwaysOnSkillRunner(t *testing.T, title *agenttest.FakeClient) (*Runner, *fakeConvStore) {
	t.Helper()
	r, store, _ := newTestRunner(t, agenttest.TitleClient{
		Main:  agenttest.NewFakeClient(agenttest.ToolCallTurn(textResponseCall("call-1", "Fatto."))),
		Title: title,
	})
	r.alwaysBlock = func(context.Context) string { return alwaysOnSkillBlock }
	return r, store
}

func TestAutoTitlePromptCarriesOnlyTheUserMessage(t *testing.T) {
	title := agenttest.NewFakeClient(agenttest.TextChunks("stop", "Rilascio di Aura"))
	r, _ := alwaysOnSkillRunner(t, title)
	id := newConvID(t)
	mustCreate(t, r, id)
	const userMsg = "Organizziamo il rilascio di Aura"
	if _, err := drain(r.Turn(context.Background(), id, new(userMsg))); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	requests := title.RecordedRequests()
	if len(requests) != 1 || requests[0].Messages[1].Content != userMsg {
		t.Fatalf("title requests = %+v, want one carrying only %q", requests, userMsg)
	}
}

func TestAutoTitleFallbackIgnoresAlwaysOnSkill(t *testing.T) {
	r, store := alwaysOnSkillRunner(t, agenttest.NewFakeClient(agenttest.FakeTurn{Err: errFake}))
	id := newConvID(t)
	mustCreate(t, r, id)
	if _, err := drain(r.Turn(context.Background(), id, new("Pianifica il rilascio"))); err != nil {
		t.Fatal(err)
	}
	if err := r.Stop(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	if conv, _ := store.Get(context.Background(), id); conv.Title != "Pianifica il rilascio" {
		t.Fatalf("fallback title = %q, want the user message", conv.Title)
	}
}
