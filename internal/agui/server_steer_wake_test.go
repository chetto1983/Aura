package agui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"iter"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/steer"
)

const wakeConv = "37373737-3737-3737-3737-373737373737"

type steerWakeCall struct {
	source, text string
	hostLocked   bool
}

// steerWakeTestRunner records each wake and whether the host held the conversation lock while
// it ran. A wake blocks on release (when set) or on its context, then answers or fails. After
// a failure it goes on only if its reader asks for more, as an iterator must.
type steerWakeTestRunner struct {
	detachLockRunner
	release chan struct{}
	turnErr error
	lockErr error

	mu    sync.Mutex
	calls []steerWakeCall
}

func (r *steerWakeTestRunner) LockThread(ctx context.Context, _ string) (func(), error) {
	if r.lockErr != nil {
		return nil, r.lockErr
	}
	for !r.locked.CompareAndSwap(false, true) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
	return func() { r.locked.Store(false) }, nil
}

func (r *steerWakeTestRunner) WakeWithSteer(ctx context.Context, _ string, _ runner.SteerPusher, source, text string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		r.mu.Lock()
		r.calls = append(r.calls, steerWakeCall{source: source, text: text, hostLocked: r.locked.Load()})
		r.mu.Unlock()
		if r.release != nil {
			select {
			case <-r.release:
			case <-ctx.Done():
				yield(nil, ctx.Err()) // as the runner's turn reports its cancellation
				return
			}
		}
		if r.turnErr != nil {
			if yield(nil, r.turnErr) {
				yield(textTurn("after the failure")[0], nil)
			}
			return
		}
		for _, ev := range textTurn("woken answer") {
			if !yield(ev, nil) {
				return
			}
		}
	}
}

func (r *steerWakeTestRunner) recorded() []steerWakeCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]steerWakeCall(nil), r.calls...)
}

func ownerCtx() context.Context {
	return identityctx.WithIdentityID(context.Background(), localIdentityID)
}

// wakeAsync runs one wake and returns everything it yielded once it ends.
func wakeAsync(ctx context.Context, s *Server) <-chan []error {
	done := make(chan []error, 1)
	go func() {
		var errs []error
		for _, err := range s.WakeWithSteer(ctx, wakeConv, acceptingPusher{}, steer.SourceShell, "Background shell sh-1 completed") {
			errs = append(errs, err)
		}
		done <- errs
	}()
	return done
}

type acceptingPusher struct{}

func (acceptingPusher) Push(string, string, string) error { return nil }

func liveWakeRun(t *testing.T, s *Server) *RunSession {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if sess, ok := s.runs.LiveForThread(localIdentityID, wakeConv); ok {
			return sess
		}
		if time.Now().After(deadline) {
			t.Fatal("the wake never became a live run of its conversation")
		}
		time.Sleep(time.Millisecond)
	}
}

func awaitWake(t *testing.T, done <-chan []error) []error {
	t.Helper()
	select {
	case errs := <-done:
		return errs
	case <-time.After(5 * time.Second):
		t.Fatal("the wake did not return")
		return nil
	}
}

// knownWakeConv also knows "conv-1", so only the id's own check can refuse it.
func knownWakeConv() *fakeConvStore {
	return &fakeConvStore{known: map[string]bool{wakeConv: true, "conv-1": true}}
}

