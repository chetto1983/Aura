package agui

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// server_run_detach_test.go pins the RS-04 detached /agent/run tail (§7.2 row 3,
// first half): the run survives a client disconnect, the thread lock releases at
// turn end (not handler end), the flag-on wire carries strictly-increasing integer
// `id:` lines while the flag-off wire is untouched, the ring stores post-redact
// frames, and detachedRunContext keeps values while shedding cancellation.
// Goroutine hygiene rides the package goleak TestMain (main_test.go): the registry
// reaper closes in t.Cleanup, which runs AFTER per-test defers — a per-test
// goleak.VerifyNone here would false-flag the still-lingering reaper.

// detachLockRunner is guardedScriptedRunner's detached-path analog: the lock flag is
// atomic because the producer goroutine releases it (via the session cleanup) while
// the test goroutine probes it.
type detachLockRunner struct {
	scriptedRunner
	locked atomic.Bool
}

func (g *detachLockRunner) TryLockThread(_ context.Context, _ string) (func(), bool) {
	if !g.locked.CompareAndSwap(false, true) {
		return nil, false
	}
	return func() { g.locked.Store(false) }, true
}

// newDetachTestServer mirrors newTestServerCfg but wires a RunRegistry (flag on) and
// returns the *Server so tests can reach the registry for ground-truth probes.
func newDetachTestServer(t *testing.T, run Runner, conv ConversationStore, cfg ServerConfig) (*Server, *httptest.Server) {
	t.Helper()
	s := NewServer(run, conv, cfg)
	s.idgen = &fixedIDGen{}
	reg := NewRunRegistry(cfg)
	t.Cleanup(reg.Close)
	s.SetRunRegistry(reg)
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return s, srv
}

var (
	seqIDLine    = regexp.MustCompile(`(?m)^id: ([0-9]+)$`)
	sdkIDLine    = regexp.MustCompile(`(?m)^id: [A-Z_]+_[0-9]+$`)
	runIDPattern = regexp.MustCompile(`"runId":"(run-[^"]+)"`)
)

// seqIDs extracts every integer `id:` line in wire order.
func seqIDs(t *testing.T, body string) []int64 {
	t.Helper()
	var out []int64
	for _, m := range seqIDLine.FindAllStringSubmatch(body, -1) {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			t.Fatalf("unparsable id line %q: %v", m[0], err)
		}
		out = append(out, n)
	}
	return out
}

func assertStrictlyIncreasingFrom1(t *testing.T, ids []int64) {
	t.Helper()
	if len(ids) == 0 {
		t.Fatal("no integer id: lines on the detached wire")
	}
	for i, id := range ids {
		if id != int64(i)+1 {
			t.Fatalf("id sequence %v: position %d carries %d, want %d (1-based, gap-free)", ids, i, id, i+1)
		}
	}
}

// TestDetachedRun_WireCarriesSeqIDs: the flag-on wire frames each carry exactly the
// run-scoped integer id (1-based, strictly monotonic — amendment #90 point 2), never
// the SDK's TYPE_timestamp id, with the frame sequence and payload of the flag-off
// path and no split frames.
func TestDetachedRun_WireCarriesSeqIDs(t *testing.T) {
	const tid = "21212121-2121-2121-2121-212121212121"
	_, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
		&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)

	types, data := sseFrames(t, body)
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("frame sequence = %v, want %v", types, want)
	}
	if !strings.Contains(data, "hi") {
		t.Errorf("payload missing the delta: %q", data)
	}
	ids := seqIDs(t, body)
	if len(ids) != len(want) {
		t.Fatalf("%d integer id lines for %d frames — every frame must carry its seq", len(ids), len(want))
	}
	assertStrictlyIncreasingFrom1(t, ids)
	if sdkIDLine.MatchString(body) {
		t.Fatalf("detached wire leaked an SDK TYPE_timestamp id line (would override the seq id):\n%s", body)
	}
	assertNoSplitFrames(t, body)
}

// TestFlagOffRun_WireParity (golden flag-off regression): with a nil registry the
// handler takes the pre-Tier-B tail — the wire carries NO integer seq ids (only the
// SDK writer's TYPE_timestamp ids) and the identical frame sequence.
func TestFlagOffRun_WireParity(t *testing.T) {
	const tid = "23232323-2323-2323-2323-232323232323"
	srv := newTestServer(t, &scriptedRunner{events: textTurn("hi")},
		&fakeConvStore{known: map[string]bool{tid: true}})

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)

	types, data := sseFrames(t, body)
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("flag-off frame sequence = %v, want %v", types, want)
	}
	if !strings.Contains(data, "hi") {
		t.Errorf("flag-off payload missing the delta: %q", data)
	}
	if ids := seqIDs(t, body); len(ids) != 0 {
		t.Fatalf("flag-off wire grew integer id lines %v — must stay byte-identical to master", ids)
	}
	if !sdkIDLine.MatchString(body) {
		t.Fatalf("flag-off wire lost the SDK TYPE_timestamp id lines — not byte-identical to master:\n%s", body)
	}
}

