package mcptools

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/elicit"
	"github.com/chetto1983/aura/internal/identityctx"
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

// runKey is what makes two open calls one run's: the same asker and the same
// operator. Runs with no cockpit (a Telegram turn, a scheduled job) all have a nil
// asker, so the identity is what keeps one operator's form off another's channel.
type runKey struct {
	asker    elicit.Asker
	identity string
}

func runOf(call context.Context) runKey {
	return runKey{asker: elicit.AskerFrom(call), identity: identityctx.IdentityID(call)}
}

// routeFor finds the run an elicitation belongs to. A multi-round-trip request
// arrives on its request's own marked context, which names the run outright. A
// classic elicitation/create arrives on the connection's context and is placed
// only when every call open on its session belongs to one run.
//
// What the classic path cannot see: a server that asks after its own call has
// returned, while another run's call is the one open. go-sdk v1.8.0 reads which
// request a streamable message belongs to and drops it (mcp/streamable.go:2617-2680),
// so no client-side fix exists; the PRD records the limit.
func routeFor(ctx context.Context, session *sdkmcp.ClientSession) route {
	if tool, ok := callToolFrom(ctx); ok {
		return route{asker: elicit.AskerFrom(ctx), tool: tool, calls: []context.Context{ctx}}
	}
	r := route{calls: inFlight.on(session)}
	var run runKey
	for i, call := range r.calls {
		tool, _ := callToolFrom(call)
		switch {
		case i == 0:
			run, r.tool = runOf(call), tool
		case runOf(call) != run:
			r.mixed = true
		}
		if tool != r.tool {
			r.tool = ""
		}
	}
	if !r.mixed {
		r.asker = run.asker
	}
	return r
}

// fallbackContext is the context the fallback consent is asked on: an open call's,
// which carries its operator's identity (every open call shares it, or the route
// is mixed), or the handler's own when no call is open.
func (r route) fallbackContext(ctx context.Context) context.Context {
	if len(r.calls) > 0 {
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

// refuse declines a request Aura will not put to anyone and tells every run
// concerned why. The notice carries none of the server's text: a refused form may
// belong to another conversation, even another operator's. Each notice goes out on
// its own goroutine, so the server hears its decline at once and a slow channel
// cannot hold the call past its own bound.
func refuse(ctx context.Context, r route, consent ElicitationConsent, q elicit.Question, why string, timeout time.Duration) elicitOutcome {
	notice := elicit.Question{Server: q.Server, Tool: q.Tool, Refusal: why}
	told := map[runKey]bool{}
	for _, call := range r.calls {
		if run := runOf(call); !told[run] {
			told[run] = true
			go tell(call, run.asker, consent, notice, timeout)
		}
	}
	if len(told) == 0 {
		go tell(ctx, nil, consent, notice, timeout)
	}
	return elicitOutcome{action: elicit.ActionDecline, fields: len(q.Fields), reason: "refused: " + why}
}

// tell delivers a refusal notice to a run's asker, or through the fallback when the
// run has none. It outlives the call on purpose (WithoutCancel), because the call
// may end the moment the server hears the decline, and it is bounded by the
// elicitation timeout instead.
func tell(ctx context.Context, asker elicit.Asker, consent ElicitationConsent, notice elicit.Question, timeout time.Duration) {
	ctx = context.WithoutCancel(ctx)
	if asker == nil {
		askFallback(ctx, consent, notice, timeout)
		return
	}
	bounded, stop := withExpiry(ctx, timeout)
	defer stop()
	_, _ = askRecovered(bounded, asker, notice)
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

// waitEnded is the outcome of a wait whose context ended before an answer. Both
// cancel: MCP defines cancel as "dismissed without making an explicit choice"
// (2025-11-25 client/elicitation), and nobody chose. The reason keeps an expiry
// apart from the call or the run ending.
func waitEnded(ctx context.Context, fields int) elicitOutcome {
	cause := context.Cause(ctx)
	if errors.Is(cause, elicit.ErrExpired) {
		return elicitOutcome{action: elicit.ActionCancel, fields: fields, reason: "expired"}
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
