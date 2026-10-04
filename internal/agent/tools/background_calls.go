package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/pausable"
)

// background_calls.go moves a tool call that outlives its window to the background
// (prd.md §15, 2026-10-04). Measured through the real MCP mount path, a 90 s call was cut
// at 60 s with nothing to show for it, and the model's only move was to pay for it again.
// shell_exec already answers this for a command (shell_bg_promote.go); this is the same
// promotion for every other tool. The call keeps the turn's values and loses its
// cancellation, bounded by a ceiling of its own; the model reads a task id instead of
// waiting, and the completion hook wakes the conversation when the call settles.

// BackgroundCallCompletion is the terminal fact of one promoted call: enough to route the
// wake, never the output, which stays behind tool_poll's owner check.
type BackgroundCallCompletion struct {
	TaskID    string
	OwnerID   string
	SessionID string
	Tool      string
	Status    string
	Duration  time.Duration
}

// BackgroundCallCompletionHook receives one completion per promoted call that settles
// without being cancelled. It must return promptly.
type BackgroundCallCompletionHook func(BackgroundCallCompletion)

// The states a promoted call reports through tool_poll and its completion.
const (
	BackgroundCallRunning   = "running"
	BackgroundCallCompleted = "completed"
	BackgroundCallFailed    = "failed"
	BackgroundCallTimedOut  = "timed_out"
	BackgroundCallCancelled = "cancelled"
)

// maxBackgroundCallsPerOwner bounds the calls one identity can leave running, the default
// cap of its background shells (AURA_SHELL_BG_MAX). Past it a call stays in its turn, as
// every call did before promotion existed.
const maxBackgroundCallsPerOwner = 8

// backgroundResultTTL is how long a settled result waits to be read, a background shell's
// default TTL (shell_bg_ttl.go).
const backgroundResultTTL = time.Hour

// backgroundCleanupTimeout bounds the removal of an unread result's files.
const backgroundCleanupTimeout = 30 * time.Second

// BackgroundCalls is the process-wide registry of promoted tool calls. The zero value is
// not usable; build it with NewBackgroundCalls.
type BackgroundCalls struct {
	resultTTL time.Duration

	mu      sync.Mutex
	calls   map[string]*backgroundCall
	hook    BackgroundCallCompletionHook
	closed  bool
	running sync.WaitGroup
}

// NewBackgroundCalls returns an empty registry with no completion hook.
func NewBackgroundCalls() *BackgroundCalls {
	return &BackgroundCalls{resultTTL: backgroundResultTTL, calls: map[string]*backgroundCall{}}
}

// SetCompletionHook installs the hook calls settling from now on report to. Nil leaves
// settled calls to tool_poll alone.
func (b *BackgroundCalls) SetCompletionHook(hook BackgroundCallCompletionHook) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.hook = hook
	b.mu.Unlock()
}

// backgroundCall is one promoted call. Its fields past cleanup are guarded by
// BackgroundCalls.mu.
type backgroundCall struct {
	id        string
	ownerID   string
	sessionID string
	tool      string
	startedAt time.Time
	call      detachedCall

	settled   bool
	discarded bool
	status    string
	result    ToolResult
	err       error
}

// callOutcome is what a call's goroutine hands back. panicked carries a recovered panic,
// so an inline call can re-raise it where runToolRecovering expects it.
type callOutcome struct {
	result   ToolResult
	err      error
	panicked any
}

