package agui

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/idempotency"
)

// server_run_resume_test.go pins the RS-05 resume/cancel contract (§7.2 rows 3-4):
// the §3.2 404 ladder, Last-Event-ID replay-from-n+1, no-header full replay, gap
// 410, terminal-tail clean close, cancel → buffered RUN_ERROR terminal, idempotency
// interplay (409 InProgress → post-stream 422), and the live_run_id DTO. Goroutine
// hygiene rides the package goleak TestMain (reaper closes in t.Cleanup).

// runDetachedToTerminal drives one flag-on run to terminal and returns the runID.
func runDetachedToTerminal(t *testing.T, srv *httptest.Server, tid string) string {
	t.Helper()
	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read run body: %v", err)
	}
	m := runIDPattern.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("no runId on the run wire: %s", raw)
	}
	return m[1]
}

// getEvents GETs the resume route with an optional Last-Event-ID header, fully read.
func getEvents(t *testing.T, srv *httptest.Server, runID, lastEventID string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/agent/runs/"+runID+"/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read events body: %v", err)
	}
	return resp, string(raw)
}

// TestRunEvents_TerminalReplay: a terminal, still-lingering run replays its tail —
// full-buffer with no header (page-reload attach), strictly from n+1 with a header —
// then closes cleanly after RUN_FINISHED (ReadAll returning IS the clean-close
// proof). The resume performs zero writes: SubmitAnswers is never called (§5.1).
func TestRunEvents_TerminalReplay(t *testing.T) {
	const tid = "31313131-3131-3131-3131-313131313131"
	run := &scriptedRunner{events: textTurn("hi")}
	_, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	runID := runDetachedToTerminal(t, srv, tid)

	t.Run("no header: full replay from seq 1", func(t *testing.T) {
		resp, body := getEvents(t, srv, runID, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
			t.Fatalf("Content-Type = %q, want text/event-stream", ct)
		}
		ids := seqIDs(t, body)
		assertStrictlyIncreasingFrom1(t, ids)
		if len(ids) != 5 {
			t.Fatalf("full replay carried %d frames, want 5", len(ids))
		}
		types, data := sseFrames(t, body)
		if got := types[len(types)-1]; got != "RUN_FINISHED" {
			t.Fatalf("replay last frame = %s, want RUN_FINISHED", got)
		}
		if !strings.Contains(data, "hi") {
			t.Errorf("replayed payload missing the delta: %q", data)
		}
		assertNoSplitFrames(t, body)
	})

	t.Run("Last-Event-ID replays strictly from n+1", func(t *testing.T) {
		resp, body := getEvents(t, srv, runID, "2")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		ids := seqIDs(t, body)
		if len(ids) != 3 || ids[0] != 3 || ids[2] != 5 {
			t.Fatalf("resume ids = %v, want [3 4 5] (never re-send an acknowledged id)", ids)
		}
		types, _ := sseFrames(t, body)
		if got := types[len(types)-1]; got != "RUN_FINISHED" {
			t.Fatalf("resume last frame = %s, want RUN_FINISHED", got)
		}
	})

	t.Run("beyond-tail Last-Event-ID replays nothing and closes", func(t *testing.T) {
		resp, body := getEvents(t, srv, runID, "5")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		if ids := seqIDs(t, body); len(ids) != 0 {
			t.Fatalf("acknowledged-everything resume replayed %v, want nothing", ids)
		}
	})

	if run.gotAnswers != nil {
		t.Fatal("resume reached SubmitAnswers — the route must be read-only by construction (§5.1)")
	}
}

// TestRunEvents_ResumeAfterDisconnect: the Tier B core scenario — the viewer drops
// mid-run, the run keeps producing, and a resume with the last seen id receives the
// gap-free remainder through the live tail to RUN_FINISHED.
func TestRunEvents_ResumeAfterDisconnect(t *testing.T) {
	const tid = "32323232-3232-3232-3232-323232323232"
	run := &detachLockRunner{}
	pair := textTurn("x")
	for i := 0; i < 30; i++ {
		run.events = append(run.events, pair[0])
	}
	run.events = append(run.events, pair[1])
	run.delay = 15 * time.Millisecond
	_, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

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
	var runID string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if m := runIDPattern.FindStringSubmatch(sc.Text()); m != nil {
			runID = m[1]
			break
		}
	}
	if runID == "" {
		t.Fatal("no runId on the live wire")
	}
	cancel()
	resp.Body.Close()

	// The client saw (at least) seq 1 (RUN_STARTED). Resume from it: the remainder
	// must be 2..34 gap-free, spanning the still-live production to terminal.
	rresp, body := getEvents(t, srv, runID, "1")
	if rresp.StatusCode != http.StatusOK {
		t.Fatalf("resume status = %d, want 200", rresp.StatusCode)
	}
	ids := seqIDs(t, body)
	if len(ids) == 0 || ids[0] != 2 {
		t.Fatalf("resume ids start at %v, want 2 (Last-Event-ID+1)", ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[i-1]+1 {
			t.Fatalf("resume ids %v: gap/duplicate at position %d", ids, i)
		}
	}
	if want := int64(30 + 4); ids[len(ids)-1] != want {
		t.Fatalf("resume final id = %d, want %d (the full turn)", ids[len(ids)-1], want)
	}
	types, _ := sseFrames(t, body)
	if got := types[len(types)-1]; got != "RUN_FINISHED" {
		t.Fatalf("resume last frame = %s, want RUN_FINISHED", got)
	}
}

