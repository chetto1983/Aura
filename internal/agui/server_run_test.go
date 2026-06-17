package agui

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestServer_RunSSERoundTrip (SC1): a known thread + a user message streams the
// RUN_STARTED…RUN_FINISHED frame sequence with ≥1 TEXT_MESSAGE_CONTENT delta over a
// text/event-stream response.
func TestServer_RunSSERoundTrip(t *testing.T) {
	const tid = "11111111-1111-1111-1111-111111111111"
	srv := newTestServer(t,
		&scriptedRunner{events: textTurn("Ciao")},
		&fakeConvStore{known: map[string]bool{tid: true}})

	resp := postRun(t, srv, runPayload(tid))
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	got, data := sseFrames(t, string(raw))
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("frame sequence = %v, want %v", got, want)
	}
	if !strings.Contains(data, "Ciao") {
		t.Errorf("TEXT_MESSAGE_CONTENT delta missing from payload: %q", data)
	}
}

// TestServer_RunUnknownThread404: an unknown threadId → 404, no SSE stream.
func TestServer_RunUnknownThread404(t *testing.T) {
	srv := newTestServer(t, &scriptedRunner{}, &fakeConvStore{known: map[string]bool{}})
	resp := postRun(t, srv, runPayload("22222222-2222-2222-2222-222222222222"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("unknown thread streamed SSE (ct=%q); want a plain 404", ct)
	}
}

// TestServer_MalformedThreadID404: a syntactically-invalid (non-UUID) thread id is
// definitionally not an existing thread — both endpoints return 404, not the store's
// parse error as a 500 (T-12-11; the live agui smoke's `does-not-exist` chokepoint).
func TestServer_MalformedThreadID404(t *testing.T) {
	srv := newTestServer(t, &scriptedRunner{}, &fakeConvStore{known: map[string]bool{}})

	t.Run("POST /agent/run", func(t *testing.T) {
		resp := postRun(t, srv, runPayload("does-not-exist"))
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
	t.Run("GET messages", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/threads/does-not-exist/messages")
		if err != nil {
			t.Fatalf("GET messages: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}

// TestServer_ResumeSubmitError: a SubmitAnswers failure on a non-empty Resume[] maps
// to a sanitized 400 before any SSE stream opens (the resume validation chokepoint).
func TestServer_ResumeSubmitError(t *testing.T) {
	const tid = "66666666-6666-6666-6666-666666666666"
	run := &scriptedRunner{events: textTurn("ok"), answersErr: errors.New("postgresql://u:secret@h/db pause not found")}
	srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}})

	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"x"}],` +
		`"resume":[{"interruptId":"tok-1","status":"resolved","payload":"yes"}]}`
	resp := postRun(t, srv, body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "secret") {
		t.Errorf("resume error leaked the DSN secret in the 400 body: %q", string(raw))
	}
}

// TestServer_MessagesLoadHistoryError: a LoadHistory failure after the thread resolves
// returns a sanitized 500 (no DSN leak in the body).
func TestServer_MessagesLoadHistoryError(t *testing.T) {
	const tid = "77777777-7777-7777-7777-777777777777"
	conv := &fakeConvStore{known: map[string]bool{tid: true}, loadErr: errors.New("postgresql://u:secret@h/db read failed")}
	srv := newTestServer(t, &scriptedRunner{}, conv)

	resp, err := http.Get(srv.URL + "/threads/" + tid + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "secret") {
		t.Errorf("LoadHistory error leaked the DSN secret in the 500 body: %q", string(raw))
	}
}

// TestServer_RunContinueAfterResume (D-05 / CR-01): an empty Messages list on a known
// thread is NOT a bad request — it is the continue-after-resume signal the cockpit re-drive
// POSTs after an inline approval resolves (ExternalStoreChat resumeNonce). handleRun must
// resolve userMsg=nil and drive Runner.Turn(…, nil) over the rehydrated history (the resolved
// ask_user answer is already a RoleTool turn), streaming the continued turn — never a 400.
func TestServer_RunContinueAfterResume(t *testing.T) {
	const tid = "44444444-4444-4444-4444-444444444444"
	run := &scriptedRunner{events: textTurn("resumed")}
	srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}})

	resp := postRun(t, srv, `{"threadId":"`+tid+`","messages":[]}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (continue-after-resume must not 400)", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	got, data := sseFrames(t, string(raw))
	if len(got) == 0 || got[0] != "RUN_STARTED" {
		t.Fatalf("frame sequence = %v, want a streamed turn starting with RUN_STARTED", got)
	}
	if !strings.Contains(data, "resumed") {
		t.Errorf("continued turn did not stream the resumed assistant text: %q", data)
	}
	if !run.turnCalled {
		t.Fatal("Runner.Turn was never called — continue-after-resume never reached the runner")
	}
	if run.gotTurnUserMsg != nil {
		t.Errorf("Turn userMsg = %q, want nil (continue-after-resume drives no fresh user message)", *run.gotTurnUserMsg)
	}
}

// TestServer_RunBadRequests: malformed JSON, an empty threadId, and an over-cap body all
// map to 400 before any turn runs. NOTE: empty messages is intentionally NOT here — it is the
// continue-after-resume signal (see TestServer_RunContinueAfterResume); the prior "empty
// messages → 400" case was removed when D-05 wired the resume re-drive over /agent/run (CR-01).
func TestServer_RunBadRequests(t *testing.T) {
	const tid = "33333333-3333-3333-3333-333333333333"
	srv := newTestServer(t, &scriptedRunner{}, &fakeConvStore{known: map[string]bool{tid: true}})

	cases := map[string]string{
		"malformed JSON": `{"threadId":`,
		"empty threadId": `{"threadId":"","messages":[{"id":"m1","role":"user","content":"x"}]}`,
		"over-cap body":  `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"` + strings.Repeat("A", (1<<20)+16) + `"}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			resp := postRun(t, srv, body)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
}
