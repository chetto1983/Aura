package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// gateTool's call lasts as long as the test says: until release is closed or, unless it
// ignores its context, until that context ends. started receives the context it ran on.
type gateTool struct {
	name      string
	release   chan struct{}
	started   chan context.Context
	result    ToolResult
	err       error
	panicWith any
	ignoreCtx bool
	// cleanupErr is what removing the call's files fails with.
	cleanupErr error
	// cleaned is set when the cleanup step the call registered runs, and cleanupLeft is how
	// long its context still had then.
	cleaned     atomic.Bool
	cleanupLeft atomic.Int64
}

func newGateTool(name string) *gateTool {
	return &gateTool{name: name, release: make(chan struct{}), started: make(chan context.Context, 1)}
}

func (g *gateTool) Spec() Spec { return Spec{Name: g.name} }

func (g *gateTool) Execute(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
	// What the MCP file sink does with a result's files: ask the call's cleanup to remove them.
	TurnCleanupFromContext(ctx).Add("files", func(cleanupCtx context.Context) error {
		if deadline, ok := cleanupCtx.Deadline(); ok {
			g.cleanupLeft.Store(int64(time.Until(deadline)))
		}
		g.cleaned.Store(true)
		return g.cleanupErr
	})
	g.started <- ctx
	if g.ignoreCtx {
		<-g.release
	} else {
		select {
		case <-g.release:
		case <-ctx.Done():
			return ToolResult{}, ctx.Err()
		}
	}
	if g.panicWith != nil {
		panic(g.panicWith)
	}
	return g.result, g.err
}

const (
	testWindow  = 20 * time.Millisecond
	testCeiling = 5 * time.Second
)

// backgroundTurn is one turn of conversation session, owned by identity, the way runTool
// leaves a call's context.
func backgroundTurn(t *testing.T, identity, session string) (context.Context, *TurnCleanup) {
	t.Helper()
	ctx := WithRequestID(ctxWithIdentity(t, session, "call-"+session, identity), "req-"+session)
	return WithTurnCleanup(ctx)
}

func recordCompletions(calls *BackgroundCalls) chan BackgroundCallCompletion {
	got := make(chan BackgroundCallCompletion, 16)
	calls.SetCompletionHook(func(c BackgroundCallCompletion) { got <- c })
	return got
}

func awaitCompletion(t *testing.T, got <-chan BackgroundCallCompletion) BackgroundCallCompletion {
	t.Helper()
	select {
	case c := <-got:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("no completion arrived")
		return BackgroundCallCompletion{}
	}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

type promotedNotice struct {
	Status string `json:"status"`
	TaskID string `json:"task_id"`
	Tool   string `json:"tool"`
	Note   string `json:"note"`
}

func promote(t *testing.T, calls *BackgroundCalls, turn context.Context, tool *gateTool) promotedNotice {
	t.Helper()
	res, err := calls.Execute(turn, tool, nil, testWindow, testCeiling)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var notice promotedNotice
	if err := json.Unmarshal([]byte(res.Preview), &notice); err != nil || notice.Status != "in_progress" || notice.TaskID == "" {
		t.Fatalf("result %q is not a background notice (err %v)", res.Preview, err)
	}
	return notice
}

func pollTask(ctx context.Context, calls *BackgroundCalls, id string, cancel bool) (ToolResult, error) {
	raw, _ := json.Marshal(map[string]any{"task_id": id, "cancel": cancel})
	return (&ToolPoll{Calls: calls}).Execute(ctx, raw)
}

func stopCalls(t *testing.T, calls *BackgroundCalls) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := calls.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestBackgroundCallsReturnsACallThatSettlesInItsWindowUnchanged(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	turn, turnCleanup := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("fast")
	tool.result = ToolResult{Preview: "done", Bytes: 4}
	close(tool.release)

	res, err := calls.Execute(turn, tool, nil, time.Second, testCeiling)
	if err != nil || res.Preview != "done" || res.Bytes != 4 {
		t.Fatalf("Execute = %+v, %v; want the tool's own result", res, err)
	}
	callCtx := <-tool.started
	if ceiling, ok := CallCeiling(callCtx); !ok || ceiling != testCeiling {
		t.Fatalf("the call's ceiling = %v (%v), want %v", ceiling, ok, testCeiling)
	}
	if tool.cleaned.Load() {
		t.Fatal("the call's files were removed before its turn ended")
	}
	if err := turnCleanup.Run(context.Background()); err != nil || !tool.cleaned.Load() {
		t.Fatalf("the turn's cleanup did not remove the call's files (err %v)", err)
	}
	if callCtx.Err() == nil {
		t.Fatal("a call that settled in its turn kept its context open")
	}
}

func TestBackgroundCallsMovesASlowCallAndHandsItsResultOverOnce(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, turnCleanup := backgroundTurn(t, "id-1", "sess-1")
	turn, endTurn := context.WithCancel(turn)
	tool := newGateTool("eleven__compose_music")
	tool.result = ToolResult{Preview: "track.mp3 ready"}

	notice := promote(t, calls, turn, tool)
	if notice.Tool != "eleven__compose_music" || !strings.Contains(notice.Note, "tool_poll") {
		t.Fatalf("notice = %+v", notice)
	}
	// The turn that started the call ends; the call does not, and keeps its files.
	endTurn()
	callCtx := <-tool.started
	if err := turnCleanup.Run(context.Background()); err != nil || tool.cleaned.Load() {
		t.Fatalf("the starting turn's cleanup removed the call's files (err %v)", err)
	}
	if callCtx.Err() != nil {
		t.Fatalf("the call ended with its turn: %v", callCtx.Err())
	}

	// Duration is time.Since(startedAt), and a coarse clock (Windows) can read the same
	// instant at both ends: hold the call until the clock has moved past its registration.
	registered := time.Now()
	for time.Since(registered) <= 0 {
		time.Sleep(time.Millisecond)
	}
	close(tool.release)
	c := awaitCompletion(t, got)
	if callCtx.Err() == nil {
		t.Fatal("a settled background call kept its context open")
	}
	if c.TaskID != notice.TaskID || c.OwnerID != "id-1" || c.SessionID != "sess-1" ||
		c.Tool != "eleven__compose_music" || c.Status != BackgroundCallCompleted || c.Duration <= 0 {
		t.Fatalf("completion = %+v", c)
	}

	reader, readerCleanup := backgroundTurn(t, "id-1", "sess-1")
	res, err := pollTask(reader, calls, notice.TaskID, false)
	if err != nil || res.Preview != "track.mp3 ready" {
		t.Fatalf("tool_poll = %+v, %v; want the call's own result", res, err)
	}
	if err := readerCleanup.Run(context.Background()); err != nil || !tool.cleaned.Load() {
		t.Fatalf("the reading turn's cleanup did not remove the call's files (err %v)", err)
	}
	if _, err := pollTask(reader, calls, notice.TaskID, false); err == nil || !strings.Contains(err.Error(), "already read") {
		t.Fatalf("second tool_poll err = %v, want the result gone", err)
	}
}

func TestBackgroundCallsCancelsACallWhoseTurnEndsInsideTheWindow(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	turn, endTurn := context.WithCancel(turn)
	tool := newGateTool("slow")
	go func() {
		<-tool.started
		endTurn()
	}()

	_, err := calls.Execute(turn, tool, nil, time.Second, testCeiling)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute err = %v, want the turn's cancellation", err)
	}
	if n := len(calls.calls); n != 0 {
		t.Fatalf("%d calls registered for a call that never outlived its window", n)
	}
}

