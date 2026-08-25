package agui

import (
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/steer"
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
	runID := runDetachedToTerminal(t, srv, tid)

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
