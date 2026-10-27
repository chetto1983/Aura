package agui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
)

func runPayloadOn(threadID, parentSeq string) string {
	return `{"threadId":"` + threadID + `","messages":[{"id":"m1","role":"user","content":"ciao"}],` +
		`"aura":{"parent_seq":` + parentSeq + `}}`
}

// branchServer serves a thread whose turn written last is seq 6.
func branchServer(t *testing.T, run *scriptedRunner, pendings ...askuser.Pending) *Server {
	t.Helper()
	s := NewServer(run, &fakeConvStore{known: map[string]bool{goodID: true}, latestSeq: 6}, ServerConfig{})
	s.idgen = &fixedIDGen{}
	s.SetApprovalStore(&fakeApprovalStore{pendings: pendings})
	return s
}

func postOn(t *testing.T, s *Server, body string) (int, string) {
	t.Helper()
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	resp, err := http.Post(srv.URL+"/agent/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /agent/run: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestRunParentSeq_RefusesWhatNamesNoTurn(t *testing.T) {
	for name, tc := range map[string]struct {
		seq  string
		want int
	}{
		"zero":          {"0", http.StatusBadRequest},
		"negative":      {"-2", http.StatusBadRequest},
		"a string":      {`"3"`, http.StatusBadRequest},
		"past the last": {"7", http.StatusNotFound},
		"far past":      {"9999", http.StatusNotFound},
		"the last":      {"6", http.StatusOK},
		"an older turn": {"2", http.StatusOK},
		"the first":     {"1", http.StatusOK},
	} {
		t.Run(name, func(t *testing.T) {
			run := &scriptedRunner{events: textTurn("ok")}
			code, body := postOn(t, branchServer(t, run), runPayloadOn(goodID, tc.seq))
			if code != tc.want {
				t.Fatalf("parent_seq %s = %d, want %d: %s", tc.seq, code, tc.want, body)
			}
			if refused := code != http.StatusOK; refused && run.turnCalled {
				t.Fatalf("parent_seq %s was refused, and the run started anyway", tc.seq)
			}
		})
	}
}

// A pending pause resumes after the turn written last, so only that branch may continue
// while it waits (spec 2026-10-27, D11).
func TestRunParentSeq_APendingPauseHoldsOtherBranches(t *testing.T) {
	pending := askuser.Pending{ConversationID: goodID, ToolCallID: "call-1"}
	run := &scriptedRunner{events: textTurn("ok")}
	if code, body := postOn(t, branchServer(t, run, pending), runPayloadOn(goodID, "2")); code != http.StatusConflict {
		t.Fatalf("another branch with a pause pending = %d, want 409: %s", code, body)
	}
	if code, body := postOn(t, branchServer(t, run, pending), runPayloadOn(goodID, "6")); code != http.StatusOK {
		t.Fatalf("the paused branch itself = %d, want 200: %s", code, body)
	}
	other := askuser.Pending{ConversationID: "another-thread", ToolCallID: "call-2"}
	if code, body := postOn(t, branchServer(t, run, other), runPayloadOn(goodID, "2")); code != http.StatusOK {
		t.Fatalf("a pause on another thread = %d, want 200: %s", code, body)
	}
}

// The stream ends with the turn written last, which the cockpit continues from next.
func TestRun_ReportsTheLeafItEndedOn(t *testing.T) {
	run := &scriptedRunner{events: textTurn("ok")}
	code, body := postOn(t, branchServer(t, run), runPayload(goodID))
	if code != http.StatusOK {
		t.Fatalf("run = %d: %s", code, body)
	}
	types, data := sseFrames(t, body)
	if !strings.Contains(data, `"path":"/leaf_seq","value":6`) {
		t.Fatalf("the stream carries no leaf_seq 6: %v %s", types, data)
	}
	if n := len(types); n < 2 || types[n-1] != "RUN_FINISHED" || types[n-2] != "STATE_DELTA" {
		t.Fatalf("frames = %v, want the leaf report right before RUN_FINISHED", types)
	}
}

// The detached path (AURA_AGUI_RUN_DETACH) validates under its own lock and releases it on
// a refusal, so the next run on the thread still starts.
func TestRunParentSeq_TheDetachedPathRefusesAndReleases(t *testing.T) {
	run := &guardedScriptedRunner{}
	run.events = textTurn("ok")
	_, srv := newDetachTestServer(t, run, &fakeConvStore{known: map[string]bool{goodID: true}, latestSeq: 6}, ServerConfig{})
	for _, tc := range []struct {
		body string
		want int
	}{
		{runPayloadOn(goodID, "9"), http.StatusNotFound},
		{runPayloadOn(goodID, "0"), http.StatusBadRequest},
		{runPayloadOn(goodID, "2"), http.StatusOK},
	} {
		resp := postRun(t, srv, tc.body)
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("%s = %d, want %d", tc.body, resp.StatusCode, tc.want)
		}
	}
}
