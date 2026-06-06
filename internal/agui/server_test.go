package agui

import (
	"bufio"
	"context"
	"errors"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

// fakeConvStore is an in-memory ConversationStore: known thread ids resolve, history
// projects the seeded turns, and any other id maps to ErrConversationNotFound (the 404
// chokepoint). loadErr injects a LoadHistory failure for the error-body test.
type fakeConvStore struct {
	known   map[string]bool
	history map[string][]llm.Message
	loadErr error
}

func (f *fakeConvStore) Get(_ context.Context, id string) (conversations.Conversation, error) {
	if f.known[id] {
		return conversations.Conversation{ID: id}, nil
	}
	return conversations.Conversation{}, conversations.ErrConversationNotFound
}

func (f *fakeConvStore) LoadHistory(_ context.Context, id string) ([]llm.Message, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.history[id], nil
}

// scriptedRunner is a fake Runner whose Turn replays a fixed *agent.Event slice and
// whose SubmitAnswers records the resume map it was given. turnErr injects a turn-level
// error (drives the RUN_ERROR path) for the redaction test.
type scriptedRunner struct {
	events     []*agent.Event
	turnErr    error
	gotAnswers map[string]runner.ResponseInput
	answersErr error
}

func (s *scriptedRunner) Turn(_ context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		for _, ev := range s.events {
			if !yield(ev, nil) {
				return
			}
		}
		if s.turnErr != nil {
			yield(nil, s.turnErr)
		}
	}
}

func (s *scriptedRunner) SubmitAnswers(_ context.Context, answers map[string]runner.ResponseInput) (int, error) {
	s.gotAnswers = answers
	if s.answersErr != nil {
		return 0, s.answersErr
	}
	return 0, nil
}

// textTurn is the canonical happy-path turn: one streamed assistant chunk + a final
// Event closing the run (FinishReason). It yields RUN_STARTED…TEXT_MESSAGE_*…RUN_FINISHED.
func textTurn(delta string) []*agent.Event {
	return []*agent.Event{
		{Author: "aura", LLMResponse: &agent.LLMResponse{Content: delta}},
		{Author: "aura", LLMResponse: &agent.LLMResponse{Content: delta, FinishReason: "stop"}},
	}
}

// runPayload is a minimal valid RunAgentInput JSON body for the given thread id.
func runPayload(threadID string) string {
	return `{"threadId":"` + threadID + `","messages":[{"id":"m1","role":"user","content":"ciao"}]}`
}

// sseFrames reads an SSE response body into the ordered list of `event:` types plus the
// concatenated `data:` payloads.
func sseFrames(t *testing.T, body string) (types []string, data string) {
	t.Helper()
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			types = append(types, strings.TrimPrefix(line, "event: "))
		case strings.HasPrefix(line, "data: "):
			data += strings.TrimPrefix(line, "data: ")
		}
	}
	return types, data
}

// postRun drives a single POST /agent/run against the mux and returns the response.
func postRun(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/agent/run", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /agent/run: %v", err)
	}
	return resp
}