// Execute runs tool for the turn ctx belongs to. A call that settles within window is
// returned exactly as Execute would return it; one still running is moved to the
// background, bounded by ceiling, and the model gets its task id instead.
func (b *BackgroundCalls) Execute(ctx context.Context, tool Tool, args json.RawMessage, window, ceiling time.Duration) (ToolResult, error) {
	call := detachCall(ctx, ceiling)
	// Unbuffered: whichever way the call goes, this turn or settle receives its outcome.
	done := make(chan callOutcome)
	go runCall(call.ctx, tool, args, done)

	timer := time.NewTimer(window)
	defer timer.Stop()
	select {
	case out := <-done:
		return call.finishInline(ctx, out)
	case <-timer.C:
	}
	// A call that settled as the window closed is not worth a wake.
	select {
	case out := <-done:
		return call.finishInline(ctx, out)
	default:
	}
	name := tool.Spec().Name
	task, err := b.promote(ctx, name, call)
	if err != nil {
		if !errors.Is(err, errBackgroundTurnEnded) {
			slog.Warn("tool call outlived its window and stays in its turn", "tool", name, "reason", err)
		}
		return call.finishInline(ctx, <-done)
	}
	go b.settle(task, done)
	return promotedResult(ctx, task.id, name, window)
}

// detachedCall is a call's own context: the turn's values without its cancellation,
// bounded by the ceiling, and following the turn's cancellation until promotion stops
// it. The ceiling is pausable like the call timeout it replaces: an operator answering
// a server's form does not spend it.
type detachedCall struct {
	ctx     context.Context
	cancel  context.CancelFunc
	follow  func() bool
	cleanup *TurnCleanup
}

func detachCall(turn context.Context, ceiling time.Duration) detachedCall {
	bounded, cancelBound := pausable.WithTimeout(context.WithoutCancel(turn), ceiling)
	ctx, cancelCall := context.WithCancelCause(bounded)
	// A call gets cleanup of its own only inside a turn: with none, nothing would ever
	// run it, and the MCP file sink must keep refusing files nobody removes.
	var cleanup *TurnCleanup
	if TurnCleanupFromContext(turn) != nil {
		ctx, cleanup = WithTurnCleanup(ctx)
	}
	ctx = WithCallCeiling(ctx, ceiling)
	follow := context.AfterFunc(turn, func() { cancelCall(context.Cause(turn)) })
	return detachedCall{
		ctx:     ctx,
		follow:  follow,
		cleanup: cleanup,
		cancel: func() {
			follow()
			cancelCall(context.Canceled)
			cancelBound()
		},
	}
}

func runCall(ctx context.Context, tool Tool, args json.RawMessage, done chan<- callOutcome) {
	defer func() {
		if recovered := recover(); recovered != nil {
			done <- callOutcome{panicked: recovered}
		}
	}()
	result, err := tool.Execute(ctx, args)
	done <- callOutcome{result: result, err: err}
}

// finishInline hands a call that settled in its turn back as if it had run there: its
// cleanup joins the turn's, and a panic is raised again for runToolRecovering.
func (c detachedCall) finishInline(turn context.Context, out callOutcome) (ToolResult, error) {
	c.cancel()
	c.handOverCleanup(turn)
	if out.panicked != nil {
		panic(out.panicked)
	}
	return out.result, out.err
}

// handOverCleanup makes the turn ctx belongs to run this call's cleanup when it ends.
func (c detachedCall) handOverCleanup(turn context.Context) {
	if c.cleanup != nil {
		TurnCleanupFromContext(turn).Add(fmt.Sprintf("background-call:%p", c.cleanup), c.cleanup.Run)
	}
}

// runCleanup removes what an unread call left, on a context of its own: the turn that
// made the call may be long gone.
func (c detachedCall) runCleanup() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), backgroundCleanupTimeout)
	defer cancel()
	if err := c.cleanup.Run(ctx); err != nil {
		slog.Warn("background tool call cleanup failed", "err", err)
	}
}

var (
	errBackgroundClosed    = errors.New("background calls are shut down")
	errBackgroundFull      = errors.New("this identity already has the most background calls it may have")
	errBackgroundTurnEnded = errors.New("the turn ended before the call could be moved")
)

