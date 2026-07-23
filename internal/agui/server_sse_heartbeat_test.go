package agui

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// server_sse_heartbeat_test.go covers the idle SSE-comment heartbeat (fix-plan 1.3 Tier
// A): the config-seconds→Duration mapping (<=0 disables), the drain loop's ticker
// branch emitting ":hb\n\n" on a quiet stream, and the wire-shape guarantee that a
// heartbeat can never land inside an event frame.

// newHeartbeatTestServer mirrors newTestServerCfg but exposes the *Server before the
// httptest listener starts so a test can override heartbeatInterval directly — the same
// seam pattern governance_api_test.go's govServer uses for probeTimeout.
// ServerConfig.SSEHeartbeatSec only resolves to whole-second granularity
// (AURA_AGUI_SSE_HEARTBEAT_SEC is seconds); the direct field override lets these tests
// run on a millisecond clock without a multi-second test.
func newHeartbeatTestServer(t *testing.T, run Runner, conv ConversationStore, interval time.Duration) *httptest.Server {
	t.Helper()
	s := NewServer(run, conv, ServerConfig{})
	s.idgen = &fixedIDGen{}
	s.heartbeatInterval = interval
	srv := httptest.NewServer(s.Mux())
	t.Cleanup(srv.Close)
	return srv
}

// assertNoSplitFrames proves the drain loop's mutual-exclusion invariant on the wire: an
// "event: " line is ALWAYS immediately followed by its own frame's "id: "/"data: " lines
// (createSSEFrame, sse/writer.go — "id: " is optional, present only when the event
// carries a Timestamp), never by a ":hb" heartbeat comment landing in between — by
// construction the heartbeat write and WriteEventWithType are mutually-exclusive select
// cases in the same goroutine, so a heartbeat can only ever land between two complete
// frames, not inside one.
func assertNoSplitFrames(t *testing.T, body string) {
	t.Helper()
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "event: ") {
			continue
		}
		next := i + 1
		if next < len(lines) && strings.HasPrefix(lines[next], "id: ") {
			next++
		}
		if next >= len(lines) || !strings.HasPrefix(lines[next], "data: ") {
			got := ""
			if next < len(lines) {
				got = lines[next]
			}
			t.Fatalf("event line %q not immediately followed by its id:/data: lines (got %q) — a heartbeat split the frame", line, got)
		}
	}
}

// TestHeartbeatIntervalFromConfig locks the seconds→Duration mapping: <=0 disables (zero
// Duration, no ticker allocated), a positive value converts verbatim.
func TestHeartbeatIntervalFromConfig(t *testing.T) {
	cases := []struct {
		sec  int
		want time.Duration
	}{
		{sec: 0, want: 0},
		{sec: -1, want: 0},
		{sec: 15, want: 15 * time.Second},
		{sec: 5, want: 5 * time.Second},
	}
	for _, c := range cases {
		if got := heartbeatIntervalFromConfig(c.sec); got != c.want {
			t.Errorf("heartbeatIntervalFromConfig(%d) = %v, want %v", c.sec, got, c.want)
		}
	}
}

// TestNewServer_HeartbeatIntervalFromConfig proves NewServer wires ServerConfig.
// SSEHeartbeatSec through to the resolved field (the AGUIBufferCap end-to-end pattern).
func TestNewServer_HeartbeatIntervalFromConfig(t *testing.T) {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{SSEHeartbeatSec: 7})
	if s.heartbeatInterval != 7*time.Second {
		t.Errorf("heartbeatInterval = %v, want 7s", s.heartbeatInterval)
	}
	s2 := NewServer(&scriptedRunner{}, nil, ServerConfig{SSEHeartbeatSec: 0})
	if s2.heartbeatInterval != 0 {
		t.Errorf("heartbeatInterval(0) = %v, want 0 (disabled)", s2.heartbeatInterval)
	}
}

// TestStreamSSE_Heartbeat drives a real turn through a slow scriptedRunner (delay
// between yielded events) so the drain loop sits idle long enough for the ticker to fire.
func TestStreamSSE_Heartbeat(t *testing.T) {
	const tid = "11111111-1111-1111-1111-111111111111"

	t.Run("emitted and interleaves without splitting frames", func(t *testing.T) {
		runner := &scriptedRunner{events: textTurn("hi"), delay: 15 * time.Millisecond}
		srv := newHeartbeatTestServer(t, runner, &fakeConvStore{known: map[string]bool{tid: true}}, 5*time.Millisecond)

		resp := postRun(t, srv, runPayload(tid))
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body := string(raw)
		if !strings.Contains(body, ":hb\n\n") {
			t.Fatalf("no heartbeat comment in body:\n%s", body)
		}
		got, data := sseFrames(t, body)
		want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("frame sequence with heartbeats interleaved = %v, want %v", got, want)
		}
		if !strings.Contains(data, "hi") {
			t.Errorf("TEXT_MESSAGE_CONTENT delta missing from payload: %q", data)
		}
		assertNoSplitFrames(t, body)
	})

	t.Run("disabled emits none", func(t *testing.T) {
		runner := &scriptedRunner{events: textTurn("hi"), delay: 15 * time.Millisecond}
		srv := newHeartbeatTestServer(t, runner, &fakeConvStore{known: map[string]bool{tid: true}}, 0)

		resp := postRun(t, srv, runPayload(tid))
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		body := string(raw)
		if strings.Contains(body, ":hb") {
			t.Fatalf("heartbeat disabled (interval=0) but body contains :hb:\n%s", body)
		}
		got, _ := sseFrames(t, body)
		want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("frame sequence = %v, want %v", got, want)
		}
	})
}