// TestRunEvents_ReplayGap410: once the ring has rotated past the requested
// position, resume answers 410 Gone with the documented JSON body — a distinct
// signal from 404 so the client knows the run exists but partial-frame recovery is
// impossible (§3.3/§4.3).
func TestRunEvents_ReplayGap410(t *testing.T) {
	const tid = "33333333-3333-3333-3333-333333333333"
	run := &scriptedRunner{}
	pair := textTurn("x")
	for i := 0; i < 8; i++ {
		run.events = append(run.events, pair[0])
	}
	run.events = append(run.events, pair[1])
	_, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}},
		ServerConfig{RunBufferEvents: 4})
	runID := runDetachedToTerminal(t, srv, tid)

	resp, body := getEvents(t, srv, runID, "")
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("status = %d, want 410 Gone", resp.StatusCode)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("410 body is not JSON: %q (%v)", body, err)
	}
	if payload["error"] != "replay window exceeded" {
		t.Fatalf(`410 body = %q, want {"error":"replay window exceeded"}`, body)
	}

	// A position still inside the surviving window resumes fine.
	resp2, body2 := getEvents(t, srv, runID, "10")
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("in-window resume status = %d, want 200", resp2.StatusCode)
	}
	ids := seqIDs(t, body2)
	if len(ids) != 2 || ids[0] != 11 || ids[1] != 12 {
		t.Fatalf("in-window resume ids = %v, want [11 12]", ids)
	}
}

// TestRunEvents_BadLastEventID400: an unparsable or negative header is a 400 —
// never treated as "no header".
func TestRunEvents_BadLastEventID400(t *testing.T) {
	const tid = "34343434-3434-3434-3434-343434343434"
	_, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
		&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	runID := runDetachedToTerminal(t, srv, tid)

	for _, bad := range []string{"abc", "-1", "1.5"} {
		resp, _ := getEvents(t, srv, runID, bad)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Last-Event-ID %q status = %d, want 400", bad, resp.StatusCode)
		}
	}
}

// TestRunRoutes_404Ladder pins §3.2: flag-off hides the whole surface; malformed run
// ids 404 before any lookup; unknown and foreign runs answer the same 404 body on
// BOTH routes (hide existence, never 403).
func TestRunRoutes_404Ladder(t *testing.T) {
	const tid = "35353535-3535-3535-3535-353535353535"

	t.Run("flag off: both routes hidden", func(t *testing.T) {
		srv := newTestServer(t, &scriptedRunner{}, &fakeConvStore{known: map[string]bool{}})
		resp, _ := getEvents(t, srv, "run-00000000-0000-0000-0000-000000000000", "")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("flag-off resume status = %d, want 404", resp.StatusCode)
		}
		cresp, err := http.Post(srv.URL+"/agent/runs/run-00000000-0000-0000-0000-000000000000/cancel", "application/json", nil)
		if err != nil {
			t.Fatalf("POST cancel: %v", err)
		}
		defer cresp.Body.Close()
		if cresp.StatusCode != http.StatusNotFound {
			t.Fatalf("flag-off cancel status = %d, want 404", cresp.StatusCode)
		}
	})

	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
		&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

	t.Run("malformed and unknown run ids", func(t *testing.T) {
		for _, id := range []string{"not-a-run", "run-zzz", "run-00000000-0000-0000-0000-00000000dead"} {
			resp, _ := getEvents(t, srv, id, "")
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("resume %q status = %d, want 404", id, resp.StatusCode)
			}
		}
	})

	t.Run("foreign identity answers the same 404", func(t *testing.T) {
		foreign, err := s.runs.Start(runParams{
			runID:      "run-99999999-9999-9999-9999-999999999999",
			threadID:   "t-foreign",
			identityID: "22222222-2222-2222-2222-222222222222",
		})
		if err != nil {
			t.Fatalf("Start foreign: %v", err)
		}
		defer foreign.finish()
		resp, body := getEvents(t, srv, foreign.RunID, "")
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("foreign resume status = %d, want 404", resp.StatusCode)
		}
		if !strings.Contains(body, "run not found") {
			t.Fatalf("foreign 404 body = %q, want the shared 'run not found'", body)
		}
		cresp, err := http.Post(srv.URL+"/agent/runs/"+foreign.RunID+"/cancel", "application/json", nil)
		if err != nil {
			t.Fatalf("POST cancel: %v", err)
		}
		defer cresp.Body.Close()
		if cresp.StatusCode != http.StatusNotFound {
			t.Fatalf("foreign cancel status = %d, want 404 (existence hidden)", cresp.StatusCode)
		}
	})
}

