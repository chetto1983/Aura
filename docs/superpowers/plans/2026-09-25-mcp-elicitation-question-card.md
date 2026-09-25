# MCP elicitation and the question card: implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A mounted MCP server that asks for input during a cockpit turn gets a form in the thread; the call waits, with its clock and the run's stopped, until the operator answers, and the answer goes back to the server. The ask_user card is redrawn in the same visual language.

**Architecture:**

- **Two new neutral packages.** `internal/pausable` is a context deadline that a hold can stop. `internal/elicit` is the question/answer vocabulary plus the `Asker` seam, so `internal/agent/mcptools` and `internal/agui` never import each other.
- **mcptools routes the request.** The handler finds the run that owns the call:
  - the call's own context on the multi-round-trip path;
  - otherwise an in-flight registry keyed by `*sdkmcp.ClientSession`.
  It asks that run's `elicit.Asker` with every clock of the call held. When no run can be asked, today's decline-and-surface consent takes the request.
- **agui carries the question.** Each `RunSession` owns a `runQuestions` asker, installed on the detached run's context:
  - it publishes `aura.elicitation` into the run's replay ring;
  - it waits for `POST /agent/runs/{runID}/elicitations/{id}`;
  - it publishes `aura.elicitation_resolved` when the question closes.
- **The cockpit renders both kinds of question in one frame.** `web/src/questions/QuestionCard`:
  - the ask_user adapter is `InlineApprovalCard`;
  - the MCP form adapter is `ElicitationCard`, one step per field.
  Both sit in the stack above the composer, as today.

**Tech Stack:** Go 1.27.1, go-sdk v1.8.0 (`sdkmcp`), google/jsonschema-go v0.4.3, goleak, `-race`; React 19, react-i18next, vitest, Stryker, shadcn registry (`@tool-ui`).

**Spec:** `D:\Aura\docs\superpowers\specs\2026-09-25-mcp-elicitation-question-card-design.md` (approved 2026-09-25). Executors read the spec and this plan together. Where they disagree, **Open points** at the end says which way this plan went and why.

> **Blocking before Task 7:** Open point 1. The Tool UI components cannot render the spec's cards unmodified: they hard-code English strings, take only option steps, and deny on Escape. Tasks 7 and 8 are written for the recommended option (port the markup into Aura files; register the `@tool-ui` registry; install nothing yet). Get the operator's decision before starting Task 7.

## Global Constraints

**Repository and commits**
- Repository: `D:\Aura`, branch `master`. Commit on `master` directly; no feature branch.
- Another session may be working in the same tree. HEAD moved while this plan was written (`914317182`).
  - Commit with explicit paths only: `git add <new files>`, then `git commit -F - -- <paths>`.
  - Unstage anything you did not write.
  - Re-read any file immediately before editing it.
- End every commit message with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`.
- No push without the operator's go. Pushing `master` publishes the edge image, and the appliances install it.

**Build and test**
- Go runs in WSL. Create `<scratchpad>/aura_go.sh`:

  ```bash
  #!/usr/bin/env bash
  # Run one command in the Aura tree from WSL, with the Go toolchain on PATH.
  set -uo pipefail
  export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
  export CGO_ENABLED=1
  cd /mnt/d/Aura || exit 1
  "$@"
  ```

- The web runs in WSL too, per the brief. Create `<scratchpad>/aura_web.sh`, identical except `cd /mnt/d/Aura/web`.
  - WSL's own node 24 is linked into `~/.local/bin` (`node`, `npm` and `npx` point to `~/.local/aura-toolchains/node24`). The script puts that directory first on PATH, and it has to: without it, `npx` resolves to the Windows install under `/mnt/c/Program Files/nodejs`, which must never run. Measured 2026-09-25.
  - `web/node_modules` carries both the linux and the win32 rolldown bindings (measured 2026-09-25).
  - Git Bash is the faster fallback if a WSL web run crawls.
- Below:
  - "Go: `X`" means `time MSYS_NO_PATHCONV=1 wsl bash /mnt/c/Users/Davide/AppData/Local/Temp/claude/D--Aura/<session>/scratchpad/aura_go.sh X`;
  - "Web: `X`" means the same with `aura_web.sh`.
  - `<session>` is your own session's scratchpad directory.
- Never run a Windows `.exe`. Edit with the Edit tool, never with a script.
- Per task, run the touched packages' tests with `-race -count=1` and `go vet` on them. The full gates run once, in Task 9.
- Mutation testing (go-mutesting, Stryker) runs in CI only. Never mutate locally, by tool or by hand. "The test can fail" is shown by each task's RED run.
- oxlint exits 0 even when it finds errors: read its `Found N errors` line.
- **File-size limit: 600 lines per file, tests included.** Measured on 2026-09-25:

  | File | Lines | This plan adds |
  |---|---|---|
  | `web/src/chat/ExternalStoreChat.tsx` | 587 | 7 (Task 8) |
  | `internal/agent/budget_test.go` | 589 | nothing (new tests go in `budget_pause_test.go`) |
  | `web/src/i18n/resources.ts` | 580 | 4 (Task 7) |
  | `web/src/chat/sseAdapter.ts` | 554 | 9 (Task 8) |
  | `internal/agui/server.go` | 539 | 3 (Task 6) |
  | `internal/agent/mcptools/bridge_supervisor.go` | 500 | about 8 (Task 4) |

  - Vendored Tool UI files, if Open point 1 goes that way, are exempt the same way `model-selector.tsx` already is (Task 7, option P).
  - Measure with `wc -l` after each edit. Split before a file passes 600; never after.

**Values from the spec** (verbatim)
- Caps: 20 fields, 50 enum options, a 2 KiB message, 256 B titles, a 1 KiB description, 4 KiB per string answer.
- Kinds: `string`, `number`, `integer`, `boolean`, `enum`. Formats: `email`, `uri`, `date`, `date-time`.
- Events: `aura.elicitation` carries id, server, tool, message, fields and deadline. `aura.elicitation_resolved` carries `{id, action}`. Neither carries an answer value.
- Route: `POST /agent/runs/{runID}/elicitations/{id}` with `{action, content}`:

  | Code | When |
  |---|---|
  | 410 | the run is terminal |
  | 404 | the question id is unknown, or the run is not the caller's |
  | 409 | the question is already resolved or expired |
  | 422 | the content fails `elicit.Validate`; the question stays open |
  | 202 | the answer was delivered |

- Bounds:
  - The wait ends at the first of: `AURA_MCP_ELICITATION_TIMEOUT_SEC` (default 300 s; `<= 0` disables, and a timeout is a decline), the end of the call (cancel), or the end of the run (cancel).
  - Only the operator's time is excluded from the clocks. The server's and the model's time still count.
- URL mode stays refused, before any asker is consulted. The handler returns `(*ElicitResult, nil)` in every case.
- Only the action, the server and the field count are recorded; the operator's values never are.
- No new environment variables, no migration. The caps are constants in `internal/elicit`, with their reason next to them.

## Review Focus

1. **A form that takes the operator longer than the call bound and the run bound.**
   - The call bound is 60 s and the run bound is 300 s.
   - Both clocks must stop, and so must the per-tool node timeout when it is set.
   - Pinned in:
     - Task 2: `TestBudgetWallclockSkipsHeldTime`, `TestRunToolNodeTimeoutStopsWhileHeld`;
     - Task 4: `TestAHeldCallOutlivesItsTimeout`;
     - Task 6: `TestDetachedRunAnswersAnMCPFormWhileBothClocksStop`, on 1 s and 2 s bounds with a 3 s operator.
2. **The elicitation timeout silently disappearing.**
   - A held call context reports a deadline earlier than the question's. `context.WithTimeout` trusts the earlier deadline and arms no timer, so the 300 s bound would never fire.
   - Pinned in Task 4: `TestAnUnansweredQuestionExpiresWhileTheCallIsHeld`, where the call bound is shorter than the question's.
3. **Two conversations using one MCP server at the same time.**
   - A classic request with calls from two runs in flight must ask neither operator.
   - Both must be told why, and the server must get a decline.
   - Pinned in Task 4: `TestClassicElicitationWithTwoRunsInFlightAsksNeither`.
4. **A reload in the middle of a form.**
   - The question comes back once, not twice.
   - Its resolution still applies, and after the run ends a replay still carries both frames.
   - Pinned in:
     - Task 6: the replay half of the integration test;
     - Task 8: `applyElicitationSignal` ignores a replayed question.
5. **An answer the server's schema refuses.**
   - The question stays open.
   - The card goes back to the failing field's step with the error there.
   - A corrected answer is then delivered.
   - Pinned in:
     - Task 6: `TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen`;
     - Task 8: `ElicitationCard` "a 422 returns to the failing step".

## File map

| File | Task | Responsibility |
|---|---|---|
| `internal/pausable/clock.go` | 1 | `Clock`: held-time accounting, nested holds |
| `internal/pausable/context.go` | 1 | the pausable deadline context, `WithDeadline`, `WithTimeout`, `Hold` |
| `internal/agent/budget.go` | 2 | the run wallclock reads the held total |
| `internal/agent/llm_agent_tool.go` | 2 | the node timeout becomes pausable |
| `internal/elicit/elicit.go` | 3 | `Question`, `Field`, `Answer`, `Asker`, context seam, refusal codes |
| `internal/elicit/schema.go` | 3 | caps, `DecodeSchema`, `FromSchema` |
| `internal/elicit/validate.go` | 3 | `FieldErrors`, `Validate` |
| `internal/agent/mcptools/bridge_inflight.go` | 4 | calls open per session, the call-context marker |
| `internal/agent/mcptools/elicitation.go` | 4 | handler, fallback consent, configuration |
| `internal/agent/mcptools/elicitation_route.go` | 4 | routing, the held wait, refusals |
| `cmd/aura/elicitation_consent.go` | 4 | the fallback reads `elicit.Question` |
| `cmd/aura/main.go`, `mcp_tools.go`, `runtime_tool_handles.go` | 5 | every mount gets the fallback consent |
| `internal/agui/run_elicitation.go` | 6 | `runQuestions`, the run's asker |
| `internal/agui/server_run_elicitation.go` | 6 | the answer route |
| `internal/agui/runsession.go`, `server_run_detach.go`, `server.go`, `idempotency_http.go` | 6 | publish, installation, route mount, idempotency inventory |
| `web/components.json` | 7 | the `@tool-ui` registry, for spec 2 |
| `web/src/questions/QuestionCard.tsx`, `QuestionOptions.tsx`, `QuestionReceipt.tsx`, `CancelControl.tsx` | 7 | the shared frame, the rows, the receipts, the cancel confirmation |
| `web/src/approvals/InlineApprovalCard.tsx`, `approvalState.ts` | 7 | the ask_user adapter |
| `web/src/i18n/resources.questions.ts` | 7, 8 | the copy, in en and it |
| `web/src/chat/sseAdapter_elicitation.ts`, and the `onElicitation` plumbing in `sseAdapter.ts`, `sseResume.ts`, `ExternalStoreChat*.ts(x)` | 8 | frame parsing, from the pump |
| `web/src/questions/useThreadElicitations.ts`, `elicitationAnswer.ts`, `elicitationSteps.ts` | 8 | the thread's forms, the answer POST, the step logic |
| `web/src/questions/FieldInput.tsx`, `ElicitationHeader.tsx`, `useCountdown.ts`, `ElicitationCard.tsx` | 8 | the MCP form adapter |

---

### Task 1: `internal/pausable`

**Files:**
- Create: `internal/pausable/clock.go`, `internal/pausable/context.go`
- Create: `internal/pausable/pausable_test.go`, `internal/pausable/main_test.go`
- Modify: `scripts/coverage_package_policy.json`, adding one entry between `internal/packs` and `internal/pgnumeric`.

**Interfaces:**
- Produces:
  - `pausable.Clock`, with `pausable.NewClock(now func() time.Time) *Clock` (a nil `now` means `time.Now`) and `(*Clock).Held() time.Duration` (nil-safe; 0 on a nil clock);
  - `pausable.WithDeadline(parent context.Context, deadline time.Time, clock *Clock) (context.Context, context.CancelFunc)`. A nil clock gets a clock of its own. The context ends at `deadline + clock.Held()`, reports that deadline (or the parent's, if earlier) from `Deadline()`, and reports `context.DeadlineExceeded` to itself and to every child when it expires;
  - `pausable.WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc)`;
  - `pausable.Hold(ctx context.Context) (release func())`. It holds every distinct clock above ctx. Holds nest, release is idempotent, and on a context with no pausable deadline it is a no-op.

**Why not a wrapper over `context.WithDeadline`:**
- A standard deadline cannot move.
- A context cancelled through a `CancelFunc` reports `context.Canceled` to its children. Code that classifies a timeout reads `DeadlineExceeded` from derived contexts: the obs boundary, the node-timeout test and `bridge_call`'s tests.
- So `deadlineCtx` implements `context.Context` itself, plus the `AfterFunc(func()) func() bool` method. `context.propagateCancel` then attaches standard children through that method, without a goroutine per child, and hands them this context's own error.

- [ ] **Step 1: Write the failing tests.** Create `internal/pausable/main_test.go`:

```go
package pausable

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
```

Create `internal/pausable/pausable_test.go`:

```go
package pausable

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeNow is a manual time source: the tests move it, never the wall clock, so
// every assertion about held time is exact.
type fakeNow struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeNow() *fakeNow {
	return &fakeNow{t: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
}

func (f *fakeNow) now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.t
}

func (f *fakeNow) advance(d time.Duration) {
	f.mu.Lock()
	f.t = f.t.Add(d)
	f.mu.Unlock()
}

func deadlineOf(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("a pausable context must report a deadline")
	}
	return deadline
}

type plainKey struct{}

func TestWithTimeoutExpiresLikeAStandardDeadline(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	child, cancelChild := context.WithCancel(ctx)
	defer cancelChild()

	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the deadline never fired")
	}
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", ctx.Err())
	}
	if !errors.Is(child.Err(), context.DeadlineExceeded) {
		t.Fatalf("child err = %v, want DeadlineExceeded: timeout classification reads derived contexts", child.Err())
	}
}

func TestAHeldDeadlineDoesNotRunDown(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	start := clock.now()
	ctx, cancel := WithDeadline(context.Background(), start.Add(10*time.Second), c)
	defer cancel()

	release := Hold(ctx)
	clock.advance(30 * time.Second)
	if got := deadlineOf(t, ctx); !got.Equal(start.Add(40 * time.Second)) {
		t.Fatalf("deadline during a 30s hold = start+%v, want start+40s", got.Sub(start))
	}
	if err := ctx.Err(); err != nil {
		t.Fatalf("a held context ended: %v", err)
	}
	release()
	if got := c.Held(); got != 30*time.Second {
		t.Fatalf("held = %v, want 30s", got)
	}
	if got := deadlineOf(t, ctx); !got.Equal(start.Add(40 * time.Second)) {
		t.Fatalf("deadline after release = start+%v, want start+40s", got.Sub(start))
	}
}

func TestAReleasedClockPastItsDeadlineExpires(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Second), c)
	defer cancel()

	release := Hold(ctx)
	clock.advance(time.Minute)
	release()
	if err := ctx.Err(); err != nil {
		t.Fatalf("expired although the whole minute was held: %v", err)
	}
	clock.advance(2 * time.Second)
	// A zero-length hold re-arms on release, which is where an expiry is decided.
	Hold(ctx)()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("err = %v one second past the pushed-back deadline, want DeadlineExceeded", ctx.Err())
	}
}

func TestNestedHoldsRunAgainOnlyAfterTheLast(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Hour), c)
	defer cancel()

	outer := Hold(ctx)
	inner := Hold(ctx)
	clock.advance(5 * time.Second)
	inner()
	clock.advance(5 * time.Second)
	if got := c.Held(); got != 10*time.Second {
		t.Fatalf("held = %v while the outer hold is open, want 10s", got)
	}
	outer()
	outer()
	clock.advance(5 * time.Second)
	if got := c.Held(); got != 10*time.Second {
		t.Fatalf("held = %v after the last release, want 10s", got)
	}
}

func TestADeadlineIsNeverShortened(t *testing.T) {
	clock := newFakeNow()
	c := NewClock(clock.now)
	ctx, cancel := WithDeadline(context.Background(), clock.now().Add(time.Hour), c)
	defer cancel()

	last := deadlineOf(t, ctx)
	var open []func()
	for i := range 50 {
		release := Hold(ctx)
		clock.advance(time.Duration(i%7) * time.Second)
		if i%2 == 0 {
			release()
		} else {
			open = append(open, release)
		}
		clock.advance(time.Second)
		now := deadlineOf(t, ctx)
		if now.Before(last) {
			t.Fatalf("step %d: deadline moved back from %v to %v", i, last, now)
		}
		last = now
	}
	for _, release := range open {
		release()
	}
}

func TestHoldReachesEveryPausableAncestor(t *testing.T) {
	clock := newFakeNow()
	run := NewClock(clock.now)
	call := NewClock(clock.now)
	runCtx, cancelRun := WithDeadline(context.Background(), clock.now().Add(time.Hour), run)
	defer cancelRun()
	between := context.WithValue(runCtx, plainKey{}, "a plain context between the two")
	callCtx, cancelCall := WithDeadline(between, clock.now().Add(time.Minute), call)
	defer cancelCall()

	release := Hold(callCtx)
	clock.advance(20 * time.Second)
	release()
	if run.Held() != 20*time.Second || call.Held() != 20*time.Second {
		t.Fatalf("held run=%v call=%v, want 20s each", run.Held(), call.Held())
	}
}

func TestHoldWithoutAPausableDeadlineIsANoOp(t *testing.T) {
	release := Hold(context.Background())
	release()
	release()
	var none *Clock
	if none.Held() != 0 {
		t.Fatal("a nil clock was never held")
	}
}

func TestTheEarlierParentDeadlineWins(t *testing.T) {
	parent, cancelParent := context.WithTimeout(context.Background(), time.Minute)
	defer cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()

	want, _ := parent.Deadline()
	if got := deadlineOf(t, ctx); !got.Equal(want) {
		t.Fatalf("deadline = %v, want the parent's %v", got, want)
	}
}

func TestParentCancellationReachesAPausableChild(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()

	cancelParent()
	select {
	case <-ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the parent's cancellation never reached the child")
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("err = %v, want Canceled", ctx.Err())
	}
	if ctx.Value(plainKey{}) != nil {
		t.Fatal("an unknown key must fall through to the parent, which does not hold it")
	}
}

func TestADoneParentEndsTheChildAtOnce(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()
	ctx, cancel := WithTimeout(parent, time.Hour)
	defer cancel()
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("err = %v right after WithTimeout on a done parent, want Canceled", ctx.Err())
	}
}

func TestCancelEndsTheContextAndItsChildren(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Hour)
	child, cancelChild := context.WithCancel(ctx)
	defer cancelChild()

	cancel()
	cancel()
	<-ctx.Done()
	select {
	case <-child.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the child was never cancelled")
	}
	if !errors.Is(child.Err(), context.Canceled) {
		t.Fatalf("child err = %v, want Canceled", child.Err())
	}
}

func TestAfterFuncStopsBeforeTheEndAndRunsAfterIt(t *testing.T) {
	ctx, cancel := WithTimeout(context.Background(), time.Hour)
	pc := ctx.(*deadlineCtx)

	stopped := pc.AfterFunc(func() { t.Error("a stopped AfterFunc ran") })
	if !stopped() {
		t.Fatal("stop before the end must report that it prevented the call")
	}
	ran := make(chan struct{})
	pc.AfterFunc(func() { close(ran) })
	cancel()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the AfterFunc never ran")
	}
	late := make(chan struct{})
	lateStop := pc.AfterFunc(func() { close(late) })
	<-late
	if lateStop() {
		t.Fatal("stop after the end must report false")
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 ./internal/pausable/`.

Expected: build FAIL with `undefined: WithTimeout`, and likewise `NewClock`, `WithDeadline`, `Hold`, `deadlineCtx`.

- [ ] **Step 3: Implement.** Create `internal/pausable/clock.go`:

```go
// Package pausable provides context deadlines that stop while a human answers.
// A held deadline does not run down, and releasing the hold gives back the time
// it lasted. It exists for MCP elicitation: a server's form waits on the operator
// inside a tool call bounded by the call timeout and by the run's wallclock, and
// neither may count the operator's time.
package pausable

import (
	"sync"
	"time"
)

// Clock accounts for the time a set of deadlines spent held. Every context built
// over one Clock moves with it, and a run's Budget reads the same total, so the
// step gate and the context bounding the run cannot disagree.
type Clock struct {
	now func() time.Time

	mu       sync.Mutex
	holds    int
	heldFrom time.Time
	held     time.Duration
	waiting  map[*deadlineCtx]struct{}
}

// NewClock returns a clock that is not held. now is its time source; nil means
// time.Now.
func NewClock(now func() time.Time) *Clock {
	if now == nil {
		now = time.Now
	}
	return &Clock{now: now, waiting: map[*deadlineCtx]struct{}{}}
}

// Held reports how long the clock has been held in total, an open hold included.
// A nil Clock was never held.
func (c *Clock) Held() time.Duration {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.holds == 0 {
		return c.held
	}
	return c.held + c.now().Sub(c.heldFrom)
}

func (c *Clock) isHeld() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.holds > 0
}

// hold stops the clock until release runs. Holds nest: the clock runs again when
// the last one is released. release is idempotent.
func (c *Clock) hold() (release func()) {
	c.mu.Lock()
	if c.holds == 0 {
		c.heldFrom = c.now()
	}
	c.holds++
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(c.release) }
}

// release ends one hold. When it is the last, the held time is banked and every
// context over this clock is re-armed for its pushed-back deadline.
func (c *Clock) release() {
	c.mu.Lock()
	c.holds--
	if c.holds > 0 {
		c.mu.Unlock()
		return
	}
	c.held += c.now().Sub(c.heldFrom)
	waiting := make([]*deadlineCtx, 0, len(c.waiting))
	for ctx := range c.waiting {
		waiting = append(waiting, ctx)
	}
	c.mu.Unlock()
	for _, ctx := range waiting {
		ctx.arm()
	}
}

func (c *Clock) watch(ctx *deadlineCtx) {
	c.mu.Lock()
	c.waiting[ctx] = struct{}{}
	c.mu.Unlock()
}

func (c *Clock) forget(ctx *deadlineCtx) {
	c.mu.Lock()
	delete(c.waiting, ctx)
	c.mu.Unlock()
}
```

Create `internal/pausable/context.go`:

```go
package pausable

import (
	"context"
	"sync"
	"time"
)

type ctxKey struct{}

// deadlineCtx ends at its deadline plus the time its clock has been held. It is
// its own context type because the standard library offers neither half: a
// standard deadline cannot move, and a context cancelled through a CancelFunc
// reports context.Canceled to every child, where a passed deadline must report
// context.DeadlineExceeded.
type deadlineCtx struct {
	parent   context.Context
	clock    *Clock
	deadline time.Time
	up       *deadlineCtx

	mu     sync.Mutex
	done   chan struct{}
	err    error
	timer  *time.Timer
	detach func() bool
	afters map[*afterFunc]struct{}
}

type afterFunc struct{ f func() }

// WithDeadline returns a copy of parent that ends at deadline plus the time clock
// has been held. Several contexts may share one clock; a nil clock gets its own.
func WithDeadline(parent context.Context, deadline time.Time, clock *Clock) (context.Context, context.CancelFunc) {
	if clock == nil {
		clock = NewClock(nil)
	}
	up, _ := parent.Value(ctxKey{}).(*deadlineCtx)
	c := &deadlineCtx{
		parent:   parent,
		clock:    clock,
		deadline: deadline,
		up:       up,
		done:     make(chan struct{}),
		afters:   map[*afterFunc]struct{}{},
	}
	clock.watch(c)
	detach := context.AfterFunc(parent, func() { c.cancel(parent.Err()) })
	c.mu.Lock()
	c.detach = detach
	c.mu.Unlock()
	if err := parent.Err(); err != nil {
		c.cancel(err)
	} else {
		c.arm()
	}
	return c, func() { c.cancel(context.Canceled) }
}

// WithTimeout is WithDeadline over a fresh clock, d from now.
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	clock := NewClock(nil)
	return WithDeadline(parent, clock.now().Add(d), clock)
}

// Hold stops every pausable deadline above ctx until release runs; each gets back
// the time the hold lasted. Deadlines that share a clock are held once. release
// is idempotent, and a ctx with no pausable deadline returns a no-op.
func Hold(ctx context.Context) (release func()) {
	var releases []func()
	seen := map[*Clock]bool{}
	for c, _ := ctx.Value(ctxKey{}).(*deadlineCtx); c != nil; c = c.up {
		if !seen[c.clock] {
			seen[c.clock] = true
			releases = append(releases, c.clock.hold())
		}
	}
	return func() {
		for _, r := range releases {
			r()
		}
	}
}

func (c *deadlineCtx) ownDeadline() time.Time {
	return c.deadline.Add(c.clock.Held())
}

// arm schedules the expiry for the current deadline, or expires now. A held clock
// leaves the context waiting: releasing the hold arms it again. The timer calls
// arm too, so a deadline pushed back while the timer ran is re-checked, not trusted.
func (c *deadlineCtx) arm() {
	if c.clock.isHeld() {
		return
	}
	remaining := c.ownDeadline().Sub(c.clock.now())
	if remaining <= 0 {
		c.cancel(context.DeadlineExceeded)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(remaining, c.arm)
}

func (c *deadlineCtx) cancel(err error) {
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return
	}
	c.err = err
	close(c.done)
	if c.timer != nil {
		c.timer.Stop()
	}
	detach, afters := c.detach, c.afters
	c.afters = nil
	c.mu.Unlock()
	if detach != nil {
		detach()
	}
	c.clock.forget(c)
	for a := range afters {
		go a.f()
	}
}

// Deadline reports the deadline as it stands now: pushed back by every hold so
// far, an open one included, and never later than the parent's.
func (c *deadlineCtx) Deadline() (time.Time, bool) {
	own := c.ownDeadline()
	if parent, ok := c.parent.Deadline(); ok && parent.Before(own) {
		return parent, true
	}
	return own, true
}

func (c *deadlineCtx) Done() <-chan struct{} { return c.done }

func (c *deadlineCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *deadlineCtx) Value(key any) any {
	if key == (ctxKey{}) {
		return c
	}
	return c.parent.Value(key)
}

// AfterFunc lets the standard library attach a child without a goroutine per child
// (context.propagateCancel prefers a parent's AfterFunc method) and hands that
// child this context's own error, DeadlineExceeded included. The already-ended
// branch is reached when the context ends between the library's Done check and
// this call.
func (c *deadlineCtx) AfterFunc(f func()) (stop func() bool) {
	a := &afterFunc{f: f}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		go f()
		return func() bool { return false }
	}
	c.afters[a] = struct{}{}
	c.mu.Unlock()
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		_, pending := c.afters[a]
		delete(c.afters, a)
		return pending
	}
}
```

Add the coverage entry to `scripts/coverage_package_policy.json`, between the `internal/packs` and `internal/pgnumeric` lines:

```json
    "github.com/chetto1983/aura/internal/pausable": {"mode": "target"},
```

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/pausable/`, then `go test -race -count=1 -cover ./internal/pausable/`.

Expected: `ok  github.com/chetto1983/aura/internal/pausable  coverage: 9x.x% of statements`. It must be at least 85%. goleak stays green: every test cancels its contexts, and a cancelled context stops its timer.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add internal/pausable/clock.go internal/pausable/context.go internal/pausable/pausable_test.go internal/pausable/main_test.go
git commit -F - -- internal/pausable/clock.go internal/pausable/context.go internal/pausable/pausable_test.go internal/pausable/main_test.go scripts/coverage_package_policy.json <<'EOF'
feat(pausable): context deadlines that stop while the operator answers

An MCP server's form waits on the operator inside a tool call that two
clocks bound: the 60 s call timeout and the run's 300 s wallclock. A
form that takes a minute to fill in would kill the call it belongs to.

pausable.WithDeadline builds a context whose deadline moves by the time
its Clock was held, and Hold holds every such deadline above a context.
It is its own context type: a standard deadline cannot move, and a
cancelled context reports Canceled to its children where an expired one
must report DeadlineExceeded. Children attach through an AfterFunc
method, with no goroutine per child.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 2: The run's clocks honour a hold

**Files:**
- Modify: `internal/agent/budget.go`, in five places:
  - the struct at 49-64;
  - `NewBudget` at 185-195;
  - `ConsumeStep` at 250-252;
  - `Child` at 342-356;
  - `WithDeadline` at 369-374.
- Modify: `internal/agent/llm_agent_tool.go:195-199`, the node timeout.
- Create: `internal/agent/budget_pause_test.go` (package `agent`), `internal/agent/llm_agent_node_hold_test.go` (package `agent_test`).

**Interfaces:**
- Consumes: `pausable.NewClock`, `pausable.WithDeadline`, `pausable.WithTimeout`, `pausable.Hold`, `(*pausable.Clock).Held` (Task 1).
- Produces:
  - `Budget.WithDeadline(parent) (context.Context, context.CancelFunc)`, same signature, now pausable;
  - `ConsumeStep`'s wallclock gate reads `deadlineWallclock + clock.Held()`, and the clock pointer is shared by `Child`.
  - The runner call site at `internal/runner/runner.go:393` is unchanged.

**The agui detached cap stays fixed.** `detachedRunContext` (`internal/agui/server_run_detach.go:40-42`) bounds a detached run at `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC`, 3600 s by default. That is twelve times the 300 s budget it backs up.
- Kept fixed, it is the one bound on a server that asks again and again: each wait is held, so only a fixed clock ends that loop.
- It cuts a legitimate run only when the operator spends more than about 55 minutes answering forms in one turn.
- Task 6 records this in the function's comment.

**The node timeout is the third clock.** `AURA_LOOP_NODE_TIMEOUT_SEC` wraps every tool call in `context.WithTimeout` when set (default off). A fixed timer there would cut every held wait. See Open point 5.

- [ ] **Step 1: Write the failing tests.** Create `internal/agent/budget_pause_test.go`:

```go
package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/pausable"
)

// manualClock drives a Budget's injected clock so held time is exact.
type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func pausableBudget(t *testing.T, wallclockSec int) (*Budget, *manualClock) {
	t.Helper()
	clock := &manualClock{now: time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)}
	b, err := NewBudget(BudgetOptions{MaxWallclockSec: &wallclockSec, Now: clock.Now})
	if err != nil {
		t.Fatalf("NewBudget: %v", err)
	}
	return b, clock
}

func TestBudgetWallclockSkipsHeldTime(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	release := pausable.Hold(ctx)
	clock.advance(10 * time.Minute)
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("step refused (%s) while the operator holds the clock", reason)
	}
	release()
	clock.advance(30 * time.Second)
	if ok, reason := b.ConsumeStep(); !ok {
		t.Fatalf("step refused (%s) after 30s of run time and a 10-minute hold, with a 60s wallclock", reason)
	}
	clock.advance(31 * time.Second)
	if ok, reason := b.ConsumeStep(); ok || reason != "wallclock" {
		t.Fatalf("ConsumeStep = %v %q after 61s of run time, want the wallclock refusal", ok, reason)
	}
}

func TestChildBudgetSeesTheParentsHeldTime(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	child := b.Child(2)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	release := pausable.Hold(ctx)
	clock.advance(5 * time.Minute)
	release()
	clock.advance(30 * time.Second)
	if ok, reason := child.ConsumeStep(); !ok {
		t.Fatalf("a sub-agent's budget refused a step (%s): it must share the parent's held time", reason)
	}
}

func TestBudgetDeadlineContextMovesWithAHold(t *testing.T) {
	b, clock := pausableBudget(t, 60)
	ctx, cancel := b.WithDeadline(context.Background())
	defer cancel()

	before, _ := ctx.Deadline()
	release := pausable.Hold(ctx)
	clock.advance(2 * time.Minute)
	release()
	after, _ := ctx.Deadline()
	if got := after.Sub(before); got != 2*time.Minute {
		t.Fatalf("the run context's deadline moved by %v, want the 2m hold", got)
	}
}
```

Create `internal/agent/llm_agent_node_hold_test.go`:

```go
package agent_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/pausable"
)

// heldTool holds its context's clocks for longer than the node timeout, the way
// an MCP call does while the operator fills in a form.
type heldTool struct{ hold time.Duration }

func (heldTool) Spec() tools.Spec {
	return tools.Spec{Name: "held", Summary: "holds its clocks", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (h heldTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	release := pausable.Hold(ctx)
	time.Sleep(h.hold)
	release()
	if err := ctx.Err(); err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Preview: "answered"}, nil
}

func TestRunToolNodeTimeoutStopsWhileHeld(t *testing.T) {
	recordingProvider(t)
	t.Setenv("AURA_LOOP_NODE_TIMEOUT_SEC", "1")

	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(heldTool{hold: 1500 * time.Millisecond})
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("c1", "held", `{}`)),
		agenttest.ToolCallTurn(textResponseCall("c2", "done")),
	)
	a := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     fc,
		LLM:        llm.Config{Model: "m", Provider: "p", TotalTimeoutSec: 30},
		Registry:   reg,
		PreviewCap: 2048,
		RunDir:     t.TempDir(),
		SessionID:  uuid.Must(uuid.NewV7()).String(),
		UserTurns:  []llm.Message{{Role: llm.RoleUser, Content: "go"}},
	})

	evs, err := collect(a.Run(newIC(t, agent.BudgetOptions{MaxSteps: new(5)})))
	if err != nil {
		t.Fatalf("run errored: %v", err)
	}
	for _, ev := range evs {
		if ti := ev.Actions.ToolInvocation; ti != nil && ti.Event == agent.ToolInvocationEnd && ti.Error != "" {
			t.Fatalf("the 1s node timeout cut a tool that held its clock for 1.5s: %s", ti.Error)
		}
	}
}
```