// promote registers a call that is still running. It refuses when the registry is shut
// down or full, and when the turn's cancellation has already reached the call: that call
// is ending, not outliving anything.
func (b *BackgroundCalls) promote(turn context.Context, tool string, call detachedCall) (*backgroundCall, error) {
	id, err := newBackgroundJobID()
	if err != nil {
		return nil, err
	}
	owner := ownerFromContext(turn)
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.closed:
		return nil, errBackgroundClosed
	case b.runningFor(owner) >= maxBackgroundCallsPerOwner:
		return nil, errBackgroundFull
	case !call.follow():
		return nil, errBackgroundTurnEnded
	}
	task := &backgroundCall{
		id: id, ownerID: owner, sessionID: shellSessionKey(turn), tool: tool,
		startedAt: time.Now(), call: call, status: BackgroundCallRunning,
	}
	b.calls[id] = task
	b.running.Add(1)
	return task, nil
}

// runningFor counts owner's calls still running. The caller holds mu.
func (b *BackgroundCalls) runningFor(owner string) int {
	n := 0
	for _, task := range b.calls {
		if task.ownerID == owner && !task.settled {
			n++
		}
	}
	return n
}

// settle waits for a promoted call and records how it ended. A call nobody cancelled
// waits backgroundResultTTL to be read and wakes its conversation; a cancelled one is
// dropped with its files, and one that settles after shutdown is dropped as Stop says.
func (b *BackgroundCalls) settle(task *backgroundCall, done <-chan callOutcome) {
	defer b.running.Done()
	out := <-done
	status := outcomeStatus(task.call.ctx, out)
	task.call.cancel()
	if out.panicked != nil {
		out.err = fmt.Errorf("panic: %v", out.panicked)
	}

	b.mu.Lock()
	task.settled, task.status, task.result, task.err = true, status, out.result, out.err
	drop := task.discarded || b.closed
	clean := task.discarded && !b.closed
	if drop {
		delete(b.calls, task.id)
	} else {
		time.AfterFunc(b.resultTTL, func() { b.expire(task.id) })
	}
	hook := b.hook
	b.mu.Unlock()

	if clean {
		task.call.runCleanup()
	}
	if drop {
		return
	}
	notifyBackgroundCall(hook, BackgroundCallCompletion{
		TaskID: task.id, OwnerID: task.ownerID, SessionID: task.sessionID,
		Tool: task.tool, Status: status, Duration: time.Since(task.startedAt),
	})
}

// outcomeStatus reads how a call ended. It runs before the call's context is released,
// so a cancellation is still the cause that ended it.
func outcomeStatus(ctx context.Context, out callOutcome) string {
	switch {
	case out.err == nil && out.panicked == nil:
		return BackgroundCallCompleted
	case errors.Is(context.Cause(ctx), context.DeadlineExceeded):
		return BackgroundCallTimedOut
	case ctx.Err() != nil:
		return BackgroundCallCancelled
	default:
		return BackgroundCallFailed
	}
}

func notifyBackgroundCall(hook BackgroundCallCompletionHook, completion BackgroundCallCompletion) {
	if hook == nil {
		return
	}
	// The hook belongs to the composition root; a panic in it must not take the
	// settling goroutine, and the process, with it.
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("background tool call completion hook panicked",
				"task_id", completion.TaskID, "panic", recovered)
		}
	}()
	hook(completion)
}

// expire drops a settled call nobody read, with its files. Only a settled call arms it,
// and ids are never reused, so a call still under id is that one; one already read,
// terminated or shut down is simply gone.
func (b *BackgroundCalls) expire(id string) {
	b.mu.Lock()
	task, ok := b.calls[id]
	delete(b.calls, id)
	b.mu.Unlock()
	if ok {
		task.call.runCleanup()
	}
}

// backgroundCallState is what tool_poll learns about one call.
type backgroundCallState struct {
	tool    string
	status  string
	elapsed time.Duration
	result  ToolResult
	err     error
}

