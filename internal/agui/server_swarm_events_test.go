package agui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
	"go.uber.org/goleak"
)

type fakeSwarmEventReader struct {
	mu        sync.Mutex
	data      map[string][]byte
	readErr   error
	listErr   error
	firstRead chan struct{}
	readOnce  sync.Once
	reads     int
}

func newFakeSwarmEventReader(children map[string][]byte) *fakeSwarmEventReader {
	return &fakeSwarmEventReader{data: children, firstRead: make(chan struct{})}
}

func (f *fakeSwarmEventReader) ReadTranscript(_ context.Context, _ string, childID string, fromOffset int64) ([]byte, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	f.readOnce.Do(func() { close(f.firstRead) })
	if f.readErr != nil {
		return nil, fromOffset, f.readErr
	}
	body, ok := f.data[childID]
	if !ok || fromOffset < 0 || fromOffset > int64(len(body)) {
		return nil, fromOffset, errors.New("transcript not found")
	}
	return append([]byte(nil), body[fromOffset:]...), int64(len(body)), nil
}

func (f *fakeSwarmEventReader) ListChildTranscripts(_ context.Context, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	children := make([]string, 0, len(f.data))
	for child := range f.data {
		children = append(children, child)
	}
	sort.Strings(children)
	return children, nil
}

func (f *fakeSwarmEventReader) append(child string, lines ...[]byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, line := range lines {
		f.data[child] = append(f.data[child], line...)
	}
}

func swarmEventLine(t *testing.T, ev agent.Event) []byte {
	t.Helper()
	if ev.RequestID == uuid.Nil {
		ev.RequestID = uuid.Must(uuid.NewV7())
	}
	if ev.Author == "" {
		ev.Author = "worker"
	}
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now().UTC()
	}
	body, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return append(body, '\n')
}

func swarmTextLine(t *testing.T, text string) []byte {
	t.Helper()
	return swarmEventLine(t, agent.Event{LLMResponse: &agent.LLMResponse{Content: text}})
}

func swarmTerminalLine(t *testing.T, status string) []byte {
	t.Helper()
	return swarmEventLine(t, agent.Event{Actions: agent.Actions{StateDelta: map[string]any{
		"swarm_child_status": status,
		"swarm_child_id":     "w1",
	}}})
}

func newSwarmWorkerEventsServer(t *testing.T, store ConversationStore, reader swarmTranscriptReader, idle time.Duration) *httptest.Server {
	t.Helper()
	s := NewServer(&scriptedRunner{conv: store}, store, ServerConfig{})
	if reader != nil {
		s.SetSwarmTranscripts(reader)
	}
	s.SetSwarmWorkerIdle(idle)
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

func swarmWorkerEventsPath(conv, child string) string {
	path := "/api/conversations/" + conv + "/swarm/events"
	if child != "" {
		path += "?child=" + child
	}
	return path
}

func TestSwarmWorkerEventsForeignIdentityIs404(t *testing.T) {
	rejected := newFakeSwarmEventReader(map[string][]byte{"w1": nil})
	rejected.readErr = errors.New("hostile child id")

	cases := []struct {
		name   string
		conv   string
		store  ConversationStore
		reader swarmTranscriptReader
	}{
		{name: "malformed", conv: "not-a-uuid", store: newOwnerConvStore(goodID, localIdentityID), reader: newFakeSwarmEventReader(map[string][]byte{"w1": nil})},
		{name: "foreign", conv: goodID, store: newOwnerConvStore(goodID, uuid.Must(uuid.NewV7()).String()), reader: newFakeSwarmEventReader(map[string][]byte{"w1": nil})},
		{name: "unwired", conv: goodID, store: newOwnerConvStore(goodID, localIdentityID)},
		{name: "rejected-child", conv: goodID, store: newOwnerConvStore(goodID, localIdentityID), reader: rejected},
	}

	wantBody := swarmTranscriptNotFoundBody + "\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSwarmWorkerEventsServer(t, tc.store, tc.reader, 0)
			code, body := req(t, srv, http.MethodGet, swarmWorkerEventsPath(tc.conv, "w1"), "")
			if code != http.StatusNotFound || body != wantBody {
				t.Fatalf("status/body = %d/%q, want opaque 404 %q", code, body, wantBody)
			}
		})
	}
}