`recordingProvider`, `collect`, `newIC` and `textResponseCall` are the package's existing test helpers, used the same way by `TestRunToolAppliesNodeTimeout` (`llm_agent_parallel_test.go:246`).

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 -run 'SkipsHeldTime|ParentsHeldTime|MovesWithAHold|StopsWhileHeld' ./internal/agent/`.

Expected FAIL:
- `TestBudgetWallclockSkipsHeldTime`: `step refused (wallclock) while the operator holds the clock`;
- `TestBudgetDeadlineContextMovesWithAHold`: `moved by 0s`;
- `TestRunToolNodeTimeoutStopsWhileHeld`: `context deadline exceeded`.

- [ ] **Step 3: Implement.** In `internal/agent/budget.go`, add `"github.com/chetto1983/aura/internal/pausable"` to the imports.

Replace the doc comment and the first three fields of `Budget` (49-55) with:

```go
// Budget bounds one agent run. The steps counter is shared by pointer across the
// whole tree (D-10), and so is clock; deadlineWallclock and now are shared by
// value; the dedup ring is per-branch (forked by Child, D-09).
type Budget struct {
	steps             *atomic.Int32 // shared step counter (D-10), decrement-then-check-then-restore (D-11)
	deadlineWallclock time.Time     // hard wallclock cap; ConsumeStep refuses new steps past it (D-13)
	// clock banks the time an operator spends answering an MCP elicitation. The
	// wallclock gate and the context WithDeadline builds both push the deadline
	// back by it, so a held run is refused by neither.
	clock *pausable.Clock
	now   func() time.Time // injectable clock (W8): tests drive the deadline deterministically; default time.Now
```

The remaining fields (`dedupWindow` onward) are unchanged.

In `NewBudget`, add one line to the literal after `now: now,`:

```go
		clock:             pausable.NewClock(now),
```

In `ConsumeStep`, replace the wallclock check:

```go
	if b.now().After(b.deadlineWallclock.Add(b.clock.Held())) {
		return false, "wallclock"
	}
```

In `Child`, add after `now: b.now,`:

```go
		clock:             b.clock, // SHARED pointer: a hold anywhere in the tree stops every branch's wallclock
```

Replace `WithDeadline`:

```go
// WithDeadline derives a context bounded by the budget's wallclock deadline so
// in-flight LLM/tool calls are cancelled end-to-end, not just blocked from new
// steps (D-13). The deadline is pausable and shares the budget's clock: while an
// MCP elicitation waits on the operator it stops, and ConsumeStep reads the same
// held total. The caller owns the returned CancelFunc.
func (b *Budget) WithDeadline(parent context.Context) (context.Context, context.CancelFunc) {
	return pausable.WithDeadline(parent, b.deadlineWallclock, b.clock)
}
```

In `internal/agent/llm_agent_tool.go:195-199`, replace the node-timeout block:

```go
	if d := budget.NodeTimeout(); d > 0 {
		// Pausable like the run's own deadline: an MCP call waiting on the operator
		// holds every clock above it, and a fixed per-node timer would still cut it.
		var cancel context.CancelFunc
		toolCtx, cancel = pausable.WithTimeout(toolCtx, d)
		defer cancel()
	}
```

Add `"github.com/chetto1983/aura/internal/pausable"` to its imports. Keep `"context"`: `context.CancelFunc` still names the type.

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/agent/`, then `go test -race -count=1 ./internal/agent/ ./internal/runner/`.

Expected: `ok` for both. These existing tests must stay green unchanged:
- `TestBudget_WithDeadline_PropagatesCancellation` (`budget_test.go:515`): with nothing held, the deadline is still exactly the budget deadline;
- `TestRunToolAppliesNodeTimeout`: still `context deadline exceeded`;
- `internal/runner`'s `runner_budget_test.go:33`: the turn context still carries a ~7 s deadline.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add internal/agent/budget_pause_test.go internal/agent/llm_agent_node_hold_test.go
git commit -F - -- internal/agent/budget.go internal/agent/llm_agent_tool.go internal/agent/budget_pause_test.go internal/agent/llm_agent_node_hold_test.go <<'EOF'
feat(agent): the run's wallclock stops while an operator answers

The run is bounded twice, by Budget.WithDeadline's context and by
ConsumeStep's wallclock gate, and an MCP form that takes a minute would
have tripped both. Both now read one pausable.Clock that Child shares by
pointer, so a hold anywhere in the tree stops every branch.

The per-node tool timeout (AURA_LOOP_NODE_TIMEOUT_SEC, off by default)
becomes pausable too. Set, a fixed timer there would cut every held
wait. The detached run's one-hour outer cap stays fixed on purpose: it
is the one bound on a server that asks again and again.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 3: `internal/elicit`

**Files:**
- Create: `internal/elicit/elicit.go`, `internal/elicit/schema.go`, `internal/elicit/validate.go`
- Create: `internal/elicit/elicit_test.go`, `internal/elicit/schema_test.go`, `internal/elicit/validate_test.go`, `internal/elicit/main_test.go`
- Modify: `scripts/coverage_package_policy.json`, adding one entry between `internal/documents/filecard` and `internal/embeddings`.

**Interfaces:**
- Produces:
  - `type Kind string`, with `KindString`, `KindNumber`, `KindInteger`, `KindBoolean`, `KindEnum`;
  - the action constants `ActionAccept = "accept"`, `ActionDecline = "decline"`, `ActionCancel = "cancel"`;
  - the refusal codes `RefusalUnrenderable = "unrenderable"` and `RefusalAmbiguousRun = "ambiguous_run"`;
  - `Field{Name, Title, Description string; Kind Kind; Required bool; Default any; Enum, EnumTitles []string; Multi bool; Format string; Min, Max *float64; MinLength, MaxLength *int}`. The JSON keys are snake_case: `name`, `title`, `description`, `kind`, `required`, `default`, `enum`, `enum_titles`, `multi`, `format`, `min`, `max`, `min_length`, `max_length`;
  - `Question{ID, Server, Tool, Message string; Fields []Field; Deadline time.Time; Refusal string; Schema *jsonschema.Schema}`. The JSON keys are `id`, `server`, `tool`, `message`, `fields`, `deadline` and `refusal`; `Schema` is `json:"-"`;
  - `Answer{Action string; Content map[string]any}`;
  - `type Asker interface { Ask(ctx context.Context, q Question) (Answer, error) }`, with `WithAsker(ctx, Asker) context.Context` and `AskerFrom(ctx) Asker` (nil when none);
  - `ErrExpired`;
  - the caps `MaxFields = 20`, `MaxEnumOptions = 50`, `MaxMessageBytes = 2 << 10`, `MaxTitleBytes = 256`, `MaxDescriptionBytes = 1 << 10`, `MaxAnswerBytes = 4 << 10`;
  - `DecodeSchema(raw any) (*jsonschema.Schema, error)`;
  - `FromSchema(server, tool, message string, schema *jsonschema.Schema) (Question, error)`;
  - `type FieldErrors map[string]string` (implements `error`), with `ErrRequired = "required"`;
  - `Validate(schema *jsonschema.Schema, content map[string]any) FieldErrors` (nil when valid).
- The `Asker` contract: `Ask` returns `(answer, nil)` when answered. When ctx ends first it returns `(Answer{}, context.Cause(ctx))`: `ErrExpired` for a deadline, anything else for the call or the run ending. A question with `Refusal` set is shown already resolved, and `Ask` returns a decline at once.

- [ ] **Step 1: Write the failing tests.** Create `internal/elicit/main_test.go` with the same goleak `TestMain` as Task 1, in package `elicit`.

Create `internal/elicit/elicit_test.go`:

```go
package elicit

import (
	"context"
	"testing"
)

type nopAsker struct{}

func (nopAsker) Ask(context.Context, Question) (Answer, error) { return Answer{Action: ActionDecline}, nil }

func TestAskerRidesTheContext(t *testing.T) {
	t.Parallel()
	if AskerFrom(context.Background()) != nil {
		t.Fatal("a bare context carries no asker")
	}
	var asker Asker = nopAsker{}
	if got := AskerFrom(WithAsker(context.Background(), asker)); got != asker {
		t.Fatalf("AskerFrom = %v, want the installed asker", got)
	}
}
```

Create `internal/elicit/schema_test.go`. The fixture is `@modelcontextprotocol/server-everything`'s `trigger-elicitation-request` form, one field of every shape it sends:

```go
package elicit

import (
	"strings"
	"testing"
)

// everythingForm mirrors the reference server's trigger-elicitation-request:
// one field of every restricted shape, as the SDK hands it to a client (a map).
func everythingForm() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":     map[string]any{"type": "string", "title": "Full name", "description": "Your full name"},
			"color":    map[string]any{"type": "string", "default": "blue"},
			"email":    map[string]any{"type": "string", "format": "email"},
			"homepage": map[string]any{"type": "string", "format": "uri"},
			"birthday": map[string]any{"type": "string", "format": "date"},
			"age":      map[string]any{"type": "integer", "minimum": 0, "maximum": 150},
			"score":    map[string]any{"type": "number", "minimum": 0.5, "maximum": 9.5},
			"agree":    map[string]any{"type": "boolean", "default": true},
			"pet":      map[string]any{"type": "string", "enum": []any{"cat", "dog"}},
			"legacy":   map[string]any{"type": "string", "enum": []any{"s", "m"}, "enumNames": []any{"Small", "Medium"}},
			"size": map[string]any{"type": "string", "oneOf": []any{
				map[string]any{"const": "sm", "title": "Small"},
				map[string]any{"const": "lg", "title": "Large"},
			}},
			"toppings": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []any{"ham", "egg"}}},
			"extras": map[string]any{"type": "array", "items": map[string]any{"anyOf": []any{
				map[string]any{"const": "x", "title": "Extra cheese"},
			}}},
		},
		"required": []any{"name", "email"},
	}
}

func fromMap(t *testing.T, raw map[string]any) (Question, error) {
	t.Helper()
	schema, err := DecodeSchema(raw)
	if err != nil {
		t.Fatalf("DecodeSchema: %v", err)
	}
	return FromSchema("everything", "trigger-elicitation-request", "Tell me about you", schema)
}

func fieldNamed(t *testing.T, q Question, name string) Field {
	t.Helper()
	for _, f := range q.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("no field %q in %+v", name, q.Fields)
	return Field{}
}

func TestFromSchemaReadsEveryRestrictedShape(t *testing.T) {
	t.Parallel()
	q, err := fromMap(t, everythingForm())
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	if q.Server != "everything" || q.Tool != "trigger-elicitation-request" || q.Message != "Tell me about you" || q.Schema == nil {
		t.Fatalf("question header = %+v", q)
	}
	if len(q.Fields) != 13 || q.Fields[0].Name != "age" || q.Fields[12].Name != "toppings" {
		t.Fatalf("fields are not the 13 asked for, sorted by name: %+v", q.Fields)
	}
	if f := fieldNamed(t, q, "name"); f.Kind != KindString || !f.Required || f.Title != "Full name" || f.Description != "Your full name" {
		t.Fatalf("name = %+v", f)
	}
	if f := fieldNamed(t, q, "color"); f.Default != "blue" || f.Required {
		t.Fatalf("color = %+v, want an optional string defaulting to blue", f)
	}
	for name, format := range map[string]string{"email": "email", "homepage": "uri", "birthday": "date"} {
		if f := fieldNamed(t, q, name); f.Kind != KindString || f.Format != format {
			t.Fatalf("%s = %+v, want format %s", name, f, format)
		}
	}
	if f := fieldNamed(t, q, "age"); f.Kind != KindInteger || *f.Min != 0 || *f.Max != 150 {
		t.Fatalf("age = %+v", f)
	}
	if f := fieldNamed(t, q, "score"); f.Kind != KindNumber || *f.Min != 0.5 || *f.Max != 9.5 {
		t.Fatalf("score = %+v", f)
	}
	if f := fieldNamed(t, q, "agree"); f.Kind != KindBoolean || f.Default != true {
		t.Fatalf("agree = %+v", f)
	}
	if f := fieldNamed(t, q, "pet"); f.Kind != KindEnum || f.Multi || strings.Join(f.Enum, ",") != "cat,dog" || f.EnumTitles != nil {
		t.Fatalf("pet = %+v", f)
	}
	if f := fieldNamed(t, q, "legacy"); strings.Join(f.EnumTitles, ",") != "Small,Medium" {
		t.Fatalf("legacy enumNames = %+v", f)
	}
	if f := fieldNamed(t, q, "size"); strings.Join(f.Enum, ",") != "sm,lg" || strings.Join(f.EnumTitles, ",") != "Small,Large" {
		t.Fatalf("size = %+v", f)
	}
	if f := fieldNamed(t, q, "toppings"); f.Kind != KindEnum || !f.Multi || strings.Join(f.Enum, ",") != "ham,egg" {
		t.Fatalf("toppings = %+v", f)
	}
	if f := fieldNamed(t, q, "extras"); !f.Multi || f.Enum[0] != "x" || f.EnumTitles[0] != "Extra cheese" {
		t.Fatalf("extras = %+v", f)
	}
}

func TestFromSchemaNilSchemaIsAMessageOnlyForm(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", "Confirm?", nil)
	if err != nil || q.Message != "Confirm?" || len(q.Fields) != 0 {
		t.Fatalf("FromSchema(nil) = %+v, %v", q, err)
	}
}

func TestFromSchemaRefusesWhatAFormCannotShow(t *testing.T) {
	t.Parallel()
	prop := func(p map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": map[string]any{"f": p}}
	}
	many := map[string]any{}
	for i := range MaxFields + 1 {
		many[string(rune('a'+i))] = map[string]any{"type": "string"}
	}
	options := make([]any, MaxEnumOptions+1)
	for i := range options {
		options[i] = strings.Repeat("o", i+1)
	}
	for name, raw := range map[string]map[string]any{
		"an object field":         prop(map[string]any{"type": "object"}),
		"a format outside the set": prop(map[string]any{"type": "string", "format": "ipv4"}),
		"an array with no items":  prop(map[string]any{"type": "array"}),
		"a non-string enum":       prop(map[string]any{"type": "string", "enum": []any{1, 2}}),
		"an option with no const": prop(map[string]any{"type": "string", "oneOf": []any{map[string]any{"title": "x"}}}),
		"too many fields":         {"type": "object", "properties": many},
		"too many options":        prop(map[string]any{"type": "string", "enum": options}),
		"an over-cap title":       prop(map[string]any{"type": "string", "title": strings.Repeat("t", MaxTitleBytes+1)}),
		"an over-cap description": prop(map[string]any{"type": "string", "description": strings.Repeat("d", MaxDescriptionBytes+1)}),
		"an over-cap option":      prop(map[string]any{"type": "string", "enum": []any{strings.Repeat("v", MaxTitleBytes+1)}}),
		"an over-cap field name":  {"type": "object", "properties": map[string]any{strings.Repeat("n", MaxTitleBytes+1): map[string]any{"type": "string"}}},
	} {
		t.Run(name, func(t *testing.T) {
			q, err := fromMap(t, raw)
			if err == nil {
				t.Fatalf("FromSchema accepted %s: %+v", name, q)
			}
			if q.Message != "" || q.Fields != nil || q.Server != "everything" {
				t.Fatalf("a refused question must keep only its server and tool: %+v", q)
			}
		})
	}
}

func TestFromSchemaRefusesAnOverCapMessage(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", strings.Repeat("m", MaxMessageBytes+1), nil)
	if err == nil || q.Message != "" {
		t.Fatalf("an over-cap message was kept: %v %+v", err, q)
	}
}

func TestDecodeSchemaRefusesWhatIsNotASchema(t *testing.T) {
	t.Parallel()
	if _, err := DecodeSchema(func() {}); err == nil {
		t.Fatal("a value that is not JSON decoded")
	}
	if _, err := DecodeSchema(map[string]any{"type": 5}); err == nil {
		t.Fatal(`{"type": 5} decoded as a schema`)
	}
	if s, err := DecodeSchema(nil); s != nil || err != nil {
		t.Fatalf("DecodeSchema(nil) = %v, %v; want a message-only form", s, err)
	}
}
```

Create `internal/elicit/validate_test.go`:

```go
package elicit

import (
	"strings"
	"testing"
)

func mustSchema(t *testing.T) Question {
	t.Helper()
	q, err := fromMap(t, everythingForm())
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	return q
}

func TestValidateAcceptsAFullAnswer(t *testing.T) {
	t.Parallel()
	q := mustSchema(t)
	content := map[string]any{
		"name": "Ada Lovelace", "email": "ada@example.com", "homepage": "https://example.com/ada",
		"birthday": "1815-12-10", "age": float64(36), "score": 7.5, "agree": true,
		"pet": "cat", "size": "lg", "toppings": []any{"ham"}, "extras": []any{"x"},
	}
	if errs := Validate(q.Schema, content); errs != nil {
		t.Fatalf("Validate = %v, want nil", errs)
	}
}

func TestValidateNamesEachFailingField(t *testing.T) {
	t.Parallel()
	q := mustSchema(t)
	errs := Validate(q.Schema, map[string]any{
		"email":    "not-an-address",
		"homepage": "example.com",
		"birthday": "10/12/1815",
		"age":      float64(200),
		"pet":      "horse",
		"agree":    "yes",
		"stranger": "x",
		"color":    strings.Repeat("c", MaxAnswerBytes+1),
	})
	for _, name := range []string{"name", "email", "homepage", "birthday", "age", "pet", "agree", "stranger", "color"} {
		if errs[name] == "" {
			t.Errorf("no error for %q in %v", name, errs)
		}
	}
	if errs["name"] != ErrRequired {
		t.Fatalf("a missing required field = %q, want %q", errs["name"], ErrRequired)
	}
	if !strings.Contains(errs.Error(), "age: ") {
		t.Fatalf("Error() = %q, want each field named", errs.Error())
	}
}

func TestValidateChecksDateTime(t *testing.T) {
	t.Parallel()
	schema, err := DecodeSchema(map[string]any{"type": "object", "properties": map[string]any{
		"at": map[string]any{"type": "string", "format": "date-time"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(schema, map[string]any{"at": "2026-09-25T08:00:00.000Z"}); errs != nil {
		t.Fatalf("a valid date-time was refused: %v", errs)
	}
	if errs := Validate(schema, map[string]any{"at": "2026-09-25 08:00"}); errs["at"] == "" {
		t.Fatal("an invalid date-time was accepted")
	}
}

func TestValidateAMessageOnlyForm(t *testing.T) {
	t.Parallel()
	if errs := Validate(nil, nil); errs != nil {
		t.Fatalf("an empty answer to a message-only form = %v", errs)
	}
	if errs := Validate(nil, map[string]any{"x": 1}); errs[""] == "" {
		t.Fatal("content for a form that asked for none was accepted")
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 ./internal/elicit/`.

Expected: build FAIL with `undefined: DecodeSchema`, and likewise `FromSchema`, `WithAsker`, `Validate`.

- [ ] **Step 3: Implement.** Create `internal/elicit/elicit.go`:

```go
// Package elicit is the vocabulary of an MCP server's form elicitation: the
// question a mounted server asks, the answer the operator gives, and the Asker
// seam that carries one from the MCP bridge to a surface that can show it.
// internal/agent/mcptools and internal/agui both import it, so neither has to
// import the other.
package elicit

import (
	"context"
	"errors"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// Kind is a field's type: exactly the set MCP's restricted elicitation schema allows.
type Kind string

const (
	KindString  Kind = "string"
	KindNumber  Kind = "number"
	KindInteger Kind = "integer"
	KindBoolean Kind = "boolean"
	KindEnum    Kind = "enum"
)

// The protocol's three actions (MCP ElicitResult.Action).
const (
	ActionAccept  = "accept"
	ActionDecline = "decline"
	ActionCancel  = "cancel"
)

// Why Aura declined a question without asking anyone. A code, not a sentence:
// each surface words it in its own language.
const (
	// RefusalUnrenderable: the form is outside the restricted schema or over a cap.
	RefusalUnrenderable = "unrenderable"
	// RefusalAmbiguousRun: calls from more than one run were open on the session
	// a classic request arrived on, so no single thread could be asked.
	RefusalAmbiguousRun = "ambiguous_run"
)

// ErrExpired is the cause a wait ends with when the question's deadline passed
// before an answer came.
var ErrExpired = errors.New("elicitation expired")

// Field is one top-level property of the requested schema, bounded and ready to show.
type Field struct {
	Name        string   `json:"name"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Kind        Kind     `json:"kind"`
	Required    bool     `json:"required"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	EnumTitles  []string `json:"enum_titles,omitempty"`
	Multi       bool     `json:"multi,omitempty"`
	Format      string   `json:"format,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	MinLength   *int     `json:"min_length,omitempty"`
	MaxLength   *int     `json:"max_length,omitempty"`
}

// Question is what a surface shows. Server is the name Aura mounted the server
// under, never a name the server gave itself.
type Question struct {
	ID       string    `json:"id"`
	Server   string    `json:"server"`
	Tool     string    `json:"tool,omitempty"`
	Message  string    `json:"message"`
	Fields   []Field   `json:"fields"`
	Deadline time.Time `json:"deadline"`
	// Refusal, when set, is why Aura declined without asking. The question is
	// still shown, already resolved, so the operator learns that a server asked.
	Refusal string `json:"refusal,omitempty"`
	// Schema is the server's requested schema, kept to validate an answer with the
	// library the SDK validates it with. It never goes on the wire.
	Schema *jsonschema.Schema `json:"-"`
}

// Answer is the operator's decision. Content rides only an accept.
type Answer struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content,omitempty"`
}

// Asker puts a question to an operator and waits for the answer. Ask returns
// (answer, nil) once answered; when ctx ends first it returns context.Cause(ctx),
// which is ErrExpired for a deadline. A refused question is shown resolved and
// answered at once with a decline.
type Asker interface {
	Ask(ctx context.Context, q Question) (Answer, error)
}

type askerKey struct{}

// WithAsker installs the asker of the run ctx belongs to.
func WithAsker(ctx context.Context, asker Asker) context.Context {
	return context.WithValue(ctx, askerKey{}, asker)
}

// AskerFrom returns the run's asker, or nil when the run has no surface to ask on.
func AskerFrom(ctx context.Context) Asker {
	asker, _ := ctx.Value(askerKey{}).(Asker)
	return asker
}
```

Create `internal/elicit/schema.go`:

```go
package elicit

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
)

// The caps on what a server may ask. A server writes every one of these strings
// and they reach a human, so each is bounded. Past a cap the form is declined
// rather than cut, because a cut question can say something the server did not.
const (
	MaxFields           = 20
	MaxEnumOptions      = 50
	MaxMessageBytes     = 2 << 10
	MaxTitleBytes       = 256 // titles, option labels and field names alike: each is shown as a label
	MaxDescriptionBytes = 1 << 10
	MaxAnswerBytes      = 4 << 10 // per string answer, checked by Validate
)

// formats are the string formats the restricted schema allows (go-sdk@v1.8.0
// mcp/client.go:1012-1022 rejects any other before the handler runs).
var formats = []string{"email", "uri", "date", "date-time"}

// DecodeSchema turns an ElicitParams.RequestedSchema, which the SDK hands a client
// as a map, into the typed schema FromSchema and Validate read. A nil schema is a
// form with a message and no fields.
func DecodeSchema(raw any) (*jsonschema.Schema, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("requested schema: %w", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("requested schema: %w", err)
	}
	return &schema, nil
}

// FromSchema projects a requested schema into the fields a form shows, sorted by
// name: the SDK hands the schema over as a map, so the server's own order is
// already gone. On error the question keeps only the server and the tool, so no
// over-cap text reaches anyone.
func FromSchema(server, tool, message string, schema *jsonschema.Schema) (Question, error) {
	refused := Question{Server: server, Tool: tool}
	if len(message) > MaxMessageBytes {
		return refused, fmt.Errorf("the message is %d bytes, over the %d-byte cap", len(message), MaxMessageBytes)
	}
	q := Question{Server: server, Tool: tool, Message: message, Fields: []Field{}, Schema: schema}
	if schema == nil {
		return q, nil
	}
	if len(schema.Properties) > MaxFields {
		return refused, fmt.Errorf("the form has %d fields, over the %d-field cap", len(schema.Properties), MaxFields)
	}
	for _, name := range slices.Sorted(maps.Keys(schema.Properties)) {
		if len(name) > MaxTitleBytes {
			return refused, fmt.Errorf("a field name is %d bytes, over the %d-byte cap", len(name), MaxTitleBytes)
		}
		field, err := fieldOf(name, schema.Properties[name], slices.Contains(schema.Required, name))
		if err != nil {
			return refused, fmt.Errorf("field %q: %w", name, err)
		}
		q.Fields = append(q.Fields, field)
	}
	return q, nil
}

func fieldOf(name string, p *jsonschema.Schema, required bool) (Field, error) {
	if p == nil {
		return Field{}, errors.New("it has no schema")
	}
	if err := capped("title", p.Title, MaxTitleBytes); err != nil {
		return Field{}, err
	}
	if err := capped("description", p.Description, MaxDescriptionBytes); err != nil {
		return Field{}, err
	}
	f := Field{Name: name, Title: p.Title, Description: p.Description, Required: required}
	if len(p.Default) > 0 {
		if err := json.Unmarshal(p.Default, &f.Default); err != nil {
			return Field{}, fmt.Errorf("its default: %w", err)
		}
	}
	switch p.Type {
	case "boolean":
		f.Kind = KindBoolean
	case "number", "integer":
		f.Kind = Kind(p.Type)
		f.Min, f.Max = p.Minimum, p.Maximum
	case "string":
		if len(p.Enum) > 0 || len(p.OneOf) > 0 {
			return enumField(f, p)
		}
		if p.Format != "" && !slices.Contains(formats, p.Format) {
			return Field{}, fmt.Errorf("format %q is not one of %v", p.Format, formats)
		}
		f.Kind, f.Format = KindString, p.Format
		f.MinLength, f.MaxLength = p.MinLength, p.MaxLength
	case "array":
		if p.Items == nil {
			return Field{}, errors.New("it is an array with no items")
		}
		f.Multi = true
		return enumField(f, p.Items)
	default:
		return Field{}, fmt.Errorf("type %q is not string, number, integer, boolean or a multi-select array", p.Type)
	}
	return f, nil
}

// enumField reads a single- or multi-select field in the three shapes MCP allows:
// enum (with the legacy enumNames), oneOf, and anyOf of {const, title}.
func enumField(f Field, p *jsonschema.Schema) (Field, error) {
	f.Kind = KindEnum
	switch {
	case len(p.Enum) > 0:
		for _, v := range p.Enum {
			s, ok := v.(string)
			if !ok {
				return Field{}, fmt.Errorf("enum value %v is not a string", v)
			}
			f.Enum = append(f.Enum, s)
		}
		if names, ok := p.Extra["enumNames"].([]any); ok {
			for _, n := range names {
				s, ok := n.(string)
				if !ok {
					return Field{}, fmt.Errorf("enumNames entry %v is not a string", n)
				}
				f.EnumTitles = append(f.EnumTitles, s)
			}
		}
	case len(p.OneOf) > 0 || len(p.AnyOf) > 0:
		for _, entry := range slices.Concat(p.OneOf, p.AnyOf) {
			value, ok := constString(entry)
			if !ok {
				return Field{}, errors.New("an option has no string const")
			}
			f.Enum = append(f.Enum, value)
			f.EnumTitles = append(f.EnumTitles, entry.Title)
		}
	default:
		return Field{}, errors.New("it offers no options")
	}
	if len(f.Enum) > MaxEnumOptions {
		return Field{}, fmt.Errorf("it has %d options, over the %d-option cap", len(f.Enum), MaxEnumOptions)
	}
	for _, label := range slices.Concat(f.Enum, f.EnumTitles) {
		if err := capped("option", label, MaxTitleBytes); err != nil {
			return Field{}, err
		}
	}
	return f, nil
}

func constString(entry *jsonschema.Schema) (string, bool) {
	if entry == nil || entry.Const == nil {
		return "", false
	}
	s, ok := (*entry.Const).(string)
	return s, ok
}

func capped(what, s string, limit int) error {
	if len(s) > limit {
		return fmt.Errorf("its %s is %d bytes, over the %d-byte cap", what, len(s), limit)
	}
	return nil
}
```

Create `internal/elicit/validate.go`:

```go
package elicit

