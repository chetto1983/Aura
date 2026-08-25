package agui

import (
	"bufio"
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
)

// server_run_steer_test.go pins T-52-11/T-52-12/T-52-13: the steer route
// answers the SAME 404 ladder cancel does (never 403), inherits the
// mandatory Idempotency-Key registration, and renders internal/steer's own
// sentinels without re-deriving any classification.

func postSteer(t *testing.T, srv string, runID, text string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv+"/agent/runs/"+runID+"/steer", "application/json", strings.NewReader(`{"text":`+quoteJSON(text)+`}`))
	if err != nil {
		t.Fatalf("POST steer: %v", err)
	}
	return resp
}

func quoteJSON(s string) string {
	// Minimal JSON string quoting sufficient for test fixtures (no control
	// chars/backslashes in the fixture text below).
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// TestHandleRunSteerHides404Ladder proves the steer route resolves through
// the IDENTICAL resolveRunSession ladder cancel uses: a nil steer inbox
// (flag off), a malformed runID, and a foreign identity's run all answer the
// SAME "run not found" 404 body — never a 403, never a distinguishable body
// (T-52-11).
func TestHandleRunSteerHides404Ladder(t *testing.T) {
	const tid = "45454545-4545-4545-4545-454545454545"

	t.Run("steer flag off (nil inbox): route hides itself", func(t *testing.T) {
		_, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
			&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
		// s.steer was never wired (SetSteerInbox not called): the surface must
		// hide exactly like a nil RunRegistry would, even for an otherwise
		// resolvable run.
		runID := runDetachedToTerminal(t, srv, tid)
		resp := postSteer(t, srv.URL, runID, "redirect")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("flag-off steer status = %d, want 404", resp.StatusCode)
		}
	})

	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
		&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	s.SetSteerInbox(steer.New(steer.Config{Max: 8, MaxBytes: 16384}))

	t.Run("malformed and unknown run ids answer the same 404", func(t *testing.T) {
		for _, id := range []string{"not-a-run", "run-zzz", "run-00000000-0000-0000-0000-00000000dead"} {
			resp := postSteer(t, srv.URL, id, "redirect")
			body := readAllClose(t, resp)
			if resp.StatusCode != http.StatusNotFound {
				t.Errorf("steer %q status = %d, want 404", id, resp.StatusCode)
			}
			if !strings.Contains(body, "run not found") {
				t.Errorf("steer %q body = %q, want the shared 'run not found'", id, body)
			}
		}
	})

	t.Run("foreign identity answers the same 404, never 403", func(t *testing.T) {
		foreign, err := s.runs.Start(runParams{
			runID:      "run-88888888-8888-8888-8888-888888888888",
			threadID:   "t-foreign-steer",
			identityID: "33333333-3333-3333-3333-333333333333",
		})
		if err != nil {
			t.Fatalf("Start foreign: %v", err)
		}
		defer foreign.finish()
		resp := postSteer(t, srv.URL, foreign.RunID, "redirect")
		body := readAllClose(t, resp)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("foreign steer status = %d, want 404 (existence hidden, never 403)", resp.StatusCode)
		}
		if !strings.Contains(body, "run not found") {
			t.Fatalf("foreign steer body = %q, want the shared 'run not found'", body)
		}
	})
}

// TestSteerRouteIsIdempotencyRegistered pins T-52-12: the steer route carries
// the SAME mandatory-header Idempotency-Key policy cancel does, so a
// replayed POST with the same key cannot enqueue a second steer.
func TestSteerRouteIsIdempotencyRegistered(t *testing.T) {
	meta, ok := httpMutationRoutes["POST /agent/runs/{runID}/steer"]
	if !ok {
		t.Fatal("POST /agent/runs/{runID}/steer is absent from httpMutationRoutes")
	}
	if meta.Scope != idempotency.ScopeHTTPMutation {
		t.Errorf("steer route Scope = %v, want ScopeHTTPMutation", meta.Scope)
	}
	if meta.Normalize == "" {
		t.Error("steer route Normalize is empty")
	}
	if meta.KeyPolicy != keyPolicyRequiredHeader {
		t.Errorf("steer route KeyPolicy = %v, want keyPolicyRequiredHeader (the same MANDATORY Idempotency-Key cancel carries)", meta.KeyPolicy)
	}
}