// ctxWaitTurnRunner blocks its turn until the TURN ctx is cancelled — the
// cancel-endpoint fake (the real runner unwinds the agent the same way).
type ctxWaitTurnRunner struct {
	scriptedRunner
	started chan struct{}
}

func (c *ctxWaitTurnRunner) Turn(ctx context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{Author: "aura", LLMResponse: &agent.LLMResponse{Content: "x"}}, nil) {
			return
		}
		close(c.started)
		<-ctx.Done()
		yield(nil, ctx.Err())
	}
}

// TestRunCancel_StopsDetachedRun (§4.4): POST cancel fires the detached ctx, the
// turn unwinds, the sanitized RUN_ERROR lands in the ring as the terminal frame,
// and the response is 202 {"status":"cancelling"}; cancelling the now-terminal run
// is a 202 no-op.
func TestRunCancel_StopsDetachedRun(t *testing.T) {
	const tid = "36363636-3636-3636-3636-363636363636"
	run := &ctxWaitTurnRunner{started: make(chan struct{})}
	s, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

	viewerDone := make(chan string, 1)
	go func() {
		resp := postRun(t, srv, runPayload(tid))
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		viewerDone <- string(raw)
	}()

	select {
	case <-run.started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn never started")
	}
	sess, ok := s.runs.LiveForThread(localIdentityID, tid)
	if !ok {
		t.Fatal("no live session for the thread")
	}

	cresp, err := http.Post(srv.URL+"/agent/runs/"+sess.RunID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("POST cancel: %v", err)
	}
	craw, _ := io.ReadAll(cresp.Body)
	cresp.Body.Close()
	if cresp.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status = %d, want 202", cresp.StatusCode)
	}
	if !strings.Contains(string(craw), `"status":"cancelling"`) {
		t.Fatalf("cancel body = %q, want {\"status\":\"cancelling\"}", craw)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		if terminal, _ := sess.terminalState(); terminal {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancel did not drive the run terminal")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ch, cancelSub, ok := sess.subscribeFrom(0)
	if !ok {
		t.Fatal("terminal replay refused")
	}
	cancelSub()
	var last seqEvent
	for sev := range ch {
		last = sev
	}
	if got := string(last.Ev.Type()); got != "RUN_ERROR" {
		t.Fatalf("terminal frame after cancel = %s, want RUN_ERROR", got)
	}

	// Cancelling an already-terminal run stays a 202 no-op (idempotent by nature).
	again, err := http.Post(srv.URL+"/agent/runs/"+sess.RunID+"/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("POST cancel again: %v", err)
	}
	defer again.Body.Close()
	_, _ = io.Copy(io.Discard, again.Body)
	if again.StatusCode != http.StatusAccepted {
		t.Fatalf("terminal cancel status = %d, want 202 no-op", again.StatusCode)
	}
	<-viewerDone
}

// detachOpsRegistry is a stateful operationRegistry fake modelling the agent_run
// lifecycle: Acquired once, InProgress while the original request still streams,
// Indeterminate (422) after the middleware's post-stream mark.
type detachOpsRegistry struct {
	mu     sync.Mutex
	begun  map[idempotency.OperationKey]bool
	marked map[idempotency.OperationKey]bool
}

func newDetachOpsRegistry() *detachOpsRegistry {
	return &detachOpsRegistry{begun: map[idempotency.OperationKey]bool{}, marked: map[idempotency.OperationKey]bool{}}
}

func (d *detachOpsRegistry) Begin(_ context.Context, req idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.marked[req.Operation] {
		return idempotency.BeginDecision{Decision: idempotency.DecisionIndeterminate}, nil
	}
	if d.begun[req.Operation] {
		return idempotency.BeginDecision{Decision: idempotency.DecisionInProgress, RetryAfter: time.Second}, nil
	}
	d.begun[req.Operation] = true
	return idempotency.BeginDecision{Decision: idempotency.DecisionAcquired, ClaimToken: 1}, nil
}

func (d *detachOpsRegistry) Complete(context.Context, idempotency.CompleteRequest) error { return nil }

func (d *detachOpsRegistry) MarkIndeterminate(_ context.Context, op idempotency.OperationKey, _ [32]byte, _ idempotency.ClaimToken) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.marked[op] = true
	return nil
}

func (d *detachOpsRegistry) begunAny() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.begun) > 0
}