func TestSwarmWorkerStatusForeignIdentityIs404(t *testing.T) {
	rejected := newFakeSwarmEventReader(map[string][]byte{"w1": nil})
	rejected.listErr = errors.New("list refused")
	cases := []struct {
		name   string
		conv   string
		store  ConversationStore
		reader swarmTranscriptReader
	}{
		{name: "malformed", conv: "not-a-uuid", store: newOwnerConvStore(goodID, localIdentityID), reader: newFakeSwarmEventReader(nil)},
		{name: "foreign", conv: goodID, store: newOwnerConvStore(goodID, uuid.Must(uuid.NewV7()).String()), reader: newFakeSwarmEventReader(nil)},
		{name: "unwired", conv: goodID, store: newOwnerConvStore(goodID, localIdentityID)},
		{name: "rejected-list", conv: goodID, store: newOwnerConvStore(goodID, localIdentityID), reader: rejected},
	}
	wantBody := swarmTranscriptNotFoundBody + "\n"
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newSwarmWorkerEventsServer(t, tc.store, tc.reader, 0)
			code, body := req(t, srv, http.MethodGet, swarmWorkerEventsPath(tc.conv, ""), "")
			if code != http.StatusNotFound || body != wantBody {
				t.Fatalf("status/body = %d/%q, want opaque 404 %q", code, body, wantBody)
			}
		})
	}
}

func TestSwarmWorkerEventsReplayAndStopsOnTerminal(t *testing.T) {
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": {}})
	reader.append("w1", swarmTextLine(t, "first"), swarmTextLine(t, "second"), swarmTerminalLine(t, "ok"))
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 0)

	resp, err := http.Get(srv.URL + swarmWorkerEventsPath(goodID, "w1"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status/content-type = %d/%q, want 200 text/event-stream", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	types, data := sseFrames(t, string(body))
	if len(types) < 2 || types[0] != "RUN_STARTED" || types[len(types)-1] != "RUN_FINISHED" {
		t.Fatalf("frame types = %v, want RUN_STARTED ... RUN_FINISHED", types)
	}
	first, second := strings.Index(data, "first"), strings.Index(data, "second")
	if first < 0 || second <= first {
		t.Fatalf("SSE data did not preserve transcript order: %s", data)
	}
}

func TestSwarmWorkerEventsTailsAppendedEvent(t *testing.T) {
	baseline := goleak.IgnoreCurrent()
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": {}})
	reader.append("w1", swarmTextLine(t, "before-open"))
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 0)

	resp, err := http.Get(srv.URL + swarmWorkerEventsPath(goodID, "w1"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	<-reader.firstRead
	reader.append("w1", swarmTextLine(t, "after-open"), swarmTerminalLine(t, "ok"))
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("read SSE: %v", err)
	}
	if !strings.Contains(string(body), "before-open") || !strings.Contains(string(body), "after-open") {
		t.Fatalf("tail did not include both sides of the append: %s", body)
	}
	srv.Close()
	goleak.VerifyNone(t, baseline)
}

func TestSwarmWorkerEventsSkipsMalformedLine(t *testing.T) {
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": {}})
	reader.append("w1", swarmTextLine(t, "before-bad"), []byte("{not-json}\n"), swarmTextLine(t, "after-bad"), swarmTerminalLine(t, "ok"))
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 0)

	resp, err := http.Get(srv.URL + swarmWorkerEventsPath(goodID, "w1"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "before-bad") || !strings.Contains(string(body), "after-bad") {
		t.Fatalf("valid events around malformed line were lost: %s", body)
	}
	if strings.Contains(string(body), "RUN_ERROR") {
		t.Fatalf("malformed line killed the stream: %s", body)
	}
}

func TestSwarmWorkerEventsStopsOnIdleWithoutMarker(t *testing.T) {
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": {}})
	reader.append("w1", swarmTextLine(t, "orphaned-worker"))
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 20*time.Millisecond)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(srv.URL + swarmWorkerEventsPath(goodID, "w1"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("idle stream did not close itself: %v", err)
	}
	types, data := sseFrames(t, string(body))
	if len(types) == 0 || types[len(types)-1] != "RUN_FINISHED" || !strings.Contains(data, `"path":"/swarm_child_status","value":"stalled"`) {
		t.Fatalf("idle terminal frames/data = %v / %s", types, data)
	}
}

func TestSwarmWorkerEventsRedactsReasoning(t *testing.T) {
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": {}})
	reader.append("w1",
		swarmEventLine(t, agent.Event{LLMResponse: &agent.LLMResponse{Reasoning: "private-chain-of-thought"}}),
		swarmTerminalLine(t, "ok"),
	)
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 0)

	resp, err := http.Get(srv.URL + swarmWorkerEventsPath(goodID, "w1"))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "private-chain-of-thought") || !strings.Contains(string(body), redactedReasoningDelta) {
		t.Fatalf("reasoning privacy posture failed: %s", body)
	}
}

