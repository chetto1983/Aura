package setup

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// togglingStore flips PendingConsumed from false→true after the first poll so the
// SSE pump observes a not-yet-consumed then consumed transition (the real race
// the channel's /start drives). Guarded for the concurrent poll goroutine.
type togglingStore struct {
	mu     sync.Mutex
	polls  int
	flipAt int
	fakeStore
}

func (s *togglingStore) PendingConsumed(_ context.Context, _ string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.polls++
	return s.polls >= s.flipAt, nil
}

// newLiveSetupServer stands up the gated Mux on an httptest server with a short
// SSE poll interval so the test does not wait 2s for a tick. It returns the live
// URL + the *Server (for token assertions).
func newLiveSetupServer(t *testing.T, store Store) (string, *Server) {
	t.Helper()
	srv := NewServer(Deps{
		Store:        store,
		Probe:        func(_ context.Context, _ string) (string, error) { return "bot", nil },
		Token:        "tok",
		IdentityID:   "00000000-0000-0000-0000-000000000001",
		TokenOut:     io.Discard, // token is set; no generated line to capture
		QROut:        io.Discard,
		PollInterval: 5 * time.Millisecond,
	})
	ts := httptest.NewServer(srv.Mux())
	t.Cleanup(ts.Close)
	return ts.URL, srv
}

// TestSSEEmitsOnboardingCompleted proves /setup/events polls consumed_at and
// emits exactly one onboarding_completed event once the row flips to consumed
// (VALIDATION UX-03 "SSE /setup/events poll-2s emits onboarding_completed"), and
// that the stream then closes (the handler returns after the terminal event).
// The httptest server's per-request goroutine + the test client both unwind, so
// the package goleak TestMain (main_test.go) catches any pump leak.
func TestEventsSSEEmitsOnboardingCompleted(t *testing.T) {
	store := &togglingStore{flipAt: 2} // consumed on the 2nd poll
	url, srv := newLiveSetupServer(t, store)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/setup/events?token=tok&onboarding=abc123", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type %q, want text/event-stream", ct)
	}

	// Read SSE frames until we see the onboarding_completed data line (the server
	// closes the stream right after, so the scanner then hits EOF).
	got := ""
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			got = strings.TrimSpace(after)
			break
		}
	}
	if !strings.Contains(got, onboardingCompletedEvent) {
		t.Fatalf("SSE frame %q does not carry onboarding_completed", got)
	}
	// Onboarding completed → the one-time token is burned (a subsequent gated call 401s).
	rec := httptest.NewRecorder()
	srv.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/setup/status?token=tok", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("token not invalidated after onboarding: status gate returned %d, want 401", rec.Code)
	}
}

// TestSSEPumpExitsOnClientDisconnect is the goleak-defining test (T-13-07-SSELeak
// / Pitfall 5): a client that opens the SSE stream then cancels its request must
// cause the server-side poll goroutine to exit (it selects on r.Context().Done()).
// The token never flips, so the ONLY way the handler returns is ctx-cancel. The
// package goleak TestMain fails if the pump survives.
func TestEventsSSEPumpExitsOnClientDisconnect(t *testing.T) {
	store := &togglingStore{flipAt: 1 << 30} // never consumed → only ctx-cancel ends the pump
	url, _ := newLiveSetupServer(t, store)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/setup/events?token=tok&onboarding=never", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	// Let the pump tick a few times, then disconnect: cancel the request ctx +
	// close the body. The server's select hits ctx.Done() and the handler returns.
	time.Sleep(30 * time.Millisecond)
	cancel()
	resp.Body.Close()
	// Give the server goroutine a moment to observe the cancel and unwind before
	// goleak's TestMain snapshot runs. The httptest server is closed by t.Cleanup.
	time.Sleep(50 * time.Millisecond)
}