// TestDetachedRun_SurvivesDisconnectAndLockSpansRun: the §1.1/§1.2 core. A viewer
// disconnect mid-stream neither cancels the turn nor frees the thread lock; the lock
// releases only at turn end (session cleanup), after which the lingering terminal
// session holds the FULL frame tail ending in RUN_FINISHED.
func TestDetachedRun_SurvivesDisconnectAndLockSpansRun(t *testing.T) {
	const tid = "24242424-2424-2424-2424-242424242424"
	run := &detachLockRunner{}
	// A long turn — 60 delayed deltas + the final chunk — keeps the run live well
	// past the disconnect.
	pair := textTurn("x")
	for i := 0; i < 60; i++ {
		run.events = append(run.events, pair[0])
	}
	run.events = append(run.events, pair[1])
	run.delay = 20 * time.Millisecond
	s, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/agent/run", strings.NewReader(runPayload(tid)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}

	// Read until RUN_STARTED surfaces the runID, then disconnect the viewer.
	var runID string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if m := runIDPattern.FindStringSubmatch(sc.Text()); m != nil {
			runID = m[1]
			break
		}
	}
	if runID == "" {
		t.Fatal("RUN_STARTED frame never carried a runId")
	}
	cancel()
	resp.Body.Close()

	// Handler unwinds while the turn keeps running: the lock must still be held —
	// a duplicate POST on the thread answers 409 for the run's true duration (§1.2).
	time.Sleep(100 * time.Millisecond)
	if !run.locked.Load() {
		t.Fatal("thread lock released at handler end; must span the detached run")
	}
	dup := postRun(t, srv, runPayload(tid))
	defer dup.Body.Close()
	_, _ = io.Copy(io.Discard, dup.Body)
	if dup.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate POST during a detached run = %d, want 409", dup.StatusCode)
	}

	sess, ok := s.runs.Get(runID)
	if !ok {
		t.Fatalf("registry lost run %s while live", runID)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if terminal, _ := sess.terminalState(); terminal {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("detached run did not reach terminal after the viewer disconnected")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.locked.Load() {
		t.Fatal("thread lock still held after turn end; cleanup must release it")
	}

	// The lingering terminal ring holds the COMPLETE turn (nothing was lost to the
	// disconnect): 60 deltas + START/END + RUN_STARTED/RUN_FINISHED.
	ch, cancelSub, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("terminal full replay refused; ring should hold the whole run")
	}
	cancelSub()
	var last seqEvent
	count := 0
	for sev := range ch {
		last = sev
		count++
	}
	if wantFrames := 60 + 4; count != wantFrames {
		t.Fatalf("terminal ring holds %d frames, want %d (the full turn)", count, wantFrames)
	}
	if got := string(last.Ev.Type()); got != "RUN_FINISHED" {
		t.Fatalf("terminal ring last frame = %s, want RUN_FINISHED", got)
	}
}

// TestDetachedRun_BuffersPostRedactFrames (amendment #90 point 7): the ring itself
// stores sanitized frames — the RUN_ERROR in the buffer (not merely on the wire)
// carries no DSN secret, so a later resume can never widen what live streaming showed.
func TestDetachedRun_BuffersPostRedactFrames(t *testing.T) {
	const tid = "25252525-2525-2525-2525-252525252525"
	run := &scriptedRunner{turnErr: errors.New("tool failed dialing postgresql://user:secret@host/db")}
	s, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "RUN_ERROR") {
		t.Fatalf("expected a RUN_ERROR frame, got: %q", body)
	}
	if strings.Contains(body, "secret") {
		t.Fatalf("detached wire leaked the DSN secret: %q", body)
	}

	m := runIDPattern.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no runId on the wire")
	}
	sess, ok := s.runs.Get(m[1])
	if !ok {
		t.Fatal("session evicted before linger")
	}
	ch, cancelSub, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("terminal replay refused")
	}
	cancelSub()
	sawRunError := false
	for sev := range ch {
		if string(sev.Ev.Type()) == "RUN_ERROR" {
			sawRunError = true
			if js := eventJSONString(sev.Ev); strings.Contains(js, "secret") {
				t.Fatalf("ring stored an UNREDACTED RUN_ERROR: %s", js)
			}
		}
	}
	if !sawRunError {
		t.Fatal("ring holds no RUN_ERROR terminal frame")
	}
}