import (
	"fmt"
	"maps"
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// ErrRequired is the message for a required field left out. The cockpit knows it
// and shows its own copy; every other message is shown as "invalid".
const ErrRequired = "required"

// FieldErrors maps a field's name to why its answer was refused; the empty name
// holds a problem with the answer as a whole.
type FieldErrors map[string]string

func (e FieldErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, name := range slices.Sorted(maps.Keys(e)) {
		parts = append(parts, name+": "+e[name])
	}
	return strings.Join(parts, "; ")
}

// Validate checks an accepted answer the way the SDK will once the handler
// returns: the same library, resolve then validate (go-sdk@v1.8.0
// mcp/client.go:895-904). It checks one field at a time so each error names its
// field, and adds two checks the SDK leaves out: the per-answer size cap, and the
// string formats the library only records as annotations. A field the form did
// not ask for is refused rather than passed on to the server.
func Validate(schema *jsonschema.Schema, content map[string]any) FieldErrors {
	if schema == nil {
		if len(content) > 0 {
			return FieldErrors{"": "this form asks for no fields"}
		}
		return nil
	}
	errs := FieldErrors{}
	for name, prop := range schema.Properties {
		value, present := content[name]
		switch {
		case !present && slices.Contains(schema.Required, name):
			errs[name] = ErrRequired
		case present:
			if msg := checkValue(prop, value); msg != "" {
				errs[name] = msg
			}
		}
	}
	for name := range content {
		if _, asked := schema.Properties[name]; !asked {
			errs[name] = "not asked for"
		}
	}
	if len(errs) > 0 {
		return errs
	}
	resolved, err := schema.Resolve(nil)
	if err == nil {
		err = resolved.Validate(content)
	}
	if err != nil {
		return FieldErrors{"": err.Error()}
	}
	return nil
}

func checkValue(prop *jsonschema.Schema, value any) string {
	if s, ok := value.(string); ok {
		if len(s) > MaxAnswerBytes {
			return fmt.Sprintf("longer than %d bytes", MaxAnswerBytes)
		}
		if !formatHolds(prop.Format, s) {
			return "not a valid " + prop.Format
		}
	}
	resolved, err := prop.Resolve(nil)
	if err == nil {
		err = resolved.Validate(value)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func formatHolds(format, s string) bool {
	switch format {
	case "email":
		addr, err := mail.ParseAddress(s)
		return err == nil && addr.Address == s
	case "uri":
		u, err := url.Parse(s)
		return err == nil && u.Scheme != "" && (u.Host != "" || u.Opaque != "")
	case "date":
		_, err := time.Parse(time.DateOnly, s)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, s)
		return err == nil
	default:
		return true
	}
}
```

Add the coverage entry between the `internal/documents/filecard` and `internal/embeddings` lines:

```json
    "github.com/chetto1983/aura/internal/elicit": {"mode": "target"},
```

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/elicit/`, then `go test -race -count=1 -cover ./internal/elicit/`.

Expected: `ok … coverage: 9x.x%`, at least 85%.
- If `TestFromSchemaReadsEveryRestrictedShape` fails on `legacy`, jsonschema-go did not keep `enumNames` in `Extra`. Read `schema.go`'s `UnmarshalJSON` in the module cache before changing anything.
- If `age` fails, `integer` validation of `float64(36)` behaves differently than assumed. Read `validate.go` in the module cache.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add internal/elicit/
git commit -F - -- internal/elicit/ scripts/coverage_package_policy.json <<'EOF'
feat(elicit): the question an MCP server asks, and the seam that carries it

A neutral package that internal/agent/mcptools and internal/agui both
import, so neither imports the other.

FromSchema projects a server's requested schema into bounded fields:
every restricted type, enum, oneOf and anyOf options, and legacy
enumNames. Past a cap a form is declined, not cut. Validate checks an
answer with the library the SDK uses, one field at a time so the
cockpit can put each error back on its step. It also checks the answer
size cap and the string formats the library only records.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 4: mcptools routes an elicitation to the run that asked

**Files:**
- Create:
  - `internal/agent/mcptools/bridge_inflight.go`
  - `internal/agent/mcptools/elicitation_route.go`
  - `internal/agent/mcptools/elicitation_route_test.go`
- Modify: `internal/agent/mcptools/elicitation.go`, rewritten from line 1 to 295. `ElicitationRequest`, `ElicitationField`, `summariseElicitationSchema`, `askOperatorBounded`, `elicitationPanicError` and `maxElicitationTypeBytes` are deleted.
- Modify: `internal/agent/mcptools/elicitation_test.go`, in these places:
  - `fakeConsent` (15-45);
  - `TestElicitationTimesOutToCancel` (181-203);
  - the two summarise tests (246-300);
  - `TestElicitationMessageIsByteCapped` (302-316);
  - `TestElicitationReachesHandlerOverARealSession` (370-395).
- Modify: `internal/agent/mcptools/bridge_supervisor.go`, the two `CallTool` sites at 309 and 339.
- Modify: `internal/agent/mcptools/bridge_call.go:41-46`.
- Modify: `cmd/aura/elicitation_consent.go`, `cmd/aura/elicitation_consent_test.go`.
  - They compile against the new `ElicitationConsent` signature, so they ship in the same commit.

**Interfaces:**
- Consumes:
  - `elicit.Question`, `elicit.Answer`, `elicit.Asker`, `elicit.AskerFrom`, `elicit.DecodeSchema`, `elicit.FromSchema`, `elicit.ErrExpired`;
  - `elicit.Action*`, `elicit.Refusal*`, `elicit.Kind*`, `elicit.MaxMessageBytes` (Task 3);
  - `pausable.WithTimeout`, `pausable.Hold` (Task 1).
- Produces:
  - `type ElicitationConsent interface { AskOperator(ctx context.Context, q elicit.Question) (action string, content map[string]any, err error) }`. The fallback now receives the bounded `elicit.Question`, whose `Refusal` says why Aura declined without asking.
  - `NewElicitationHandler(server string, consent ElicitationConsent)` keeps its signature. The handler puts a form to `elicit.AskerFrom(<the call's context>)` with the call's clocks held, and falls back to `consent`.
  - A run's asker is reached only through the context of a call made by `MountedServer.CallTool`. Task 6 installs it with `elicit.WithAsker`.

**The routing, as implemented:**

| Arrives on | Placed with | Waits on |
|---|---|---|
| the call's context (MRTR: `mrtr.go:273-305`) | that context's asker | that call |
| the connection's context (classic `elicitation/create`) | the asker shared by every call open on `req.Session` | every call open on the session, until all have ended |

- Calls from more than one run are open, or the form fails `FromSchema`: the request is refused. Every asker concerned gets the question with `Refusal` set, and a run with no asker is told through the fallback.
- No asker: the fallback consent.
- URL mode is refused first, and so is a timeout `<= 0`.

A test cannot fake the classic path: go-sdk refuses `ServerSession.Elicit` when the client asked for protocol 2026-07-28 (`server.go:1619-1627`). The fixture narrows its server to `2025-11-25`, and the client then falls back to the legacy initialize at that version (`client.go:371-386`).

- [ ] **Step 1: Write the failing tests.** Create `internal/agent/mcptools/elicitation_route_test.go`:

```go
package mcptools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/elicit"
)

// fakeAsker stands in for a run's cockpit: it records every question, takes
// after to answer, and keeps the Asker contract when ctx ends first.
type fakeAsker struct {
	answer elicit.Answer
	after  time.Duration
	asked  chan elicit.Question
	ended  chan error
}

const never = time.Hour

func newFakeAsker(answer elicit.Answer, after time.Duration) *fakeAsker {
	return &fakeAsker{answer: answer, after: after, asked: make(chan elicit.Question, 4), ended: make(chan error, 4)}
}

func acceptName(name string) elicit.Answer {
	return elicit.Answer{Action: elicit.ActionAccept, Content: map[string]any{"name": name}}
}

func (f *fakeAsker) Ask(ctx context.Context, q elicit.Question) (elicit.Answer, error) {
	f.asked <- q
	if q.Refusal != "" {
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	timer := time.NewTimer(f.after)
	defer timer.Stop()
	select {
	case <-timer.C:
		return f.answer, nil
	case <-ctx.Done():
		f.ended <- context.Cause(ctx)
		return elicit.Answer{}, context.Cause(ctx)
	}
}

func (f *fakeAsker) question(t *testing.T) elicit.Question {
	t.Helper()
	select {
	case q := <-f.asked:
		return q
	case <-time.After(5 * time.Second):
		t.Fatal("the run's asker was never asked")
		return elicit.Question{}
	}
}

func nameForm() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string", "title": "Name"}},
		"required":   []any{"name"},
	}
}

func textResult(format string, args ...any) *sdkmcp.CallToolResult {
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: fmt.Sprintf(format, args...)}}}
}

// greeting is what every fixture answers once it has a reply: the action, or
// the name when the operator accepted.
func greeting(reply *sdkmcp.ElicitResult) *sdkmcp.CallToolResult {
	if reply.Action != elicit.ActionAccept {
		return textResult("%s", reply.Action)
	}
	return textResult("hello %v", reply.Content["name"])
}

// mrtrAsk elicits the way a 2026-07-28 server must: it returns InputRequests, and
// the client calls the tool again with the reply (go-sdk@v1.8.0 mcp/mrtr.go).
func mrtrAsk(message string, form map[string]any) sdkmcp.ToolHandler {
	return func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return greeting(reply), nil
		}
		return &sdkmcp.CallToolResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: message, RequestedSchema: form},
		}}, nil
	}
}

// classicAsk elicits with a server-to-client request made while its call is open.
func classicAsk(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	reply, err := req.Session.Elicit(ctx, &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: nameForm()})
	if err != nil {
		return nil, err
	}
	return greeting(reply), nil
}

// classicOnly narrows a fixture to the last protocol that allows a classic
// elicitation/create (go-sdk@v1.8.0 mcp/server.go:1619-1627).
var classicOnly = &sdkmcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}}

// elicitingMount serves handlers from an in-memory fixture, mounted with the
// elicitation handler production installs.
func elicitingMount(t *testing.T, opts *sdkmcp.ServerOptions, consent ElicitationConsent, handlers map[string]sdkmcp.ToolHandler) (*MountedServer, *sdkmcp.ServerSession) {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, opts)
	for name, handler := range handlers {
		server.AddTool(mustTool(name, "Elicits.", nil, nil), handler)
	}
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	o := mcpSessionOptionsFor(srv)
	o.Elicitation = NewElicitationHandler("fixture", consent)
	session, err := connectClient(ctx, clientTransport, o)
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)
	return srv, serverSession
}

func declining() *fakeConsent { return &fakeConsent{action: elicit.ActionDecline} }

func TestMRTRElicitationAsksTheCallsRun(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; want the operator's answer back from the server", got, err)
	}
	q := asker.question(t)
	if q.Server != "fixture" || q.Tool != "ask_name" || q.Message != "what is your name" {
		t.Fatalf("question = %+v", q)
	}
	if len(q.Fields) != 1 || q.Fields[0].Name != "name" || !q.Fields[0].Required || q.Fields[0].Kind != elicit.KindString {
		t.Fatalf("fields = %+v", q.Fields)
	}
	if time.Until(q.Deadline) <= 0 {
		t.Fatalf("deadline %v is not ahead", q.Deadline)
	}
	session, err := srv.currentSession()
	if err != nil {
		t.Fatal(err)
	}
	if open := inFlight.on(session); len(open) != 0 {
		t.Fatalf("%d calls still recorded open after they returned", len(open))
	}
}

func TestClassicElicitationAsksTheOneRunInFlight(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, classicOnly, declining(), map[string]sdkmcp.ToolHandler{"ask_name": classicAsk})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "hello Ada" {
		t.Fatalf("CallToolText = %q, %v; a classic request with one run in flight goes to that run", got, err)
	}
	if q := asker.question(t); q.Tool != "ask_name" {
		t.Fatalf("tool = %q, want the open call's", q.Tool)
	}
}

// holdingTool keeps its call open until release closes, so a second run can
// share the session.
func holdingTool(entered, release chan struct{}) sdkmcp.ToolHandler {
	return func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return textResult("held"), nil
	}
}

// Review Focus 3.
func TestClassicElicitationWithTwoRunsInFlightAsksNeither(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	seen := make(chan elicit.Question, 1)
	srv, _ := elicitingMount(t, classicOnly, &fakeConsent{action: elicit.ActionAccept, seen: seen}, map[string]sdkmcp.ToolHandler{
		"hold": holdingTool(entered, release), "ask_name": classicAsk,
	})
	runA, runB := newFakeAsker(acceptName("Ada"), 0), newFakeAsker(acceptName("Bob"), 0)

	held := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(elicit.WithAsker(context.Background(), runA), "hold", nil)
		held <- err
	}()
	<-entered
	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), runB), "ask_name", nil)
	close(release)
	if heldErr := <-held; heldErr != nil {
		t.Fatalf("the held call failed: %v", heldErr)
	}
	if err != nil || got != "decline" {
		t.Fatalf("CallToolText = %q, %v; with two runs on the session the server must be declined", got, err)
	}
	for name, run := range map[string]*fakeAsker{"A": runA, "B": runB} {
		if q := run.question(t); q.Refusal != elicit.RefusalAmbiguousRun {
			t.Fatalf("run %s was shown %+v, want the ambiguous-run refusal", name, q)
		}
	}
	select {
	case q := <-seen:
		t.Fatalf("the fallback was told (%+v) although both runs have a cockpit", q)
	default:
	}
}

func TestClassicElicitationSharedWithAChannelRunTellsItsOperatorToo(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	seen := make(chan elicit.Question, 1)
	srv, _ := elicitingMount(t, classicOnly, &fakeConsent{action: elicit.ActionAccept, seen: seen}, map[string]sdkmcp.ToolHandler{
		"hold": holdingTool(entered, release), "ask_name": classicAsk,
	})
	cockpit := newFakeAsker(acceptName("Ada"), 0)

	held := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(context.Background(), "hold", nil)
		held <- err
	}()
	<-entered
	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), cockpit), "ask_name", nil)
	close(release)
	<-held
	if err != nil || got != "decline" {
		t.Fatalf("CallToolText = %q, %v, want decline", got, err)
	}
	if q := cockpit.question(t); q.Refusal != elicit.RefusalAmbiguousRun {
		t.Fatalf("the cockpit run was shown %+v", q)
	}
	select {
	case q := <-seen:
		if q.Refusal != elicit.RefusalAmbiguousRun {
			t.Fatalf("the channel run's operator was told %+v", q)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run with no cockpit was never told through the fallback")
	}
}

func TestClassicElicitationOutsideAnyCallFallsBack(t *testing.T) {
	seen := make(chan elicit.Question, 1)
	_, serverSession := elicitingMount(t, classicOnly, &fakeConsent{action: elicit.ActionDecline, seen: seen}, map[string]sdkmcp.ToolHandler{"ask_name": classicAsk})

	res, err := serverSession.Elicit(context.Background(), &sdkmcp.ElicitParams{Mode: "form", Message: "anyone there?", RequestedSchema: nameForm()})
	if err != nil || res.Action != elicit.ActionDecline {
		t.Fatalf("Elicit = %+v, %v; want the fallback's decline", res, err)
	}
	select {
	case q := <-seen:
		if q.Message != "anyone there?" || q.Server != "fixture" || q.Refusal != "" {
			t.Fatalf("fallback saw %+v", q)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the fallback consent was never asked")
	}
}

func TestAnOverCapFormIsShownAsARefusal(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{
		"ask_name": mrtrAsk(strings.Repeat("m", elicit.MaxMessageBytes+1), nameForm()),
	})

	got, err := srv.CallToolText(elicit.WithAsker(context.Background(), asker), "ask_name", nil)
	if err != nil || got != "decline" {
		t.Fatalf("CallToolText = %q, %v, want decline", got, err)
	}
	if q := asker.question(t); q.Refusal != elicit.RefusalUnrenderable || q.Message != "" || q.Fields != nil {
		t.Fatalf("question = %+v; an over-cap form is shown as a refusal carrying none of the server's text", q)
	}
}

func TestURLModeIsRefusedBeforeTheRunIsAsked(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), 0)
	ctx := withCallTool(elicit.WithAsker(context.Background(), asker), "login")
	res, err := NewElicitationHandler("fixture", &fakeConsent{action: elicit.ActionAccept})(ctx, &sdkmcp.ElicitRequest{
		Params: &sdkmcp.ElicitParams{Mode: "url", URL: "https://evil.example/phish", ElicitationID: "e1"},
	})
	if err != nil || res.Action != elicit.ActionDecline {
		t.Fatalf("handler = %+v, %v; want decline", res, err)
	}
	select {
	case q := <-asker.asked:
		t.Fatalf("the run was asked a url-mode question: %+v", q)
	default:
	}
}

// bridgedFormTool mounts one MRTR form tool and bridges it the way a registry
// sees it, so Execute runs the real call bound (bridge_call.go).
func bridgedFormTool(t *testing.T) tools.Tool {
	t.Helper()
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})
	bridged, err := bridgeDefault(context.Background(), "forms", srv)
	if err != nil || len(bridged) != 1 {
		t.Fatalf("bridgeDefault = %d tools, %v", len(bridged), err)
	}
	return bridged[0]
}

func runCtx(t *testing.T, asker elicit.Asker) context.Context {
	return elicit.WithAsker(tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048), asker)
}

// Review Focus 1: the operator's 600 ms must not count against a 200 ms call bound.
func TestAHeldCallOutlivesItsTimeout(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.2")
	tool := bridgedFormTool(t)
	asker := newFakeAsker(acceptName("Ada"), 600*time.Millisecond)

	res, err := tool.Execute(runCtx(t, asker), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute: %v; the call's clock must stop while the operator answers", err)
	}
	if !strings.Contains(res.Preview, "hello Ada") {
		t.Fatalf("preview = %q, want the server's greeting", res.Preview)
	}
}

// Review Focus 2: the call's 200 ms bound is earlier than the question's 1 s one,
// and the question must still expire on its own clock.
func TestAnUnansweredQuestionExpiresWhileTheCallIsHeld(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.2")
	t.Setenv(envMCPElicitationTimeoutSec, "1")
	tool := bridgedFormTool(t)
	asker := newFakeAsker(acceptName("Ada"), never)

	start := time.Now()
	res, err := tool.Execute(runCtx(t, asker), json.RawMessage(`{}`))
	elapsed := time.Since(start)
	if err != nil || !strings.Contains(res.Preview, "decline") {
		t.Fatalf("Execute = %q, %v; an expired question declines, it does not fail the call", res.Preview, err)
	}
	if cause := <-asker.ended; !errors.Is(cause, elicit.ErrExpired) {
		t.Fatalf("the wait ended with %v, want elicit.ErrExpired", cause)
	}
	if elapsed < time.Second || elapsed > 5*time.Second {
		t.Fatalf("the question closed after %v, want its own 1s bound", elapsed)
	}
}

func TestTheWaitCancelsWhenTheCallEnds(t *testing.T) {
	asker := newFakeAsker(acceptName("Ada"), never)
	srv, _ := elicitingMount(t, nil, declining(), map[string]sdkmcp.ToolHandler{"ask_name": mrtrAsk("what is your name", nameForm())})
	ctx, cancel := context.WithCancel(elicit.WithAsker(context.Background(), asker))
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := srv.CallToolText(ctx, "ask_name", nil)
		done <- err
	}()
	asker.question(t)
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled call returned no error")
	}
	if cause := <-asker.ended; cause == nil || errors.Is(cause, elicit.ErrExpired) {
		t.Fatalf("the wait ended with %v, want the call's own end, not an expiry", cause)
	}
}

func TestAClassicWaitEndsOnlyWhenEveryCallHasEnded(t *testing.T) {
	first, endFirst := context.WithCancel(context.Background())
	second, endSecond := context.WithCancel(context.Background())
	wait, stop := waitContext(context.Background(), []context.Context{first, second})
	defer stop()

	endFirst()
	select {
	case <-wait.Done():
		t.Fatal("the wait ended with a call still open")
	case <-time.After(50 * time.Millisecond):
	}
	endSecond()
	select {
	case <-wait.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the wait outlived every call")
	}
	if !errors.Is(context.Cause(wait), context.Canceled) {
		t.Fatalf("cause = %v, want the calls' own", context.Cause(wait))
	}
}
```

In `internal/agent/mcptools/elicitation_test.go`:
- Add `"github.com/chetto1983/aura/internal/elicit"` to the imports.
- Use the Edit tool with `replace_all` to replace `elicitActionAccept` with `elicit.ActionAccept`, and likewise `elicitActionDecline` and `elicitActionCancel`.
- Change `fakeConsent`'s `seen` field to `seen chan elicit.Question`. Change its method to:

```go
func (f *fakeConsent) AskOperator(ctx context.Context, q elicit.Question) (string, map[string]any, error) {
	if f.seen != nil {
		select {
		case f.seen <- q:
		default:
		}
	}
	if f.panics {
		panic("consent surface blew up")
	}
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	return f.action, f.content, f.err
}
```

Replace `TestElicitationTimesOutToCancel` with the test below. The expected action changes from cancel to decline. That is the spec's rule, not the test being bent: "No answer within `AURA_MCP_ELICITATION_TIMEOUT_SEC` → Decline", and cancel is kept for the call or the run ending. The commit message says so.

```go
// TestElicitationFallbackExpiresToDecline pins T-45.1-30: a surface that ignores
// ctx cannot hold the in-flight agent turn open. An expired wait declines; cancel
// is kept for the call or the run ending.
func TestElicitationFallbackExpiresToDecline(t *testing.T) {
	blocked := make(chan struct{})
	t.Cleanup(func() { close(blocked) })
	t.Setenv(envMCPElicitationTimeoutSec, "1")

	start := time.Now()
	res := callHandler(t, "fixture", &fakeConsent{action: elicit.ActionAccept, block: blocked}, &sdkmcp.ElicitParams{Message: "hi"})
	if res.Action != elicit.ActionDecline {
		t.Fatalf("action = %q, want decline on expiry", res.Action)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("handler took %v; the timeout did not bound it", elapsed)
	}
}
```

Delete `TestSummariseElicitationSchemaIsSortedAndCapped` and `TestSummariseElicitationSchemaDegradesToNoFields`. The function is gone, and `internal/elicit`'s tests cover the projection that replaces it.

Replace `TestElicitationMessageIsByteCapped` with:

```go
// TestAnOverCapFormReachesTheFallbackAsARefusal pins T-45.1-29 on the new path:
// an over-cap message is not cut down and shown, it is refused, and the fallback
// learns why without any of the server's text.
func TestAnOverCapFormReachesTheFallbackAsARefusal(t *testing.T) {
	t.Parallel()
	seen := make(chan elicit.Question, 1)
	res := callHandler(t, "fixture", &fakeConsent{action: elicit.ActionAccept, seen: seen},
		&sdkmcp.ElicitParams{Message: strings.Repeat("z", elicit.MaxMessageBytes+1)})
	if res.Action != elicit.ActionDecline {
		t.Fatalf("action = %q, want decline even though the fallback would accept", res.Action)
	}
	if q := <-seen; q.Refusal != elicit.RefusalUnrenderable || q.Message != "" || q.Server != "fixture" {
		t.Fatalf("fallback saw %+v", q)
	}
}
```

In `TestElicitationReachesHandlerOverARealSession`:
- make `seen := make(chan elicit.Question, 1)`;
- replace the fields check with:

```go
		if len(req.Fields) != 1 || req.Fields[0].Name != "name" || !req.Fields[0].Required || req.Fields[0].Kind != elicit.KindString {
			t.Fatalf("fields = %+v, want one required string field named 'name'", req.Fields)
		}
```

In `cmd/aura/elicitation_consent_test.go`:
- Replace the `mcptools` import with `"github.com/chetto1983/aura/internal/elicit"`.
- Replace each `mcptools.ElicitationRequest{` with `elicit.Question{`.
- Replace `TestRenderElicitationPromptFields` and `TestRenderElicitationPromptBoundsAFloodOfFields` with the three tests below.
  - The flood test pinned the "and N more field(s)" line. `FromSchema` now refuses a form with more than 20 fields, so that line cannot be reached and is deleted.
  - The byte bound is what still protects the channel, and the new test pins it at the caps' worst case.

```go
func TestRenderElicitationPromptFields(t *testing.T) {
	t.Parallel()
	got := renderElicitationPrompt(elicit.Question{
		Server:  "fixture",
		Message: "details please",
		Fields: []elicit.Field{
			{Name: "name", Kind: elicit.KindString, Required: true, Description: "your name"},
			{Name: "age", Kind: elicit.KindNumber},
		},
	})
	for _, want := range []string{"- name (string, required): your name", "- age (number)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// TestRenderElicitationPromptStaysUnderTheChannelLimit renders the largest form
// FromSchema lets through and checks it still fits the channel's message.
func TestRenderElicitationPromptStaysUnderTheChannelLimit(t *testing.T) {
	t.Parallel()
	fields := make([]elicit.Field, 0, elicit.MaxFields)
	for i := range elicit.MaxFields {
		fields = append(fields, elicit.Field{
			Name:        strings.Repeat(string(rune('a'+i)), elicit.MaxTitleBytes),
			Kind:        elicit.KindString,
			Description: strings.Repeat("d", elicit.MaxDescriptionBytes),
		})
	}
	got := renderElicitationPrompt(elicit.Question{Server: "flood", Message: strings.Repeat("m", elicit.MaxMessageBytes), Fields: fields})
	if len(got) > maxRenderedPromptBytes || !strings.HasSuffix(got, "(truncated)") {
		t.Fatalf("rendered %d bytes ending %q, want <= %d and an announced cut", len(got), got[max(len(got)-20, 0):], maxRenderedPromptBytes)
	}
}

func TestRenderElicitationPromptSaysWhyItRefused(t *testing.T) {
	t.Parallel()
	for refusal, want := range map[string]string{
		"":                         "Aura declined it automatically.",
		elicit.RefusalUnrenderable: "its form cannot be shown",
		elicit.RefusalAmbiguousRun: "more than one conversation",
	} {
		if got := renderElicitationPrompt(elicit.Question{Server: "fixture", Refusal: refusal}); !strings.Contains(got, want) {
			t.Fatalf("refusal %q: missing %q in:\n%s", refusal, want, got)
		}
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go vet ./internal/agent/mcptools/ ./cmd/aura/`.

Expected: build FAIL:
- `undefined: inFlight`, and likewise `withCallTool` and `waitContext`;
- `cannot use consent (variable of type *fakeConsent) as ElicitationConsent value … wrong type for method AskOperator`;
- in `cmd/aura`: `undefined: elicit.Question` used as the argument of `renderElicitationPrompt`.

- [ ] **Step 3: Implement.** Create `internal/agent/mcptools/bridge_inflight.go`:

```go
package mcptools

import (
	"context"
	"sync"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// bridge_inflight.go remembers which tool calls are open on which session, so an
// elicitation can find the run that asked. The SDK hands a multi-round-trip
// elicitation to the handler on the call's own context (go-sdk@v1.8.0
// mcp/mrtr.go:273-305), which callOnSession marks. A classic elicitation/create
// arrives on the session's connection context instead, and only the calls open on
// that session can say which run it belongs to.

type callToolKey struct{}

func withCallTool(ctx context.Context, tool string) context.Context {
	return context.WithValue(ctx, callToolKey{}, tool)
}

// callToolFrom reports the tool whose call ctx is, if it is a call's context.
func callToolFrom(ctx context.Context) (string, bool) {
	tool, ok := ctx.Value(callToolKey{}).(string)
	return tool, ok
}

type inFlightCall struct{ ctx context.Context }

// inFlightCalls is process-wide because its key already is: a *ClientSession
// belongs to exactly one mount, so two mounts never share an entry.
type inFlightCalls struct {
	mu        sync.Mutex
	bySession map[*sdkmcp.ClientSession]map[*inFlightCall]struct{}
}

var inFlight = &inFlightCalls{bySession: map[*sdkmcp.ClientSession]map[*inFlightCall]struct{}{}}

func (f *inFlightCalls) enter(ctx context.Context, session *sdkmcp.ClientSession) (leave func()) {
	call := &inFlightCall{ctx: ctx}
	f.mu.Lock()
	calls := f.bySession[session]
	if calls == nil {
		calls = map[*inFlightCall]struct{}{}
		f.bySession[session] = calls
	}
	calls[call] = struct{}{}
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		delete(calls, call)
		if len(calls) == 0 {
			delete(f.bySession, session)
		}
	}
}

// on returns the contexts of the calls open on session, in no order.
func (f *inFlightCalls) on(session *sdkmcp.ClientSession) []context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	open := make([]context.Context, 0, len(f.bySession[session]))
	for call := range f.bySession[session] {
		open = append(open, call.ctx)
	}
	return open
}

// callOnSession makes the tools/call both of MountedServer.CallTool's attempts
// make, with its context marked and recorded for as long as it is open.
func callOnSession(ctx context.Context, session *sdkmcp.ClientSession, name string, args map[string]any) (*sdkmcp.CallToolResult, error) {
	ctx = withCallTool(ctx, name)
	defer inFlight.enter(ctx, session)()
	return session.CallTool(ctx, &sdkmcp.CallToolParams{Name: name, Arguments: args})
}
```

In `internal/agent/mcptools/bridge_supervisor.go`:
- line 309 becomes `res, callErr = callOnSession(ctx, session, name, args)`;
- line 339 becomes `res, callErr := callOnSession(ctx, retry, name, args)`.
- An identity-scoped mount recurses into its child's `CallTool` at 299, so its calls register on the child's session with no further change.

In `internal/agent/mcptools/bridge_call.go`, replace lines 41-46 with the block below, and add `"github.com/chetto1983/aura/internal/pausable"` to the imports:

```go
	callCtx := ctx
	cancel := func() {}
	if b.callTimeout > 0 {
		// Pausable: while the server's form waits on the operator the call is held,
		// and the operator's time does not count against its bound.
		callCtx, cancel = pausable.WithTimeout(ctx, b.callTimeout)
	}
	defer cancel()
```

Replace `internal/agent/mcptools/elicitation.go` in full:

```go
package mcptools

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/redact"
)

// elicitation.go answers SEP-2322 server-initiated elicitation. A form a mounted
// server asks for inside a cockpit run is put to that run's operator
// (elicitation_route.go), and the call waits, with its clocks held, for the
// answer. Anything Aura cannot place in one run falls back to
// decline-and-surface: the ElicitationConsent the composition root wires declines
// and tells the operator on their channel.
//
// Aura writes the handler body and nothing else. The SDK owns the multi-round-trip
// loop (go-sdk@v1.8.0 mcp/mrtr.go:73-115), the classic elicitation/create request,
// and the schema checks made before and after the handler (mcp/client.go:869-920).

// elicitModeURL is the one mode Aura refuses without consulting anyone. Opening a
// server-supplied URL that the operator reads as Aura-sanctioned is a phishing
// primitive (T-45.1-31), so the URL is never rendered anywhere, not even in a log.
// Hermes declined it for the same reason (tools/mcp_tool.py:1720-1731).
const elicitModeURL = "url"

const envMCPElicitationTimeoutSec = "AURA_MCP_ELICITATION_TIMEOUT_SEC"

// defaultElicitationTimeout matches hermes' reference default and the value
// recorded in 45.1-06-SUMMARY.md.
//
// A configured value <= 0 DISABLES elicitation: the handler declines at once. It
// does NOT mean "wait forever", the reading a future reader will assume and the
// dangerous one: an unbounded wait holds the call, and with it the turn, until the
// server gives up.
const defaultElicitationTimeout = 300 * time.Second

// maxLoggedValueBytes caps a server- or surface-supplied value that reaches a log
// line: an unrecognised action, a malformed timeout.
const maxLoggedValueBytes = 32

// The boundary reuses the MCP call counter with its own operation value rather
// than registering a second instrument: obs exposes emission ONLY through
// Boundary, whose outcome is derived from the error (nil→success,
// context.Canceled→canceled, context.DeadlineExceeded→timeout, else error). The
// action and the reason ride the structured log line instead, which keeps a
// server-supplied string out of a metric dimension.
var mcpElicitationBoundary = obs.NewGlobalBoundary("github.com/chetto1983/aura/internal/agent/mcptools", obs.BoundaryConfig{
	Operation: "mcp_elicitation", ToolClass: obs.ToolClassMCP, Transport: "in_process",
	Count: obs.MCPCallsID, Duration: obs.MCPDurationID,
})

// ElicitationConsent is the fallback for a request no run can be asked: the
// composition root's decline-and-surface. It is declared here, consumer-side, so
// this package imports neither internal/runner nor internal/channels.
//
// It receives the bounded elicit.Question, never the SDK params, so a
// composition-root implementation cannot reach the raw schema or a URL even by
// mistake. Returning anything other than "accept" yields a non-accept result, and
// an error, a panic or a surface that never returns all decline or cancel.
type ElicitationConsent interface {
	AskOperator(ctx context.Context, q elicit.Question) (action string, content map[string]any, err error)
}

// elicitOutcome is what the handler answers and records. err is for
// observability only: the SDK never sees it.
type elicitOutcome struct {
	action  string
	content map[string]any
	fields  int
	reason  string
	err     error
}

// NewElicitationHandler builds the closure mcp.SessionOptions.Elicitation takes.
//
// It returns (*ElicitResult, nil) in EVERY case. An error returned from here
// propagates through fulfillInputRequests (go-sdk@v1.8.0 mcp/mrtr.go:273-305) and
// fails the whole CallTool with an opaque message, where a decline hands the
// server a protocol answer it can respond to.
func NewElicitationHandler(server string, consent ElicitationConsent) func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
	return func(ctx context.Context, req *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
		ctx, end := mcpElicitationBoundary.Start(ctx)
		out := decideElicitation(ctx, server, consent, req)
		end.End(out.err)
		level := slog.LevelInfo
		if out.err != nil {
			level = slog.LevelWarn
		}
		// The operator's values never reach a log: the action, the server and the
		// field count do.
		slog.Log(ctx, level, "mcp elicitation resolved", "server", redact.Line(server),
			"action", out.action, "fields", out.fields, "reason", out.reason, "err", out.err)
		return &sdkmcp.ElicitResult{Action: out.action, Content: out.content}, nil
	}
}

func decideElicitation(ctx context.Context, server string, consent ElicitationConsent, req *sdkmcp.ElicitRequest) elicitOutcome {
	if req == nil || req.Params == nil {
		return elicitOutcome{action: elicit.ActionDecline, reason: "no params"}
	}
	params := req.Params
	if strings.EqualFold(strings.TrimSpace(params.Mode), elicitModeURL) {
		// The URL is deliberately absent from every record. T-45.1-31.
		return elicitOutcome{action: elicit.ActionDecline, reason: "url mode is refused"}
	}
	timeout := configuredElicitationTimeout()
	if timeout <= 0 {
		return elicitOutcome{action: elicit.ActionDecline, reason: "disabled by " + envMCPElicitationTimeoutSec}
	}

	r := routeFor(ctx, req.Session)
	q, err := questionFor(server, r.tool, params)
	switch {
	case err != nil:
		out := refuse(ctx, r, consent, q, elicit.RefusalUnrenderable, timeout)
		out.err = err
		return out
	case r.mixed:
		return refuse(ctx, r, consent, q, elicit.RefusalAmbiguousRun, timeout)
	case r.asker != nil:
		return askRun(ctx, r, q, timeout)
	default:
		return askFallback(r.fallbackContext(ctx), consent, q, timeout)
	}
}

func questionFor(server, tool string, params *sdkmcp.ElicitParams) (elicit.Question, error) {
	schema, err := elicit.DecodeSchema(params.RequestedSchema)
	if err != nil {
		return elicit.Question{Server: server, Tool: tool}, err
	}
	return elicit.FromSchema(server, tool, params.Message, schema)
}

// answered maps an operator's decision onto the protocol. Content rides only an
// accept, and an action the protocol does not define declines: an unrecognised
// action is not a permissive one.
func answered(a elicit.Answer, fields int) elicitOutcome {
	switch a.Action {
	case elicit.ActionAccept:
		return elicitOutcome{action: elicit.ActionAccept, content: a.Content, fields: fields, reason: "answered"}
	case elicit.ActionDecline, elicit.ActionCancel:
		return elicitOutcome{action: a.Action, fields: fields, reason: "answered"}
	default:
		return elicitOutcome{action: elicit.ActionDecline, fields: fields,
			reason: "unrecognised action " + redact.Line(strconv.Quote(truncateUTF8Bytes(a.Action, maxLoggedValueBytes)))}
	}
}

// configuredElicitationTimeout reads AURA_MCP_ELICITATION_TIMEOUT_SEC, mirroring
// configuredMCPCallTimeout's convention in timeout.go.
//
// A value <= 0 returns 0, which the handler reads as DISABLED, not infinite. An
// unparseable value falls back to the default rather than failing the mount: a
// malformed knob must not stop a server from being usable.
func configuredElicitationTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv(envMCPElicitationTimeoutSec))
	if raw == "" {
		return defaultElicitationTimeout
	}
	sec, err := strconv.Atoi(raw)
	if err != nil {
		slog.Warn("ignoring malformed elicitation timeout",
			"env", envMCPElicitationTimeoutSec, "value", truncateUTF8Bytes(raw, maxLoggedValueBytes))
		return defaultElicitationTimeout
	}
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec) * time.Second
}
```

Create `internal/agent/mcptools/elicitation_route.go`:

```go
package mcptools

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/pausable"
)

var errElicitationPanic = errors.New("elicitation surface panicked")

// route is where one elicitation goes: the asker of the run whose call it arrived
// in, that call's tool, and the open calls the wait belongs to. mixed means calls
// from more than one run are open, so the request cannot be placed.
type route struct {
	asker elicit.Asker
	tool  string
	calls []context.Context
	mixed bool
}

// routeFor finds the run an elicitation belongs to. A multi-round-trip request
// arrives on its call's own context, which names the run outright. A classic
// elicitation/create arrives on the connection's context and is placed only when
// every call open on its session belongs to one run.
func routeFor(ctx context.Context, session *sdkmcp.ClientSession) route {
	if tool, ok := callToolFrom(ctx); ok {
		return route{asker: elicit.AskerFrom(ctx), tool: tool, calls: []context.Context{ctx}}
	}
	r := route{calls: inFlight.on(session)}
	for i, call := range r.calls {
		asker := elicit.AskerFrom(call)
		tool, _ := callToolFrom(call)
		switch {
		case i == 0:
			r.asker, r.tool = asker, tool
		case asker != r.asker:
			r.mixed = true
		}
		if tool != r.tool {
			r.tool = ""
		}
	}
	if r.mixed {
		r.asker = nil
	}
	return r
}

// fallbackContext is the context the fallback consent is asked on: the one open
// call's, which carries its operator's identity, or the handler's own.
func (r route) fallbackContext(ctx context.Context) context.Context {
	if len(r.calls) == 1 {
		return r.calls[0]
	}
	return ctx
}

// askRun puts q to the run's operator and waits. Every clock of the waiting calls
// is held from the moment the question is put until the wait ends, so only the
// operator's time is excluded. The asker is Aura's own and returns as soon as its
// context ends, so it is called in place: what it decided is exactly what the
// server is told, with no race against the deadline.
func askRun(ctx context.Context, r route, q elicit.Question, timeout time.Duration) elicitOutcome {
	releases := make([]func(), 0, len(r.calls))
	for _, call := range r.calls {
		releases = append(releases, pausable.Hold(call))
	}
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	wait, stopWait := waitContext(ctx, r.calls)
	defer stopWait()
	bounded, stop := withExpiry(wait, timeout)
	defer stop()

	q.Deadline = time.Now().Add(timeout)
	answer, err := askRecovered(bounded, r.asker, q)
	switch {
	case err != nil && bounded.Err() != nil:
		return waitEnded(bounded, len(q.Fields))
	case err != nil:
		return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "the run's asker failed", err: err}
	}
	return answered(answer, len(q.Fields))
}

// askFallback is decline-and-surface. The consent may ignore ctx or never return,
// so it runs on its own goroutine and is abandoned when the wait ends; the
// channel is buffered so that goroutine can still finish.
func askFallback(ctx context.Context, consent ElicitationConsent, q elicit.Question, timeout time.Duration) elicitOutcome {
	if consent == nil {
		return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "no consent surface wired"}
	}
	bounded, stop := withExpiry(ctx, timeout)
	defer stop()

	type reply struct {
		action  string
		content map[string]any
		err     error
	}
	done := make(chan reply, 1)
	go func() {
		defer func() {
			if recover() != nil {
				done <- reply{err: errElicitationPanic}
			}
		}()
		action, content, err := consent.AskOperator(bounded, q)
		done <- reply{action: action, content: content, err: err}
	}()
	select {
	case got := <-done:
		switch {
		case got.err != nil && bounded.Err() != nil:
			return waitEnded(bounded, len(q.Fields))
		case got.err != nil:
			return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "the consent surface failed", err: got.err}
		}
		return answered(elicit.Answer{Action: got.action, Content: got.content}, len(q.Fields))
	case <-bounded.Done():
		return waitEnded(bounded, len(q.Fields))
	}
}

// refuse declines a request Aura will not put to anyone and tells whoever can be
// told why: every run with an asker sees the question already resolved, and a run
// with none (or no run at all) is told through the fallback, whose answer is
// ignored.
func refuse(ctx context.Context, r route, consent ElicitationConsent, q elicit.Question, why string, timeout time.Duration) elicitOutcome {
	q.Refusal = why
	told := map[elicit.Asker]bool{}
	var untold context.Context
	for _, call := range r.calls {
		switch asker := elicit.AskerFrom(call); {
		case asker == nil:
			if untold == nil {
				untold = call
			}
		case !told[asker]:
			told[asker] = true
			_, _ = askRecovered(call, asker, q)
		}
	}
	switch {
	case untold != nil:
		askFallback(untold, consent, q, timeout)
	case len(told) == 0:
		askFallback(ctx, consent, q, timeout)
	}
	return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "refused: " + why}
}

// waitContext ends when ctx does or when every call in calls has ended. A classic
// request cannot say which open call it belongs to, so it is waited on until none
// is left.
func waitContext(ctx context.Context, calls []context.Context) (context.Context, func()) {
	wait, cancel := context.WithCancelCause(ctx)
	var open atomic.Int64
	open.Store(int64(len(calls)))
	stops := make([]func() bool, 0, len(calls))
	for _, call := range calls {
		stops = append(stops, context.AfterFunc(call, func() {
			if open.Add(-1) == 0 {
				cancel(context.Cause(call))
			}
		}))
	}
	return wait, func() {
		for _, stop := range stops {
			stop()
		}
		cancel(nil)
	}
}

// withExpiry ends ctx with elicit.ErrExpired once timeout passes. It runs its own
// timer rather than context.WithTimeout: a held call's context reports a deadline
// earlier than the question's, WithTimeout trusts an earlier parent deadline and
// arms no timer, and the bound would never fire.
func withExpiry(ctx context.Context, timeout time.Duration) (context.Context, func()) {
	bounded, cancel := context.WithCancelCause(ctx)
	timer := time.AfterFunc(timeout, func() { cancel(elicit.ErrExpired) })
	return bounded, func() {
		timer.Stop()
		cancel(nil)
	}
}

// waitEnded is the outcome of a wait whose context ended before an answer: a
// passed deadline declines, and the call or the run ending cancels.
func waitEnded(ctx context.Context, fields int) elicitOutcome {
	cause := context.Cause(ctx)
	if errors.Is(cause, elicit.ErrExpired) {
		return elicitOutcome{action: elicit.ActionDecline, fields: fields, reason: "expired"}
	}
	return elicitOutcome{action: elicit.ActionCancel, fields: fields, reason: "the call or the run ended", err: cause}
}

func askRecovered(ctx context.Context, asker elicit.Asker, q elicit.Question) (answer elicit.Answer, err error) {
	defer func() {
		if recover() != nil {
			answer, err = elicit.Answer{}, errElicitationPanic
		}
	}()
	return asker.Ask(ctx, q)
}
```

In `cmd/aura/elicitation_consent.go`:
- Replace the `mcptools` import with `"github.com/chetto1983/aura/internal/elicit"`.
- Delete `maxRenderedFields` and its comment (32-35).
- Replace the file comment's first paragraph (16-22) with:

```go
// elicitation_consent.go is the composition-root half of SEP-2322 elicitation:
// the fallback for a request no cockpit run can answer (mcptools
// elicitation_route.go). It declines, and delivers the ask to the operator on
// their own channel, so a server that asked is never refused in silence.
```

- Change `AskOperator`'s signature to `AskOperator(ctx context.Context, q elicit.Question) (string, map[string]any, error)`.
- In its body, replace every `req.Server` with `q.Server`, every `"decline"` with `elicit.ActionDecline`, and `renderElicitationPrompt(req)` with `renderElicitationPrompt(q)`.
- Replace `renderElicitationPrompt` with:

```go
func renderElicitationPrompt(q elicit.Question) string {
	var b strings.Builder
	fmt.Fprintf(&b, "MCP server %q is asking for input.\n", q.Server)

	message := strings.TrimSpace(q.Message)
	if message == "" {
		message = "(the server sent no message)"
	}
	b.WriteString("\nIt says:\n")
	for line := range strings.SplitSeq(message, "\n") {
		fmt.Fprintf(&b, "> %s\n", strings.TrimRight(line, "\r"))
	}

	if len(q.Fields) > 0 {
		b.WriteString("\nIt is asking for:\n")
		for _, f := range q.Fields {
			fmt.Fprintf(&b, "- %s (%s", f.Name, f.Kind)
			if f.Required {
				b.WriteString(", required")
			}
			b.WriteString(")")
			if desc := strings.TrimSpace(f.Description); desc != "" {
				b.WriteString(": " + desc)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n" + declinedBecause(q.Refusal))
	return capPromptBytes(b.String(), maxRenderedPromptBytes)
}

// declinedBecause is the prompt's last line: what Aura did, and why when it did
// not even try to ask anyone.
func declinedBecause(refusal string) string {
	switch refusal {
	case elicit.RefusalUnrenderable:
		return "Aura declined it automatically because its form cannot be shown. Nothing was sent to the server."
	case elicit.RefusalAmbiguousRun:
		return "Aura declined it automatically because calls from more than one conversation are open on that server, so Aura cannot tell which one it belongs to. Nothing was sent to the server."
	default:
		return "Aura declined it automatically. Nothing was sent to the server."
	}
}
```

Update the comment on `maxRenderedPromptBytes`: "Each part is already capped upstream by internal/elicit (message, titles, descriptions, 20 fields), so this bites only on a form at every cap at once; it sits under Telegram's 4096-character message limit so the chosen surface can actually deliver what it renders."

- [ ] **Step 4: Run the packages.** Go: `go vet ./internal/agent/mcptools/ ./cmd/aura/`, then `go test -race -count=1 ./internal/agent/mcptools/`, then `go test -race -count=1 -run 'Elicitation|RenderElicitation|CapPrompt|MCPMountOptions' ./cmd/aura/`.

Expected: `ok` for both.
- `TestBridgedToolExecuteAppliesConfiguredCallTimeout` still passes unchanged. The pausable context still expires, and it still reports `context deadline exceeded` through the SDK's derived contexts (Task 1's `TestWithTimeoutExpiresLikeAStandardDeadline` is why).
- If either classic test fails with `cannot be sent while serving a request on protocol version 2026-07-28`, the client did not fall back. Stop and read `client.go:314-386` before changing the fixture. Do not pass a client protocol version: production does not.
- Check the size: `wc -l internal/agent/mcptools/bridge_supervisor.go internal/agent/mcptools/elicitation*.go internal/agent/mcptools/bridge_inflight.go`. Every file must be under 600 lines.
- Measure coverage: Go: `go test -race -count=1 -coverprofile=/tmp/mcpt.out ./internal/agent/mcptools/ && go tool cover -func=/tmp/mcpt.out | grep -E 'elicitation|inflight'`. Every new function must be covered. Missing functions go in the task report, not behind a skip.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add internal/agent/mcptools/bridge_inflight.go internal/agent/mcptools/elicitation_route.go internal/agent/mcptools/elicitation_route_test.go
git commit -F - -- internal/agent/mcptools/bridge_inflight.go internal/agent/mcptools/elicitation_route.go internal/agent/mcptools/elicitation_route_test.go internal/agent/mcptools/elicitation.go internal/agent/mcptools/elicitation_test.go internal/agent/mcptools/bridge_supervisor.go internal/agent/mcptools/bridge_call.go cmd/aura/elicitation_consent.go cmd/aura/elicitation_consent_test.go <<'EOF'
feat(mcptools): put a server's form to the run that made the call

An MCP server's form elicitation now reaches the run that made the call.

- A multi-round-trip request arrives on the call's own context and names
  its run outright.
- A classic elicitation/create is matched through the calls open on its
  session. With calls from two runs open it is declined, and both are
  told why.

The wait holds every clock of the call, so the operator's time counts
against neither the 60 s call bound nor the run's wallclock. The wait
has its own expiry timer: a held call reports an earlier deadline, and
context.WithTimeout would have armed no timer at all.

Two tests change on purpose:
- TestElicitationTimesOutToCancel becomes ...ExpiresToDecline. The spec
  makes an unanswered form a decline and keeps cancel for the call or
  the run ending.
- The flood-of-fields render test becomes a byte-bound test. FromSchema
  now refuses a form of more than 20 fields, so the "and N more" line
  could never be reached and is gone.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---

### Task 5: Every runtime mount can be asked

**Files:**
- Modify: `cmd/aura/runtime_tool_handles.go:36-40`. Add the `Elicitation` handle after `MCPFiles`.
- Modify: `cmd/aura/main.go:337-355`. `buildRegistryWithMCP` stores `consent` on the handles, before the empty-set early return.
- Modify: `cmd/aura/main.go:418`. The stdio mount takes `stdioMountOptions`.
- Modify: `cmd/aura/mcp_tools.go:266-276`. `mcpMountOptions` carries the consent. Add `stdioMountOptions` below it.
- Modify: `internal/agent/mcptools/mount.go`, in three places:
  - the doc comments at 28-31 and 37-45;
  - the local variables named `elicit` at 124/127 and 181/185, renamed to `handler`, so that no local shadows the package the sibling files import.
- Test: `cmd/aura/mcp_mount_options_test.go`.

**Interfaces:**
- Consumes: `mcptools.ElicitationConsent` (Task 4); `newElicitationConsent()` (`cmd/aura/elicitation_consent.go:68`).
- Produces:
  - `runtimeToolHandles.Elicitation mcptools.ElicitationConsent`;
  - `stdioMountOptions(handles *runtimeToolHandles) mcptools.MountOptions`.
  - Every boot and live mount now advertises form elicitation.
  - `aura tools` (`main.go:549`) and the one-shot pipe (`toolpipe.go:80-89`) keep passing `nil`. They have no operator, and a nil consent keeps the capability unadvertised.

- [ ] **Step 1: Write the failing tests.** Append to `cmd/aura/mcp_mount_options_test.go`:

```go
func TestEveryRuntimeMountCarriesTheElicitationFallback(t *testing.T) {
	consent := newElicitationConsent()
	handles := &runtimeToolHandles{MCPFiles: &tools.MCPFileSink{}, Elicitation: consent}

	managed := mcpMountOptions(context.Background(), true, mcp.ManagedServer{URL: "https://mcp.example/mcp"}, handles)
	stdio := stdioMountOptions(handles)

	if managed.Elicitation != consent || stdio.Elicitation != consent {
		t.Fatalf("managed=%v stdio=%v, want both mounts handed the fallback consent", managed.Elicitation, stdio.Elicitation)
	}
	if stdio.Files != handles.MCPFiles {
		t.Fatalf("stdio options lost the file sink: %+v", stdio)
	}
}

// Live mounts read the consent off the boot handles, so it must be there even when
// boot mounted nothing and left through the empty-set early return.
func TestBuildRegistryWithMCP_NoBootServersStillHandsLiveMountsTheConsent(t *testing.T) {
	withMemoryMCPRegistry(t)
	seedMCPRegistry(t, withDefaultOnRecipesOff(mcp.ManagedConfig{}))
	consent := newElicitationConsent()

	_, handles, closers, err := buildRegistryWithMCP(context.Background(), config.LoadDB(), nil, nil, nil, consent)
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	defer func() { _ = closeMCPServers(closers) }()

	if handles.Elicitation != consent {
		t.Fatalf("handles.Elicitation = %v, want the consent boot was given", handles.Elicitation)
	}
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go test -race -count=1 -run 'CarriesTheElicitationFallback|StillHandsLiveMountsTheConsent' ./cmd/aura/`.

Expected: build FAIL with `unknown field Elicitation in struct literal of type runtimeToolHandles` and `undefined: stdioMountOptions`.

- [ ] **Step 3: Implement.** In `cmd/aura/runtime_tool_handles.go`, add after `MCPFiles mcptools.FileSink`:

```go
	// Elicitation is the fallback consent every runtime mount is handed: a mounted
	// server's form reaches a cockpit run through that run's own asker, and this
	// only for the requests no run can answer. Nil on `aura tools` and the one-shot
	// pipe, which have no operator and so advertise no elicitation at all.
	Elicitation mcptools.ElicitationConsent
```

In `cmd/aura/main.go`, after `handles.MCPFiles = &tools.MCPFileSink{Router: sandboxRouter}`, add:

```go
	handles.Elicitation = consent
```

Line 418 becomes:

```go
				return mcptools.MountServer(mountCtx, c, reg, name, mcpServers[name], stdioMountOptions(&handles))
```

In `cmd/aura/mcp_tools.go`, replace `mcpMountOptions` and add `stdioMountOptions` after it:

```go
// mcpMountOptions is the option set every runtime mount of a managed server uses --
// at boot and live after an authorization -- so neither can forget one the other
// carries. That already happened once with the grant store (runtimeMCPOAuth).
func mcpMountOptions(ctx context.Context, strict bool, server mcp.ManagedServer, handles *runtimeToolHandles) mcptools.MountOptions {
	return mcptools.MountOptions{
		Egress:      mcp.RuntimeEgressPolicy(strict, server),
		Views:       handles.MCPViews,
		OAuth:       runtimeMCPOAuth(ctx),
		Files:       handles.MCPFiles,
		Elicitation: handles.Elicitation,
	}
}

// stdioMountOptions is the same for a stdio-configured server, which needs no
// Egress or OAuth: those shape an HTTP connection.
func stdioMountOptions(handles *runtimeToolHandles) mcptools.MountOptions {
	return mcptools.MountOptions{Files: handles.MCPFiles, Elicitation: handles.Elicitation}
}
```

In `internal/agent/mcptools/mount.go`, replace the tail of `MountServer`'s comment (lines 26-31, from "cmd/aura's boot mounts" to "shape an HTTP connection.") with:

```go
// carries the per-mount choices. cmd/aura's boot mounts a stdio-configured server
// through here with stdioMountOptions (Files, Elicitation); a managed server, stdio
// or HTTP, goes through MountManagedServerWithOptions with mcpMountOptions (Egress,
// Views, OAuth, Files, Elicitation) at boot and live. Every runtime mount is given
// Elicitation, so every one advertises form elicitation; `aura tools` and the
// one-shot pipe pass none. A stdio mount has no use for Egress or OAuth, which
// shape an HTTP connection.
```

Replace `MountOptions`'s second paragraph (41-45) with:

```go
// A zero Elicitation is meaningful, not merely absent: it leaves
// SessionOptions.Elicitation nil, and a nil ClientOptions.ElicitationHandler means
// the client does NOT advertise the capability. The paths with no operator at all
// pass none. Every other mount passes the composition root's fallback, and a form
// asked inside a cockpit run reaches that run's own elicit.Asker first
// (elicitation_route.go).
```

At 124 and 181, rename the local `elicit` to `handler`: `handler := elicitationHandlerFor(name, opts.Elicitation)` and `o.Elicitation = handler`.

- [ ] **Step 4: Run the packages.** Go: `go vet ./cmd/aura/ ./internal/agent/mcptools/`, then `go test -race -count=1 -run 'MCPMountOptions|CarriesTheElicitationFallback|StillHandsLiveMountsThe|BuildRegistryWithMCP' ./cmd/aura/`, then `go test -race -count=1 ./internal/agent/mcptools/`.

Expected: `ok`. `TestBuildRegistryWithMCP_HandsMountsTheBoxFileSink` still passes with `nil` consent.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git commit -F - -- cmd/aura/runtime_tool_handles.go cmd/aura/main.go cmd/aura/mcp_tools.go cmd/aura/mcp_mount_options_test.go internal/agent/mcptools/mount.go <<'EOF'
feat(aura): hand every runtime mount the elicitation fallback

buildRegistryWithMCP took a consent parameter and had dropped it since
89688bd27, so no mount advertised elicitation. It now stores it on the
runtime handles, before the empty-set early return, because live
mounts read their options off those handles too. mcpMountOptions and
the new stdioMountOptions both carry it.

`aura tools` and the one-shot pipe still pass nil. They have no
operator, and a nil consent keeps the capability unadvertised.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 6: A detached run carries the form and takes the answer

**Files:**
- Create:
  - `internal/agui/run_elicitation.go`
  - `internal/agui/server_run_elicitation.go`
  - `internal/agui/run_elicitation_test.go`
  - `internal/agui/server_run_elicitation_e2e_test.go`
- Modify: `internal/agui/runsession.go`, in three places:
  - the struct at 52-84 gains `questions`;
  - `newRunSession` at 89-108;
  - the `append` comment at 110-117.
  - It also gains `publish`.
- Modify: `internal/agui/server_run_detach.go`, in three places:
  - the `detachedRunContext` comment at 33-39;
  - the asker installation after line 106;
  - `runProducer` at 142-143.
- Modify: `internal/agui/server.go:365`, which mounts the route.
- Modify: `internal/agui/idempotency_http.go:68`, which adds the inventory entry.

**Interfaces:**
- Consumes:
  - `elicit.Question`, `elicit.Answer`, `elicit.Validate`, `elicit.FieldErrors`, `elicit.ErrExpired`, `elicit.WithAsker`, `elicit.Action*` (Task 3);
  - the mcptools handler's contract (Task 4): it calls `Ask` with every clock held and reads its outcome as final.
- Produces:
  - **The two CUSTOM events** (Task 9 parses them):
    - `aura.elicitation`, whose value is `{run_id, id, server, tool, message, fields: Field[], deadline (RFC 3339), refusal?}`;
    - `aura.elicitation_resolved`, whose value is `{id, action, expired?}`.
  - **The route** `POST /agent/runs/{runID}/elicitations/{id}`, with body `{action, content?}`:

    | Code | Body | When |
    |---|---|---|
    | 202 | `{"status":"delivered"}` | the answer was delivered |
    | 400 | | an unknown action or an invalid body |
    | 404 | | the run or the question is not the caller's |
    | 409 | `{"error":"question already resolved"}` | the question is closed |
    | 410 | | the run is terminal |
    | 422 | `{"errors":{field: message}}` | the content fails `elicit.Validate` |

  - `RunSession.publish(ctx, ev) bool`.

- [ ] **Step 1: Write the failing tests.** Create `internal/agui/run_elicitation_test.go`:

```go
package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/elicit"
)

type askResult struct {
	answer elicit.Answer
	err    error
}

func nameQuestion(t *testing.T) elicit.Question {
	t.Helper()
	schema, err := elicit.DecodeSchema(map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	})
	if err != nil {
		t.Fatal(err)
	}
	q, err := elicit.FromSchema("forms", "ask_name", "what is your name", schema)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// openRun starts a run owned by the local identity, as the route resolves it,
// and subscribes to its stream from the first frame.
func openRun(t *testing.T) (*Server, *httptest.Server, *RunSession, <-chan seqEvent) {
	t.Helper()
	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")}, &fakeConvStore{}, ServerConfig{})
	sess, err := s.runs.Start(runParams{runID: "run-" + uuid.NewString(), threadID: "t-forms", identityID: localIdentityID})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, cancel, _ := sess.subscribeFrom(0)
	t.Cleanup(cancel)
	t.Cleanup(sess.finish)
	// Registered last, so it runs first: releases any Ask a test left waiting.
	t.Cleanup(sess.questions.cancelAll)
	return s, srv, sess, ch
}

// ask puts q to the run's asker on its own goroutine.
func ask(ctx context.Context, sess *RunSession, q elicit.Question) <-chan askResult {
	out := make(chan askResult, 1)
	go func() {
		answer, err := sess.questions.Ask(ctx, q)
		out <- askResult{answer: answer, err: err}
	}()
	return out
}

// nextCustom reads the session's stream up to the next CUSTOM frame named name.
func nextCustom(t *testing.T, ch <-chan seqEvent, name string) any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case sev, ok := <-ch:
			if !ok {
				t.Fatalf("the stream closed before a %s frame", name)
			}
			if ce, isCustom := sev.Ev.(*events.CustomEvent); isCustom && ce.Name == name {
				return ce.Value
			}
		case <-timeout:
			t.Fatalf("no %s frame within 5s", name)
			return nil
		}
	}
}

func answerPost(t *testing.T, srv *httptest.Server, runID, id, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/agent/runs/"+runID+"/elicitations/"+id, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST answer: %v", err)
	}
	return resp.StatusCode, readFullBody(t, resp)
}

func result(t *testing.T, got <-chan askResult) askResult {
	t.Helper()
	select {
	case r := <-got:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("Ask never returned")
		return askResult{}
	}
}

func TestAnAnswerIsDeliveredOnce(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	if q.RunID != sess.RunID || q.Server != "forms" || q.Tool != "ask_name" || len(q.Fields) != 1 {
		t.Fatalf("frame = %+v", q)
	}

	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("first answer = %d %s, want 202", status, body)
	}
	if r := result(t, got); r.err != nil || r.answer.Action != elicit.ActionAccept || r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.ID != q.ID || resolved.Action != elicit.ActionAccept {
		t.Fatalf("resolved = %+v", resolved)
	}
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"decline"}`); status != http.StatusConflict {
		t.Fatalf("a late answer = %d, want 409", status)
	}
}

