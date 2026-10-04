package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// The cap is the default of AURA_SHELL_BG_MAX, 8, and counts only the calls still running.
func TestBackgroundCallsKeepACallInItsTurnPastTheOwnersCap(t *testing.T) {
	logs := captureLockedLogs(t)
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	for range 8 {
		finished := newGateTool("finished")
		promote(t, calls, turn, finished)
		close(finished.release)
		awaitCompletion(t, got)
	}
	running := make([]*gateTool, 8)
	for i := range running {
		running[i] = newGateTool("slow")
		promote(t, calls, turn, running[i])
	}

	extra := newGateTool("one-too-many")
	extra.result = ToolResult{Preview: "waited for"}
	go func() {
		<-extra.started
		time.Sleep(3 * testWindow)
		close(extra.release)
	}()
	res, err := calls.Execute(turn, extra, nil, testWindow, testCeiling)
	if err != nil || res.Preview != "waited for" {
		t.Fatalf("a call past the cap = %+v, %v; want it kept in its turn to the end", res, err)
	}
	if !strings.Contains(logs.String(), "stays in its turn") || !strings.Contains(logs.String(), "the most background calls") {
		t.Fatalf("log %q, want the kept call reported with its reason", logs.String())
	}

	// Another identity has a cap of its own.
	other, _ := backgroundTurn(t, "id-2", "sess-9")
	otherTool := newGateTool("slow")
	promote(t, calls, other, otherTool)
	close(otherTool.release)
	for _, tool := range running {
		close(tool.release)
	}
	for range len(running) + 1 {
		awaitCompletion(t, got)
	}
}

func TestBackgroundCallsRaiseAnInlinePanicAndRecordABackgroundOne(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")

	inline := newGateTool("boom")
	inline.panicWith = "inline boom"
	close(inline.release)
	func() {
		defer func() {
			if recovered := recover(); recovered != "inline boom" {
				t.Fatalf("recovered %v, want the tool's own panic raised for runToolRecovering", recovered)
			}
		}()
		_, _ = calls.Execute(turn, inline, nil, time.Second, testCeiling)
	}()

	background := newGateTool("boom")
	background.panicWith = "background boom"
	notice := promote(t, calls, turn, background)
	close(background.release)
	if c := awaitCompletion(t, got); c.Status != BackgroundCallFailed {
		t.Fatalf("status = %q, want %q", c.Status, BackgroundCallFailed)
	}
	if _, err := pollTask(turn, calls, notice.TaskID, false); err == nil || !strings.Contains(err.Error(), "panic: background boom") {
		t.Fatalf("tool_poll err = %v, want the panic as the call's failure", err)
	}
}