// TestDetachedRun_SubmitAnswersErrorReleasesLock: a pre-Start failure on the
// detached path must release the lock explicitly (there is no handler defer) and
// register no session.
func TestDetachedRun_SubmitAnswersErrorReleasesLock(t *testing.T) {
	const tid = "26262626-2626-2626-2626-262626262626"
	run := &detachLockRunner{}
	run.answersErr = errors.New("resume rejected")
	s, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"x"}],` +
		`"resume":[{"interruptId":"tok-1","status":"resolved","payload":"yes"}]}`
	resp := postRun(t, srv, body)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if run.locked.Load() {
		t.Fatal("SubmitAnswers failure left the thread lock held")
	}
	if _, ok := s.runs.LiveForThread(localIdentityID, tid); ok {
		t.Fatal("failed pre-work still registered a session")
	}
}

// TestDetachedRun_MaxLive503: a saturated registry refuses BEFORE the thread lock
// with 503 + Retry-After (§2.4) — the lock is never contended, so the running
// thread's lock state is untouched.
func TestDetachedRun_MaxLive503(t *testing.T) {
	const tid = "27272727-2727-2727-2727-272727272727"
	run := &detachLockRunner{}
	run.events = textTurn("x")
	s, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}},
		ServerConfig{RunMaxLive: 1})

	// Occupy the single live slot directly (deterministic — no concurrent stream).
	blocker, err := s.runs.Start(runParams{runID: "run-blocker", threadID: "other-thread", identityID: localIdentityID})
	if err != nil {
		t.Fatalf("Start blocker: %v", err)
	}

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 at max live", resp.StatusCode)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("503 without Retry-After")
	}
	if run.locked.Load() {
		t.Fatal("max-live refusal contended the thread lock")
	}

	blocker.finish()
	ok := postRun(t, srv, runPayload(tid))
	defer ok.Body.Close()
	_, _ = io.Copy(io.Discard, ok.Body)
	if ok.StatusCode != http.StatusOK {
		t.Fatalf("post-terminal Start = %d, want 200 (slot freed)", ok.StatusCode)
	}
}

// TestDetachedRun_HeartbeatBetweenFrames: drainSession carries the Tier A idle
// heartbeat with the same single-writer invariant — and the `:hb` comment never
// advances Last-Event-ID (no id line of its own).
func TestDetachedRun_HeartbeatBetweenFrames(t *testing.T) {
	const tid = "28282828-2828-2828-2828-282828282828"
	run := &scriptedRunner{events: textTurn("hi"), delay: 15 * time.Millisecond}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	s.idgen = &fixedIDGen{}
	s.heartbeatInterval = 5 * time.Millisecond
	reg := NewRunRegistry(ServerConfig{})
	t.Cleanup(reg.Close)
	s.SetRunRegistry(reg)
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, ":hb\n\n") {
		t.Fatalf("no heartbeat on the detached drain:\n%s", body)
	}
	assertNoSplitFrames(t, body)
	ids := seqIDs(t, body)
	assertStrictlyIncreasingFrom1(t, ids)
	if len(ids) != 5 {
		t.Fatalf("%d id lines, want exactly 5 (one per frame, none for heartbeats)", len(ids))
	}
}

// TestDetachedRunContext pins §1.1: request-ctx VALUES ride into the detached ctx,
// the parent's cancellation does NOT propagate, and the wallclock cap does bound it.
func TestDetachedRunContext(t *testing.T) {
	type key struct{}
	parent, pcancel := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	dctx, dcancel := detachedRunContext(parent, 60*time.Millisecond)
	defer dcancel()

	if got, _ := dctx.Value(key{}).(string); got != "v" {
		t.Fatalf("ctx value = %q, want v (values must survive the detach)", got)
	}
	pcancel()
	select {
	case <-dctx.Done():
		t.Fatal("parent cancel reached the detached ctx; WithoutCancel must shed it")
	case <-time.After(10 * time.Millisecond):
	}
	select {
	case <-dctx.Done():
		if !errors.Is(dctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("detached ctx ended with %v, want DeadlineExceeded (wallclock)", dctx.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wallclock cap never fired")
	}
}