// Review Focus 5.
func TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{}}`)
	var refusal struct {
		Errors map[string]string `json:"errors"`
	}
	if status != http.StatusUnprocessableEntity || json.Unmarshal([]byte(body), &refusal) != nil || refusal.Errors["name"] != elicit.ErrRequired {
		t.Fatalf("answer without the required field = %d %s, want 422 naming it", status, body)
	}
	if status, body := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
		t.Fatalf("the corrected answer = %d %s, want 202: a 422 must leave the question open", status, body)
	}
	if r := result(t, got); r.answer.Content["name"] != "Ada" {
		t.Fatalf("Ask = %+v", r)
	}
}

func TestTheRouteRefusesWhatItCannotDeliver(t *testing.T) {
	s, srv, sess, ch := openRun(t)
	ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	foreign, err := s.runs.Start(runParams{runID: "run-88888888-8888-8888-8888-888888888888", threadID: "t-foreign", identityID: "33333333-3333-3333-3333-333333333333"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(foreign.finish)
	for name, tc := range map[string]struct {
		runID, id, body string
		want            int
	}{
		"unknown question": {sess.RunID, "no-such-question", `{"action":"decline"}`, http.StatusNotFound},
		"foreign run":      {foreign.RunID, q.ID, `{"action":"decline"}`, http.StatusNotFound},
		"unknown action":   {sess.RunID, q.ID, `{"action":"sure"}`, http.StatusBadRequest},
		"not json":         {sess.RunID, q.ID, `accept`, http.StatusBadRequest},
	} {
		if status, body := answerPost(t, srv, tc.runID, tc.id, tc.body); status != tc.want {
			t.Errorf("%s: %d %s, want %d", name, status, body, tc.want)
		}
	}
	sess.finish()
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"decline"}`); status != http.StatusGone {
		t.Fatalf("an answer to an ended run = %d, want 410", status)
	}
}

func TestAnExpiredQuestionIsResolvedAndClosed(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	got := ask(ctx, sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)

	cancel(elicit.ErrExpired)
	if r := result(t, got); !errors.Is(r.err, elicit.ErrExpired) {
		t.Fatalf("Ask = %+v, want the expiry back", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionDecline || !resolved.Expired {
		t.Fatalf("resolved = %+v, want an expired decline", resolved)
	}
	if status, _ := answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusConflict {
		t.Fatalf("an answer after expiry = %d, want 409", status)
	}
}

func TestACallThatEndsCancelsItsQuestion(t *testing.T) {
	_, _, sess, ch := openRun(t)
	ctx, cancel := context.WithCancel(context.Background())
	got := ask(ctx, sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)

	cancel()
	if r := result(t, got); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("Ask = %+v, want the call's cancellation", r)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel || resolved.Expired {
		t.Fatalf("resolved = %+v, want a cancel", resolved)
	}
}

func TestTheRunEndingCancelsEveryPendingQuestion(t *testing.T) {
	_, _, sess, ch := openRun(t)
	first := ask(context.Background(), sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)
	second := ask(context.Background(), sess, nameQuestion(t))
	nextCustom(t, ch, ElicitationEventName)

	sess.questions.cancelAll()
	for _, got := range []<-chan askResult{first, second} {
		if r := result(t, got); r.err != nil || r.answer.Action != elicit.ActionCancel {
			t.Fatalf("Ask = %+v, want cancel when the run ends", r)
		}
	}
	for range 2 {
		if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionCancel {
			t.Fatalf("resolved = %+v", resolved)
		}
	}
	if r, err := sess.questions.Ask(context.Background(), nameQuestion(t)); err != nil || r.Action != elicit.ActionCancel {
		t.Fatalf("a question put after the run ended = %+v, %v; want cancel at once", r, err)
	}
}

func TestARefusedQuestionIsShownAlreadyResolved(t *testing.T) {
	_, _, sess, ch := openRun(t)
	q := elicit.Question{Server: "forms", Tool: "ask_name", Refusal: elicit.RefusalAmbiguousRun}

	if r, err := sess.questions.Ask(context.Background(), q); err != nil || r.Action != elicit.ActionDecline {
		t.Fatalf("Ask = %+v, %v; a refusal declines at once", r, err)
	}
	if shown := nextCustom(t, ch, ElicitationEventName).(elicitationFrame); shown.Refusal != elicit.RefusalAmbiguousRun {
		t.Fatalf("shown = %+v", shown)
	}
	if resolved := nextCustom(t, ch, ElicitationResolvedEventName).(elicitationResolvedFrame); resolved.Action != elicit.ActionDecline {
		t.Fatalf("resolved = %+v", resolved)
	}
}

func TestTheEventsCarryNoAnswerValues(t *testing.T) {
	_, srv, sess, ch := openRun(t)
	got := ask(context.Background(), sess, nameQuestion(t))
	q := nextCustom(t, ch, ElicitationEventName).(elicitationFrame)
	answerPost(t, srv, sess.RunID, q.ID, `{"action":"accept","content":{"name":"Zebedee"}}`)
	result(t, got)

	sess.finish()
	replay, _, _ := sess.subscribeFrom(0)
	for sev := range replay {
		raw, err := json.Marshal(sev.Ev)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "Zebedee") {
			t.Fatalf("frame %d carries the operator's value: %s", sev.Seq, raw)
		}
	}
}

func TestPublishInterleavesWithTheProducer(t *testing.T) {
	_, _, sess, ch := openRun(t)
	// CUSTOM is a lifecycle frame: append blocks on a subscriber that stops reading.
	go func() {
		for range ch {
		}
	}()
	const each = 200
	var wg sync.WaitGroup
	for _, publisher := range []func(context.Context, events.Event) bool{sess.append, sess.publish} {
		wg.Go(func() {
			for i := range each {
				publisher(context.Background(), events.NewCustomEvent("probe", events.WithValue(i)))
			}
		})
	}
	wg.Wait()
	sess.finish()
	replay, _, _ := sess.subscribeFrom(0)
	var last int64
	count := 0
	for sev := range replay {
		if sev.Seq != last+1 {
			t.Fatalf("seq %d after %d: a publish broke the ring's order", sev.Seq, last)
		}
		last = sev.Seq
		count++
	}
	if count != 2*each {
		t.Fatalf("replayed %d frames, want %d", count, 2*each)
	}
}
```

Create `internal/agui/server_run_elicitation_e2e_test.go`:

```go
package agui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/mcptools"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/runner"
)

// server_run_elicitation_e2e_test.go drives a real runner, a real MCP mount and
// the real detached handler: a server's form reaches the SSE stream, the answer
// is POSTed, and the server greets the name it received.

func formSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{"name": map[string]any{"type": "string"}},
		"required":   []any{"name"},
	}
}

func greet(reply *sdkmcp.ElicitResult) *sdkmcp.CallToolResult {
	text := reply.Action
	if reply.Action == elicit.ActionAccept {
		text = fmt.Sprintf("hello %v", reply.Content["name"])
	}
	return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}}}
}

// formServer serves one tool, ask_name, over streamable HTTP. Classic narrows the
// server to 2025-11-25, the last protocol that allows elicitation/create during a
// call (go-sdk@v1.8.0 mcp/server.go:1619-1627).
func formServer(t *testing.T, classic bool) string {
	t.Helper()
	var opts *sdkmcp.ServerOptions
	if classic {
		opts = &sdkmcp.ServerOptions{SupportedProtocolVersions: []string{"2025-11-25"}}
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "forms", Version: "0.0.1"}, opts)
	tool := &sdkmcp.Tool{Name: "ask_name", Description: "Asks the operator for a name.", InputSchema: map[string]any{"type": "object"}}
	server.AddTool(tool, func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		if classic {
			reply, err := req.Session.Elicit(ctx, &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: formSchema()})
			if err != nil {
				return nil, err
			}
			return greet(reply), nil
		}
		if reply, ok := req.Params.InputResponses["who"].(*sdkmcp.ElicitResult); ok {
			return greet(reply), nil
		}
		return &sdkmcp.CallToolResult{InputRequests: sdkmcp.InputRequestMap{
			"who": &sdkmcp.ElicitParams{Mode: "form", Message: "what is your name", RequestedSchema: formSchema()},
		}}, nil
	})
	ts := httptest.NewServer(sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server }, nil))
	t.Cleanup(func() {
		for session := range server.Sessions() {
			_ = session.Close()
		}
		ts.Close()
	})
	return ts.URL
}

// unreachableFallback fails the test if a detached cockpit run's form ever falls
// back to decline-and-surface.
type unreachableFallback struct{ t *testing.T }

func (f unreachableFallback) AskOperator(context.Context, elicit.Question) (string, map[string]any, error) {
	f.t.Errorf("the fallback consent was asked: a detached run must be asked through its own asker")
	return elicit.ActionDecline, nil, nil
}

// mountForms mounts the fixture the way production mounts a managed server.
func mountForms(t *testing.T, reg *tools.Registry, url string) string {
	t.Helper()
	server := mcp.ManagedServer{URL: url, Type: mcp.ServerTypeStreamableHTTP, Env: []string{"MCP_OAUTH_DISABLED=true"}}
	closer, names, _, err := mcptools.MountManagedServerWithOptions(context.Background(), context.Background(), reg, "forms", server,
		mcptools.MountOptions{Egress: mcp.RuntimeEgressPolicy(false, server), Elicitation: unreachableFallback{t}})
	if err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { _ = closer() })
	if len(names) != 1 {
		t.Fatalf("mounted %v, want one tool", names)
	}
	return names[0]
}

// newRealFormRunner is newRealSteerRunner without the steer inbox and with a
// short wallclock, over a registry the test has already mounted into.
func newRealFormRunner(t *testing.T, client llm.Client, reg *tools.Registry, wallclockSec int) (*runner.Runner, *steerE2EConvStore) {
	t.Helper()
	conv := newSteerE2EConvStore()
	r := runner.New(runner.Deps{
		Conv:            conv,
		Pause:           steerE2EPauseStore{},
		ApprovalExpiry:  steerE2EPauseStore{},
		Identity:        steerE2EIdentityStore{},
		CacheMetrics:    steerE2ECacheMetricStore{},
		ToolInvocations: steerE2EToolInvocationStore{},
		Client:          agenttest.TitleClient{Main: client, Title: agenttest.NewFakeClient(agenttest.TextChunks("stop", "Form test conversation"))},
		Registry:        reg,
		LLM:             llm.Config{Model: "test-model", ContextWindow: 1000000, MaxOutputTokens: 32768, LoopMaxWallclockSec: wallclockSec},
		TitleTimeout:    2 * time.Second,
		StopTimeout:     2 * time.Second,
	})
	return r, conv
}

// sseData streams an SSE body's data payloads until the body ends.
func sseData(body io.Reader) <-chan string {
	out := make(chan string, 1024)
	go func() {
		defer close(out)
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				out <- data
			}
		}
	}()
	return out
}

func awaitQuestion(t *testing.T, frames <-chan string) elicitationFrame {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				t.Fatal("the stream ended before the form arrived")
			}
			var frame struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			}
			if json.Unmarshal([]byte(data), &frame) != nil || frame.Name != ElicitationEventName {
				continue
			}
			var q elicitationFrame
			if err := json.Unmarshal(frame.Value, &q); err != nil {
				t.Fatalf("decode %s: %v", ElicitationEventName, err)
			}
			return q
		case <-timeout:
			t.Fatalf("no %s frame within 10s", ElicitationEventName)
		}
	}
}

func drainFrames(t *testing.T, frames <-chan string) string {
	t.Helper()
	var b strings.Builder
	timeout := time.After(20 * time.Second)
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return b.String()
			}
			b.WriteString(data + "\n")
		case <-timeout:
			t.Fatalf("the run did not end within 20s; so far:\n%s", b.String())
		}
	}
}

// Review Focus 1 end to end, and Review Focus 4's replay. The operator takes 3 s,
// over a 1 s MCP call bound and a 2 s run wallclock.
func TestDetachedRunAnswersAnMCPFormWhileBothClocksStop(t *testing.T) {
	for _, mode := range []struct {
		name    string
		classic bool
	}{{"mrtr", false}, {"classic", true}} {
		t.Run(mode.name, func(t *testing.T) {
			t.Setenv("AURA_MCP_CALL_TIMEOUT_SEC", "1")
			reg := tools.NewRegistry()
			reg.Register(tools.TextResponse{})
			toolName := mountForms(t, reg, formServer(t, mode.classic))
			client := agenttest.NewFakeClient(
				agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", toolName, "{}")),
				agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
			)
			r, conv := newRealFormRunner(t, client, reg, 2)
			convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
			if err != nil {
				t.Fatalf("NewConversationWithID: %v", err)
			}
			_, srv := newDetachTestServer(t, r, conv, ServerConfig{})

			resp := postRun(t, srv, steerRunPayload(convID))
			defer resp.Body.Close()
			frames := sseData(resp.Body)
			q := awaitQuestion(t, frames)
			if q.Server != "forms" || q.Tool != "ask_name" || len(q.Fields) != 1 || q.Fields[0].Name != "name" {
				t.Fatalf("question = %+v", q)
			}

			time.Sleep(3 * time.Second)
			if status, body := answerPost(t, srv, q.RunID, q.ID, `{"action":"accept","content":{"name":"Ada"}}`); status != http.StatusAccepted {
				t.Fatalf("answer = %d %s, want 202", status, body)
			}
			rest := drainFrames(t, frames)
			if !strings.Contains(rest, `"type":"RUN_FINISHED"`) || !strings.Contains(rest, ElicitationResolvedEventName) {
				t.Fatalf("the run did not finish with the form resolved:\n%s", rest)
			}
			reqs := client.RecordedRequests()
			if len(reqs) < 2 || !strings.Contains(joinMessageContents(reqs[1].Messages), "hello Ada") {
				t.Fatalf("the model never saw the server's greeting: %+v", reqs)
			}

			replay := readFullBody(t, mustGet(t, srv.URL+"/agent/runs/"+q.RunID+"/events"))
			if strings.Count(replay, `"name":"`+ElicitationEventName+`"`) != 1 || strings.Count(replay, `"name":"`+ElicitationResolvedEventName+`"`) != 1 {
				t.Fatalf("a replay must carry the question and its resolution once each:\n%s", replay)
			}
		})
	}
}

// sleepyTool waits 3 s holding no clock: the control that shows the harness's 2 s
// wallclock is real, without which the held test above would prove nothing.
type sleepyTool struct{}

func (sleepyTool) Spec() tools.Spec {
	return tools.Spec{Name: "sleepy", Summary: "waits three seconds", Parameters: json.RawMessage(`{"type":"object"}`)}
}

func (sleepyTool) Execute(ctx context.Context, _ json.RawMessage) (tools.ToolResult, error) {
	select {
	case <-ctx.Done():
		return tools.ToolResult{}, ctx.Err()
	case <-time.After(3 * time.Second):
		return tools.ToolResult{Preview: "slept"}, nil
	}
}

func TestTheShortWallclockCutsAnUnheldTool(t *testing.T) {
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(sleepyTool{})
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "sleepy", "{}")),
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
	)
	r, conv := newRealFormRunner(t, client, reg, 2)
	convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
	if err != nil {
		t.Fatalf("NewConversationWithID: %v", err)
	}
	_, srv := newDetachTestServer(t, r, conv, ServerConfig{})

	readFullBody(t, postRun(t, srv, steerRunPayload(convID)))
	for i, req := range client.RecordedRequests() {
		if strings.Contains(joinMessageContents(req.Messages), "slept") {
			t.Fatalf("request %d carries the tool's result: the 2 s wallclock never cut the 3 s tool", i)
		}
	}
}

func mustGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}
```

- [ ] **Step 2: Run them to verify they fail.** Go: `go vet ./internal/agui/`.

Expected: build FAIL with `undefined: ElicitationEventName`, `elicitationFrame`, `elicitationResolvedFrame`, and `sess.questions undefined (type *RunSession has no field or method questions)`.

- [ ] **Step 3: Implement.** Create `internal/agui/run_elicitation.go`:

```go
package agui

import (
	"context"
	"errors"
	"sync"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/elicit"
)