// TestRunIdempotencyInterplay (§5.2): retrying the SAME Idempotency-Key while the
// original request still streams answers 409 InProgress + Retry-After; after the
// stream ends (agent_run marks terminally indeterminate) the retry answers 422 —
// never a duplicate turn. (Same-thread duplicate-POST 409 ErrThreadBusy: pinned by
// TestDetachedRun_SurvivesDisconnectAndLockSpansRun.)
func TestRunIdempotencyInterplay(t *testing.T) {
	const tid = "37373737-3737-3737-3737-373737373737"
	run := &scriptedRunner{delay: 20 * time.Millisecond}
	pair := textTurn("x")
	for i := 0; i < 30; i++ {
		run.events = append(run.events, pair[0])
	}
	run.events = append(run.events, pair[1])
	ops := newDetachOpsRegistry()

	s := NewServer(run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	s.idgen = &fixedIDGen{}
	s.SetOperationRegistry(ops)
	reg := NewRunRegistry(ServerConfig{})
	t.Cleanup(reg.Close)
	s.SetRunRegistry(reg)
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)

	const key = "interplay-key-1"
	body := runPayload(tid)
	post := func() *http.Response {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/agent/run", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /agent/run: %v", err)
		}
		return resp
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		resp := post()
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
	}()

	deadline := time.Now().Add(5 * time.Second)
	for !ops.begunAny() {
		if time.Now().After(deadline) {
			t.Fatal("original request never began its operation")
		}
		time.Sleep(5 * time.Millisecond)
	}

	retry := post()
	defer retry.Body.Close()
	_, _ = io.Copy(io.Discard, retry.Body)
	if retry.StatusCode != http.StatusConflict {
		t.Fatalf("in-flight retry status = %d, want 409 InProgress", retry.StatusCode)
	}
	if retry.Header.Get("Retry-After") == "" {
		t.Fatal("in-flight 409 without Retry-After")
	}

	<-firstDone
	after := post()
	defer after.Body.Close()
	araw, _ := io.ReadAll(after.Body)
	if after.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("post-stream retry status = %d, want 422 indeterminate: %s", after.StatusCode, araw)
	}
}

// TestConversationDTO_LiveRunID (amendment #90 point 6): the conversation single-get
// and list rows carry live_run_id exactly while the identity's run is live — absent
// with the flag off (nil registry) and absent again once terminal.
func TestConversationDTO_LiveRunID(t *testing.T) {
	const tid = "38383838-3838-3838-3838-383838383838"

	t.Run("flag off: field absent", func(t *testing.T) {
		srv := newTestServer(t, &scriptedRunner{}, &fakeConvStore{known: map[string]bool{tid: true}})
		resp, err := http.Get(srv.URL + "/api/conversations/" + tid)
		if err != nil {
			t.Fatalf("GET conversation: %v", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(raw), "live_run_id") {
			t.Fatalf("flag-off DTO grew live_run_id: %s", raw)
		}
	})

	t.Run("live then terminal", func(t *testing.T) {
		run := &ctxWaitTurnRunner{started: make(chan struct{})}
		s, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

		viewerDone := make(chan struct{})
		go func() {
			defer close(viewerDone)
			resp := postRun(t, srv, runPayload(tid))
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()
		select {
		case <-run.started:
		case <-time.After(5 * time.Second):
			t.Fatal("turn never started")
		}
		sess, ok := s.runs.LiveForThread(localIdentityID, tid)
		if !ok {
			t.Fatal("no live session")
		}

		get := func() map[string]any {
			resp, err := http.Get(srv.URL + "/api/conversations/" + tid)
			if err != nil {
				t.Fatalf("GET conversation: %v", err)
			}
			defer resp.Body.Close()
			var out map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
				t.Fatalf("decode conversation: %v", err)
			}
			return out
		}
		if got := get()["live_run_id"]; got != sess.RunID {
			t.Fatalf("live_run_id = %v, want %s", got, sess.RunID)
		}
		// The list rows ride the same DTO decoration. The fake store's ListForIdentity
		// returns nil, so only the projection seam is provable here: the wire stays
		// valid JSON (`null` — the pre-DTO empty-list byte-parity).
		listResp, err := http.Get(srv.URL + "/api/conversations")
		if err != nil {
			t.Fatalf("GET list: %v", err)
		}
		defer listResp.Body.Close()
		lraw, _ := io.ReadAll(listResp.Body)
		if strings.TrimSpace(string(lraw)) != "null" {
			t.Fatalf("empty list projection = %q, want null (pre-DTO byte parity)", lraw)
		}

		sess.cancel()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if terminal, _ := sess.terminalState(); terminal {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("run never terminal after cancel")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if _, present := get()["live_run_id"]; present {
			t.Fatal("live_run_id still present after terminal")
		}
		<-viewerDone
	})
}