// lockedLogs is the default logger's buffer for a test whose records are written by the
// goroutine that settles a call while the test reads them.
type lockedLogs struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLogs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func captureLockedLogs(t *testing.T) *lockedLogs {
	t.Helper()
	logs := &lockedLogs{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return logs
}

func TestBackgroundCallsSurviveAPanickingCompletionHook(t *testing.T) {
	logs := captureLockedLogs(t)
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	calls.SetCompletionHook(func(BackgroundCallCompletion) { panic("hook") })
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("slow")
	tool.result = ToolResult{Preview: "kept"}
	notice := promote(t, calls, turn, tool)
	close(tool.release)

	eventually(t, "the result is readable after the hook panicked", func() bool {
		res, err := pollTask(turn, calls, notice.TaskID, false)
		return err == nil && res.Preview == "kept"
	})
	eventually(t, "the panic is reported with the task id", func() bool {
		return strings.Contains(logs.String(), "completion hook panicked") && strings.Contains(logs.String(), notice.TaskID)
	})
}

// With no hook a settled call waits to be read; nothing is called, so nothing panics.
func TestBackgroundCallsWithoutAHookOnlyKeepTheResult(t *testing.T) {
	logs := captureLockedLogs(t)
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("slow")
	tool.result = ToolResult{Preview: "kept"}
	notice := promote(t, calls, turn, tool)
	close(tool.release)

	eventually(t, "the result is readable", func() bool {
		res, err := pollTask(turn, calls, notice.TaskID, false)
		return err == nil && res.Preview == "kept"
	})
	if strings.Contains(logs.String(), "panicked") {
		t.Fatalf("log %q, want no hook called", logs.String())
	}
}

func TestBackgroundCallsForgetAnUnreadResultAndItsFiles(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	calls.resultTTL = 20 * time.Millisecond
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("slow")
	notice := promote(t, calls, turn, tool)
	close(tool.release)
	awaitCompletion(t, got)

	eventually(t, "the unread result's files are removed", tool.cleaned.Load)
	if _, err := pollTask(turn, calls, notice.TaskID, false); err == nil {
		t.Fatal("an expired result is still readable")
	}
	// The removal runs long after the turn that made the call, on a budget of its own.
	if left := time.Duration(tool.cleanupLeft.Load()); left <= 29*time.Second || left > 30*time.Second {
		t.Fatalf("the removal had %v left, want the 30 s it is given", left)
	}
}

func TestBackgroundCallsTerminateADeletedConversationsCalls(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	settled := newGateTool("settled")
	settledNotice := promote(t, calls, turn, settled)
	close(settled.release)
	awaitCompletion(t, got)
	running := newGateTool("running")
	promote(t, calls, turn, running)
	runningCtx := <-running.started
	other, _ := backgroundTurn(t, "id-1", "sess-2")
	kept := newGateTool("kept")
	keptNotice := promote(t, calls, other, kept)
	// A co-tenant whose conversation happens to share the session id is someone else's.
	tenant, _ := backgroundTurn(t, "id-2", "sess-1")
	tenantTool := newGateTool("co-tenant")
	tenantNotice := promote(t, calls, tenant, tenantTool)

	(&ToolPoll{Calls: calls}).TerminateSession("id-1", "sess-1")

	if runningCtx.Err() == nil {
		t.Fatal("the deleted conversation's running call was not cancelled")
	}
	if !settled.cleaned.Load() {
		t.Fatal("the deleted conversation's unread result kept its files")
	}
	eventually(t, "the running call's files are removed", running.cleaned.Load)
	if _, err := pollTask(turn, calls, settledNotice.TaskID, false); err == nil {
		t.Fatal("the deleted conversation's result is still readable")
	}
	close(kept.release)
	close(tenantTool.release)
	awaitCompletion(t, got)
	awaitCompletion(t, got)
	if _, err := pollTask(other, calls, keptNotice.TaskID, false); err != nil {
		t.Fatalf("another conversation's call was touched: %v", err)
	}
	if _, err := pollTask(tenant, calls, tenantNotice.TaskID, false); err != nil {
		t.Fatalf("another identity's call in the same session was touched: %v", err)
	}
	(&ToolPoll{}).TerminateSession("id-1", "sess-1") // a poll with no registry is a no-op
	var none *ToolPoll
	none.TerminateSession("id-1", "sess-1")
}

// Removing an unread result's files runs with nobody to tell, so a failure is logged.
func TestBackgroundCallsLogAFailedRemovalOfAnUnreadResult(t *testing.T) {
	logs := captureLockedLogs(t)
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("slow")
	tool.cleanupErr = errors.New("box gone")
	promote(t, calls, turn, tool)
	close(tool.release)
	awaitCompletion(t, got)

	calls.TerminateSession("id-1", "sess-1")
	if !strings.Contains(logs.String(), "background tool call cleanup failed") || !strings.Contains(logs.String(), "box gone") {
		t.Fatalf("log %q, want the failed removal reported", logs.String())
	}
}

func TestBackgroundCallsStopCancelsRunningCallsAndRefusesNewOnes(t *testing.T) {
	calls := NewBackgroundCalls()
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	running := newGateTool("running")
	promote(t, calls, turn, running)
	runningCtx := <-running.started
	settled := newGateTool("settled")
	promote(t, calls, turn, settled)
	close(settled.release)
	awaitCompletion(t, got)

	stopCalls(t, calls)
	if runningCtx.Err() == nil {
		t.Fatal("Stop left a call running")
	}
	select {
	case c := <-got:
		t.Fatalf("a call cancelled by Stop woke its conversation: %+v", c)
	case <-time.After(30 * time.Millisecond):
	}
	if n := len(calls.calls); n != 0 {
		t.Fatalf("%d calls kept after Stop", n)
	}

	late := newGateTool("late")
	late.result = ToolResult{Preview: "inline"}
	go func() {
		<-late.started
		time.Sleep(3 * testWindow)
		close(late.release)
	}()
	if res, err := calls.Execute(turn, late, nil, testWindow, testCeiling); err != nil || res.Preview != "inline" {
		t.Fatalf("a call after Stop = %+v, %v; want it kept in its turn", res, err)
	}
	var nilCalls *BackgroundCalls
	nilCalls.SetCompletionHook(nil)
	nilCalls.TerminateSession("id-1", "sess-1")
	if err := nilCalls.Stop(context.Background()); err != nil {
		t.Fatalf("nil Stop: %v", err)
	}
}

// A conversation deleted while the process shuts down: its call is dropped without a box
// exec, which shutdown leaves to the next turn's stale sweep.
func TestBackgroundCallsLeaveTheFilesOfACallDroppedAtShutdownToTheSweep(t *testing.T) {
	calls := NewBackgroundCalls()
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	stubborn := newGateTool("stubborn")
	stubborn.ignoreCtx = true
	promote(t, calls, turn, stubborn)
	calls.TerminateSession("id-1", "sess-1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_ = calls.Stop(ctx)

	close(stubborn.release)
	stopCalls(t, calls)
	if stubborn.cleaned.Load() {
		t.Fatal("a call dropped at shutdown ran its removal")
	}
}

func TestBackgroundCallsStopGivesUpOnACallThatIgnoresCancellation(t *testing.T) {
	calls := NewBackgroundCalls()
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	stubborn := newGateTool("stubborn")
	stubborn.ignoreCtx = true
	promote(t, calls, turn, stubborn)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := calls.Stop(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop = %v, want it to give up at its deadline", err)
	}
	close(stubborn.release)
	stopCalls(t, calls)
}

func TestToolPollRefusesWhatItCannotRead(t *testing.T) {
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	if _, err := (&ToolPoll{}).Execute(turn, json.RawMessage(`{"task_id":"x"}`)); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("no registry err = %v", err)
	}
	poll := &ToolPoll{Calls: NewBackgroundCalls()}
	for raw, want := range map[string]string{
		`{"task_id":"  "}`: "task_id is required",
		`not json`:         "tool_poll args",
		`{"task_id":"x"}`:  `unknown task_id "x"`,
	} {
		if _, err := poll.Execute(turn, json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Execute(%s) err = %v, want %q", raw, err, want)
		}
	}
}

func TestToolPollReportsARunningCallWithoutWaitingForIt(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")
	tool := newGateTool("slow")
	notice := promote(t, calls, turn, tool)

	res, err := pollTask(turn, calls, notice.TaskID, false)
	var state struct {
		TaskID    string `json:"task_id"`
		Tool      string `json:"tool"`
		Status    string `json:"status"`
		ElapsedMS int64  `json:"elapsed_ms"`
	}
	if err != nil || json.Unmarshal([]byte(res.Preview), &state) != nil ||
		state.TaskID != notice.TaskID || state.Tool != "slow" || state.Status != BackgroundCallRunning || state.ElapsedMS < 0 {
		t.Fatalf("tool_poll = %q, %v", res.Preview, err)
	}
	close(tool.release)
}

func TestToolPollIsAForegroundLoadedTool(t *testing.T) {
	spec := (&ToolPoll{}).Spec()
	if spec.Name != "tool_poll" || spec.Deferred || !spec.Foreground || spec.Mutating {
		t.Fatalf("spec = %+v, want a loaded, foreground, read-only tool_poll", spec)
	}
	var schema map[string]any
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatalf("parameters: %v", err)
	}
}