// TestSteerWakeIsADiscoverableRun pins the fix measured on 2026-10-04: a woken turn is
// registered where an open cockpit looks for a run it did not start -- live_run_id and the
// conversation stream's coordinator frame -- and its frames replay through the resume route.
func TestSteerWakeIsADiscoverableRun(t *testing.T) {
	r := &steerWakeTestRunner{release: make(chan struct{})}
	s, srv := newDetachTestServer(t, r, knownWakeConv(), ServerConfig{})

	done := wakeAsync(ownerCtx(), s)
	sess := liveWakeRun(t, s)
	if got := s.runs.coordinatorStatus(localIdentityID, wakeConv); got != (coordinatorRunStatus{RunID: sess.RunID, Status: "running"}) {
		t.Fatalf("coordinator frame = %+v, want the wake's run announced as running", got)
	}
	if got := s.runs.coordinatorStatus("foreign", wakeConv); got.RunID != "" {
		t.Fatalf("a foreign identity discovered the wake: %+v", got)
	}
	close(r.release)
	resp, err := http.Get(srv.URL + "/agent/runs/" + sess.RunID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || !strings.Contains(string(body), "woken answer") || !strings.Contains(string(body), "RUN_FINISHED") {
		t.Fatalf("resume stream err=%v body=%s", err, body)
	}
	if errs := awaitWake(t, done); len(errs) != 0 {
		t.Fatalf("a wake that answered reported %v", errs)
	}
	calls := r.recorded()
	if len(calls) != 1 || !calls[0].hostLocked || calls[0].source != steer.SourceShell || calls[0].text != "Background shell sh-1 completed" {
		t.Fatalf("runner wakes = %+v, want one shell wake run under the lock the host held", calls)
	}
	if r.locked.Load() {
		t.Fatal("the finished wake kept the conversation locked")
	}
	if _, live := s.runs.LiveForThread(localIdentityID, wakeConv); live {
		t.Fatal("the finished wake is still live")
	}
	if got := s.runs.coordinatorStatus(localIdentityID, wakeConv); got != (coordinatorRunStatus{RunID: sess.RunID, Status: "finished"}) {
		t.Fatalf("coordinator frame = %+v, want the wake's run announced as finished", got)
	}
}

func TestSteerWakeQueuesBehindTheTurnInProgress(t *testing.T) {
	r := &steerWakeTestRunner{}
	s, _ := newDetachTestServer(t, r, knownWakeConv(), ServerConfig{})
	r.locked.Store(true)

	done := wakeAsync(ownerCtx(), s)
	time.Sleep(30 * time.Millisecond)
	if _, live := s.runs.LiveForThread(localIdentityID, wakeConv); live || len(r.recorded()) != 0 {
		t.Fatal("the wake started while another turn held the conversation")
	}
	r.locked.Store(false)
	if errs := awaitWake(t, done); len(errs) != 0 {
		t.Fatalf("wake = %v", errs)
	}
	if calls := r.recorded(); len(calls) != 1 || !calls[0].hostLocked {
		t.Fatalf("runner wakes = %+v, want one, after the turn in progress", calls)
	}
}

func TestSteerWakeCancelledWhileWaitingStartsNothing(t *testing.T) {
	r := &steerWakeTestRunner{}
	s, _ := newDetachTestServer(t, r, knownWakeConv(), ServerConfig{})
	r.locked.Store(true)
	ctx, cancel := context.WithCancel(ownerCtx())

	done := wakeAsync(ctx, s)
	cancel()
	errs := awaitWake(t, done)
	if len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("wake = %v, want the cancellation", errs)
	}
	if len(r.recorded()) != 0 {
		t.Fatal("a cancelled wake reached the runner")
	}
	if _, live := s.runs.LiveForThread(localIdentityID, wakeConv); live {
		t.Fatal("a cancelled wake registered a run")
	}
}

// TestSteerWakeReportsTheTurnError: the dispatcher logs the failure it is handed, and the
// attached tab sees the run end in error.
func TestSteerWakeReportsTheTurnError(t *testing.T) {
	failure := errors.New("model unavailable")
	r := &steerWakeTestRunner{release: make(chan struct{}), turnErr: failure}
	s, _ := newDetachTestServer(t, r, knownWakeConv(), ServerConfig{})

	done := wakeAsync(ownerCtx(), s)
	sess := liveWakeRun(t, s)
	close(r.release)
	if errs := awaitWake(t, done); len(errs) != 1 || !errors.Is(errs[0], failure) {
		t.Fatalf("wake = %v, want the turn's error", errs)
	}
	if got := lastFrameType(t, sess); got != "RUN_ERROR" {
		t.Fatalf("terminal frame = %s, want RUN_ERROR", got)
	}
	if r.locked.Load() {
		t.Fatal("the failed wake kept the conversation locked")
	}
}

// TestSteerWakeCanBeStopped: a woken turn is a run, so the cockpit's stop reaches it.
func TestSteerWakeCanBeStopped(t *testing.T) {
	r := &steerWakeTestRunner{release: make(chan struct{})}
	s, srv := newDetachTestServer(t, r, knownWakeConv(), ServerConfig{})

	done := wakeAsync(ownerCtx(), s)
	sess := liveWakeRun(t, s)
	resp, err := http.Post(srv.URL+"/agent/runs/"+sess.RunID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status = %d, want 202", resp.StatusCode)
	}
	if errs := awaitWake(t, done); len(errs) != 1 || !errors.Is(errs[0], context.Canceled) {
		t.Fatalf("a stopped wake reported %v, want its cancellation", errs)
	}
	if got := lastFrameType(t, sess); got != "RUN_ERROR" {
		t.Fatalf("terminal frame = %s, want RUN_ERROR", got)
	}
	if r.locked.Load() {
		t.Fatal("the stopped wake kept the conversation locked")
	}
}