// A call the turn already cancelled is ending, not outliving anything, even when it takes
// longer than the window to notice: it is never moved.
func TestBackgroundCallsNeverMoveACallTheirTurnAlreadyCancelled(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	turn, endTurn := context.WithCancel(turn)
	tool := newGateTool("slow-to-stop")
	tool.ignoreCtx = true
	tool.result = ToolResult{Preview: "stopped late"}
	go func() {
		<-tool.started
		endTurn()
		time.Sleep(3 * testWindow)
		close(tool.release)
	}()

	res, err := calls.Execute(turn, tool, nil, testWindow, testCeiling)
	if err != nil || res.Preview != "stopped late" {
		t.Fatalf("Execute = %q, %v; want the call kept in its turn", res.Preview, err)
	}
	if n := len(calls.calls); n != 0 {
		t.Fatalf("%d calls registered for a call its turn had cancelled", n)
	}
}

func TestBackgroundCallsStopsACallAtItsCeiling(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("never")

	res, err := calls.Execute(turn, tool, nil, testWindow, 80*time.Millisecond)
	if err != nil || !strings.Contains(res.Preview, `"in_progress"`) {
		t.Fatalf("Execute = %q, %v", res.Preview, err)
	}
	c := awaitCompletion(t, got)
	if c.Status != BackgroundCallTimedOut {
		t.Fatalf("status = %q, want %q", c.Status, BackgroundCallTimedOut)
	}
	if _, err := pollTask(turn, calls, c.TaskID, false); err == nil ||
		!strings.Contains(err.Error(), "never timed_out") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("tool_poll err = %v, want the timed-out call's own error", err)
	}
}

func TestBackgroundCallsCancelDropsTheCallItsFilesAndItsWake(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("slow")
	notice := promote(t, calls, turn, tool)
	callCtx := <-tool.started

	res, err := pollTask(turn, calls, notice.TaskID, true)
	if err != nil || !strings.Contains(res.Preview, `"status":"cancelled"`) {
		t.Fatalf("cancel = %q, %v", res.Preview, err)
	}
	if callCtx.Err() == nil {
		t.Fatal("the cancelled call is still running")
	}
	eventually(t, "the cancelled call's files are removed", tool.cleaned.Load)
	eventually(t, "the cancelled call is forgotten", func() bool {
		_, err := pollTask(turn, calls, notice.TaskID, false)
		return err != nil
	})
	select {
	case c := <-got:
		t.Fatalf("a cancelled call woke its conversation: %+v", c)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestBackgroundCallsAnswerOnlyTheirOwnConversation(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("slow")
	tool.result = ToolResult{Preview: "mine"}
	notice := promote(t, calls, turn, tool)
	close(tool.release)
	awaitCompletion(t, got)

	for _, other := range []struct{ identity, session string }{{"id-2", "sess-1"}, {"id-1", "sess-2"}} {
		ctx, _ := backgroundTurn(t, other.identity, other.session)
		if _, err := pollTask(ctx, calls, notice.TaskID, true); err == nil || !strings.Contains(err.Error(), "unknown task_id") {
			t.Fatalf("%+v reached another conversation's call: err %v", other, err)
		}
	}
	if res, err := pollTask(turn, calls, notice.TaskID, false); err != nil || res.Preview != "mine" {
		t.Fatalf("the owner's tool_poll = %+v, %v", res, err)
	}
}