// run_elicitation.go is a detached run's elicit.Asker. A mounted MCP server's form
// is published into the run's own stream, so a reload brings it back through the
// replay ring, and the answer arrives on POST
// /agent/runs/{runID}/elicitations/{id} (server_run_elicitation.go). Nothing is
// persisted: the server's request does not survive a restart either.

// The CUSTOM events a question and its outcome travel as. Neither carries an
// answer's values: only the action reaches the stream.
const (
	ElicitationEventName         = "aura.elicitation"
	ElicitationResolvedEventName = "aura.elicitation_resolved"
)

// elicitationFrame is the question as the cockpit receives it. run_id rides along
// because the answer is posted to the run, and a card restored from a replay has
// no other place to read it from.
type elicitationFrame struct {
	RunID string `json:"run_id"`
	elicit.Question
}

type elicitationResolvedFrame struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	// Expired tells an expiry from a decline the operator chose: both are a decline
	// to the server, and only the card needs the difference.
	Expired bool `json:"expired,omitempty"`
}

var (
	errQuestionUnknown = errors.New("question not found")
	errQuestionClosed  = errors.New("question already resolved")
)

type pendingQuestion struct {
	q      elicit.Question
	answer chan elicit.Answer // buffered: the closer never waits for Ask
}

// runQuestions holds a run's open questions. mu also orders the stream: a question
// is published under it and so is its resolution, so a resolution can never reach
// a subscriber before the question it closes, and cancelAll, which runs before the
// session turns terminal, waits for any close already publishing.
type runQuestions struct {
	sess *RunSession

	mu      sync.Mutex
	pending map[string]*pendingQuestion
	closed  map[string]struct{}
	ended   bool
}

func newRunQuestions(sess *RunSession) *runQuestions {
	return &runQuestions{sess: sess, pending: map[string]*pendingQuestion{}, closed: map[string]struct{}{}}
}

// Ask implements elicit.Asker. It returns when the operator answers, when ctx ends
// (with context.Cause(ctx)), or at once for a refusal or a run that has ended. An
// answer delivered as ctx ends wins: it was already published as the outcome.
func (rq *runQuestions) Ask(ctx context.Context, q elicit.Question) (elicit.Answer, error) {
	q.ID = uuid.NewString()
	p := &pendingQuestion{q: q, answer: make(chan elicit.Answer, 1)}
	rq.mu.Lock()
	if rq.ended {
		rq.mu.Unlock()
		return elicit.Answer{Action: elicit.ActionCancel}, nil
	}
	rq.pending[q.ID] = p
	rq.sess.publish(ctx, events.NewCustomEvent(ElicitationEventName, events.WithValue(elicitationFrame{RunID: rq.sess.RunID, Question: q})))
	rq.mu.Unlock()

	if q.Refusal != "" {
		rq.close(q.ID, elicit.ActionDecline, false)
		return elicit.Answer{Action: elicit.ActionDecline}, nil
	}
	select {
	case a := <-p.answer:
		return a, nil
	case <-ctx.Done():
		cause := context.Cause(ctx)
		expired := errors.Is(cause, elicit.ErrExpired)
		action := elicit.ActionCancel
		if expired {
			action = elicit.ActionDecline
		}
		if !rq.close(q.ID, action, expired) {
			return <-p.answer, nil
		}
		return elicit.Answer{}, cause
	}
}

// answer delivers the operator's answer. An accept that fails the server's schema
// leaves the question open and returns the per-field errors.
func (rq *runQuestions) answer(id string, a elicit.Answer) (elicit.FieldErrors, error) {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	p, ok := rq.pending[id]
	if !ok {
		if _, done := rq.closed[id]; done {
			return nil, errQuestionClosed
		}
		return nil, errQuestionUnknown
	}
	if a.Action == elicit.ActionAccept {
		if errs := elicit.Validate(p.q.Schema, a.Content); errs != nil {
			return errs, nil
		}
	} else {
		a.Content = nil
	}
	rq.closeLocked(id, a.Action, false)
	p.answer <- a
	return nil, nil
}

// cancelAll resolves every pending question as cancel and refuses new ones: a form
// never outlives its run. The producer calls it as the stream ends, before finish.
func (rq *runQuestions) cancelAll() {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	rq.ended = true
	for id, p := range rq.pending {
		rq.closeLocked(id, elicit.ActionCancel, false)
		p.answer <- elicit.Answer{Action: elicit.ActionCancel}
	}
}

// close resolves a question once, whoever gets there first, and reports whether
// this call was the one.
func (rq *runQuestions) close(id, action string, expired bool) bool {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	return rq.closeLocked(id, action, expired)
}

func (rq *runQuestions) closeLocked(id, action string, expired bool) bool {
	if _, ok := rq.pending[id]; !ok {
		return false
	}
	delete(rq.pending, id)
	rq.closed[id] = struct{}{}
	// Background, not the asking ctx: a question closed because its call or its
	// run is ending must still reach the subscriber waiting on it, the same reason
	// the producer's own last frame outlives its cancellation.
	rq.sess.publish(context.Background(), events.NewCustomEvent(ElicitationResolvedEventName,
		events.WithValue(elicitationResolvedFrame{ID: id, Action: action, Expired: expired})))
	return true
}
```

Create `internal/agui/server_run_elicitation.go`:

```go
package agui

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"

	"github.com/chetto1983/aura/internal/elicit"
)

// server_run_elicitation.go carries POST /agent/runs/{runID}/elicitations/{id}: the
// operator's answer to a mounted MCP server's form (run_elicitation.go). It
// resolves the run through the same owner-scoped 404 ladder as steer and cancel.

type elicitationAnswerRequest struct {
	Action  string         `json:"action"`
	Content map[string]any `json:"content"`
}

var elicitationActions = []string{elicit.ActionAccept, elicit.ActionDecline, elicit.ActionCancel}

func (s *Server) handleRunElicitation(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.resolveRunSession(w, r)
	if !ok {
		return
	}
	if terminal, _ := sess.terminalState(); terminal {
		http.Error(w, "run has ended", http.StatusGone)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRunBodyBytes+1))
	if err != nil || len(body) > maxRunBodyBytes {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	var req elicitationAnswerRequest
	if err := json.Unmarshal(body, &req); err != nil || !slices.Contains(elicitationActions, req.Action) {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	fieldErrs, err := sess.questions.answer(r.PathValue("id"), elicit.Answer{Action: req.Action, Content: req.Content})
	switch {
	case errors.Is(err, errQuestionUnknown):
		http.Error(w, "question not found", http.StatusNotFound)
	case errors.Is(err, errQuestionClosed):
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": err.Error()})
	case fieldErrs != nil:
		writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"errors": fieldErrs})
	default:
		writeJSONStatus(w, http.StatusAccepted, map[string]string{"status": "delivered"})
	}
}
```

In `internal/agui/runsession.go`:

1. Add a field after `now func() time.Time`:

```go
	// questions is the run's elicit.Asker (run_elicitation.go), built with the
	// session so it exists before the producer or the route can reach it.
	questions *runQuestions
```

2. In `newRunSession`, bind the literal and set it:

```go
	s := &RunSession{
		// the existing fields, unchanged
	}
	s.questions = newRunQuestions(s)
	return s
```

3. Replace the sentence `Producer-only.` in `append`'s comment with: `Called by the producer and, through publish, by the run's question asker; both serialize on mu, so an asker's frame lands between two producer frames and never inside one.`

4. Add after `append`:

```go
// publish appends a frame that does not come from the turn's own stream, redacted
// like every producer frame (runProducer).
func (s *RunSession) publish(ctx context.Context, ev events.Event) bool {
	return s.append(ctx, redactEvent(ev))
}
```

In `internal/agui/server_run_detach.go`:

1. Append to `detachedRunContext`'s comment: `This cap stays fixed although the budget's deadline is pausable (internal/pausable). An MCP form holds the budget while the operator answers it, so this is the one bound left on a server that asks again and again; at the default 3600 s it cuts a legitimate run only after some 55 minutes of forms in one turn.`

2. After the `Start` error branch (line 106), insert:

```go
	// The run's own asker: a mounted MCP server's form reaches this run's stream
	// and waits for the operator (run_elicitation.go). A non-detached run gets none,
	// the same rule steer follows.
	dctx = elicit.WithAsker(dctx, sess.questions)
```

3. In `runProducer`, after `defer sess.finish()`, add:

```go
	// Deferred after finish so it runs before it: every form still open is resolved
	// as cancel while the session can still carry the frame.
	defer sess.questions.cancelAll()
```

4. Add `"github.com/chetto1983/aura/internal/elicit"` to the imports.

In `internal/agui/server.go`, after the steer route (line 365):

```go
	// A mounted MCP server's form, answered from the cockpit (run_elicitation.go).
	mux.HandleFunc("POST /agent/runs/{runID}/elicitations/{id}", s.handleRunElicitation)
```

In `internal/agui/idempotency_http.go`, after the steer entry (line 68):

```go
	// A replayed POST with the same Idempotency-Key returns the first answer's
	// response instead of meeting the 409 a second delivery would.
	"POST /agent/runs/{runID}/elicitations/{id}": httpMutationMeta("agent_run_elicitation_answer"),
```

- [ ] **Step 4: Run the package.** Go: `go vet ./internal/agui/`, then `go test -race -count=1 ./internal/agui/`.

Expected: `ok`. The integration test takes about 8 s: two modes of about 3.5 s each.
- It must not skip. It needs no container: the MCP server, the runner and the HTTP server are all in-process.
- `TestEveryRegisteredUnsafeHTTPRouteIsClassified` passes only because of the inventory entry. Check that it fails without the entry by removing the entry, running the test, and putting the entry back. Do this by hand, once. It shows the sweep sees the route; it is not a mutation run.
- If the tool call fails as unknown or not loaded, the one-tool mount came up deferred. Read `managedBridgePolicy` and its three-tool slot rule (`TestMount_ThreeToolServerEarnsAlwaysLoadedSlot`) before changing the fixture.
- `wc -l internal/agui/server.go internal/agui/runsession.go internal/agui/server_run_detach.go internal/agui/run_elicitation*.go` must show every file under 600 lines.
- Coverage: Go: `go test -race -count=1 -coverprofile=/tmp/agui.out ./internal/agui/ && go tool cover -func=/tmp/agui.out | grep -E 'run_elicitation|server_run_elicitation'`. Every function must be at 85% or above.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add internal/agui/run_elicitation.go internal/agui/server_run_elicitation.go internal/agui/run_elicitation_test.go internal/agui/server_run_elicitation_e2e_test.go
git commit -F - -- internal/agui/run_elicitation.go internal/agui/server_run_elicitation.go internal/agui/run_elicitation_test.go internal/agui/server_run_elicitation_e2e_test.go internal/agui/runsession.go internal/agui/server_run_detach.go internal/agui/server.go internal/agui/idempotency_http.go <<'EOF'
feat(agui): carry an MCP server's form in the run and take the answer

A detached run now installs its own elicit.Asker. A mounted server's
form is published as aura.elicitation into the run's replay ring, so a
reload brings it back. The answer arrives on
POST /agent/runs/{runID}/elicitations/{id}, owner-scoped like steer:
- 202 when delivered;
- 422 with per-field errors, and the question stays open;
- 409 once the question is closed;
- 410 on an ended run.

aura.elicitation_resolved closes each question once. Its cause is the
answer, the expiry, the call ending or the run ending. The resolution
is published under the same lock as the question, and before finish,
so no form outlives its run on anyone's screen.

The detached run's one-hour cap stays fixed on purpose. It is the one
bound left on a server that asks again and again.

Measured: a 3 s operator over a 1 s call bound and a 2 s run wallclock,
classic and multi-round-trip, through a real runner and a real mount.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 7: The question frame, and ask_user drawn in it

**Blocked on Open point 1.** This task is written for **option V**, the recommended one:
- register the `@tool-ui` registry;
- port Question Flow's and ApprovalCard's markup into Aura's own files, translated and tested;
- install no vendored file yet.

Option P installs the three components as well. It keeps every step below and adds the block at the end of this task.

**Files:**
- Modify: `web/components.json`, adding `@tool-ui` to `registries`.
- Create:
  - `web/src/questions/QuestionCard.tsx`
  - `web/src/questions/QuestionOptions.tsx`
  - `web/src/questions/QuestionReceipt.tsx`
  - `web/src/questions/CancelControl.tsx`
  - `web/src/i18n/resources.questions.ts`
- Modify: `web/src/i18n/resources.ts`, which imports the bundle and spreads it into both locales, as `...updateEn` is spread.
- Modify: `web/src/approvals/InlineApprovalCard.tsx`, rewritten from line 1 to 371.
- Modify: `web/src/approvals/approvalState.ts`, adding `isDestructiveApproval`.
- Test:
  - Create `web/src/questions/__tests__/QuestionFrame.test.tsx`.
  - Modify `web/src/approvals/__tests__/InlineApprovalCard.test.tsx` (tests 89-97 and 123-143), `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx` (lines 132, 206 and 213) and `web/src/approvals/__tests__/approvalState.test.ts`.
  - Modify `web/e2e/chat.spec.ts:472-482` and `web/e2e/chat-calm-prism.spec.ts:232`.
- Modify: `web/stryker.config.json`, adding the four `src/questions/*.tsx` files and `src/approvals/InlineApprovalCard.tsx` to `mutate`.

**Interfaces:**
- Produces, used by Task 8:
  - `QuestionCard` with props:
    - `titleId: string`, `title: ReactNode`;
    - `description?: ReactNode`, `descriptionId?: string`;
    - `icon?: ReactNode`, `header?: ReactNode`;
    - `step?: {current: number; total: number}`;
    - `variant?: 'default' | 'destructive'`;
    - `footer?: ReactNode`, `status?: ReactNode`;
    - `dataAttributes?: Record<\`data-${string}\`, string>`;
    - `children?: ReactNode`.
  - `QuestionOptions` with props `{labelledBy, options: QuestionOption[], mode: 'single' | 'multi', selected: ReadonlySet<string>, disabled?, onToggle(id), onSubmit?()}`, where `QuestionOption = {id, label, description?}`.
  - `QuestionReceipt` with props `{tone: ReceiptTone, label, summary?: {label, value}[], announce?}`, where `ReceiptTone = 'success' | 'neutral' | 'warning' | 'danger'`.
  - `CancelControl` with props `{isStreaming?: boolean, disabled: boolean, labels: {cancel, confirm, yes, no}, onCancel()}`.
  - The i18n bundle `questionCard.*`.
- Kept: `InlineApprovalCard`'s props and resolve lifecycle are unchanged (`onResolutionStarted`, `onResolved`, `onResolutionFailed`, attempt ids). So `ThreadApprovalCards`, `useThreadApprovals` and `useApprovalFocus` (`[data-approval-token]`) need no change.

**What moves where:**

| Today | After |
|---|---|
| option buttons that submit on click (`InlineApprovalCard.tsx:168-185`) | radio rows plus a pill **Answer** that stays grey until a row is chosen (spec §Cockpit) |
| `kind: approval`: free text plus **Answer** | an **Approve** pill; the gateway's scopes become single-choice rows; the destructive variant when `presentation.params.risk === 'destructive'` (`scoring.Destructive`, `internal/scoring/scoring.go:26`) |
| the inline Cancel confirmation (`:231-275`) | `CancelControl`, shared with Task 8, with the same copy, focus moves and `min-h-11` |
| `TerminalChip` (`:333-371`) | `QuestionReceipt`, which adds the answer given under an **Answered** chip; a success chip shows a check instead of the dot |

- [ ] **Step 1: Write the failing tests.** Create `web/src/questions/__tests__/QuestionFrame.test.tsx`:

```tsx
import { describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import { CancelControl } from '../CancelControl';
import { QuestionCard } from '../QuestionCard';
import { QuestionOptions, type QuestionOption } from '../QuestionOptions';
import { QuestionReceipt } from '../QuestionReceipt';

const OPTIONS: QuestionOption[] = [
  { id: 'rome', label: 'Rome' },
  { id: 'milan', label: 'Milan', description: 'the north' },
  { id: 'turin', label: 'Turin' },
];

const LABELS = { cancel: 'Cancel run', confirm: 'Stop this run?', yes: 'Stop run', no: 'Keep running' };

describe('QuestionCard', () => {
  it('is a form named by its title, with the description kept as plain, wrapped text', () => {
    render(
      <QuestionCard titleId="t" title="Pick a city" descriptionId="d" description={'line one\n<b>two</b>'}>
        <span>body</span>
      </QuestionCard>,
    );
    const form = screen.getByRole('form', { name: 'Pick a city' });
    expect(form.getAttribute('data-slot')).toBe('card');
    expect(form.getAttribute('data-variant')).toBe('default');
    const description = screen.getByText((_, el) => el?.textContent === 'line one\n<b>two</b>');
    expect(description.className).toMatch(/whitespace-pre-wrap/);
    expect(form.querySelector('b')).toBeNull();
  });

  it('shows the step label and the segmented bar only on a form of more than one step', () => {
    const { rerender } = render(<QuestionCard titleId="t" title="One" step={{ current: 1, total: 1 }} />);
    expect(screen.queryByRole('progressbar')).toBeNull();
    rerender(<QuestionCard titleId="t" title="Two" step={{ current: 2, total: 3 }} />);
    expect(screen.getByText('Step 2 of 3')).toBeTruthy();
    const bar = screen.getByRole('progressbar');
    expect(bar.getAttribute('aria-valuenow')).toBe('2');
    expect(bar.getAttribute('aria-valuemax')).toBe('3');
    expect(bar.querySelectorAll('.scale-x-100')).toHaveLength(2);
  });

  it('carries its variant and the data attributes an adapter asks for', () => {
    render(<QuestionCard titleId="t" title="Risky" variant="destructive" dataAttributes={{ 'data-approval-token': 'tok' }} />);
    const form = screen.getByRole('form', { name: 'Risky' });
    expect(form.getAttribute('data-variant')).toBe('destructive');
    expect(form.getAttribute('data-approval-token')).toBe('tok');
  });
});

describe('QuestionOptions', () => {
  function renderOptions(mode: 'single' | 'multi', selected: ReadonlySet<string> = new Set()) {
    const onToggle = vi.fn();
    const onSubmit = vi.fn();
    render(
      <>
        <span id="lbl">Cities</span>
        <QuestionOptions labelledBy="lbl" options={OPTIONS} mode={mode} selected={selected} onToggle={onToggle} onSubmit={onSubmit} />
      </>,
    );
    return { onToggle, onSubmit };
  }

  it('is a listbox of options that marks the selected row', () => {
    renderOptions('multi', new Set(['milan']));
    const list = screen.getByRole('listbox', { name: 'Cities' });
    expect(list.getAttribute('aria-multiselectable')).toBe('true');
    expect(screen.getByRole('option', { name: /Milan/ }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('option', { name: 'Rome' }).getAttribute('aria-selected')).toBe('false');
    expect(screen.getByText('the north')).toBeTruthy();
  });

  it('moves with the arrow keys, Home and End, and wraps', () => {
    renderOptions('single');
    const list = screen.getByRole('listbox');
    const [rome, milan, turin] = screen.getAllByRole('option');
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(milan);
    fireEvent.keyDown(list, { key: 'End' });
    expect(document.activeElement).toBe(turin);
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    expect(document.activeElement).toBe(rome);
    fireEvent.keyDown(list, { key: 'ArrowUp' });
    expect(document.activeElement).toBe(turin);
    fireEvent.keyDown(list, { key: 'Home' });
    expect(document.activeElement).toBe(rome);
  });

  it('selects with Space or Enter, and Enter on the chosen single row submits', () => {
    const { onToggle, onSubmit } = renderOptions('single', new Set(['rome']));
    const list = screen.getByRole('listbox');
    fireEvent.keyDown(list, { key: 'Enter' });
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onToggle).not.toHaveBeenCalled();
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    fireEvent.keyDown(list, { key: ' ' });
    expect(onToggle).toHaveBeenCalledWith('milan');
    fireEvent.click(screen.getByRole('option', { name: 'Turin' }));
    expect(onToggle).toHaveBeenLastCalledWith('turin');
  });

  it('never submits from a multi-choice list', () => {
    const { onToggle, onSubmit } = renderOptions('multi', new Set(['rome']));
    fireEvent.keyDown(screen.getByRole('listbox'), { key: 'Enter' });
    expect(onToggle).toHaveBeenCalledWith('rome');
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe('QuestionReceipt', () => {
  it('shows the chip in its tone and the answer given', () => {
    render(<QuestionReceipt tone="success" label="Answered." summary={[{ label: 'Your answer', value: 'Milan' }]} />);
    expect(screen.getByText('Answered.').closest('[data-tone]')?.getAttribute('data-tone')).toBe('success');
    expect(screen.getByText('Your answer')).toBeTruthy();
    expect(screen.getByText('Milan')).toBeTruthy();
  });

  it('announces itself only when asked to', () => {
    const { rerender } = render(<QuestionReceipt tone="warning" label="Expired." />);
    expect(screen.queryByRole('status')).toBeNull();
    rerender(<QuestionReceipt tone="warning" label="Expired." announce />);
    expect(screen.getByRole('status').textContent).toContain('Expired.');
  });
});

describe('CancelControl', () => {
  it('cancels at once while idle', () => {
    const onCancel = vi.fn();
    render(<CancelControl isStreaming={false} disabled={false} labels={LABELS} onCancel={onCancel} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });

  it('asks first while streaming, and Escape keeps the run going', async () => {
    const onCancel = vi.fn();
    render(<CancelControl isStreaming disabled={false} labels={LABELS} onCancel={onCancel} />);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    const keep = screen.getByRole('button', { name: 'Keep running' });
    await waitFor(() => {
      expect(document.activeElement).toBe(keep);
    });
    expect(keep.className).toContain('min-h-11');
    fireEvent.keyDown(keep, { key: 'Escape' });
    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByRole('button', { name: 'Cancel run' }));
    });
    expect(onCancel).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Cancel run' }));
    fireEvent.click(screen.getByRole('button', { name: 'Stop run' }));
    expect(onCancel).toHaveBeenCalledTimes(1);
  });
});
```

In `web/src/approvals/__tests__/InlineApprovalCard.test.tsx`, replace the first test (89-97) and `Answer (option) resolves …` (123-143) with the four tests below, and add the rest after the last test.
- The rewrite has to change these two tests, and the reason is the spec, not the tests: a choice is now a radio row plus a pill **Answer**, so a click on a row no longer submits.
- Every other assertion in the file stays as it is. That includes:
  - the resolve payloads and the attempt identities;
  - the Cancel confirmation and its focus moves;
  - the terminal and failure states;
  - the tokens never shown;
  - `whitespace-pre-wrap` and `data-slot="card"`.

```tsx
  it('renders the backend question VERBATIM + one row per option', () => {
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
    });
    expect(screen.getByText('Which city should I check?')).toBeTruthy();
    expect(screen.getByRole('option', { name: 'Rome' })).toBeTruthy();
    expect(screen.getByRole('option', { name: 'Milan' })).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Answer' }) as HTMLButtonElement).disabled).toBe(true);
  });

  it('Answer (option) resolves {action:"accept", content} → answered receipt with the answer given', async () => {
    const onResolved = vi.fn();
    renderCard({
      approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }),
      onResolved,
    });
    fireEvent.click(screen.getByRole('option', { name: 'Milan' }));
    expect(calls).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    await waitFor(() => {
      expect(screen.getByText('Answered.')).toBeTruthy();
    });
    expect(screen.getByText('Answered.').closest('[data-tone]')?.getAttribute('data-tone')).toBe('success');
    expect(screen.getByText('Milan')).toBeTruthy();
    expect(calls).toHaveLength(1);
    expect(calls[0]?.url).toContain('/api/approvals/t-1/resolve');
    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'Milan' });
    const resolution = onResolved.mock.calls[0]?.[0] as ApprovalResolution | undefined;
    expect(resolution?.approval.token).toBe('t-1');
    expect(resolution?.approval.conversation_id).toBe('c-1');
    expect(resolution?.action).toBe('accept');
  });

  it('Enter on the chosen row answers, from the keyboard alone', async () => {
    renderCard({ approval: approval({ token: 't-1', conversation_id: 'c-1', options: ['Rome', 'Milan'] }) });
    const list = screen.getByRole('listbox');
    fireEvent.keyDown(list, { key: 'ArrowDown' });
    fireEvent.keyDown(list, { key: 'Enter' });
    fireEvent.keyDown(list, { key: 'Enter' });
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });
    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'Milan' });
  });

  it('an approval shows the gateway scopes as a single choice and Approve sends the chosen scope', async () => {
    renderCard({
      approval: approval({
        token: 't-scope',
        conversation_id: 'c-1',
        kind: 'approval',
        options: [
          { label: 'Approve once', value: 'gateway_scope:once:shell_exec' },
          { label: 'Approve for this conversation', value: 'gateway_scope:session:shell_exec' },
        ],
      }),
    });
    const approve = screen.getByRole('button', { name: 'Approve' }) as HTMLButtonElement;
    expect(approve.disabled).toBe(true);
    fireEvent.click(screen.getByRole('option', { name: 'Approve shell_exec for this conversation' }));
    expect(approve.disabled).toBe(false);
    fireEvent.click(approve);
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });
    expect(calls[0]?.body).toEqual({ action: 'accept', content: 'gateway_scope:session:shell_exec' });
  });

  it('a gateway approval graded destructive draws the destructive variant', () => {
    renderCard({
      approval: approval({
        token: 't-rm',
        conversation_id: 'c-1',
        kind: 'approval',
        question: 'Approve shell_exec?',
        presentation: { key: 'approval.gateway.mutation', params: { tool: 'shell_exec', risk: 'destructive', args: 'rm -rf /tmp/x' } },
      }),
    });
    expect(screen.getByRole('form').getAttribute('data-variant')).toBe('destructive');
    expect(screen.getByRole('button', { name: 'Approve' }).className).toContain('bg-destructive');
  });

  it('an approval with no options approves with empty content', async () => {
    renderCard({ approval: approval({ token: 't-plain', conversation_id: 'c-1', kind: 'approval' }) });
    expect(screen.queryByPlaceholderText('Type your answer')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Approve' }));
    await waitFor(() => {
      expect(calls).toHaveLength(1);
    });
    expect(calls[0]?.body).toEqual({ action: 'accept', content: '' });
  });
```

In `web/src/approvals/__tests__/approvalState.test.ts`, add:

```ts
describe('isDestructiveApproval', () => {
  it('is true only for a presentation the gateway graded destructive', () => {
    const graded = (risk: string) => ({ presentation: { key: 'approval.gateway.mutation', params: { tool: 't', risk, args: '' } } });
    expect(isDestructiveApproval(graded('destructive'))).toBe(true);
    expect(isDestructiveApproval(graded('risky'))).toBe(false);
    expect(isDestructiveApproval({})).toBe(false);
  });
});
```

Add `isDestructiveApproval` to that file's import from `'../approvalState'`.

Three more places clicked an option button. Each becomes a row choice followed by **Answer**, for the same reason as above.
- In `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx`:
  - line 132 becomes:

    ```tsx
        fireEvent.click(screen.getByRole('option', { name: 'Yes' }));
        fireEvent.click(screen.getByRole('button', { name: 'Answer' }));
    ```

  - line 206 becomes:

    ```tsx
        fireEvent.click(first(screen.getAllByRole('option', { name: 'Yes' })));
        fireEvent.click(first(screen.getAllByRole('button', { name: 'Answer' })));
    ```

  - line 213 becomes the same pair as line 132. By then only the second card still has rows.
- The Playwright specs run in CI (`ci.yml:1834`) and follow the same change:
  - In `web/e2e/chat.spec.ts:472-482`, the fixture's `kind: 'approval'` (line 302) is now drawn with an **Approve** pill and no text field. Replace the block from the step-2 comment through `await answer.click();` with:

    ```ts
        // 2) The ask_user interrupt renders an inline approval card IN-thread (D-03): the
        // backend question verbatim + the Approve verb an approval kind carries.
        await expect(page.getByText('Serve conferma per procedere')).toBeVisible({ timeout: 15000 });
        const answer = page.getByRole('button', { name: 'Approve' });
        await expect(answer).toBeVisible();

        // 3) Resolve it (Approve) → the resolve POSTs → the run re-drives (continue-after-resume)
        // and the resume turn streams (APRV-02 / D-05).
        await answer.click();
    ```

  - In `web/e2e/chat-calm-prism.spec.ts:232`, the fixture's options (`e2e/support/calmPrismFixture.ts:119`) are rows now:

    ```ts
        await expect(page.getByRole('option', { name: 'Pilot workspace' })).toBeVisible();
    ```

- [ ] **Step 2: Run them to verify they fail.** Web: `npx vitest run src/questions src/approvals`.

Expected FAIL:
- `Failed to resolve import "../CancelControl"`, and likewise for the other new modules;
- in `InlineApprovalCard.test.tsx`, `Unable to find an accessible element with the role "option"`;
- `isDestructiveApproval is not a function`.

- [ ] **Step 3: Implement.**
  - First read `https://github.com/assistant-ui/tool-ui/blob/main/LICENSE`. The spec gives the repository as `assistant-ui/tool-ui`, MIT.
  - MIT asks that its notice travel with substantial portions. Each ported file's header comment below says "(MIT)". Extend it to "(© <the LICENSE's copyright line>, MIT)" in `QuestionCard.tsx`, `QuestionOptions.tsx` and `QuestionReceipt.tsx`.

  In `web/components.json`, `registries` becomes:

```json
  "registries": {
    "@assistant-ui": "https://r.assistant-ui.com/{name}.json",
    "@tool-ui": "https://www.tool-ui.com/r/{name}.json"
  }
```

Create `web/src/i18n/resources.questions.ts`:

```ts
// The `questionCard.*` bundle: the frame every question the cockpit asks renders in
// (web/src/questions), shared by ask_user and a mounted MCP server's form. Split out of
// resources.ts to keep that file under the 600-LOC cap, the resources.steer.ts precedent.
// Every key exists in BOTH locales; the parity gate fails on any drift.

export const questionCardEn = {
  questionCard: {
    step: 'Step {{current}} of {{total}}',
    progress: 'Form progress',
    approve: 'Approve',
    yourAnswer: 'Your answer',
    next: 'Next',
    back: 'Back',
    skip: 'Skip',
    submit: 'Submit',
    decline: 'Decline',
    yes: 'Yes',
    no: 'No',
    placeholder: 'Type your answer',
    form: {
      title: 'A form from {{server}}',
      server: 'MCP server {{server}}',
      expiresIn: 'Declines itself in {{time}}',
    },
    cancel: {
      label: 'Cancel',
      confirm: 'Cancel this request?',
      yes: 'Cancel request',
      no: 'Keep answering',
    },
    receipt: {
      answered: 'Answered.',
      declined: 'Declined.',
      cancelled: 'Cancelled.',
      expired: 'Expired: declined automatically.',
    },
    refusal: {
      unrenderable: "Aura declined this form because it can't be shown here.",
      ambiguous_run: 'Aura declined this form because more than one conversation was using this server.',
    },
    error: {
      required: 'This field is required.',
      invalid: 'This value is not valid.',
      failed: "Couldn't send your answer. Try again.",
      closed: 'This form was already resolved.',
    },
  },
};

export const questionCardIt = {
  questionCard: {
    step: 'Passo {{current}} di {{total}}',
    progress: 'Avanzamento del modulo',
    approve: 'Approva',
    yourAnswer: 'La tua risposta',
    next: 'Avanti',
    back: 'Indietro',
    skip: 'Salta',
    submit: 'Invia',
    decline: 'Rifiuta',
    yes: 'Sì',
    no: 'No',
    placeholder: 'Scrivi la tua risposta',
    form: {
      title: 'Un modulo da {{server}}',
      server: 'Server MCP {{server}}',
      expiresIn: 'Si rifiuta da solo tra {{time}}',
    },
    cancel: {
      label: 'Annulla',
      confirm: 'Annullare questa richiesta?',
      yes: 'Annulla richiesta',
      no: 'Continua a rispondere',
    },
    receipt: {
      answered: 'Risposto.',
      declined: 'Rifiutato.',
      cancelled: 'Annullato.',
      expired: 'Scaduto: rifiutato automaticamente.',
    },
    refusal: {
      unrenderable: 'Aura ha rifiutato questo modulo perché qui non si può mostrare.',
      ambiguous_run: 'Aura ha rifiutato questo modulo perché più di una conversazione stava usando questo server.',
    },
    error: {
      required: 'Questo campo è obbligatorio.',
      invalid: 'Questo valore non è valido.',
      failed: 'Impossibile inviare la risposta. Riprova.',
      closed: 'Questo modulo è già stato risolto.',
    },
  },
};
```

In `web/src/i18n/resources.ts`:
- add `import { questionCardEn, questionCardIt } from './resources.questions';` after the `update` import;
- add `...questionCardEn,` after `...updateEn,` (line 196);
- add `...questionCardIt,` after `...updateIt,` (line 468).

Create `web/src/questions/QuestionCard.tsx`:

```tsx
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { cn } from '@/lib/utils';

// QuestionCard is the one frame every question the cockpit asks is drawn in: ask_user's three
// kinds and a mounted MCP server's form (spec 2026-09-25). The markup and classes are ported
// from Tool UI's Question Flow (question-flow.tsx: StepContent, ProgressBar; MIT), on Aura's
// tokens and with its copy translated where the component hard-codes English.

export type QuestionVariant = 'default' | 'destructive';

export interface QuestionCardProps {
  readonly titleId: string;
  readonly title: ReactNode;
  readonly description?: ReactNode;
  readonly descriptionId?: string;
  readonly icon?: ReactNode;
  /** Above the step label: the MCP form's server chip, tool name, countdown and message. */
  readonly header?: ReactNode;
  readonly step?: { readonly current: number; readonly total: number };
  readonly variant?: QuestionVariant;
  readonly footer?: ReactNode;
  readonly status?: ReactNode;
  readonly dataAttributes?: Readonly<Record<`data-${string}`, string>>;
  readonly children?: ReactNode;
}

export function QuestionCard({
  titleId,
  title,
  description,
  descriptionId,
  icon,
  header,
  step,
  variant = 'default',
  footer,
  status,
  dataAttributes,
  children,
}: QuestionCardProps) {
  const { t } = useTranslation();
  const described = description !== undefined && descriptionId !== undefined;
  return (
    <div
      role="form"
      aria-labelledby={titleId}
      {...(described ? { 'aria-describedby': descriptionId } : {})}
      data-slot="card"
      data-variant={variant}
      tabIndex={-1}
      {...dataAttributes}
      className={cn(
        'flex w-full flex-col gap-4 rounded-2xl border bg-surface-2 p-5 text-text shadow-xs',
        'motion-safe:animate-in motion-safe:fade-in motion-safe:duration-300',
        variant === 'destructive' ? 'border-danger/60' : 'border-accent/40',
      )}
    >
      {header}
      {step !== undefined && step.total > 1 ? (
        <div className="flex flex-col gap-2">
          <span className="text-xs font-medium tracking-wide text-text-muted uppercase">
            {t('questionCard.step', { current: step.current, total: step.total })}
          </span>
          <StepBar current={step.current} total={step.total} label={t('questionCard.progress')} />
        </div>
      ) : null}
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {icon}
          <h2 id={titleId} className="text-lg leading-tight font-semibold">
            {title}
          </h2>
        </div>
        {description !== undefined ? (
          <p
            id={descriptionId}
            className="overflow-x-auto text-sm leading-relaxed whitespace-pre-wrap break-words text-text-muted [overflow-wrap:anywhere]"
          >
            {description}
          </p>
        ) : null}
      </div>
      {children}
      {footer !== undefined ? (
        <div className="flex flex-wrap items-center justify-between gap-2 pt-2">{footer}</div>
      ) : null}
      {status}
    </div>
  );
}

function StepBar({ current, total, label }: { readonly current: number; readonly total: number; readonly label: string }) {
  return (
    <div
      className="flex h-1.5 gap-1"
      role="progressbar"
      aria-label={label}
      aria-valuenow={current}
      aria-valuemin={1}
      aria-valuemax={total}
    >
      {Array.from({ length: total }, (_, index) => (
        <div key={index} className="relative flex-1 overflow-hidden rounded-full bg-surface">
          <div
            className={cn(
              'absolute inset-0 origin-left rounded-full bg-accent motion-safe:transition-transform motion-safe:duration-300',
              index < current ? 'scale-x-100' : 'scale-x-0',
            )}
          />
        </div>
      ))}
    </div>
  );
}
```