// Aura's own notices are trusted, so their instructions are not discounted as a tool's
// content; a result read back through tool_poll is never more trusted than it was.
func TestBackgroundCallsKeepEveryResultsTrustWhereItBelongs(t *testing.T) {
	calls := NewBackgroundCalls()
	defer stopCalls(t, calls)
	got := recordCompletions(calls)
	turn, _ := backgroundTurn(t, "id-1", "sess-1")

	bare := newGateTool("web_fetch")
	bare.result = ToolResult{Preview: "page"}
	res, err := calls.Execute(turn, bare, nil, testWindow, testCeiling)
	if err != nil || res.Provenance == nil || res.Provenance.Trust != TrustTrusted || res.Provenance.Source != "aura:background" {
		t.Fatalf("notice provenance = %+v (err %v), want Aura's own, trusted", res.Provenance, err)
	}
	var notice promotedNotice
	if err := json.Unmarshal([]byte(res.Preview), &notice); err != nil {
		t.Fatal(err)
	}
	running, err := pollTask(turn, calls, notice.TaskID, false)
	if err != nil || running.Provenance == nil || running.Provenance.Trust != TrustTrusted {
		t.Fatalf("running status provenance = %+v (err %v)", running.Provenance, err)
	}
	close(bare.release)
	awaitCompletion(t, got)
	if read, err := pollTask(turn, calls, notice.TaskID, false); err != nil ||
		read.Provenance == nil || read.Provenance.Trust != TrustUntrusted || read.Provenance.Source != "web_fetch" {
		t.Fatalf("read provenance = %+v (err %v), want untrusted under web_fetch", read.Provenance, err)
	}

	mcp := newGateTool("mail__read")
	mcp.result = ToolResult{Preview: "mail", Provenance: &ToolResultProvenance{Source: "mcp:mail__read", Trust: TrustTrusted}}
	notice = promote(t, calls, turn, mcp)
	close(mcp.release)
	awaitCompletion(t, got)
	if read, err := pollTask(turn, calls, notice.TaskID, false); err != nil || read.Provenance.Source != "mcp:mail__read" || read.Provenance.Trust != TrustTrusted {
		t.Fatalf("read provenance = %+v (err %v), want the tool's own", read.Provenance, err)
	}
}