func TestSteerWakeRunsUnobservedWhenTheRegistryIsFull(t *testing.T) {
	r := &steerWakeTestRunner{}
	s, _ := newDetachTestServer(t, r, knownWakeConv(), ServerConfig{RunMaxLive: 1})
	busy, err := s.runs.Start(runParams{runID: "run-busy", threadID: "other", identityID: localIdentityID, cancel: func() {}})
	if err != nil {
		t.Fatal(err)
	}
	defer busy.finish()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	errs, events := drainWake(s.WakeWithSteer(ownerCtx(), wakeConv, acceptingPusher{}, steer.SourceShell, "done"))
	if len(errs) != 0 || events == 0 {
		t.Fatalf("wake errs=%v events=%d, want the turn run anyway and its events handed back", errs, events)
	}
	if calls := r.recorded(); len(calls) != 1 || !calls[0].hostLocked {
		t.Fatalf("runner wakes = %+v, want one under the host's lock", calls)
	}
	if r.locked.Load() {
		t.Fatal("the unobserved wake kept the conversation locked")
	}
	if _, live := s.runs.LiveForThread(localIdentityID, wakeConv); live {
		t.Fatal("a wake past the cap was registered anyway")
	}
	if !strings.Contains(logs.String(), "steer wake runs unobserved") || !strings.Contains(logs.String(), wakeConv) {
		t.Fatalf("an unobserved wake was not reported: %q", logs.String())
	}
}

func TestSteerWakeWithoutARegistryIsTheRunnersOwn(t *testing.T) {
	r := &steerWakeTestRunner{}
	s := NewServer(r, knownWakeConv(), ServerConfig{})

	errs, events := drainWake(s.WakeWithSteer(context.Background(), wakeConv, acceptingPusher{}, steer.SourceShell, "done"))
	if len(errs) != 0 || events == 0 {
		t.Fatalf("wake errs=%v events=%d", errs, events)
	}
	if calls := r.recorded(); len(calls) != 1 || calls[0].hostLocked {
		t.Fatalf("runner wakes = %+v, want one the runner locks itself", calls)
	}
	for range s.WakeWithSteer(context.Background(), wakeConv, acceptingPusher{}, steer.SourceShell, "done") {
		break // a caller that stops reading stops the wake
	}
}

func TestSteerWakeRefusesWhatItCannotRoute(t *testing.T) {
	r := &steerWakeTestRunner{}
	s, _ := newDetachTestServer(t, r, knownWakeConv(), ServerConfig{})
	for name, wake := range map[string]iter.Seq2[*agent.Event, error]{
		"no owner":             s.WakeWithSteer(context.Background(), wakeConv, acceptingPusher{}, steer.SourceShell, "x"),
		"invalid conversation": s.WakeWithSteer(ownerCtx(), "conv-1", acceptingPusher{}, steer.SourceShell, "x"),
		"unknown conversation": s.WakeWithSteer(ownerCtx(), "38383838-3838-3838-3838-383838383838", acceptingPusher{}, steer.SourceShell, "x"),
	} {
		if errs, _ := drainWake(wake); len(errs) != 1 {
			t.Errorf("%s: wake = %v, want one refusal", name, errs)
		}
	}
	r.lockErr = errors.New("lock unavailable")
	if errs, _ := drainWake(s.WakeWithSteer(ownerCtx(), wakeConv, acceptingPusher{}, steer.SourceShell, "x")); len(errs) != 1 || !errors.Is(errs[0], r.lockErr) {
		t.Errorf("lock failure: wake = %v", errs)
	}
	if calls := r.recorded(); len(calls) != 0 {
		t.Fatalf("a refused wake reached the runner: %+v", calls)
	}

	plain, _ := newDetachTestServer(t, &detachLockRunner{}, knownWakeConv(), ServerConfig{})
	if errs, _ := drainWake(plain.WakeWithSteer(ownerCtx(), wakeConv, acceptingPusher{}, steer.SourceShell, "x")); len(errs) != 1 {
		t.Fatalf("a runner that cannot wake: wake = %v, want one refusal", errs)
	}
}

func drainWake(seq iter.Seq2[*agent.Event, error]) (errs []error, events int) {
	for ev, err := range seq {
		if err != nil {
			errs = append(errs, err)
		}
		if ev != nil {
			events++
		}
	}
	return errs, events
}

func lastFrameType(t *testing.T, sess *RunSession) string {
	t.Helper()
	ch, cancel, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("terminal replay refused")
	}
	defer cancel()
	var last seqEvent
	for sev := range ch {
		last = sev
	}
	return string(last.Ev.Type())
}