Create `web/src/questions/QuestionOptions.tsx`:

```tsx
import { Fragment, useRef, useState, type KeyboardEvent } from 'react';
import { Check } from 'lucide-react';
import { cn } from '@/lib/utils';

// QuestionOptions is Question Flow's option list (question-flow.tsx: OptionItem,
// SelectionIndicator, the listbox keyboard; MIT) as radio rows (single) or checkbox rows
// (multi). Enter on a row that is already chosen submits a single choice, so a keyboard user
// can answer without leaving the list.

export interface QuestionOption {
  readonly id: string;
  readonly label: string;
  readonly description?: string;
}

export interface QuestionOptionsProps {
  readonly labelledBy: string;
  readonly options: readonly QuestionOption[];
  readonly mode: 'single' | 'multi';
  readonly selected: ReadonlySet<string>;
  readonly disabled?: boolean;
  readonly onToggle: (id: string) => void;
  readonly onSubmit?: () => void;
}

export function QuestionOptions({
  labelledBy,
  options,
  mode,
  selected,
  disabled = false,
  onToggle,
  onSubmit,
}: QuestionOptionsProps) {
  const rows = useRef<Array<HTMLButtonElement | null>>([]);
  const [active, setActive] = useState(() => Math.max(options.findIndex((o) => selected.has(o.id)), 0));
  const last = options.length - 1;

  function focusAt(index: number) {
    rows.current[index]?.focus();
    setActive(index);
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (disabled || options.length === 0) return;
    const moves: Record<string, number> = {
      ArrowDown: active === last ? 0 : active + 1,
      ArrowUp: active === 0 ? last : active - 1,
      Home: 0,
      End: last,
    };
    const target = moves[event.key];
    if (target !== undefined) {
      event.preventDefault();
      focusAt(target);
      return;
    }
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    const option = options[active];
    if (option === undefined) return;
    if (event.key === 'Enter' && mode === 'single' && selected.has(option.id)) onSubmit?.();
    else onToggle(option.id);
  }

  return (
    <div
      role="listbox"
      aria-labelledby={labelledBy}
      aria-multiselectable={mode === 'multi'}
      tabIndex={-1}
      onKeyDown={onKeyDown}
      className="flex flex-col px-1"
    >
      {options.map((option, index) => {
        const isSelected = selected.has(option.id);
        return (
          <Fragment key={option.id}>
            {index > 0 ? <div aria-hidden="true" className="h-px bg-border-strong/40" /> : null}
            <button
              ref={(el) => {
                rows.current[index] = el;
              }}
              type="button"
              role="option"
              aria-selected={isSelected}
              tabIndex={index === active ? 0 : -1}
              disabled={disabled}
              onFocus={() => {
                setActive(index);
              }}
              onClick={() => {
                setActive(index);
                onToggle(option.id);
              }}
              className="group relative flex min-h-[50px] w-full items-start gap-3 py-2.5 text-left text-sm font-medium outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-60"
            >
              <span
                aria-hidden="true"
                className="absolute inset-0 -mx-3 -my-0.5 rounded-xl bg-accent/5 opacity-0 transition-opacity group-hover:opacity-100"
              />
              <span className="relative flex h-6 items-center">
                <SelectionIndicator mode={mode} selected={isSelected} />
              </span>
              <span className="relative flex flex-col">
                <span className="leading-6 text-pretty">{option.label}</span>
                {option.description !== undefined ? (
                  <span className="text-sm font-normal text-pretty text-text-muted">{option.description}</span>
                ) : null}
              </span>
            </button>
          </Fragment>
        );
      })}
    </div>
  );
}

function SelectionIndicator({ mode, selected }: { readonly mode: 'single' | 'multi'; readonly selected: boolean }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-4 shrink-0 items-center justify-center border-2 motion-safe:transition-colors motion-safe:duration-200',
        mode === 'single' ? 'rounded-full' : 'rounded',
        selected
          ? 'border-accent bg-accent text-primary-foreground motion-safe:animate-in motion-safe:fade-in motion-safe:zoom-in-75'
          : 'border-text-muted/50',
      )}
    >
      {selected && mode === 'multi' ? <Check className="size-3" strokeWidth={3} /> : null}
      {selected && mode === 'single' ? <span className="size-2 rounded-full bg-current" /> : null}
    </span>
  );
}
```

Create `web/src/questions/QuestionReceipt.tsx`:

```tsx
import { Check } from 'lucide-react';
import { Badge } from '@/components/ui/badge';

// QuestionReceipt is what a question becomes once it closes: Question Flow's read-only receipt
// (question-flow.tsx: QuestionFlowReceipt; MIT) as a chip in the outcome's tone, and under an
// answer the value given. Nothing here is interactive.

export type ReceiptTone = 'success' | 'neutral' | 'warning' | 'danger';

const CHIP_TEXT: Record<ReceiptTone, string> = {
  success: 'text-success',
  neutral: 'text-text-muted',
  warning: 'text-warning',
  danger: 'text-danger',
};
const CHIP_DOT: Record<ReceiptTone, string> = {
  success: 'bg-success',
  neutral: 'bg-text-muted',
  warning: 'bg-warning',
  danger: 'bg-danger',
};

export interface ReceiptLine {
  readonly label: string;
  readonly value: string;
}

export interface QuestionReceiptProps {
  readonly tone: ReceiptTone;
  readonly label: string;
  readonly summary?: readonly ReceiptLine[];
  readonly announce?: boolean;
}

export function QuestionReceipt({ tone, label, summary = [], announce = false }: QuestionReceiptProps) {
  return (
    <div className="flex flex-col gap-3">
      <Badge
        variant={tone === 'neutral' ? 'secondary' : tone}
        data-tone={tone}
        {...(announce ? { role: 'status', 'aria-live': 'polite' as const } : {})}
        className={`self-start text-[0.8125rem] ${CHIP_TEXT[tone]}`}
      >
        {tone === 'success' ? (
          <Check aria-hidden="true" className="size-3.5" />
        ) : (
          <span aria-hidden="true" className={`inline-block h-2 w-2 shrink-0 rounded-sm ${CHIP_DOT[tone]}`} />
        )}
        {label}
      </Badge>
      {summary.length > 0 ? (
        <dl className="flex flex-col gap-2 text-sm">
          {summary.map((line) => (
            <div key={line.label} className="flex flex-col gap-0.5 motion-safe:animate-in motion-safe:fade-in">
              <dt className="text-text-muted">{line.label}</dt>
              <dd className="font-medium whitespace-pre-wrap break-words [overflow-wrap:anywhere]">{line.value}</dd>
            </div>
          ))}
        </dl>
      ) : null}
    </div>
  );
}
```

Create `web/src/questions/CancelControl.tsx`. Its body is `InlineApprovalCard.tsx:95-108` and `231-275` moved, not rewritten:

```tsx
import { useEffect, useRef, useState, type KeyboardEvent } from 'react';
import { Button } from '@/components/ui/button';

// CancelControl is a question's Cancel, confirmed inline while its run streams. Escape
// dismisses the confirmation and never cancels anything.

export interface CancelLabels {
  readonly cancel: string;
  readonly confirm: string;
  readonly yes: string;
  readonly no: string;
}

export interface CancelControlProps {
  readonly isStreaming?: boolean | undefined;
  readonly disabled: boolean;
  readonly labels: CancelLabels;
  readonly onCancel: () => void;
}

export function CancelControl({ isStreaming, disabled, labels, onCancel }: CancelControlProps) {
  const [confirming, setConfirming] = useState(false);
  const cancelRef = useRef<HTMLButtonElement | null>(null);
  const keepRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (confirming) keepRef.current?.focus();
  }, [confirming]);

  function close() {
    setConfirming(false);
    requestAnimationFrame(() => cancelRef.current?.focus());
  }

  function onKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    if (event.key !== 'Escape') return;
    event.preventDefault();
    close();
  }

  if (confirming) {
    return (
      <span className="flex items-center gap-2">
        <span className="text-[0.8125rem] text-warning">{labels.confirm}</span>
        <Button
          type="button"
          variant="destructive"
          size="sm"
          disabled={disabled}
          onKeyDown={onKeyDown}
          onClick={onCancel}
          className="min-h-11 text-[0.8125rem]"
        >
          {labels.yes}
        </Button>
        <Button
          ref={keepRef}
          type="button"
          variant="ghost"
          size="sm"
          onKeyDown={onKeyDown}
          onClick={close}
          className="min-h-11 text-[0.8125rem] text-text-muted hover:text-text"
        >
          {labels.no}
        </Button>
      </span>
    );
  }
  return (
    <Button
      ref={cancelRef}
      type="button"
      variant="ghost"
      disabled={disabled}
      onClick={() => {
        if (isStreaming === true) setConfirming(true);
        else onCancel();
      }}
      className="text-[0.8125rem] text-danger hover:bg-danger/15 hover:text-danger"
    >
      {labels.cancel}
    </Button>
  );
}
```

In `web/src/approvals/approvalState.ts`, add after `isTerminal`:

```ts
/**
 * True when the gateway graded the approval's action Destructive (scoring.Destructive, carried
 * as the presentation's `risk` param): the card then draws its destructive variant.
 */
export function isDestructiveApproval(approval: Pick<Approval, 'presentation'>): boolean {
  return approval.presentation?.params.risk === 'destructive';
}
```

Replace `web/src/approvals/InlineApprovalCard.tsx` in full. `useScopeLabel` and `cardStateFor` keep their bodies. The misplaced `cardStateFor` doc block at 27-32 moves onto `cardStateFor`, where it belongs.

```tsx
import { useId, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { MessageSquareText, ShieldCheck, TriangleAlert } from 'lucide-react';
import { ariaInvalid } from '../a11y/aria';
import { CancelControl } from '../questions/CancelControl';
import { QuestionCard } from '../questions/QuestionCard';
import { QuestionOptions } from '../questions/QuestionOptions';
import { QuestionReceipt, type ReceiptTone } from '../questions/QuestionReceipt';
import { approvalQuestion } from './approvalQuestion';
import {
  isDestructiveApproval,
  isTerminal,
  parseOptions,
  parseScopeChoice,
  type ApprovalOption,
} from './approvalState';
import type { ApprovalResolution, ApprovalResolutionAttempt } from './useThreadApprovals';
import {
  useResolveApproval,
  type Approval,
  type ResolveAction,
  type ResolveOutcome,
} from './useApprovals';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';

// InlineApprovalCard is ask_user's adapter onto QuestionCard (spec 2026-09-25). A choice is
// radio rows and a pill Answer, a clarification is a text field, and an approval is Tool UI's
// ApprovalCard shape with the gateway's scopes as a single choice. Recognized approval metadata
// is rendered in the active locale; legacy/unknown questions remain escaped React text, and the
// URL capability token never appears as a visible field.

type CardState = 'pending' | 'answered' | 'declined' | 'cancelled';

const RECEIPTS: Record<Exclude<CardState, 'pending'>, { readonly tone: ReceiptTone; readonly key: string }> = {
  answered: { tone: 'success', key: 'approval.card.answered' },
  declined: { tone: 'neutral', key: 'approval.card.declined' },
  cancelled: { tone: 'danger', key: 'approval.card.cancelled' },
};

/**
 * useScopeLabel renders a gateway approval-scope option in the operator's language. The
 * gateway ships a stable code plus an English fallback label; a scope it does not
 * recognise falls back to that label rather than rendering a raw code.
 */
function useScopeLabel(): (option: ApprovalOption) => string {
  const { t } = useTranslation();
  return (option) => {
    const choice = parseScopeChoice(option.value);
    if (choice === null) return option.label;
    return t(`approval.scope.${choice.scope}`, { subject: choice.subject });
  };
}

/**
 * cardStateFor maps the server's verdict to the terminal chip. The server is authoritative — a
 * scheduled gate reports approved/rejected regardless of which button produced it — and the
 * action is only the fallback for the in-session verdicts ('continue'/'pending'), where the chip
 * reflects what the operator just did while the turn goes on.
 */
function cardStateFor(outcome: ResolveOutcome, action: ResolveAction): CardState {
  switch (outcome) {
    case 'approved':
      return 'answered';
    case 'rejected':
      return 'declined';
    case 'terminated':
      return 'cancelled';
    default:
      return action === 'accept' ? 'answered' : action === 'decline' ? 'declined' : 'cancelled';
  }
}

export interface InlineApprovalCardProps {
  readonly approval: Approval;
  /** True while the owning run is actively streaming; gates cancel confirmation. */
  readonly isStreaming?: boolean;
  /** Notify the shared thread gate after a successful local resolution. */
  readonly onResolved?: (resolution: ApprovalResolution) => void | Promise<void>;
  /** Notify the gate before the resolve request starts so Cancel owns the generation. */
  readonly onResolutionStarted?: (attempt: ApprovalResolutionAttempt) => void;
  /** Release a matching lifecycle attempt if its resolve request fails. */
  readonly onResolutionFailed?: (attempt: ApprovalResolutionAttempt) => void | Promise<void>;
}

export function InlineApprovalCard({
  approval,
  isStreaming,
  onResolved,
  onResolutionStarted,
  onResolutionFailed,
}: InlineApprovalCardProps) {
  const { t } = useTranslation();
  const resolve = useResolveApproval();
  const options = parseOptions(approval.options);
  const scopeLabel = useScopeLabel();
  const [freeText, setFreeText] = useState('');
  const [chosen, setChosen] = useState<ApprovalOption | null>(null);
  const [state, setState] = useState<CardState>('pending');
  const [given, setGiven] = useState('');
  const attemptSequence = useRef(0);
  const baseId = useId();
  const busy = resolve.isPending;
  const failed = resolve.isError;
  const isApproval = approval.kind === 'approval';
  const destructive = isApproval && isDestructiveApproval(approval);
  const choosing = options.length > 0;
  const typing = !choosing && !isApproval;

  function submit(action: ResolveAction, content?: string, shown = '') {
    attemptSequence.current += 1;
    const attempt: ApprovalResolution = {
      approval,
      action,
      attemptId: `${baseId}:${String(attemptSequence.current)}`,
    };
    onResolutionStarted?.(attempt);
    resolve.mutate(
      action === 'accept'
        ? { token: approval.token, action, content: content ?? '' }
        : { token: approval.token, action },
      {
        onSuccess: (directive) => {
          setGiven(shown);
          setState(cardStateFor(directive.outcome, action));
          // The verdict rides along so the thread gate re-drives the turn only when the model
          // actually has more work ('continue'); a scheduled gate is already complete.
          void onResolved?.({ ...attempt, outcome: directive.outcome });
        },
        onError: () => {
          void onResolutionFailed?.(attempt);
        },
      },
    );
  }

  function answer() {
    if (choosing) {
      if (chosen !== null) submit('accept', chosen.value, scopeLabel(chosen));
      return;
    }
    submit('accept', typing ? freeText : '', freeText.trim());
  }

  function frame(children: ReactNode, footer?: ReactNode) {
    return (
      <QuestionCard
        titleId={`${baseId}-title`}
        descriptionId={`${baseId}-question`}
        icon={<FrameIcon approval={isApproval} destructive={destructive} />}
        title={t(isApproval ? 'approval.frame.approval' : 'approval.frame.input')}
        description={approvalQuestion(approval, t)}
        variant={destructive ? 'destructive' : 'default'}
        dataAttributes={{ 'data-approval-token': approval.token }}
        {...(footer !== undefined ? { footer } : {})}
      >
        {children}
      </QuestionCard>
    );
  }

  if (isTerminal(approval)) {
    return frame(<QuestionReceipt tone="warning" label={t('approval.terminal.expired')} announce />);
  }
  if (state !== 'pending') {
    const receipt = RECEIPTS[state];
    return frame(
      <QuestionReceipt
        tone={receipt.tone}
        label={t(receipt.key)}
        {...(state === 'answered' && given !== '' ? { summary: [{ label: t('questionCard.yourAnswer'), value: given }] } : {})}
      />,
    );
  }

  const footer = (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={busy}
          onClick={() => {
            submit('decline');
          }}
          className="text-[0.8125rem] text-text-muted hover:text-text"
        >
          {t('approval.card.decline')}
        </Button>
        <CancelControl
          isStreaming={isStreaming}
          disabled={busy}
          labels={{
            cancel: t('approval.card.cancel'),
            confirm: t('approval.card.confirmCancel'),
            yes: t('approval.card.confirmCancelYes'),
            no: t('approval.card.confirmCancelNo'),
          }}
          onCancel={() => {
            submit('cancel');
          }}
        />
      </div>
      <Button
        type="button"
        variant={destructive ? 'destructive' : 'default'}
        disabled={busy || (choosing && chosen === null)}
        onClick={answer}
        className="rounded-full text-[0.8125rem]"
      >
        {t(isApproval ? 'questionCard.approve' : 'approval.card.answer')}
      </Button>
    </>
  );

  return frame(
    <>
      {isApproval ? <p className="text-xs text-text-muted">{t('approval.frame.review')}</p> : null}
      {choosing ? (
        <QuestionOptions
          labelledBy={`${baseId}-title`}
          options={options.map((option) => ({ id: option.value, label: scopeLabel(option) }))}
          mode="single"
          selected={new Set(chosen === null ? [] : [chosen.value])}
          disabled={busy}
          onToggle={(value) => {
            setChosen(options.find((option) => option.value === value) ?? null);
          }}
          onSubmit={answer}
        />
      ) : null}
      {typing ? (
        <div className="flex flex-col gap-1">
          <Label htmlFor={`${baseId}-answer`} className="text-[0.75rem] font-normal text-text-muted">
            {t('approval.card.freeText')}
          </Label>
          <Textarea
            id={`${baseId}-answer`}
            value={freeText}
            disabled={busy}
            onChange={(event) => {
              setFreeText(event.target.value);
            }}
            placeholder={t('approval.card.freeTextPlaceholder')}
            rows={2}
            aria-invalid={ariaInvalid(failed && freeText.trim().length === 0)}
            className="resize-y bg-surface text-sm"
          />
        </div>
      ) : null}
      {failed ? (
        <Alert role="status" aria-live="polite" data-tone="danger" variant="destructive" className="bg-surface">
          <AlertDescription>{t('approval.card.error')}</AlertDescription>
        </Alert>
      ) : null}
    </>,
    footer,
  );
}

function FrameIcon({ approval, destructive }: { readonly approval: boolean; readonly destructive: boolean }) {
  if (destructive) return <TriangleAlert aria-hidden="true" className="size-5 text-danger" />;
  if (approval) return <ShieldCheck aria-hidden="true" className="size-5 text-warning" />;
  return <MessageSquareText aria-hidden="true" className="size-5 text-accent" />;
}
```

Option rows are keyed by `option.value`. The server's options are distinct by label, and ask_user rejects duplicate labels (`ask_user.go` validation), but values could repeat. If `parseOptions` can yield two options with one value, key the rows by index instead. Read `parseOptions`'s callers before choosing.

- [ ] **Step 4: Run the checks.**
  - Web: `npx vitest run src/questions src/approvals src/i18n`. Expected: every test passes, the i18n parity and usage gates included.
  - Web: `npm run typecheck`. Expected: exit 0.
  - Web: `npm run lint`. Read the `Found N errors` line; N must be 0. oxlint exits 0 even with errors.
  - Web: `npx prettier --check src/questions src/approvals src/i18n components.json`.
  - Web: `npm run dup`. jscpd runs with threshold 0; `CancelControl` exists so the two adapters share no clone.
  - Run `wc -l` on every touched `.ts`/`.tsx` file; each must stay under 600 lines.
  - Stryker runs in CI only.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add web/src/questions/ web/src/i18n/resources.questions.ts
git commit -F - -- web/components.json web/src/questions/ web/src/i18n/resources.questions.ts web/src/i18n/resources.ts web/src/approvals/InlineApprovalCard.tsx web/src/approvals/approvalState.ts web/src/approvals/__tests__/InlineApprovalCard.test.tsx web/src/approvals/__tests__/ThreadApprovalCards.test.tsx web/src/approvals/__tests__/approvalState.test.ts web/e2e/chat.spec.ts web/e2e/chat-calm-prism.spec.ts web/stryker.config.json <<'EOF'
feat(cockpit): draw ask_user in Tool UI's question frame

Every question the cockpit asks now renders in one frame, QuestionCard.
Its markup is ported from Tool UI's Question Flow (MIT), on Aura's
tokens and with the copy in en and it.

ask_user is its first adapter:
- a choice is radio rows and a pill Answer that stays grey until a row
  is chosen, and Enter on the chosen row answers;
- a clarification is a text field in the same frame;
- an approval is an Approve pill, with the gateway's scopes as a single
  choice and the destructive variant when the gateway grades the action
  Destructive.

The resolve API and the lifecycle are unchanged.

Six tests change, and the redesign is the reason: a click on a row no
longer submits, and an approval says Approve.
- Two in InlineApprovalCard and three sites in ThreadApprovalCards now
  choose a row and then press Answer.
- The two Playwright specs follow: chat.spec's approval fixture now
  presses Approve, and calm-prism's options are rows.

The @tool-ui registry is registered for spec 2. No vendored component
is installed yet (plan Open point 1).

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

**If the operator chooses option P**, add these steps after Step 3:
- Web: `npx shadcn@latest add @tool-ui/question-flow @tool-ui/option-list @tool-ui/approval-card --yes`.
- Then `git status --short web/`. `web/src/components/ui/button.tsx` must be unchanged; if it changed, restore it with `git checkout -- web/src/components/ui/button.tsx`.
- Expect three new directories and `shared/` under `web/src/components/tool-ui/`, a new `web/src/components/ui/separator.tsx`, and `zod` in `package.json`.
- `npm run typecheck` will fail inside the vendored files. Two errors are known: `size="lg"` at `option-list.tsx:109` and `question-flow.tsx:126`, where Aura's Button has no `lg` size. `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes` add others.
  - Fix each with the smallest edit the compiler accepts, and list every edit in the commit body.
  - If the list passes 20 edits, stop and go back to the operator.
- Exempt `web/src/components/tool-ui/**` in the same places `model-selector.tsx` is exempt:
  - `.oxlintrc.json` `ignorePatterns`;
  - `knip.json` `ignore`;
  - `.prettierignore`;
  - the vitest `coverage.exclude`;
  - `scripts/check-file-size.sh` `select_targets`, adding `| grep -v -E '^web/src/components/tool-ui/'`. This one is required: `question-flow.tsx` is 793 lines and `option-list.tsx` 625.
- `separator.tsx` then replaces the `h-px` divider in `QuestionOptions`.

---
### Task 8: A mounted server's form in the cockpit thread

**Files:**
- Create:
  - `web/src/chat/sseAdapter_elicitation.ts`
  - `web/src/questions/useThreadElicitations.ts`
  - `web/src/questions/elicitationAnswer.ts`
  - `web/src/questions/elicitationSteps.ts`
  - `web/src/questions/useCountdown.ts`
  - `web/src/questions/FieldInput.tsx`
  - `web/src/questions/ElicitationHeader.tsx`
  - `web/src/questions/ElicitationCard.tsx`
- Modify: `web/src/chat/sseAdapter.ts`, at five places:
  - the imports at 1-28;
  - `StreamRunOptions` at 398-422;
  - `StreamPostOptions` at 423-434;
  - `StreamSSEOptions` at 442-448;
  - the pump at 465-476, together with `streamPost` and `streamRun`.
  - The CUSTOM-branch comment at 305-308 is updated too.
- Modify: `web/src/chat/sseResume.ts`, at four places:
  - the imports;
  - `AttachRunOptions` at 53-71;
  - `EngineOptions` at 73-85;
  - `makeEngine` at 112, and `pumpBody` at 179.
- Modify: `web/src/chat/ExternalStoreChat_liveRun.ts:28-43,45-55,75-76,103-104`.
- Modify: `web/src/chat/ExternalStoreChat_streams.ts:38,72,118,135-140,182,186`.
- Modify: `web/src/chat/ExternalStoreChat.tsx`, at lines 139, 247, 296, 414-429, 438-448 and 560-566, plus one import. This adds 7 lines: 587 → 594.
- Modify: `web/src/approvals/ThreadApprovalCards.tsx`.
- Modify: `web/stryker.config.json`, adding every new non-test file to `mutate`.
- Test:
  - Create:
    - `web/src/chat/sseAdapter.onElicitation.test.ts`
    - `web/src/questions/__tests__/elicitationSteps.test.ts`
    - `web/src/questions/__tests__/useThreadElicitations.test.ts`
    - `web/src/questions/__tests__/elicitationAnswer.test.ts`
    - `web/src/questions/__tests__/useCountdown.test.ts`
    - `web/src/questions/__tests__/ElicitationCard.test.tsx`
  - Modify: `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx`.

**Interfaces:**
- Consumes:
  - Task 6's two frames:
    - `aura.elicitation`, with value `{run_id, id, server, tool?, message, fields: Field[] | null, deadline, refusal?}`. `fields` is `null` on a refusal.
    - `aura.elicitation_resolved`, with value `{id, action, expired?}`.
  - Task 6's route `POST /agent/runs/{runID}/elicitations/{id}`: 202, 409, 410, or 422 with `{"errors": {...}}`.
  - Task 7's `QuestionCard`, `QuestionOptions`, `QuestionReceipt`, `CancelControl` and `questionCard.*`.
- Produces:
  - `elicitationSignalValue(frame: AguiFrame): ElicitationSignal | null`;
  - `onElicitation?: (signal: ElicitationSignal) => void` on `streamRun`, `streamPost`, `attachRun` and the resilient run;
  - `useThreadElicitations(threadId): {items, onSignal}`;
  - `ThreadApprovalCards`'s new `elicitations?: readonly ElicitationItem[]` prop.

- [ ] **Step 1: Write the failing tests.** Create `web/src/chat/sseAdapter.onElicitation.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { streamRun, type AguiFrame } from './sseAdapter';
import { elicitationSignalValue } from './sseAdapter_elicitation';
import { attachRun } from './sseResume';

// The aura.elicitation pump signal, on the driving pump and the reattach pump alike, shaped
// like sseAdapter.onSteer.test.ts. A form is never reduced into a message part.

function sseResponse(frames: readonly AguiFrame[]): Response {
  const enc = new TextEncoder();
  const wire = frames.map((f) => `event: ${f.type}\ndata: ${JSON.stringify(f)}\n\n`).join('');
  const body = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(enc.encode(wire));
      controller.close();
    },
  });
  return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } });
}

const RUN_STARTED = { type: 'RUN_STARTED' } as AguiFrame;
const RUN_FINISHED = { type: 'RUN_FINISHED', outcome: { type: 'success' } } as AguiFrame;

const QUESTION = {
  run_id: 'run-1',
  id: 'q-1',
  server: 'forms',
  tool: 'ask_name',
  message: 'what is your name',
  fields: [{ name: 'name', kind: 'string', required: true }],
  deadline: '2026-09-25T10:05:00Z',
};

function custom(name: string, value: unknown): AguiFrame {
  return { type: 'CUSTOM', name, value };
}

describe('elicitationSignalValue', () => {
  it('reads a question and its resolution', () => {
    expect(elicitationSignalValue(custom('aura.elicitation', QUESTION))).toEqual({ kind: 'question', question: QUESTION });
    expect(elicitationSignalValue(custom('aura.elicitation_resolved', { id: 'q-1', action: 'decline', expired: true }))).toEqual({
      kind: 'resolved',
      resolved: { id: 'q-1', action: 'decline', expired: true },
    });
  });

  it('reads a refusal, whose fields the server sends as null', () => {
    const refused = { ...QUESTION, fields: null, message: '', refusal: 'unrenderable' };
    expect(elicitationSignalValue(custom('aura.elicitation', refused))).toEqual({
      kind: 'question',
      question: { ...refused, fields: [] },
    });
  });

  it('drops what it cannot trust', () => {
    for (const bad of [
      { ...QUESTION, id: 7 },
      { ...QUESTION, fields: [{ name: 'x', kind: 'object', required: true }] },
      { ...QUESTION, fields: [{ name: 'x', kind: 'string', required: true, format: 'ipv4' }] },
      { ...QUESTION, refusal: 'because' },
      'not an object',
    ]) {
      expect(elicitationSignalValue(custom('aura.elicitation', bad))).toBeNull();
    }
    expect(elicitationSignalValue(custom('aura.elicitation_resolved', { id: 'q-1', action: 'sure' }))).toBeNull();
    expect(elicitationSignalValue(custom('aura.steer', QUESTION))).toBeNull();
    expect(elicitationSignalValue({ type: 'TEXT_MESSAGE_START', messageId: 'm1' })).toBeNull();
  });
});

describe('the pumps fire onElicitation', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const frames = [RUN_STARTED, custom('aura.elicitation', QUESTION), custom('aura.elicitation_resolved', { id: 'q-1', action: 'accept' }), RUN_FINISHED];

  it('on the driving pump', async () => {
    const onElicitation = vi.fn();
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(sseResponse(frames))));
    await streamRun({
      threadId: 'conv-1',
      userText: 'ask me',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onElicitation,
    });
    expect(onElicitation.mock.calls.map(([signal]) => (signal as { kind: string }).kind)).toEqual(['question', 'resolved']);
  });

  it('on the reattach pump, so a reloaded tab gets the form back', async () => {
    const onElicitation = vi.fn();
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(sseResponse(frames))));
    await attachRun({
      threadId: 'conv-1',
      runId: 'run-1',
      signal: new AbortController().signal,
      newId: () => 'fixed-id',
      onUpdate: () => undefined,
      onElicitation,
    });
    expect(onElicitation).toHaveBeenCalledTimes(2);
  });
});
```

Create `web/src/questions/__tests__/useThreadElicitations.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import type { ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import { applyElicitationSignal, useThreadElicitations, type ElicitationItem } from '../useThreadElicitations';

function question(id: string, runId = 'run-1'): ElicitationQuestion {
  return { run_id: runId, id, server: 'forms', message: 'm', fields: [], deadline: '2026-09-25T10:05:00Z' };
}

const asked = (id: string, runId?: string) => ({ kind: 'question' as const, question: question(id, runId) });
const resolved = (id: string, action: 'accept' | 'decline' | 'cancel', expired?: true) => ({
  kind: 'resolved' as const,
  resolved: { id, action, ...(expired ? { expired } : {}) },
});

function fold(...signals: Parameters<typeof applyElicitationSignal>[1][]): readonly ElicitationItem[] {
  return signals.reduce<readonly ElicitationItem[]>(applyElicitationSignal, []);
}

describe('applyElicitationSignal', () => {
  it('keeps questions in arrival order', () => {
    expect(fold(asked('a'), asked('b')).map((item) => item.question.id)).toEqual(['a', 'b']);
  });

  // Review Focus 4: a reload replays the run from its first frame.
  it('holds a replayed question once, and its resolution still applies', () => {
    const items = fold(asked('a'), resolved('a', 'accept'), asked('a'), resolved('a', 'accept'));
    expect(items).toHaveLength(1);
    expect(items[0]?.outcome).toBe('accepted');
  });

  it('settles a question from its first resolution only', () => {
    expect(fold(asked('a'), resolved('a', 'cancel'), resolved('a', 'accept'))[0]?.outcome).toBe('cancelled');
  });

  it('tells an expiry from a decline', () => {
    expect(fold(asked('a'), resolved('a', 'decline', true))[0]?.outcome).toBe('expired');
    expect(fold(asked('b'), resolved('b', 'decline'))[0]?.outcome).toBe('declined');
  });

  it('ignores a resolution for a question it never saw', () => {
    expect(fold(asked('a'), resolved('zzz', 'accept'))[0]?.outcome).toBeUndefined();
  });

  it("drops an earlier run's settled cards when a new run asks, and keeps its open ones", () => {
    const items = fold(asked('a', 'run-1'), resolved('a', 'accept'), asked('b', 'run-1'), asked('c', 'run-2'));
    expect(items.map((item) => item.question.id)).toEqual(['b', 'c']);
  });
});

describe('useThreadElicitations', () => {
  it('scopes its forms to the thread it serves', () => {
    const { result, rerender } = renderHook(({ threadId }) => useThreadElicitations(threadId), {
      initialProps: { threadId: 't-1' },
    });
    act(() => {
      result.current.onSignal(asked('a'));
    });
    expect(result.current.items).toHaveLength(1);
    rerender({ threadId: 't-2' });
    expect(result.current.items).toHaveLength(0);
  });
});
```

Create `web/src/questions/__tests__/elicitationAnswer.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { postElicitationAnswer } from '../elicitationAnswer';

function respond(response: Response) {
  const fetchStub = vi.fn((_input: RequestInfo | URL, _init?: RequestInit) => Promise.resolve(response));
  vi.stubGlobal('fetch', fetchStub);
  return fetchStub;
}

describe('postElicitationAnswer', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('posts the answer once, owner-cookied and keyed', async () => {
    const fetchStub = respond(new Response('{"status":"delivered"}', { status: 202 }));
    await expect(postElicitationAnswer('run-1', 'q/1', { action: 'accept', content: { name: 'Ada' } }, 'key-1')).resolves.toEqual({ kind: 'delivered' });
    const [url, init] = fetchStub.mock.calls[0] ?? [];
    expect(url).toBe('/agent/runs/run-1/elicitations/q%2F1');
    expect(init?.method).toBe('POST');
    expect(init?.credentials).toBe('same-origin');
    expect(new Headers(init?.headers).get('Idempotency-Key')).toBe('key-1');
    expect(JSON.parse(init?.body as string)).toEqual({ action: 'accept', content: { name: 'Ada' } });
  });

  it('classifies every refusal the route has', async () => {
    respond(new Response(JSON.stringify({ errors: { email: 'not a valid email', '': 'x', bad: 3 } }), { status: 422 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'accept' }, 'k')).resolves.toEqual({
      kind: 'invalid',
      errors: { email: 'not a valid email', '': 'x' },
    });
    respond(new Response('{"error":"question already resolved"}', { status: 409 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).resolves.toEqual({ kind: 'closed' });
    respond(new Response('run has ended', { status: 410 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).resolves.toEqual({ kind: 'gone' });
    respond(new Response('question not found', { status: 404 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'decline' }, 'k')).rejects.toThrow('question not found');
  });

  it('reads a 422 with no usable body as no field errors', async () => {
    respond(new Response('<html>', { status: 422 }));
    await expect(postElicitationAnswer('r', 'q', { action: 'accept' }, 'k')).resolves.toEqual({ kind: 'invalid', errors: {} });
  });
});
```