// poll reads the caller's call id. A running call is reported, or cancelled when cancel
// is set; a settled one is handed over once, its files to the caller's turn, and
// forgotten. A call of another identity or conversation reads as missing.
func (b *BackgroundCalls) poll(ctx context.Context, id string, cancel bool) (backgroundCallState, bool) {
	b.mu.Lock()
	task, ok := b.calls[id]
	if !ok || task.ownerID != ownerFromContext(ctx) || task.sessionID != shellSessionKey(ctx) {
		b.mu.Unlock()
		return backgroundCallState{}, false
	}
	state := backgroundCallState{tool: task.tool, status: task.status, elapsed: time.Since(task.startedAt)}
	if !task.settled {
		if cancel {
			task.discarded = true
			state.status = BackgroundCallCancelled
		}
		b.mu.Unlock()
		if cancel {
			task.call.cancel()
		}
		return state, true
	}
	delete(b.calls, id)
	b.mu.Unlock()
	task.call.handOverCleanup(ctx)
	state.result, state.err = task.result, task.err
	return state, true
}

// TerminateSession cancels the calls (ownerID, sessionID) still has running and drops
// the settled ones: a deleted conversation has nobody left to read them.
func (b *BackgroundCalls) TerminateSession(ownerID, sessionID string) {
	if b == nil {
		return
	}
	var running, settled []*backgroundCall
	b.mu.Lock()
	for id, task := range b.calls {
		if task.ownerID == ownerID && task.sessionID == sessionID {
			delete(b.calls, id)
			task.discarded = true
			if task.settled {
				settled = append(settled, task)
			} else {
				running = append(running, task)
			}
		}
	}
	b.mu.Unlock()
	for _, task := range running {
		task.call.cancel()
	}
	for _, task := range settled {
		task.call.runCleanup()
	}
}

// Stop refuses further promotions, cancels the running calls and waits for them or for
// ctx. Their results and any unread one are dropped: the conversation would be woken by
// a process that is going away. Files they left are removed by the next turn's stale
// sweep (mcp_files.go), the path every turn a process died in takes.
func (b *BackgroundCalls) Stop(ctx context.Context) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	b.closed = true
	var running []*backgroundCall
	for id, task := range b.calls {
		if task.settled {
			delete(b.calls, id)
		} else {
			running = append(running, task)
		}
	}
	b.mu.Unlock()
	for _, task := range running {
		task.call.cancel()
	}
	stopped := make(chan struct{})
	go func() {
		b.running.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// promotedResult is what the model reads instead of the call's result.
func promotedResult(ctx context.Context, id, tool string, window time.Duration) (ToolResult, error) {
	body, err := json.Marshal(struct {
		Status string `json:"status"`
		TaskID string `json:"task_id"`
		Tool   string `json:"tool"`
		Note   string `json:"note"`
	}{
		Status: "in_progress", TaskID: id, Tool: tool,
		Note: fmt.Sprintf("Still running after %s and NOT cancelled: it continues in the background. "+
			"Aura notifies this conversation when it finishes; then call tool_poll once with this task_id "+
			"to read its result. Do not call %s again for this; continue with other work meanwhile.", window, tool),
	})
	if err != nil {
		return ToolResult{}, err
	}
	return runtimeResult(ctx, string(body))
}

// runtimeResult is text Aura itself writes about a call -- that it moved, that it is still
// running -- marked trusted so its instructions are not discounted as a tool's content.
func runtimeResult(ctx context.Context, text string) (ToolResult, error) {
	res, err := NewResult(ctx, text)
	if err != nil {
		return ToolResult{}, err
	}
	res.Provenance = &ToolResultProvenance{Source: "aura:background", Trust: TrustTrusted}
	return res, nil
}

type callCeilingKey struct{}

// WithCallCeiling records how long the caller lets one tool call run. A tool with a
// timeout of its own (the MCP bridge) takes this one instead: the caller has already
// stopped waiting for the call long before it.
func WithCallCeiling(ctx context.Context, ceiling time.Duration) context.Context {
	return context.WithValue(ctx, callCeilingKey{}, ceiling)
}

// CallCeiling reports the ceiling WithCallCeiling recorded.
func CallCeiling(ctx context.Context) (time.Duration, bool) {
	ceiling, ok := ctx.Value(callCeilingKey{}).(time.Duration)
	return ceiling, ok
}