func TestSwarmWorkerStatusEmitsOnlyOnChange(t *testing.T) {
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": swarmTextLine(t, "private transcript sentinel")})
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 0)
	resp, scanner := openSwarmStatusStream(t, srv)
	defer resp.Body.Close()

	first := readSwarmStatusFrame(t, scanner)
	if first.Status != "running" {
		t.Fatalf("first status = %q", first.Status)
	}
	reader.append("w1", swarmTerminalLine(t, "ok"))
	second := readSwarmStatusFrame(t, scanner)
	if second.Status != "ok" {
		t.Fatalf("second status = %q, want ok without an unchanged duplicate", second.Status)
	}
}

func TestSwarmWorkerStatusCarriesNoTranscriptText(t *testing.T) {
	reader := newFakeSwarmEventReader(map[string][]byte{"w1": swarmTextLine(t, "private transcript sentinel")})
	srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, 0)
	resp, scanner := openSwarmStatusStream(t, srv)
	defer resp.Body.Close()
	frame := readSwarmStatusFrame(t, scanner)
	body, _ := json.Marshal(frame)
	if strings.Contains(string(body), "private transcript sentinel") {
		t.Fatalf("status payload leaked transcript text: %s", body)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("decode status payload: %v", err)
	}
	for _, key := range []string{"child_id", "status", "last_event_at", "events", "duration_sec"} {
		if _, ok := fields[key]; !ok {
			t.Fatalf("status payload missing %q: %s", key, body)
		}
	}
	if len(fields) != 5 {
		t.Fatalf("status payload exposed fields outside the bounded contract: %s", body)
	}
}

func TestSwarmWorkerStatusVocabulary(t *testing.T) {
	now := time.Date(2026, 8, 29, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		ev   agent.Event
		idle time.Duration
		want string
	}{
		{name: "ok marker", ev: agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"swarm_child_status": "ok"}}}, want: "ok"},
		{name: "failed marker", ev: agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"swarm_child_status": "failed"}}}, want: "failed"},
		{name: "needs marker", ev: agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"swarm_child_status": "needs_user_input"}}}, want: "needs_user_input"},
		{name: "running marker", ev: agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"swarm_child_status": "running"}}}, want: "running"},
		{name: "stalled marker", ev: agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"swarm_child_status": "stalled"}}}, want: "stalled"},
		{name: "dead letter marker", ev: agent.Event{Actions: agent.Actions{StateDelta: map[string]any{"swarm_child_status": "dead_letter"}}}, want: "dead_letter"},
		{name: "awaiting input", ev: agent.Event{Actions: agent.Actions{AwaitingInput: &agent.AwaitingInput{Question: "continue?"}}}, want: "needs_user_input"},
		{name: "fresh", ev: agent.Event{Timestamp: time.Now().UTC()}, idle: time.Second, want: "running"},
		{name: "idle", ev: agent.Event{Timestamp: now.Add(-2 * time.Second)}, idle: time.Second, want: "stalled"},
		{name: "idle disabled", ev: agent.Event{Timestamp: now.Add(-2 * time.Second)}, want: "running"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := newFakeSwarmEventReader(map[string][]byte{"w1": swarmEventLine(t, tc.ev)})
			srv := newSwarmWorkerEventsServer(t, newOwnerConvStore(goodID, localIdentityID), reader, tc.idle)
			resp, scanner := openSwarmStatusStream(t, srv)
			defer resp.Body.Close()
			if got := readSwarmStatusFrame(t, scanner).Status; got != tc.want {
				t.Fatalf("status = %q, want %q", got, tc.want)
			}
		})
	}
}

type swarmStatusFrame struct {
	ChildID     string `json:"child_id"`
	Status      string `json:"status"`
	LastEventAt string `json:"last_event_at"`
	Events      int    `json:"events"`
	DurationSec int64  `json:"duration_sec"`
}

func openSwarmStatusStream(t *testing.T, srv *httptest.Server) (*http.Response, *bufio.Scanner) {
	t.Helper()
	resp, err := http.Get(srv.URL + swarmWorkerEventsPath(goodID, ""))
	if err != nil {
		t.Fatalf("GET status stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		t.Fatalf("status/content-type/body = %d/%q/%q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	return resp, bufio.NewScanner(resp.Body)
}

func readSwarmStatusFrame(t *testing.T, scanner *bufio.Scanner) swarmStatusFrame {
	t.Helper()
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var envelope struct {
			Name  string           `json:"name"`
			Value swarmStatusFrame `json:"value"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &envelope); err != nil {
			t.Fatalf("decode CUSTOM event: %v", err)
		}
		if envelope.Name == "aura.swarm.worker" {
			return envelope.Value
		}
	}
	t.Fatalf("status stream ended before a worker event: %v", scanner.Err())
	return swarmStatusFrame{}
}