Create `web/src/questions/__tests__/elicitationSteps.test.ts`:

```ts
import { describe, expect, it } from 'vitest';
import type { ElicitationField } from '../../chat/sseAdapter_elicitation';
import {
  contentFrom,
  fieldTitle,
  firstFailingStep,
  hasValue,
  initialValue,
  optionsFor,
  selectedIds,
  summaryOf,
  toggleValue,
} from '../elicitationSteps';

const LABELS = { yes: 'Yes', no: 'No' };
const field = (over: Partial<ElicitationField> & Pick<ElicitationField, 'name' | 'kind'>): ElicitationField => ({ required: false, ...over });

describe('elicitationSteps', () => {
  it("starts each field at the server's default, when the default fits", () => {
    expect(initialValue(field({ name: 'a', kind: 'boolean', default: true }))).toBe(true);
    expect(initialValue(field({ name: 'a', kind: 'boolean', default: 'yes' }))).toBeUndefined();
    expect(initialValue(field({ name: 'a', kind: 'integer', default: 3 }))).toBe(3);
    expect(initialValue(field({ name: 'a', kind: 'string', default: 'blue' }))).toBe('blue');
    expect(initialValue(field({ name: 'a', kind: 'enum', enum: ['x'], default: 'x' }))).toBe('x');
    expect(initialValue(field({ name: 'a', kind: 'enum', multi: true, enum: ['x'], default: ['x'] }))).toEqual(['x']);
    expect(initialValue(field({ name: 'a', kind: 'enum', multi: true, enum: ['x'], default: 'x' }))).toBeUndefined();
  });

  it('knows when a step has a value', () => {
    expect([hasValue(undefined), hasValue('  '), hasValue([]), hasValue('a'), hasValue(0), hasValue(false), hasValue(['a'])]).toEqual([
      false, false, false, true, true, true, true,
    ]);
  });

  it('sends only what has a value, and a local date-time as an RFC 3339 instant', () => {
    const fields = [
      field({ name: 'when', kind: 'string', format: 'date-time' }),
      field({ name: 'n', kind: 'number' }),
      field({ name: 'empty', kind: 'string' }),
    ];
    const content = contentFrom(fields, { when: '2026-09-25T10:30', n: 2.5, empty: '' });
    expect(content.n).toBe(2.5);
    expect('empty' in content).toBe(false);
    expect(content.when).toBe(new Date('2026-09-25T10:30').toISOString());
  });

  it('draws enum rows with their titles, and a boolean as Yes and No', () => {
    expect(optionsFor(field({ name: 'p', kind: 'enum', enum: ['cat', 'dog'], enum_titles: ['Cat', ''] }), LABELS)).toEqual([
      { id: 'cat', label: 'Cat' },
      { id: 'dog', label: 'dog' },
    ]);
    expect(optionsFor(field({ name: 'b', kind: 'boolean' }), LABELS).map((o) => o.label)).toEqual(['Yes', 'No']);
  });

  it('toggles a row into the value its field keeps', () => {
    const multi = field({ name: 'm', kind: 'enum', multi: true, enum: ['a', 'b'] });
    expect(toggleValue(field({ name: 'b', kind: 'boolean' }), undefined, 'false')).toBe(false);
    expect(toggleValue(field({ name: 's', kind: 'enum', enum: ['a'] }), 'b', 'a')).toBe('a');
    expect(toggleValue(multi, ['a'], 'b')).toEqual(['a', 'b']);
    expect(toggleValue(multi, ['a', 'b'], 'a')).toEqual(['b']);
    expect([...selectedIds(true)]).toEqual(['true']);
    expect([...selectedIds(['a', 'b'])]).toEqual(['a', 'b']);
    expect([...selectedIds(undefined)]).toEqual([]);
  });

  it('goes back to the first refused field, or the first step for a whole-answer error', () => {
    const fields = [field({ name: 'a', kind: 'string' }), field({ name: 'b', kind: 'string' })];
    expect(firstFailingStep(fields, { b: 'bad' })).toBe(1);
    expect(firstFailingStep(fields, { '': 'bad' })).toBe(0);
  });

  it('summarises the answer as the operator saw it', () => {
    const fields = [
      field({ name: 'pet', kind: 'enum', enum: ['cat'], enum_titles: ['Cat'], title: 'Pet' }),
      field({ name: 'agree', kind: 'boolean' }),
      field({ name: 'tags', kind: 'enum', multi: true, enum: ['a', 'b'] }),
      field({ name: 'age', kind: 'integer' }),
      field({ name: 'note', kind: 'string' }),
    ];
    expect(summaryOf(fields, { pet: 'cat', agree: false, tags: ['a', 'b'], age: 36 }, LABELS)).toEqual([
      { label: 'Pet', value: 'Cat' },
      { label: 'agree', value: 'No' },
      { label: 'tags', value: 'a, b' },
      { label: 'age', value: '36' },
    ]);
    expect(fieldTitle(field({ name: 'n', kind: 'string', title: '' }))).toBe('n');
  });
});
```

Create `web/src/questions/__tests__/useCountdown.test.ts`:

```ts
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { formatRemaining, secondsLeft, useCountdown } from '../useCountdown';

describe('useCountdown', () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it('counts down each second and stops at zero', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-25T10:00:00Z'));
    const { result } = renderHook(() => useCountdown('2026-09-25T10:00:02Z'));
    expect(result.current).toBe(2);
    act(() => {
      vi.advanceTimersByTime(3000);
    });
    expect(result.current).toBe(0);
  });

  it('reads an unparseable deadline as already passed, and shows m:ss', () => {
    expect(secondsLeft('never', Date.now())).toBe(0);
    expect(formatRemaining(300)).toBe('5:00');
    expect(formatRemaining(59)).toBe('0:59');
  });
});
```

Create `web/src/questions/__tests__/ElicitationCard.test.tsx`:

```tsx
import { afterEach, describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import '../../i18n/i18n';
import type { ElicitationField, ElicitationQuestion } from '../../chat/sseAdapter_elicitation';
import { ElicitationCard } from '../ElicitationCard';
import type { ElicitationItem } from '../useThreadElicitations';

const EMAIL: ElicitationField = { name: 'email', kind: 'string', required: false, format: 'email', title: 'Email' };
const NAME: ElicitationField = { name: 'name', kind: 'string', required: true, title: 'Name', description: 'Your full name' };
const PET: ElicitationField = { name: 'pet', kind: 'enum', required: true, enum: ['cat', 'dog'], enum_titles: ['Cat', ''] };
const TOPPINGS: ElicitationField = { name: 'toppings', kind: 'enum', required: false, multi: true, enum: ['ham', 'egg'] };

function question(over: Partial<ElicitationQuestion> = {}): ElicitationQuestion {
  return {
    run_id: 'run-1',
    id: 'q-1',
    server: 'forms',
    tool: 'ask_name',
    message: 'Tell me about <b>you</b>',
    fields: [EMAIL, NAME, PET, TOPPINGS],
    deadline: new Date(Date.now() + 300_000).toISOString(),
    ...over,
  };
}

interface Post {
  readonly url: string;
  readonly body: unknown;
  readonly key: string | null;
}

function stubAnswers(posts: Post[], ...responses: Response[]) {
  vi.stubGlobal(
    'fetch',
    vi.fn((input: RequestInfo | URL, init?: RequestInit) => {
      const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      posts.push({ url, body: JSON.parse(init?.body as string), key: new Headers(init?.headers).get('Idempotency-Key') });
      return Promise.resolve(responses.shift() ?? new Response('{"status":"delivered"}', { status: 202 }));
    }),
  );
}

function renderCard(item: ElicitationItem, isStreaming = true) {
  return render(<ElicitationCard item={item} isStreaming={isStreaming} />);
}

describe('ElicitationCard', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it('walks one step per field, skips an optional one, and submits the content typed', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question() });

    expect(screen.getByText('Step 1 of 4')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Email' })).toBeTruthy();
    expect(screen.getByRole('textbox').getAttribute('type')).toBe('email');
    fireEvent.click(screen.getByRole('button', { name: 'Skip' }));

    expect(screen.getByText('Step 2 of 4')).toBeTruthy();
    expect(screen.getByText('Your full name')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Next' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    fireEvent.keyDown(screen.getByRole('textbox'), { key: 'Enter' });

    expect(screen.getByText('Step 3 of 4')).toBeTruthy();
    expect(screen.getByRole('option', { name: 'dog' })).toBeTruthy();
    fireEvent.click(screen.getByRole('option', { name: 'Cat' }));
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));

    expect(screen.getByRole('listbox').getAttribute('aria-multiselectable')).toBe('true');
    fireEvent.click(screen.getByRole('option', { name: 'ham' }));
    fireEvent.click(screen.getByRole('option', { name: 'egg' }));
    fireEvent.click(screen.getByRole('button', { name: 'Back' }));
    expect(screen.getByRole('option', { name: 'Cat' }).getAttribute('aria-selected')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'Submit' }));

    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.url).toBe('/agent/runs/run-1/elicitations/q-1');
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { name: 'Ada', pet: 'cat', toppings: ['ham', 'egg'] } });
    expect(posts[0]?.key).toMatch(/^[0-9a-f-]{36}$/);
  });

  it('draws a boolean as Yes and No, prefilled, and a number with its bounds', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const agree: ElicitationField = { name: 'agree', kind: 'boolean', required: true, default: true };
    const age: ElicitationField = { name: 'age', kind: 'integer', required: true, min: 0, max: 150 };
    renderCard({ question: question({ fields: [age, agree] }) });

    const input = screen.getByRole('spinbutton');
    expect([input.getAttribute('min'), input.getAttribute('max'), input.getAttribute('step')]).toEqual(['0', '150', '1']);
    fireEvent.change(input, { target: { value: '36' } });
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    expect(screen.getByRole('option', { name: 'Yes' }).getAttribute('aria-selected')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'Submit' }));
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'accept', content: { age: 36, agree: true } });
  });

  // Review Focus 5.
  it('a 422 puts the card back on the failing step, with the error there', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, new Response(JSON.stringify({ errors: { email: 'not a valid email' } }), { status: 422 }));
    renderCard({ question: question({ fields: [EMAIL, NAME] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'nope' } });
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    fireEvent.click(screen.getByRole('button', { name: 'Submit' }));

    await waitFor(() => {
      expect(screen.getByText('Step 1 of 2')).toBeTruthy();
    });
    const email = screen.getByRole('textbox') as HTMLInputElement;
    expect(email.value).toBe('nope');
    expect(email.getAttribute('aria-invalid')).toBe('true');
    expect(screen.getByText('This value is not valid.')).toBeTruthy();
    fireEvent.change(email, { target: { value: 'ada@example.com' } });
    expect(email.getAttribute('aria-invalid')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'Submit' }));
    await waitFor(() => {
      expect(posts).toHaveLength(2);
    });
    expect(posts[1]?.body).toEqual({ action: 'accept', content: { email: 'ada@example.com', name: 'Ada' } });
  });

  it('shows the receipt, with the answer given, once the stream says it was accepted', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    const q = question({ fields: [NAME] });
    const { rerender } = renderCard({ question: q });
    expect(screen.queryByText(/Step \d/)).toBeNull();
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Ada' } });
    fireEvent.click(screen.getByRole('button', { name: 'Submit' }));
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    rerender(<ElicitationCard item={{ question: q, outcome: 'accepted' }} isStreaming />);
    expect(screen.getByText('Answered.').closest('[data-tone]')?.getAttribute('data-tone')).toBe('success');
    expect(screen.getByText('Ada')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });

  it('makes every other ending a muted receipt', () => {
    for (const [outcome, label] of [
      ['declined', 'Declined.'],
      ['cancelled', 'Cancelled.'],
      ['expired', 'Expired: declined automatically.'],
    ] as const) {
      const { unmount } = renderCard({ question: question(), outcome });
      expect(screen.getByText(label).closest('[data-tone]')?.getAttribute('data-tone')).toBe('neutral');
      unmount();
    }
  });

  it('always offers Decline, and Cancel asks first while the run streams', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question() });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(screen.getByText('Cancel this request?')).toBeTruthy();
    expect(posts).toHaveLength(0);
    fireEvent.click(screen.getByRole('button', { name: 'Cancel request' }));
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'cancel' });
  });

  it('declines without sending anything the operator typed', async () => {
    const posts: Post[] = [];
    stubAnswers(posts);
    renderCard({ question: question({ fields: [NAME] }) });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    await waitFor(() => {
      expect(posts).toHaveLength(1);
    });
    expect(posts[0]?.body).toEqual({ action: 'decline' });
  });

  it('says a closed form was already resolved, and a failed send can be retried', async () => {
    const posts: Post[] = [];
    stubAnswers(posts, new Response('{"error":"question already resolved"}', { status: 409 }));
    const { unmount } = renderCard({ question: question() });
    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    expect(await screen.findByText('This form was already resolved.')).toBeTruthy();
    unmount();

    vi.stubGlobal('fetch', vi.fn(() => Promise.reject(new Error('offline'))));
    renderCard({ question: question() });
    fireEvent.click(screen.getByRole('button', { name: 'Decline' }));
    expect(await screen.findByText("Couldn't send your answer. Try again.")).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Decline' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('a form Aura refused says why and offers nothing', () => {
    renderCard({ question: question({ refusal: 'ambiguous_run', fields: [], message: '' }) });
    expect(screen.getByText('Aura declined this form because more than one conversation was using this server.')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.queryByRole('timer')).toBeNull();
  });

  it('names the server by its mount, keeps the message plain text, and counts down', () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-09-25T10:00:00Z'));
    renderCard({ question: question({ deadline: '2026-09-25T10:05:00Z' }) });
    expect(screen.getByText('forms')).toBeTruthy();
    expect(screen.getByText('ask_name')).toBeTruthy();
    expect(screen.getByText('Tell me about <b>you</b>')).toBeTruthy();
    expect(document.querySelector('b')).toBeNull();
    expect(screen.getByRole('timer').textContent).toContain('5:00');
    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(screen.getByRole('timer').textContent).toContain('4:59');
  });
});
```

In `web/src/approvals/__tests__/ThreadApprovalCards.test.tsx`, add:

```tsx
  it("draws a mounted server's forms after the approvals, and a settled one only while the run streams", () => {
    const form = (id: string) => ({ run_id: 'run-1', id, server: 'forms', message: 'm', fields: [], deadline: '2026-09-25T10:05:00Z' });
    const elicitations = [{ question: form('open') }, { question: form('done'), outcome: 'declined' as const }];
    const { rerender } = render(
      <QueryClientProvider client={client()}>
        <ThreadApprovalCards approvals={[]} elicitations={elicitations} isStreaming />
      </QueryClientProvider>,
    );
    expect(Array.from(document.querySelectorAll('[data-elicitation-id]')).map((el) => el.getAttribute('data-elicitation-id'))).toEqual(['open', 'done']);
    rerender(
      <QueryClientProvider client={client()}>
        <ThreadApprovalCards approvals={[]} elicitations={elicitations} isStreaming={false} />
      </QueryClientProvider>,
    );
    expect(document.querySelectorAll('[data-elicitation-id]')).toHaveLength(1);
  });
```

- [ ] **Step 2: Run them to verify they fail.** Web: `npx vitest run src/chat/sseAdapter.onElicitation.test.ts src/questions src/approvals/__tests__/ThreadApprovalCards.test.tsx`.

Expected FAIL: `Failed to resolve import "./sseAdapter_elicitation"`, and likewise for `../useThreadElicitations`, `../elicitationAnswer`, `../elicitationSteps`, `../useCountdown` and `../ElicitationCard`.

- [ ] **Step 3: Implement.** Create `web/src/chat/sseAdapter_elicitation.ts`:

```ts
import type { AguiFrame } from './sseAdapter_frames';

// sseAdapter_elicitation reads the two CUSTOM frames a mounted MCP server's form travels as
// (internal/agui/run_elicitation.go): aura.elicitation carries the question and
// aura.elicitation_resolved how it closed. Neither carries an answer value. Like aura.steer
// they are a PUMP-level signal, fired from streamSSE and sseResume's pumpBody and never reduced
// into a message part: a form is not part of the assistant's answer.

export type ElicitationKind = 'string' | 'number' | 'integer' | 'boolean' | 'enum';
export type ElicitationFormat = 'email' | 'uri' | 'date' | 'date-time';
export type ElicitationRefusal = 'unrenderable' | 'ambiguous_run';
export type ElicitationAction = 'accept' | 'decline' | 'cancel';

export interface ElicitationField {
  readonly name: string;
  readonly title?: string;
  readonly description?: string;
  readonly kind: ElicitationKind;
  readonly required: boolean;
  readonly default?: unknown;
  readonly enum?: readonly string[];
  readonly enum_titles?: readonly string[];
  readonly multi?: boolean;
  readonly format?: ElicitationFormat;
  readonly min?: number;
  readonly max?: number;
  readonly min_length?: number;
  readonly max_length?: number;
}

export interface ElicitationQuestion {
  readonly run_id: string;
  readonly id: string;
  /** The name Aura mounted the server under, never one the server gave itself. */
  readonly server: string;
  readonly tool?: string;
  readonly message: string;
  readonly fields: readonly ElicitationField[];
  readonly deadline: string;
  readonly refusal?: ElicitationRefusal;
}

export interface ElicitationResolved {
  readonly id: string;
  readonly action: ElicitationAction;
  readonly expired?: boolean;
}

export type ElicitationSignal =
  | { readonly kind: 'question'; readonly question: ElicitationQuestion }
  | { readonly kind: 'resolved'; readonly resolved: ElicitationResolved };

const KINDS: readonly string[] = ['string', 'number', 'integer', 'boolean', 'enum'];
const FORMATS: readonly string[] = ['email', 'uri', 'date', 'date-time'];
const REFUSALS: readonly string[] = ['unrenderable', 'ambiguous_run'];
const ACTIONS: readonly string[] = ['accept', 'decline', 'cancel'];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isStringList(value: unknown): boolean {
  return Array.isArray(value) && value.every((entry) => typeof entry === 'string');
}

function optional(value: unknown, holds: (value: unknown) => boolean): boolean {
  return value === undefined || holds(value);
}

const isString = (value: unknown) => typeof value === 'string';
const isNumber = (value: unknown) => typeof value === 'number';

function isField(value: unknown): value is ElicitationField {
  return (
    isRecord(value) &&
    typeof value.name === 'string' &&
    typeof value.kind === 'string' &&
    KINDS.includes(value.kind) &&
    typeof value.required === 'boolean' &&
    optional(value.title, isString) &&
    optional(value.description, isString) &&
    optional(value.enum, isStringList) &&
    optional(value.enum_titles, isStringList) &&
    optional(value.multi, (multi) => typeof multi === 'boolean') &&
    optional(value.format, (format) => typeof format === 'string' && FORMATS.includes(format)) &&
    optional(value.min, isNumber) &&
    optional(value.max, isNumber) &&
    optional(value.min_length, isNumber) &&
    optional(value.max_length, isNumber)
  );
}

function questionOf(value: unknown): ElicitationQuestion | null {
  if (!isRecord(value)) return null;
  const { run_id: runId, id, server, tool, message, deadline, refusal } = value;
  if (typeof runId !== 'string' || typeof id !== 'string' || typeof server !== 'string') return null;
  if (typeof message !== 'string' || typeof deadline !== 'string' || !optional(tool, isString)) return null;
  if (!optional(refusal, (code) => typeof code === 'string' && REFUSALS.includes(code))) return null;
  // A refused form's fields arrive as null: Go encodes the refusal's nil slice that way.
  const fields = value.fields ?? [];
  if (!Array.isArray(fields) || !fields.every(isField)) return null;
  return {
    run_id: runId,
    id,
    server,
    message,
    deadline,
    fields,
    ...(typeof tool === 'string' ? { tool } : {}),
    ...(typeof refusal === 'string' ? { refusal: refusal as ElicitationRefusal } : {}),
  };
}

function resolvedOf(value: unknown): ElicitationResolved | null {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.action !== 'string') return null;
  if (!ACTIONS.includes(value.action) || !optional(value.expired, (expired) => typeof expired === 'boolean')) return null;
  return {
    id: value.id,
    action: value.action as ElicitationAction,
    ...(value.expired === true ? { expired: true } : {}),
  };
}

/** Narrow a frame to its elicitation signal, or null: the aura.steer twin (steerNoticeValue). */
export function elicitationSignalValue(frame: AguiFrame): ElicitationSignal | null {
  if (frame.type !== 'CUSTOM') return null;
  if (frame.name === 'aura.elicitation') {
    const question = questionOf(frame.value);
    return question === null ? null : { kind: 'question', question };
  }
  if (frame.name === 'aura.elicitation_resolved') {
    const resolved = resolvedOf(frame.value);
    return resolved === null ? null : { kind: 'resolved', resolved };
  }
  return null;
}
```

In `web/src/chat/sseAdapter.ts`:
1. Add the import `import { elicitationSignalValue, type ElicitationSignal } from './sseAdapter_elicitation';`.
2. In `StreamRunOptions`, after `onSteer`, add:

```ts
  /** Fires once per aura.elicitation / aura.elicitation_resolved frame: a mounted MCP server's
   *  form and how it closed, from the PUMP, never from reduceFrame. */
  readonly onElicitation?: (signal: ElicitationSignal) => void;
```

3. In `StreamPostOptions`, after `onSteer`, add `readonly onElicitation?: (signal: ElicitationSignal) => void;`.
4. In `StreamSSEOptions`, add `readonly onElicitation?: ((signal: ElicitationSignal) => void) | undefined;`.
5. In `streamSSE`'s loop, after the steer lines, add:

```ts
    const elicitation = elicitationSignalValue(frame);
    if (elicitation !== null) opts.onElicitation?.(elicitation);
```

6. In both `streamPost` and `streamRun`, add `onElicitation: opts.onElicitation,` after `onSteer: opts.onSteer,`.
7. The CUSTOM-branch comment at 305 becomes: `aura.steer (amendment #132, STEER-03) and aura.elicitation* (spec 2026-09-25) are deliberately NOT handled here: …`. The rest of the sentence stays, naming `steerNoticeValue` and `elicitationSignalValue` as the decision points.

In `web/src/chat/sseResume.ts`:
- import `elicitationSignalValue, type ElicitationSignal` from `'./sseAdapter_elicitation'`;
- in `AttachRunOptions`, after `onSteer`, add `/** Mirrors StreamRunOptions.onElicitation on the reattach pump: a reloaded tab's form comes back. */` and `readonly onElicitation?: (signal: ElicitationSignal) => void;`;
- in `EngineOptions`, add `readonly onElicitation?: ((signal: ElicitationSignal) => void) | undefined;`;
- in `makeEngine`, add `onElicitation: opts.onElicitation,` after `onSteer: opts.onSteer,`;
- in `pumpBody`, after the steer lines, add:

```ts
    const elicitation = elicitationSignalValue(frame);
    if (elicitation !== null) eng.onElicitation?.(elicitation);
```

`ResilientRunOptions` extends `StreamRunOptions`, so the resilient run inherits the field and hands it to `makeEngine` with no further change.

In `web/src/chat/ExternalStoreChat_liveRun.ts`:
- import `type ElicitationSignal` from `'./sseAdapter_elicitation'`;
- add `readonly onElicitation?: ((signal: ElicitationSignal) => void) | undefined;` to `LiveRunAttachArgs`, after `onSteer`;
- destructure `onElicitation`;
- add `...(onElicitation !== undefined ? { onElicitation } : {}),` to the `attachRun` options, after the `onSteer` spread;
- add `onElicitation,` to `attachLiveRun`'s dependency list.

In `web/src/chat/ExternalStoreChat_streams.ts`:
- add the same field to `StreamFoldDeps`, after `onArtifact`, and destructure it;
- add `...(onElicitation !== undefined ? { onElicitation } : {}),` to both `streamPost` calls;
- add `onElicitation` to `foldReRun`'s and `foldResumeRun`'s dependency lists. A resumed ask_user turn runs detached through `/agent/run` and can meet a form too.

In `web/src/chat/ExternalStoreChat.tsx`:
- add `import { useThreadElicitations } from '../questions/useThreadElicitations';` after the `useThreadApprovals` import;
- after line 139 (`const steer = …`), add `const { items: elicitations, onSignal: onElicitation } = useThreadElicitations(threadId);`;
- after `onSteer: steer.onFrame,` at line 247, add `onElicitation,`;
- add `onElicitation,` to that callback's dependency list, after `steer,` at line 296;
- add `onElicitation,` to the `useStreamFolds({…})` object, after `onArtifact,`;
- add `onElicitation,` to `useLiveRunAttach({…})`, after `onSteer: steer.onFrame,`;
- add `elicitations={elicitations}` to `<ThreadApprovalCards …>`, after `approvals=`.
- Then check the size: `wc -l web/src/chat/ExternalStoreChat.tsx` must print at most 599.

Create `web/src/questions/useThreadElicitations.ts`:

```ts
import { useCallback, useState } from 'react';
import type { ElicitationQuestion, ElicitationResolved, ElicitationSignal } from '../chat/sseAdapter_elicitation';

// useThreadElicitations holds the thread's MCP forms as the stream tells them: a question
// arrives, and its resolution settles it. The stream is the only source, because the server
// persists nothing: after a reload the attach replays the run from its first frame and the
// forms come back from there.

export type ElicitationOutcome = 'accepted' | 'declined' | 'cancelled' | 'expired';

export interface ElicitationItem {
  readonly question: ElicitationQuestion;
  readonly outcome?: ElicitationOutcome;
}

function outcomeOf(resolved: ElicitationResolved): ElicitationOutcome {
  if (resolved.expired === true) return 'expired';
  if (resolved.action === 'accept') return 'accepted';
  return resolved.action === 'decline' ? 'declined' : 'cancelled';
}

/**
 * Fold one signal into the list. A question already held (a replay) changes nothing; the first
 * question of a new run drops the settled cards of earlier runs; a resolution settles its
 * question once and is ignored for a question never seen.
 */
export function applyElicitationSignal(
  items: readonly ElicitationItem[],
  signal: ElicitationSignal,
): readonly ElicitationItem[] {
  if (signal.kind === 'question') {
    const { question } = signal;
    if (items.some((item) => item.question.id === question.id)) return items;
    const kept = items.filter((item) => item.outcome === undefined || item.question.run_id === question.run_id);
    return [...kept, { question }];
  }
  const outcome = outcomeOf(signal.resolved);
  return items.map((item) =>
    item.question.id === signal.resolved.id && item.outcome === undefined ? { ...item, outcome } : item,
  );
}

export function useThreadElicitations(threadId: string): {
  readonly items: readonly ElicitationItem[];
  readonly onSignal: (signal: ElicitationSignal) => void;
} {
  const [state, setState] = useState<{ readonly threadId: string; readonly items: readonly ElicitationItem[] }>({
    threadId,
    items: [],
  });
  const onSignal = useCallback(
    (signal: ElicitationSignal) => {
      setState((current) => ({
        threadId,
        items: applyElicitationSignal(current.threadId === threadId ? current.items : [], signal),
      }));
    },
    [threadId],
  );
  return { items: state.threadId === threadId ? state.items : [], onSignal };
}
```

Create `web/src/questions/elicitationAnswer.ts`:

```ts
import { errorDetail } from '../chat/http';
import type { ElicitationAction } from '../chat/sseAdapter_elicitation';

// elicitationAnswer posts the operator's answer to POST /agent/runs/{runID}/elicitations/{id}
// (internal/agui/server_run_elicitation.go). The route requires an Idempotency-Key
// (idempotency_http.go: agent_run_elicitation_answer). The card mints one per submit, and
// nothing retries a submit, so a replay can only be the transport's own.

/** internal/elicit ErrRequired: the one per-field error the card has its own copy for. */
export const REQUIRED_ERROR = 'required';

export interface ElicitationAnswerBody {
  readonly action: ElicitationAction;
  readonly content?: Readonly<Record<string, unknown>>;
}

export type ElicitationAnswerResult =
  | { readonly kind: 'delivered' }
  | { readonly kind: 'invalid'; readonly errors: Readonly<Record<string, string>> }
  | { readonly kind: 'closed' }
  | { readonly kind: 'gone' };

export async function postElicitationAnswer(
  runId: string,
  id: string,
  body: ElicitationAnswerBody,
  idempotencyKey: string,
): Promise<ElicitationAnswerResult> {
  const res = await fetch(`/agent/runs/${encodeURIComponent(runId)}/elicitations/${encodeURIComponent(id)}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idempotencyKey },
    credentials: 'same-origin',
    body: JSON.stringify(body),
  });
  switch (res.status) {
    case 202:
      return { kind: 'delivered' };
    case 409:
      return { kind: 'closed' };
    case 410:
      return { kind: 'gone' };
    case 422:
      return { kind: 'invalid', errors: await fieldErrors(res) };
    default:
      throw new Error(await errorDetail(res));
  }
}

async function fieldErrors(res: Response): Promise<Record<string, string>> {
  const body: unknown = await res.json().catch(() => null);
  if (typeof body !== 'object' || body === null) return {};
  const errors: unknown = (body as { errors?: unknown }).errors;
  if (typeof errors !== 'object' || errors === null) return {};
  return Object.fromEntries(
    Object.entries(errors).filter((entry): entry is [string, string] => typeof entry[1] === 'string'),
  );
}
```

Read `web/src/chat/http.ts:5` before relying on `errorDetail`'s message format. The test expects the 404 body text in the thrown message.

Create `web/src/questions/elicitationSteps.ts`:

```ts
import type { ElicitationField } from '../chat/sseAdapter_elicitation';
import type { ReceiptLine } from './QuestionReceipt';

// elicitationSteps is the MCP form's pure logic: where each step starts, whether it can go on,
// what an accept sends, and what the receipt shows. Kept out of the .tsx files so those export
// only components.

export type FieldValue = string | number | boolean | readonly string[];
export type FieldValues = Readonly<Record<string, FieldValue | undefined>>;

export interface BooleanLabels {
  readonly yes: string;
  readonly no: string;
}

export interface StepOption {
  readonly id: string;
  readonly label: string;
}

function isStringList(value: unknown): value is readonly string[] {
  return Array.isArray(value) && value.every((entry) => typeof entry === 'string');
}

/** A field's starting value: the server's default when it fits the field, else nothing. */
export function initialValue(field: ElicitationField): FieldValue | undefined {
  const { default: fallback } = field;
  switch (field.kind) {
    case 'boolean':
      return typeof fallback === 'boolean' ? fallback : undefined;
    case 'number':
    case 'integer':
      return typeof fallback === 'number' ? fallback : undefined;
    case 'enum':
      if (field.multi === true) return isStringList(fallback) ? fallback : undefined;
      return typeof fallback === 'string' ? fallback : undefined;
    case 'string':
      return typeof fallback === 'string' ? fallback : undefined;
  }
}

export function initialValues(fields: readonly ElicitationField[]): FieldValues {
  return Object.fromEntries(fields.map((field) => [field.name, initialValue(field)]));
}

export function hasValue(value: FieldValue | undefined): boolean {
  if (value === undefined) return false;
  if (typeof value === 'string') return value.trim() !== '';
  if (isStringList(value)) return value.length > 0;
  return true;
}

/**
 * The content an accept sends: every field with a value. A datetime-local value becomes the
 * RFC 3339 instant the server's date-time check reads (internal/elicit/validate.go).
 */
export function contentFrom(fields: readonly ElicitationField[], values: FieldValues): Record<string, unknown> {
  const content: Record<string, unknown> = {};
  for (const field of fields) {
    const value = values[field.name];
    if (value === undefined || !hasValue(value)) continue;
    content[field.name] = field.format === 'date-time' && typeof value === 'string' ? new Date(value).toISOString() : value;
  }
  return content;
}

export function fieldTitle(field: ElicitationField): string {
  return field.title !== undefined && field.title !== '' ? field.title : field.name;
}

/** The rows an enum or a boolean field shows. An option the server gave no title reads as its value. */
export function optionsFor(field: ElicitationField, labels: BooleanLabels): StepOption[] {
  if (field.kind === 'boolean') {
    return [
      { id: 'true', label: labels.yes },
      { id: 'false', label: labels.no },
    ];
  }
  return (field.enum ?? []).map((value, index) => {
    const title = field.enum_titles?.[index];
    return { id: value, label: title !== undefined && title !== '' ? title : value };
  });
}

export function selectedIds(value: FieldValue | undefined): ReadonlySet<string> {
  if (typeof value === 'boolean') return new Set([String(value)]);
  if (typeof value === 'string') return new Set([value]);
  if (isStringList(value)) return new Set(value);
  return new Set();
}

/** A row chosen: a boolean becomes true or false, a single choice its value, and a multi choice gains or loses it. */
export function toggleValue(field: ElicitationField, value: FieldValue | undefined, id: string): FieldValue {
  if (field.kind === 'boolean') return id === 'true';
  if (field.multi !== true) return id;
  const chosen = isStringList(value) ? value : [];
  return chosen.includes(id) ? chosen.filter((entry) => entry !== id) : [...chosen, id];
}

/** The step a 422 sends the card back to: the first refused field in step order, else the first step. */
export function firstFailingStep(fields: readonly ElicitationField[], errors: Readonly<Record<string, string>>): number {
  return Math.max(
    fields.findIndex((field) => errors[field.name] !== undefined),
    0,
  );
}

/** The receipt's lines: each field that was answered, as the operator saw it. */
export function summaryOf(fields: readonly ElicitationField[], values: FieldValues, labels: BooleanLabels): ReceiptLine[] {
  return fields.flatMap((field) => {
    const value = values[field.name];
    if (value === undefined || !hasValue(value)) return [];
    return [{ label: fieldTitle(field), value: display(field, value, labels) }];
  });
}

function display(field: ElicitationField, value: FieldValue, labels: BooleanLabels): string {
  if (typeof value === 'boolean') return value ? labels.yes : labels.no;
  if (typeof value === 'number') return String(value);
  const ids = typeof value === 'string' ? [value] : value;
  if (field.kind !== 'enum') return ids.join(', ');
  const options = optionsFor(field, labels);
  return ids.map((id) => options.find((option) => option.id === id)?.label ?? id).join(', ');
}
```

Create `web/src/questions/useCountdown.ts`:

```ts
import { useEffect, useState } from 'react';

/** Seconds left until an RFC 3339 deadline, re-read every second. Never negative. */
export function useCountdown(deadline: string): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => {
      setNow(Date.now());
    }, 1000);
    return () => {
      window.clearInterval(timer);
    };
  }, []);
  return secondsLeft(deadline, now);
}