// TestSteerRouteRendersInboxSentinels drives every internal/steer sentinel
// through the real route and asserts the ratified refusal ladder — the
// route performs no TrimSpace/len/cap logic of its own (T-52-13): it only
// renders what internal/steer already decided.
func TestSteerRouteRendersInboxSentinels(t *testing.T) {
	const tid = "46464646-4646-4646-4646-464646464646"
	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
		&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	inbox := steer.New(steer.Config{Max: 1, MaxBytes: 8})
	s.SetSteerInbox(inbox)
	// A LIVE run, deliberately never driven to terminal: the sentinel-rendering
	// ladder below (empty/oversize/queue-full/closed) is exercised only once
	// Push is actually attempted, which the terminal-run 410 branch (52-05)
	// now gates on the session being live — runDetachedToTerminal would make
	// every subtest below observe the terminal refusal instead of the
	// sentinel it exists to test.
	sess, err := s.runs.Start(runParams{
		runID:      "run-46464646-4646-4646-4646-464646464646",
		threadID:   tid,
		identityID: localIdentityID,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.finish()
	runID := sess.RunID

	t.Run("empty text -> 400", func(t *testing.T) {
		resp := postSteer(t, srv.URL, runID, "   ")
		body := readAllClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%q", resp.StatusCode, body)
		}
	})

	t.Run("oversize text -> 400", func(t *testing.T) {
		resp := postSteer(t, srv.URL, runID, "this text is definitely over eight bytes")
		body := readAllClose(t, resp)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%q", resp.StatusCode, body)
		}
	})

	t.Run("queue full -> 429 with Retry-After", func(t *testing.T) {
		first := postSteer(t, srv.URL, runID, "hi")
		if first.StatusCode != http.StatusAccepted {
			t.Fatalf("first push status = %d, want 202", first.StatusCode)
		}
		first.Body.Close()
		resp := postSteer(t, srv.URL, runID, "hi")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429", resp.StatusCode)
		}
		if resp.Header.Get("Retry-After") == "" {
			t.Error("429 response missing Retry-After")
		}
		inbox.Drain(runIDToThreadID(s, runID))
	})

	t.Run("closed inbox -> 410", func(t *testing.T) {
		inbox.Close()
		resp := postSteer(t, srv.URL, runID, "hi")
		body := readAllClose(t, resp)
		if resp.StatusCode != http.StatusGone {
			t.Fatalf("status = %d, want 410; body=%q", resp.StatusCode, body)
		}
	})
}

// sseFrame is one parsed `event:`/`id:`/`data:` triple off the run-scoped
// wire (writeSeqFrame's own line order — server_run_detach.go).
type sseFrame struct {
	event string
	seq   int64
	data  string
}

// parseSeqFrames walks an SSE body into its ordered seq-carrying frames.
func parseSeqFrames(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	var cur sseFrame
	have := 0
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			cur = sseFrame{event: strings.TrimPrefix(line, "event: ")}
			have = 1
		case strings.HasPrefix(line, "id: ") && have == 1:
			n, err := strconv.ParseInt(strings.TrimPrefix(line, "id: "), 10, 64)
			if err != nil {
				t.Fatalf("unparsable id line %q: %v", line, err)
			}
			cur.seq = n
			have = 2
		case strings.HasPrefix(line, "data: ") && have == 2:
			cur.data = strings.TrimPrefix(line, "data: ")
			frames = append(frames, cur)
			have = 0
		}
	}
	return frames
}

// findSteerFrame locates the CUSTOM aura.steer frame among parsed frames.
func findSteerFrame(t *testing.T, frames []sseFrame) sseFrame {
	t.Helper()
	for _, f := range frames {
		if f.event == "CUSTOM" && strings.Contains(f.data, `"name":"`+SteerEventName+`"`) {
			return f
		}
	}
	t.Fatalf("no CUSTOM %s frame found among %d frames", SteerEventName, len(frames))
	return sseFrame{}
}