func newTestServer(t *testing.T, run Runner, conv ConversationStore) *httptest.Server {
	t.Helper()
	s := NewServer(run, conv, ServerConfig{})
	s.idgen = &fixedIDGen{}
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

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

// TestServer_RunBadRequests: malformed JSON, empty messages, and an over-cap body all
// map to 400 before any turn runs.
func TestServer_RunBadRequests(t *testing.T) {
	const tid = "33333333-3333-3333-3333-333333333333"
	srv := newTestServer(t, &scriptedRunner{}, &fakeConvStore{known: map[string]bool{tid: true}})

	cases := map[string]string{
		"malformed JSON": `{"threadId":`,
		"empty messages": `{"threadId":"` + tid + `","messages":[]}`,
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

// TestServer_ResumeMapsToSubmitAnswers: a resolved Resume entry maps to an accept
// action and a cancelled one to cancel, both carrying the payload content; SubmitAnswers
// is called before the Turn streams.
func TestServer_ResumeMapsToSubmitAnswers(t *testing.T) {
	const tid = "44444444-4444-4444-4444-444444444444"
	run := &scriptedRunner{events: textTurn("ok")}
	srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}})

	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"x"}],` +
		`"resume":[{"interruptId":"tok-1","status":"resolved","payload":"yes"},` +
		`{"interruptId":"tok-2","status":"cancelled"}]}`
	resp := postRun(t, srv, body)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if run.gotAnswers == nil {
		t.Fatal("SubmitAnswers was not called for a non-empty Resume[]")
	}
	if a := run.gotAnswers["tok-1"]; a.Action != "accept" || a.Content != "yes" {
		t.Errorf("tok-1 = %+v, want accept/yes", a)
	}
	if a := run.gotAnswers["tok-2"]; a.Action != "cancel" {
		t.Errorf("tok-2 action = %q, want cancel", a.Action)
	}
}

// TestServer_MessagesSnapshot (SC3): GET /threads/<id>/messages returns an
// application/json MESSAGES_SNAPSHOT whose messages match the LoadHistory projection.
func TestServer_MessagesSnapshot(t *testing.T) {
	const tid = "55555555-5555-5555-5555-555555555555"
	conv := &fakeConvStore{
		known: map[string]bool{tid: true},
		history: map[string][]llm.Message{tid: {
			{Role: llm.RoleUser, Content: "ciao"},
			{Role: llm.RoleAssistant, Content: "salve"},
		}},
	}
	srv := newTestServer(t, &scriptedRunner{}, conv)

	resp, err := http.Get(srv.URL + "/threads/" + tid + "/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "MESSAGES_SNAPSHOT") {
		t.Errorf("body is not a MESSAGES_SNAPSHOT: %q", body)
	}
	for _, want := range []string{"ciao", "salve", `"msg-1"`, `"msg-2"`} {
		if !strings.Contains(body, want) {
			t.Errorf("snapshot missing %q in %q", want, body)
		}
	}
}

// TestServer_MessagesUnknownThread404: GET on an unknown thread → 404.
func TestServer_MessagesUnknownThread404(t *testing.T) {
	srv := newTestServer(t, &scriptedRunner{}, &fakeConvStore{known: map[string]bool{}})
	resp, err := http.Get(srv.URL + "/threads/66666666-6666-6666-6666-666666666666/messages")
	if err != nil {
		t.Fatalf("GET messages: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// TestServer_RunErrorRedaction (T-12-10): a Runner whose turn errors with a synthetic
// DSN must NOT leak the secret onto the wire — the RUN_ERROR frame routes through
// sanitizeErr.
func TestServer_RunErrorRedaction(t *testing.T) {
	const tid = "77777777-7777-7777-7777-777777777777"
	const dsn = "postgresql://user:secret@host/db"
	run := &scriptedRunner{turnErr: errors.New("tool failed dialing " + dsn)}
	srv := newTestServer(t, run, &fakeConvStore{known: map[string]bool{tid: true}})

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
		t.Errorf("RUN_ERROR leaked the DSN secret onto the wire: %q", body)
	}
}

// TestServer_DisconnectClosesPump (Pitfall 4): a client that disconnects mid-SSE leaves
// no leaked pump goroutine — NumGoroutine returns to baseline. The scripted turn yields
// one event then the connection is cancelled before the stream completes.
func TestServer_DisconnectClosesPump(t *testing.T) {
	const tid = "88888888-8888-8888-8888-888888888888"
	// A long turn: enough deltas that the client can cancel mid-stream.
	evs := make([]*agent.Event, 0, 200)
	for i := 0; i < 200; i++ {
		evs = append(evs, &agent.Event{Author: "aura", LLMResponse: &agent.LLMResponse{Content: "x"}})
	}
	srv := newTestServer(t, &scriptedRunner{events: evs}, &fakeConvStore{known: map[string]bool{tid: true}})

	baseline := runtime.NumGoroutine()
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

	// Read the first frame to confirm the stream is live, then disconnect.
	sc := bufio.NewScanner(resp.Body)
	if sc.Scan() {
		_ = sc.Text()
	}
	cancel()
	resp.Body.Close()

	// Goroutines return to baseline (poll — teardown is async). goleak TestMain also guards.
	for i := 0; i < 100; i++ {
		if runtime.NumGoroutine() <= baseline+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutines = %d, baseline %d — SSE pump leaked", runtime.NumGoroutine(), baseline)
}

// TestSanitizeErr is a focused unit test on the redaction helper across DSN schemes and
// the nil error.
func TestSanitizeErr(t *testing.T) {
	cases := map[string]struct {
		in        error
		wantNoSub string
		want      string
	}{
		"nil":      {in: nil, want: ""},
		"postgres": {in: errors.New("dial postgresql://u:p@h/db failed"), wantNoSub: "p@h"},
		"redis":    {in: errors.New("redis://x:y@z:6379 timeout"), wantNoSub: "y@z"},
		"plain":    {in: errors.New("plain message"), want: "plain message"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := sanitizeErr(tc.in)
			if tc.want != "" || tc.in == nil {
				if got != tc.want {
					t.Fatalf("sanitizeErr = %q, want %q", got, tc.want)
				}
				return
			}
			if strings.Contains(got, tc.wantNoSub) {
				t.Errorf("sanitizeErr leaked %q in %q", tc.wantNoSub, got)
			}
		})
	}
}