export function secondsLeft(deadline: string, now: number): number {
  const at = Date.parse(deadline);
  return Number.isNaN(at) ? 0 : Math.max(0, Math.ceil((at - now) / 1000));
}

/** The countdown's face, m:ss. */
export function formatRemaining(seconds: number): string {
  return `${String(Math.floor(seconds / 60))}:${String(seconds % 60).padStart(2, '0')}`;
}
```

Create `web/src/questions/FieldInput.tsx`:

```tsx
import { useTranslation } from 'react-i18next';
import { ariaInvalid } from '../a11y/aria';
import type { ElicitationField, ElicitationFormat } from '../chat/sseAdapter_elicitation';
import type { FieldValue } from './elicitationSteps';
import { Input } from '@/components/ui/input';

// FieldInput is a string or number step of an MCP form: an input typed for the field's format,
// or a numeric one with the field's bounds. Enter goes on to the next step.

const INPUT_TYPE: Record<ElicitationFormat, string> = {
  email: 'email',
  uri: 'url',
  date: 'date',
  'date-time': 'datetime-local',
};

export interface FieldInputProps {
  readonly field: ElicitationField;
  readonly labelledBy: string;
  readonly describedBy?: string;
  readonly value: FieldValue | undefined;
  readonly invalid: boolean;
  readonly disabled: boolean;
  readonly onChange: (value: FieldValue | undefined) => void;
  readonly onEnter: () => void;
}

export function FieldInput({ field, labelledBy, describedBy, value, invalid, disabled, onChange, onEnter }: FieldInputProps) {
  const { t } = useTranslation();
  const numeric = field.kind === 'number' || field.kind === 'integer';
  const type = numeric ? 'number' : field.format === undefined ? 'text' : INPUT_TYPE[field.format];
  return (
    <Input
      type={type}
      aria-labelledby={labelledBy}
      {...(describedBy !== undefined ? { 'aria-describedby': describedBy } : {})}
      aria-invalid={ariaInvalid(invalid)}
      value={typeof value === 'string' || typeof value === 'number' ? value : ''}
      disabled={disabled}
      placeholder={t('questionCard.placeholder')}
      {...(numeric ? { step: field.kind === 'integer' ? 1 : 'any' } : {})}
      {...(field.min !== undefined ? { min: field.min } : {})}
      {...(field.max !== undefined ? { max: field.max } : {})}
      {...(field.min_length !== undefined ? { minLength: field.min_length } : {})}
      {...(field.max_length !== undefined ? { maxLength: field.max_length } : {})}
      onChange={(event) => {
        const raw = event.target.value;
        if (raw === '') onChange(undefined);
        else onChange(numeric ? Number(raw) : raw);
      }}
      onKeyDown={(event) => {
        if (event.key !== 'Enter') return;
        event.preventDefault();
        onEnter();
      }}
      className="bg-surface text-sm"
    />
  );
}
```

Create `web/src/questions/ElicitationHeader.tsx`:

```tsx
import { useTranslation } from 'react-i18next';
import { Clock3, Server } from 'lucide-react';
import type { ElicitationQuestion } from '../chat/sseAdapter_elicitation';
import { formatRemaining, useCountdown } from './useCountdown';
import { Badge } from '@/components/ui/badge';

// ElicitationHeader says who is asking. The chip carries the name Aura mounted the server
// under, never one the server gave itself, and the message is the server's own words as plain
// text: React escapes it, and nothing here parses markup.

export interface ElicitationHeaderProps {
  readonly question: ElicitationQuestion;
  readonly countdown: boolean;
}

export function ElicitationHeader({ question, countdown }: ElicitationHeaderProps) {
  const { t } = useTranslation();
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
        <Badge variant="secondary" className="gap-1">
          <Server aria-hidden="true" className="size-3.5" />
          <span className="sr-only">{t('questionCard.form.server', { server: question.server })}</span>
          <span aria-hidden="true">{question.server}</span>
        </Badge>
        {question.tool !== undefined ? <span className="font-mono">{question.tool}</span> : null}
        {countdown ? <Countdown deadline={question.deadline} /> : null}
      </div>
      {question.message !== '' ? (
        <p className="text-sm leading-relaxed whitespace-pre-wrap break-words [overflow-wrap:anywhere]">
          {question.message}
        </p>
      ) : null}
    </div>
  );
}

function Countdown({ deadline }: { readonly deadline: string }) {
  const { t } = useTranslation();
  const time = formatRemaining(useCountdown(deadline));
  return (
    <span
      role="timer"
      aria-label={t('questionCard.form.expiresIn', { time })}
      className="ms-auto inline-flex items-center gap-1 tabular-nums"
    >
      <Clock3 aria-hidden="true" className="size-3.5" />
      {time}
    </span>
  );
}
```

Create `web/src/questions/ElicitationCard.tsx`:

```tsx
import { useId, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { ChevronLeft } from 'lucide-react';
import type { ElicitationAction } from '../chat/sseAdapter_elicitation';
import { CancelControl } from './CancelControl';
import { ElicitationHeader } from './ElicitationHeader';
import { FieldInput } from './FieldInput';
import { QuestionCard, type QuestionCardProps } from './QuestionCard';
import { QuestionOptions } from './QuestionOptions';
import { QuestionReceipt, type ReceiptTone } from './QuestionReceipt';
import { postElicitationAnswer, REQUIRED_ERROR } from './elicitationAnswer';
import {
  contentFrom,
  fieldTitle,
  firstFailingStep,
  hasValue,
  initialValues,
  optionsFor,
  selectedIds,
  summaryOf,
  toggleValue,
  type FieldValue,
  type FieldValues,
} from './elicitationSteps';
import type { ElicitationItem, ElicitationOutcome } from './useThreadElicitations';
import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';

// ElicitationCard is a mounted MCP server's form drawn as Question Flow (spec 2026-09-25): one
// step per field, Back and Next, Submit on the last step, and Decline and Cancel always in the
// footer. The answer goes to the run's route. How the question closed arrives on the stream,
// so the receipt shows the server's state rather than the card's guess.

const RECEIPTS: Record<ElicitationOutcome, { readonly tone: ReceiptTone; readonly key: string }> = {
  accepted: { tone: 'success', key: 'questionCard.receipt.answered' },
  declined: { tone: 'neutral', key: 'questionCard.receipt.declined' },
  cancelled: { tone: 'neutral', key: 'questionCard.receipt.cancelled' },
  expired: { tone: 'neutral', key: 'questionCard.receipt.expired' },
};

type Problem = 'failed' | 'closed' | 'invalid';

const PROBLEM_KEYS: Record<Problem, string> = {
  failed: 'questionCard.error.failed',
  closed: 'questionCard.error.closed',
  invalid: 'questionCard.error.invalid',
};

export interface ElicitationCardProps {
  readonly item: ElicitationItem;
  readonly isStreaming?: boolean;
}

export function ElicitationCard({ item, isStreaming }: ElicitationCardProps) {
  const { t } = useTranslation();
  const baseId = useId();
  const { question, outcome } = item;
  const { fields } = question;
  const [step, setStep] = useState(0);
  const [values, setValues] = useState<FieldValues>(() => initialValues(fields));
  const [errors, setErrors] = useState<Readonly<Record<string, string>>>({});
  const [busy, setBusy] = useState(false);
  const [sent, setSent] = useState<ElicitationAction | null>(null);
  const [problem, setProblem] = useState<Problem | null>(null);
  const titleId = `${baseId}-title`;
  const labels = { yes: t('questionCard.yes'), no: t('questionCard.no') };
  const settled = outcome !== undefined || question.refusal !== undefined;

  function frame(children: ReactNode, extra: Partial<QuestionCardProps> = {}) {
    return (
      <QuestionCard
        titleId={titleId}
        title={t('questionCard.form.title', { server: question.server })}
        header={<ElicitationHeader question={question} countdown={!settled} />}
        dataAttributes={{ 'data-elicitation-id': question.id }}
        {...extra}
      >
        {children}
      </QuestionCard>
    );
  }

  if (question.refusal !== undefined) {
    return frame(<QuestionReceipt tone="neutral" label={t(`questionCard.refusal.${question.refusal}`)} />);
  }
  if (outcome !== undefined) {
    const receipt = RECEIPTS[outcome];
    return frame(
      <QuestionReceipt
        tone={receipt.tone}
        label={t(receipt.key)}
        announce
        {...(outcome === 'accepted' && sent === 'accept' ? { summary: summaryOf(fields, values, labels) } : {})}
      />,
    );
  }

  const field = fields[step];
  const last = step >= fields.length - 1;
  const locked = busy || sent !== null;
  const value = field === undefined ? undefined : values[field.name];
  const error = field === undefined ? undefined : errors[field.name];
  const canGoOn = field === undefined || !field.required || hasValue(value);
  const errorId = `${baseId}-error`;

  async function send(action: ElicitationAction, current: FieldValues = values) {
    setBusy(true);
    setProblem(null);
    try {
      const result = await postElicitationAnswer(
        question.run_id,
        question.id,
        action === 'accept' ? { action, content: contentFrom(fields, current) } : { action },
        crypto.randomUUID(),
      );
      if (result.kind === 'delivered') {
        setSent(action);
      } else if (result.kind === 'invalid') {
        setErrors(result.errors);
        setStep(firstFailingStep(fields, result.errors));
        if (result.errors[''] !== undefined) setProblem('invalid');
      } else {
        setProblem('closed');
      }
    } catch {
      setProblem('failed');
    } finally {
      setBusy(false);
    }
  }

  function advance(current: FieldValues = values) {
    if (last) void send('accept', current);
    else setStep(step + 1);
  }

  function update(name: string, next: FieldValue | undefined) {
    setValues((current) => ({ ...current, [name]: next }));
    setErrors((current) => Object.fromEntries(Object.entries(current).filter(([key]) => key !== name)));
  }

  function skip() {
    if (field === undefined) return;
    const cleared = { ...values, [field.name]: undefined };
    setValues(cleared);
    advance(cleared);
  }

  const next = () => {
    if (canGoOn && !locked) advance();
  };

  const body =
    field === undefined ? null : field.kind === 'enum' || field.kind === 'boolean' ? (
      <QuestionOptions
        key={field.name}
        labelledBy={titleId}
        options={optionsFor(field, labels)}
        mode={field.kind === 'enum' && field.multi === true ? 'multi' : 'single'}
        selected={selectedIds(value)}
        disabled={locked}
        onToggle={(id) => {
          update(field.name, toggleValue(field, value, id));
        }}
        onSubmit={next}
      />
    ) : (
      <FieldInput
        key={field.name}
        field={field}
        labelledBy={titleId}
        {...(error !== undefined ? { describedBy: errorId } : {})}
        value={value}
        invalid={error !== undefined}
        disabled={locked}
        onChange={(changed) => {
          update(field.name, changed);
        }}
        onEnter={next}
      />
    );

  const footer = (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button
          type="button"
          variant="ghost"
          disabled={locked}
          onClick={() => void send('decline')}
          className="text-[0.8125rem] text-text-muted hover:text-text"
        >
          {t('questionCard.decline')}
        </Button>
        <CancelControl
          isStreaming={isStreaming}
          disabled={locked}
          labels={{
            cancel: t('questionCard.cancel.label'),
            confirm: t('questionCard.cancel.confirm'),
            yes: t('questionCard.cancel.yes'),
            no: t('questionCard.cancel.no'),
          }}
          onCancel={() => void send('cancel')}
        />
      </div>
      <div className="flex items-center gap-2">
        {step > 0 ? (
          <Button
            type="button"
            variant="ghost"
            disabled={locked}
            onClick={() => {
              setStep(step - 1);
            }}
            className="gap-1 rounded-full text-text-muted"
          >
            <ChevronLeft aria-hidden="true" className="size-4" />
            {t('questionCard.back')}
          </Button>
        ) : null}
        {field !== undefined && !field.required ? (
          <Button type="button" variant="ghost" disabled={locked} onClick={skip} className="rounded-full text-text-muted">
            {t('questionCard.skip')}
          </Button>
        ) : null}
        <Button type="button" disabled={locked || !canGoOn} onClick={next} className="rounded-full">
          {last ? t('questionCard.submit') : t('questionCard.next')}
        </Button>
      </div>
    </>
  );

  const status =
    problem === null ? null : (
      <Alert role="status" aria-live="polite" data-tone="danger" variant="destructive" className="bg-surface">
        <AlertDescription>{t(PROBLEM_KEYS[problem])}</AlertDescription>
      </Alert>
    );

  return frame(
    <>
      {body}
      {error !== undefined ? (
        <p id={errorId} className="text-[0.8125rem] text-danger">
          {error === REQUIRED_ERROR ? t('questionCard.error.required') : t('questionCard.error.invalid')}
        </p>
      ) : null}
    </>,
    {
      ...(field !== undefined ? { title: fieldTitle(field) } : {}),
      ...(field?.description !== undefined
        ? { description: field.description, descriptionId: `${baseId}-description` }
        : {}),
      step: { current: step + 1, total: Math.max(fields.length, 1) },
      footer,
      ...(status !== null ? { status } : {}),
    },
  );
}
```

`frame`'s `extra` spreads after the defaults, so a field step's own `title` replaces the form title.

In `web/src/approvals/ThreadApprovalCards.tsx`:
- import `ElicitationCard` from `'../questions/ElicitationCard'` and `type ElicitationItem` from `'../questions/useThreadElicitations'`;
- add the prop:

```tsx
  /** A mounted MCP server's forms for this thread, in arrival order. A settled one stays as a
   *  receipt while the run streams and goes with the run. */
  readonly elicitations?: readonly ElicitationItem[];
```

- destructure `elicitations = []` and compute `const forms = elicitations.filter((item) => item.outcome === undefined || isStreaming === true);`;
- the container's `className` condition becomes `approvals.length + forms.length > 0`;
- after the approvals `map`, render:

```tsx
      {forms.map((item) => (
        <ElicitationCard key={item.question.id} item={item} {...(isStreaming !== undefined ? { isStreaming } : {})} />
      ))}
```

Add to `web/stryker.config.json`'s `mutate`:
- `src/chat/sseAdapter_elicitation.ts`
- `src/questions/useThreadElicitations.ts`
- `src/questions/elicitationAnswer.ts`
- `src/questions/elicitationSteps.ts`
- `src/questions/useCountdown.ts`
- `src/questions/FieldInput.tsx`
- `src/questions/ElicitationHeader.tsx`
- `src/questions/ElicitationCard.tsx`

- [ ] **Step 4: Run the checks.**
  - Web: `npx vitest run src/chat src/questions src/approvals src/i18n`. Expected: every test passes. The existing `sseAdapter.onSteer.test.ts` and `sseResume` suites must stay green unchanged.
  - Web: `npm run typecheck`. Expected: exit 0.
  - Web: `npm run lint`. `Found 0 errors` is required.
  - Web: `npx prettier --check src`.
  - Web: `npm run dup`.
  - Web: `npm run deadcode`. knip must report no unused file or export among the new ones.
  - Web: `npm run test`. This is the whole suite with coverage; the thresholds are 85% on statements, branches, functions and lines.
  - Check the sizes: `wc -l web/src/chat/ExternalStoreChat.tsx web/src/chat/sseAdapter.ts web/src/chat/sseResume.ts web/src/questions/*.tsx`. Every file must be under 600 lines.

- [ ] **Step 5: Commit.**

```bash
cd /d/Aura
git add web/src/chat/sseAdapter_elicitation.ts web/src/chat/sseAdapter.onElicitation.test.ts web/src/questions/
git commit -F - -- web/src/chat/sseAdapter_elicitation.ts web/src/chat/sseAdapter.onElicitation.test.ts web/src/chat/sseAdapter.ts web/src/chat/sseResume.ts web/src/chat/ExternalStoreChat.tsx web/src/chat/ExternalStoreChat_liveRun.ts web/src/chat/ExternalStoreChat_streams.ts web/src/questions/ web/src/approvals/ThreadApprovalCards.tsx web/src/approvals/__tests__/ThreadApprovalCards.test.tsx web/stryker.config.json <<'EOF'
feat(cockpit): answer a mounted server's form in the thread

aura.elicitation and aura.elicitation_resolved are read from the pump,
like aura.steer: the live stream and the reattach pump both fire them,
so a reload brings an open form back. They are never reduced into a
message part.

The thread holds its forms in arrival order. A replayed question is
ignored, and a resolution settles its question once.

ElicitationCard draws the form as Question Flow:
- one step per field, with Back, Next, Skip on an optional field, and
  Submit on the last step;
- Decline and Cancel always in the footer, and Cancel confirms while
  the run streams;
- a chip naming the server as Aura mounted it, the message as plain
  text, and a countdown.

The fields:
- an enum is radio or checkbox rows;
- a boolean is Yes and No;
- strings and numbers are typed inputs with the server's bounds;
- defaults are prefilled.

A 422 puts the card back on the failing field's step with the error
there. The receipt follows the server's resolution, not the card's
guess.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

---
### Task 9: Gates, then push

- [ ] **Step 1: The pre-push gate.**
  - Check `git status` first: no other session may be mid-change in the tree. Gates run alone.
  - Go: `make quality`. Expected: `ok: quality gate passed`.
    - A `deadcode` finding on `summariseElicitationSchema`, `askOperatorBounded` or `elicitationPanicError` means a deletion was missed.
    - A `dupl` finding in non-test code means a helper was copied rather than shared.
  - Web, one command at a time: `npm run lint` (read `Found N errors`), `npm run typecheck`, `npm run format:check`, `npm run dup`, `npm run deadcode`, `npm run test`.
    - `npm run test` fails below 85% on statements, branches, functions or lines, and that is the gate.

- [ ] **Step 2: The coverage gate.** Bring the stack up if it is down: `make db-migrate memory-up`. Then Go: `bash -c 'unset AURA_WEB_AUTH_SECRET; bash scripts/coverage_docker.sh'`.
  - The `unset` is needed because `.env` leaks into the config tests.
  - The script provisions and drops only the disposable `aura_cov` database.

  Expected:
  - `ok: owned coverage NN.N% >= 85%`;
  - the package policy passes: `internal/pausable` and `internal/elicit` at 85% or above, their new `target` entries; `internal/agent`, `internal/agent/mcptools` and `internal/agui` at or above their pinned floors.

  If a package falls under its floor, write daemon-free tests for the uncovered lines. Never lower a floor.

- [ ] **Step 3: The embedded bundle.** The tree commits the cockpit build (`internal/webui/dist`, `.gitignore:18-22`; last done in `ad7701b41`).
  - The image rebuilds it anyway (`docker/aura/Dockerfile:19,62`). This step keeps a local `go build` in step with the source.
  - Web: `npm run build`.
  - Then commit it on its own:

```bash
cd /d/Aura
git add internal/webui/dist
git commit -F - -- internal/webui/dist <<'EOF'
build(web): embed the question card and the MCP form

Regenerates the committed cockpit bundle after the QuestionCard frame,
the ask_user redesign and the MCP elicitation card.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

- [ ] **Step 4: Push. ASK THE OPERATOR FIRST.** A push of `master` publishes the edge image, and the appliances install it. With the go, push from WSL so the lefthook gates run on the pushed commit:

```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
LEFTHOOK_BIN=$HOME/go/bin/lefthook git -c core.hooksPath=.git/hooks push origin master
```

Expected: the lefthook banner, with every pre-push command green. No banner means no gate ran.

Then run `gh run list --branch master --limit 10`. Every job must end green:
- the coverage job;
- the web jobs, which include Playwright (`ci.yml:1834`) and `web-mutation`, where Stryker must reach 70% on the new `mutate` entries;
- the Go mutation job;
- `Publish Aura edge image`.

A red job is fixed, whether or not it looks related to this change.

---

### Task 10: E2E on the lab VM, then the PRD records it

The PRD amendment comes after the E2E and records it (CLAUDE.md, "misura, poi emenda"). That is why the brief's Task 10 (PRD §13 and docs) and Task 11 (E2E) are one task here, in that order.

Every step changes the VM only through its updater. Mailbox and chat access is read-only. The operator drives the cockpit with their own account; you watch the backend.

- [ ] **Step 1: Measure the "before".** On the VM (`192.168.101.158`, over the WSL sshpass script pattern), record the tool count of each MCP server mounted today (memory, calendar, WhatsApp) from the aura mount log lines. Record the image id too. Spec step 7 compares against these.

- [ ] **Step 2: Let the updater converge.** Check `sudo systemctl list-timers` for the updater's next run and wait for it. The update applies after about 15 minutes of idle. Do not run the updater by hand.

  Then confirm the running `aura` container uses the new image: its `Image` id must match the pulled tag's `Id`. Then re-read the tool counts from Step 1. Each server must mount with the same count as before (spec step 7). Ask the operator for one ordinary call to each of the three, and check that each still returns.

- [ ] **Step 3: Mount the reference server** (spec step 1). The operator installs it in the cockpit (Governance, MCP, install), as their own server, with command `npx` and args `-y @modelcontextprotocol/server-everything`.
  - This is the body `POST /api/governance/mcp` takes (`MCPInstallRequest`, `governance_write_seam.go:47`).
  - Watch the install verify initialize and tools/list, and the mount log's tool count.
  - Record which protocol version it negotiates: `2025-11-25` means the classic path, `2026-07-28` means multi-round-trip. The run proves only the path it took.

- [ ] **Step 4: The form** (spec steps 2 and 3). The operator asks Aura to use `trigger-elicitation-request`. Watch:
  - in the cockpit, the card: the `everything` chip, the tool name, the countdown, "Step 1 of N", one step per field, and the defaults prefilled;
  - the operator reloads the page in the middle of the form, and the card comes back from the replay at step 1 with its fields;
  - the operator waits more than 60 s of human time and then submits;
  - the server echoes every value in the tool result;
  - `aura.tool_invocations` shows that call ending `ok`, with a duration over 60 s;
  - the aura log shows `mcp elicitation resolved` with `action=accept`, the field count, and none of the values. Grep the log for one of the values typed; it must not be found.

- [ ] **Step 5: Decline, expire, URL** (spec steps 4 and 5).
  - The operator declines a second form. The card shows the declined receipt, and the server reports the decline.
  - A third form is left alone for `AURA_MCP_ELICITATION_TIMEOUT_SEC` (300 s by default; the VM's configuration is not changed). The card shows expired, and the server gets a decline.
  - The operator asks for `trigger-url-elicitation`. No card appears; the log shows `reason="url mode is refused"`, and the server is declined.

- [ ] **Step 6: ask_user, redrawn** (spec step 6). The operator triggers one ask_user of each kind and takes screenshots of each card and its receipt:
  - a choice (radio rows, Answer grey until chosen);
  - a clarification;
  - an approval, where a gateway-gated mutation gives the scope rows. A destructive one shows the destructive variant.

- [ ] **Step 7: Clean up** (spec step 8). The operator unmounts `everything` in the cockpit. Confirm the tools are gone from the mount log and that the sidecar environment was removed. Delete any personal copy the test produced, with the operator watching.

- [ ] **Step 8: Score.** Score the run against the spec's E2E list, steps 1 to 8. A score of 9.8 or more closes it. Anything less goes back to the task that owns the gap.

- [ ] **Step 9: Record it in the PRD.** In `prd.md` §13, replace lines 656-658, from "The production elicitation wiring follows decline-and-surface: …" to "… approval row.", with the spec's wording:

```markdown
A cockpit turn shows a mounted server's form elicitation in its thread. The call stays open,
with its clock and the run's paused, until the operator accepts, declines or cancels, or
`AURA_MCP_ELICITATION_TIMEOUT_SEC` passes. Turns with no cockpit, and requests Aura cannot
place in a single run, keep decline-and-surface. URL mode stays refused.
```

  Follow it with a dated paragraph (2026-MM-DD, the day of the run) recording what was measured:
  - the protocol path server-everything took;
  - the human time the call outlived, against the 60 s call bound and the 300 s run bound;
  - the reload replay;
  - the decline, the expiry and the URL refusal;
  - the unchanged tool counts of memory, calendar and WhatsApp;
  - the request ids used as evidence.

  It also says what the run does NOT prove:
  - the protocol path the run did not take;
  - two conversations sharing one session, which only the integration test covers;
  - the per-node timeout, which is off on the VM;
  - forms at the caps;
  - a Telegram operator's decline-and-surface after this change.

  The spec has no status line. Add one sentence after its first paragraph (`docs/superpowers/specs/2026-09-25-mcp-elicitation-question-card-design.md:3-8`): "Implemented on 2026-MM-DD (<the commit range>); measured on the lab VM, prd.md §13." 

  Commit with explicit paths:

```bash
cd /d/Aura
git commit -F - -- prd.md docs/superpowers/specs/2026-09-25-mcp-elicitation-question-card-design.md <<'EOF'
docs(prd): record MCP form elicitation as measured on the lab VM

§13 no longer says decline-and-surface for everything. A cockpit turn
now shows a mounted server's form in its thread, with the call's and
the run's clocks paused. The paragraph carries the VM run's
measurements and what that run does not prove.

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
EOF
```

  Push after the operator's go, as in Task 9 Step 4.

---

## Open points

1. **Tool UI cannot be used as the spec says. This one blocks Task 7.** The spec wants three things at once:
   - `question-flow`, `option-list` and `approval-card` installed and "adapted only where Aura's tokens or lint require it";
   - every string in en and it;
   - text, number, email, URI and date steps, plus Skip, Decline and Cancel.

   The installed sources, read on 2026-09-25 from `https://www.tool-ui.com/r/{name}.json`, cannot meet the other two unmodified:
   - Question Flow hard-codes "Step N of M", "Back", "Next", "Complete" and the receipt's "Complete";
   - its steps are option lists only;
   - its footer has only Back and Next;
   - `ApprovalCard` denies on Escape (the spec: "Escape never cancels a run") and has no slot for scopes;
   - the internals the cards need (`OptionItem`, `SelectionIndicator`, `ProgressBar`) are not exported;
   - `question-flow.tsx` is 793 lines and `option-list.tsx` 625;
   - `size="lg"` does not exist on Aura's Button;
   - `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes` will flag more lines in the vendored code.

   There are two options:
   - **V (recommended; Tasks 7 and 8 are written for it).** Register `@tool-ui` in `components.json` and install nothing yet. Port the markup and classes into Aura files under 600 lines, translated and tested. Spec 2, the review of every tool against Tool UI, installs what it will actually use. Nothing unused lands, and CLAUDE.md forbids dark code.
   - **P.** Install the three as the spec says, excluded from lint, knip, prettier, coverage and the size cap like `model-selector.tsx`, and fix the typecheck. They would sit unimported until spec 2. That is dark code the operator would have to accept.

   Task 7 ends with P's extra steps.
2. **The plan adds four things the spec's types do not list:**
   - `Question.Refusal`, so a refused form is still shown, resolved;
   - `Question.Schema` (`json:"-"`), the server's schema kept for `Validate`;
   - `run_id` in `aura.elicitation`, because the answer route is run-scoped and a card restored from a replay has nowhere else to read it;
   - `expired` in `aura.elicitation_resolved`, because both an expiry and a decline reach the server as a decline, and only the card needs the difference.

   Each is additive. None carries an answer value.
3. **Fields come sorted by name, not in the server's order.** go-sdk decodes `RequestedSchema` into `map[string]any`, and jsonschema-go's `PropertyOrder` is `json:"-"`. The server's order is gone before the handler runs, and there is none to keep.
4. **An expired wait now declines on the fallback path too.** Today `TestElicitationTimesOutToCancel` pins cancel. The spec's error table makes an expiry a decline, and Task 4 applies that everywhere and renames the test. Cancel stays for the call or the run ending.
5. **The per-node tool timeout is a third clock the spec does not name.** When `AURA_LOOP_NODE_TIMEOUT_SEC` is set (it is off by default), it would cut every held wait, so Task 2 makes it pausable.
6. **The detached run's outer cap stays fixed.** It is `AURA_AGUI_RUN_MAX_WALLCLOCK_SEC`, 3600 s. With every other clock held, it is the one bound on a server that asks again and again. It cuts a real run only after about 55 minutes of forms in one turn. Recorded in `detachedRunContext`'s comment (Task 6).
7. **Only `handleRunDetached` installs an asker.** The coordinator-wake detached runs (`server_coordinator_wake.go:47`) get none, so a form there is decline-and-surfaced. The spec names `handleRunDetached` alone.
8. **"The operator is told" reaches a cockpit operator only if their run has an asker.** A classic request with no call in flight goes to the fallback. So does a request whose run has no cockpit. The fallback delivers on a channel, and a cockpit-only operator with no channel sees only the log. That is today's behaviour, unchanged.
9. **Routing step 1 is read as "the call's own context".** On the multi-round-trip path the handler's context is the call's, so the plan routes on that call alone, asker or none. The in-flight registry is consulted only for a classic request. The spec's order would, for a call with no asker, look at other runs' calls on the same session. That could decline a request the fallback should take.
10. **The brief's facts, checked:**
    - `bridge_supervisor.go` is 500 lines, not 509.
    - It has a second `session.CallTool` site at line 339, the redial retry, and Task 4 covers both.
    - `buildRegistryWithMCP`'s `consent` became dead in 89688bd27, as the brief says. `aura tools` (`main.go:549`) and the one-shot pipe keep passing nil, deliberately.
11. **Web tests run in WSL, per the brief**, through WSL's own node 24 in `~/.local/bin`. The memory note says Git Bash is faster when the win32 bindings exist; both binding sets are installed. The plan follows the brief.
12. **Task decomposition changed from the brief's eleven to ten, for two reasons:**
    - The web foundation, the ask_user card and the MCP card became two tasks. The frame lands with its first adapter, so no commit ships a component nothing uses.
    - The PRD amendment moved behind the E2E and merged with it. CLAUDE.md says to measure, then amend.

## Facts not verified (each is checked by the step that first depends on it)

- Whether go-sdk's server answers `server/discover` from a `2025-11-25`-only server in a way that makes the client fall back to `initialize` at `2025-11-25`. Read in `client.go:314-386`; not run. Task 4's classic tests show it.
- Whether jsonschema-go keeps `enumNames` in `Schema.Extra`, and whether it validates `float64(36)` as `integer`. Task 3's tests show both.
- Whether a streamable-HTTP classic server-to-client request completes under go-sdk at 2025-11-25. Task 6's classic subtest shows it.
- Whether a one-tool managed mount is always loaded rather than deferred, so the fake model can call it by name. Task 6's integration test shows it.
- Whether `runner.Deps` accepts a nil `Steer`, and whether `llm.Config.LoopMaxWallclockSec` reaches the budget in `runner.New`. Task 6 shows both, and its control test `TestTheShortWallclockCutsAnUnheldTool` exists for the second.
- Which protocol version `@modelcontextprotocol/server-everything` negotiates, and whether the governance npx resolver installs it. Task 10 Step 3 shows both.
- Whether `import-order/order` accepts the new files' import order as written. Task 7 and Task 8 lint show it; reorder to match the neighbouring files, never add a disable comment.
- Tool UI's licence notice. The spec names the source repository, `assistant-ui/tool-ui`, as MIT (spec lines 72-73). The registry JSON carries no licence field, and the plan did not read the repository's LICENSE. Task 7 Step 3 reads it before the ported headers are written.
- For option P only: how shadcn's `--yes` treats the existing `button.tsx`, and whether `zod` 4.6.5 and `lucide-react` 1.40 satisfy the vendored `z.ZodIssueCode` and the `icons` import.

## Self-review

**Spec coverage.** Each spec section maps to a task:

| Spec section | Task |
|---|---|
| Decisions | 4, 6, 7, 8 |
| `internal/elicit`: types, seam, `FromSchema`, `Validate`, caps | 3 |
| mcptools: in-flight registry, routing order, the bound, URL mode, handler contract, wiring | 4 and 5 |
| The paused clock: `pausable`, the MCP call, the run, the handler's hold | 1, 2 and 4 |
| agui: the asker, `publish`, the route with its codes, the events without values | 6 |
| Cockpit: registry, `QuestionCard`, the ask_user shapes, the MCP form, inputs, header, footer, receipts, a 422 back to the failing step, placement, streaming file, keyboard and accessibility, en and it | 7 and 8 |
| Errors table | 4 (routing, URL, caps), 6 (409, 410, 422, parallel questions), 8 (card states) |
| Security: plain text, the server's name from Aura, no values in logs, an owner-scoped route, no form outliving its run | 4, 6 and 8 |
| Testing: unit, integration (classic and MRTR through the real detached handler, a shortened bound), web vitest and Stryker in CI, E2E steps 1-8 | 1-8, 9 and 10 |
| PRD | 10 |
| No new env vars, no migration | nothing added in any task |

Gaps found and fixed while writing:
- `ThreadApprovalCards`' two option clicks and the two Playwright specs, which the redesign breaks, were added to Task 7.
- The control test proving the harness's 2 s wallclock is real was added to Task 6.
- The `Skip`-on-last-step stale-values bug was fixed with an explicit `cleared` value.

**Placeholder scan.** No "TBD", "similar to Task N" or step without code.
- The only date left open is the PRD paragraph's `2026-MM-DD`, which is the day of the run and cannot be known now.
- Each "read X before choosing" sentence names the file and the decision it settles.

**Type consistency.** Checked across tasks:
- **Go:**
  - `elicit.Question`, `Field`, `Answer`, `Asker`, the `Action*`, `Refusal*` and `Kind*` constants, `ErrExpired`, `ErrRequired`, `FieldErrors`, `DecodeSchema`, `FromSchema` and `Validate` (Task 3) are used unchanged in Tasks 4 and 6.
  - `ElicitationConsent.AskOperator(ctx, elicit.Question)` (Task 4) is the one `cmd/aura` implements (Task 4) and hands to mounts (Task 5).
  - `pausable.WithDeadline`, `WithTimeout`, `Hold` and `NewClock` (Task 1) are what Tasks 2 and 4 call.
- **Wire:** `elicitationFrame` and `elicitationResolvedFrame` produce exactly the JSON keys `sseAdapter_elicitation.ts` parses (`run_id`, `enum_titles`, `min_length`, `max_length`, `expired`, `refusal`). `fields: null` on a refusal is handled.
- **Web:**
  - `QuestionCardProps`, `QuestionOption`, `ReceiptTone`, `ReceiptLine` and `CancelLabels` (Task 7) are what Task 8 imports.
  - `ElicitationAction` is defined once, in `sseAdapter_elicitation.ts`, and imported by `elicitationAnswer.ts` and `ElicitationCard.tsx`.

**Review Focus.** Each of the five has a test in its owning task:
1. `TestBudgetWallclockSkipsHeldTime`, `TestRunToolNodeTimeoutStopsWhileHeld`, `TestAHeldCallOutlivesItsTimeout`, `TestDetachedRunAnswersAnMCPFormWhileBothClocksStop`;
2. `TestAnUnansweredQuestionExpiresWhileTheCallIsHeld`;
3. `TestClassicElicitationWithTwoRunsInFlightAsksNeither`;
4. the replay counts in the integration test, and `applyElicitationSignal`'s replay test;
5. `TestAnAnswerThatFailsTheSchemaLeavesTheQuestionOpen`, and `ElicitationCard`'s "a 422 puts the card back on the failing step".