// TestSteerFrameReplaysFromSeq closes STEER-03's third leg: the aura.steer
// CUSTOM frame carries the SAME run-scoped `id:` seq as every other frame, so
// a Last-Event-ID resume includes it when acknowledged up to n-1 and omits it
// once acknowledged at n — no separate replay channel, no special-casing.
func TestSteerFrameReplaysFromSeq(t *testing.T) {
	const tid = "47474747-4747-4747-4747-474747474747"
	delta := map[string]any{
		"conversation_id": tid,
		"round":           uint32(1),
		"steers": []map[string]any{
			{"id": "s1", "source": "cockpit", "text": "redirect", "delivery": "tool_result_append"},
		},
	}
	events := []*agent.Event{
		{Author: "aura", LLMResponse: &agent.LLMResponse{Content: "before"}},
		steerEvent(delta),
		{Author: "aura", LLMResponse: &agent.LLMResponse{Content: "after", FinishReason: "stop"}},
	}
	_, srv := newDetachTestServer(t, &scriptedRunner{events: events},
		&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	runID := runDetachedToTerminal(t, srv, tid)

	fullResp, full := getEvents(t, srv, runID, "")
	if fullResp.StatusCode != http.StatusOK {
		t.Fatalf("full-replay status = %d, want 200", fullResp.StatusCode)
	}
	steerSeq := findSteerFrame(t, parseSeqFrames(t, full)).seq

	t.Run("Last-Event-ID = steerSeq-1 replays the steer frame", func(t *testing.T) {
		resp, body := getEvents(t, srv, runID, strconv.FormatInt(steerSeq-1, 10))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		frames := parseSeqFrames(t, body)
		found := false
		for _, f := range frames {
			if f.seq != steerSeq {
				continue
			}
			if f.event != "CUSTOM" || !strings.Contains(f.data, `"name":"`+SteerEventName+`"`) {
				t.Fatalf("frame at seq %d = %+v, want the aura.steer CUSTOM frame", steerSeq, f)
			}
			found = true
		}
		if !found {
			t.Fatalf("resume from %d did not replay seq %d (the steer frame): %v", steerSeq-1, steerSeq, frames)
		}
	})

	t.Run("Last-Event-ID = steerSeq does not replay the steer frame again", func(t *testing.T) {
		resp, body := getEvents(t, srv, runID, strconv.FormatInt(steerSeq, 10))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		frames := parseSeqFrames(t, body)
		for _, f := range frames {
			if f.seq <= steerSeq {
				t.Fatalf("resume acknowledged through %d still replayed already-acknowledged seq %d (steer frame at %d): %v", steerSeq, f.seq, steerSeq, frames)
			}
		}
	})
}

func readAllClose(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
}

// runIDToThreadID resolves runID's conversation id through the SAME registry
// the route uses, so the test can drain the exact conv key Push used.
func runIDToThreadID(s *Server, runID string) string {
	sess, ok := s.runs.Get(runID)
	if !ok {
		return ""
	}
	return sess.ThreadID
}

// TestSteerAtTerminalRunIs410 closes the D-09 complement (52-05): a steer
// POSTed against a run whose session is ALREADY terminal is refused 410 with
// a body that says the message was NOT queued, and the inbox for that
// conversation is left genuinely untouched -- Push is never even attempted.
func TestSteerAtTerminalRunIs410(t *testing.T) {
	const tid = "48484848-4848-4848-4848-484848484848"
	s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
		&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
	s.SetSteerInbox(inbox)
	runID := runDetachedToTerminal(t, srv, tid)

	resp := postSteer(t, srv.URL, runID, "too late")
	body := readAllClose(t, resp)
	if resp.StatusCode != http.StatusGone {
		t.Fatalf("status = %d, want 410; body=%q", resp.StatusCode, body)
	}
	if !strings.Contains(body, "not queued") {
		t.Fatalf("body = %q, want it to say plainly the message was NOT queued", body)
	}
	if strings.Contains(body, "replay window exceeded") {
		t.Fatalf("body = %q, collides with the resume route's OWN 410 body -- must be distinguishable", body)
	}
	if msgs := inbox.Drain(tid); len(msgs) != 0 {
		t.Fatalf("inbox after a terminal-run POST = %+v, want empty -- Push must never have been attempted", msgs)
	}
}

// TestSteerHasNoFourthOutcome is FA-3's closure and the plan's own named
// deliverable: a table drives the three EXHAUSTIVE outcomes of a steer POST
// against a REAL runner + a REAL HTTP round-trip, each verified by an
// independent observable — the model's next request, the persisted next
// turn, and the HTTP status respectively — so it fails the moment a fourth
// path (accept, then silently drop) is ever introduced.
//
// The "auto-delivered next turn" subtest drives Runner.Turn directly rather
// than racing a live HTTP POST against a detached run's producer goroutine:
// forcing that exact race deterministically requires code to execute WHILE
// llm_agent.go's runTerminal processes the terminal text_response call, and
// runTerminal never touches the tool registry, so no test hook reaches that
// window without either modifying production code or accepting genuine
// goroutine-scheduling non-determinism -- precisely the flakiness class this
// codebase's own steerInjectorTool precedent (internal/runner) exists to
// avoid. The exact HTTP-transport race is proven deterministic and thorough
// at its own layer by 52-05's runner-package suite
// (TestLeftoverSteerAutoDeliversAsNextTurn et al.); this subtest instead
// proves the SAME outcome is reachable and observable through the real
// runner + the real conversation store, which is what a "fourth outcome"
// regression would actually corrupt.
func TestSteerHasNoFourthOutcome(t *testing.T) {
	t.Run("delivered mid-run", func(t *testing.T) {
		// steerViaHTTPTool fires a REAL http.Post (http.DefaultClient) against the
		// httptest.Server below; its keep-alive connection would otherwise leave
		// persistConn read/write-loop goroutines parked in the shared default
		// transport past this test's own return, which a LATER test's own
		// goleak.VerifyNone(t) (this package runs several) would then flag as
		// unowned -- the exact class of false-positive internal/runner's TestMain
		// already documents and works around at package scope. This test is the
		// specific one that tips the shared pool over the edge (verified: the
		// pre-existing TestSteerEndToEndRedirectsNextRound's own two real POSTs do
		// not, by themselves, trigger it), so the cleanup lives here.
		t.Cleanup(func() { http.DefaultClient.CloseIdleConnections() })
		inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
		client := agenttest.NewFakeClient(
			agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "steer_via_http", "{}")),
			agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"done"}`)),
		)
		tool := &steerViaHTTPTool{convID: "", text: "mid-run redirect"}
		r, conv := newRealSteerRunner(t, client, inbox, tool)
		convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
		if err != nil {
			t.Fatalf("NewConversationWithID: %v", err)
		}
		tool.convID = convID
		s, srv := newDetachTestServer(t, r, conv, ServerConfig{})
		s.SetSteerInbox(inbox)
		tool.baseURL = srv.URL
		tool.runs = s.runs

		resp := postRun(t, srv, steerRunPayload(convID))
		defer resp.Body.Close()
		_ = readFullBody(t, resp)

		status, postErr := tool.result()
		if postErr != nil {
			t.Fatalf("steer POST from tool: %v", postErr)
		}
		if status != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 (delivered mid-run)", status)
		}
		reqs := client.RecordedRequests()
		if len(reqs) < 2 {
			t.Fatalf("recorded %d requests, want at least 2 (round 1 + round 2)", len(reqs))
		}
		if !strings.Contains(joinMessageContents(reqs[1].Messages), "mid-run redirect") {
			t.Fatalf("round 2 missing the steer text -- outcome was not 'delivered mid-run': %+v", reqs[1].Messages)
		}
	})

	t.Run("auto-delivered next turn", func(t *testing.T) {
		inbox := steer.New(steer.Config{Max: 8, MaxBytes: 16384})
		client := agenttest.NewFakeClient(
			agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "text_response", `{"text":"round one done"}`)),
			agenttest.ToolCallTurn(agenttest.MakeToolCall("call-2", "text_response", `{"text":"round two done"}`)),
		)
		r, conv := newRealSteerRunner(t, client, inbox)
		convID, err := r.NewConversationWithID(context.Background(), uuid.NewString())
		if err != nil {
			t.Fatalf("NewConversationWithID: %v", err)
		}

		pushed := false
		var sawNotice bool
		for ev, err := range r.Turn(context.Background(), convID, new("please handle this task")) {
			if err != nil {
				t.Fatalf("turn: %v", err)
			}
			if !pushed && ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
				if pushErr := inbox.Push(convID, "cockpit", "switch to plan B"); pushErr != nil {
					t.Fatalf("Push: %v", pushErr)
				}
				pushed = true
			}
			if ev.Actions.SteerDelta != nil {
				if steers, ok := ev.Actions.SteerDelta["steers"].([]map[string]any); ok {
					for _, s := range steers {
						if s["delivery"] == "auto_delivery_next_turn" {
							sawNotice = true
						}
					}
				}
			}
		}
		if !pushed {
			t.Fatal("race window never opened")
		}
		if !sawNotice {
			t.Fatal("no aura.steer notice Event naming the auto-delivery-next-turn form")
		}
		hist, err := conv.LoadHistory(context.Background(), convID)
		if err != nil {
			t.Fatalf("LoadHistory: %v", err)
		}
		count := 0
		for _, m := range hist {
			if m.Role == llm.RoleUser && strings.Contains(m.Content, "switch to plan B") {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("persisted RoleUser rows carrying the leftover = %d, want exactly 1 (the persisted-next-turn observable): %+v", count, hist)
		}
	})

	t.Run("refused with an actionable status", func(t *testing.T) {
		const tid = "49494949-4949-4949-4949-494949494949"
		s, srv := newDetachTestServer(t, &scriptedRunner{events: textTurn("hi")},
			&fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
		s.SetSteerInbox(steer.New(steer.Config{Max: 8, MaxBytes: 16384}))
		runID := runDetachedToTerminal(t, srv, tid)

		resp := postSteer(t, srv.URL, runID, "too late")
		body := readAllClose(t, resp)
		if resp.StatusCode != http.StatusGone {
			t.Fatalf("status = %d, want 410 (an actionable, non-silent refusal)", resp.StatusCode)
		}
		if !strings.Contains(body, "not queued") {
			t.Fatalf("body = %q, want the operator-actionable 'not queued' wording", body)
		}
	})
}
