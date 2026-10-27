package agui

import (
	"context"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
)

// startShutdownServer serves s with BeginShutdown registered the way the daemon registers it.
func startShutdownServer(t *testing.T, s *Server) *httptest.Server {
	t.Helper()
	ts := httptest.NewUnstartedServer(s.Mux())
	ts.Config.RegisterOnShutdown(s.BeginShutdown)
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}

// openStream opens an SSE stream and fails unless the server answered with one.
func openStream(t *testing.T, ts *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("%s: status=%d type=%q", path, resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	return resp
}

// shutdown runs Shutdown under a deadline that only a stream which never ends exhausts.
func shutdown(ts *httptest.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ts.Config.Shutdown(ctx)
}

// TestShutdownEndsObserverStreams pins the observer half of finding 6 of the
// gateway-clarity spec: a stream with nothing to say held http.Server.Shutdown for its
// whole timeout, because only its client leaving could end it.
func TestShutdownEndsObserverStreams(t *testing.T) {
	cases := []struct {
		name   string
		server func(t *testing.T) *Server
		path   string
	}{
		{
			name: "browser live view",
			server: func(*testing.T) *Server {
				s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
				s.SetBrowserRelay(&echoRelay{})
				return s
			},
			path: "/api/browser/sessions/s1/stream",
		},
		{
			name: "swarm worker statuses",
			server: func(*testing.T) *Server {
				store := newOwnerConvStore(goodID, localIdentityID)
				s := NewServer(&scriptedRunner{conv: store}, store, ServerConfig{})
				s.SetSwarmTranscripts(newFakeSwarmEventReader(map[string][]byte{}))
				return s
			},
			path: swarmWorkerEventsPath(goodID, ""),
		},
		{
			name: "swarm worker transcript",
			server: func(t *testing.T) *Server {
				store := newOwnerConvStore(goodID, localIdentityID)
				s := NewServer(&scriptedRunner{conv: store}, store, ServerConfig{})
				s.SetSwarmTranscripts(newFakeSwarmEventReader(map[string][]byte{"w1": swarmTextLine(t, "working")}))
				return s
			},
			path: swarmWorkerEventsPath(goodID, "w1"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := startShutdownServer(t, tc.server(t))
			resp := openStream(t, ts, http.MethodGet, tc.path, "")
			if err := shutdown(ts); err != nil {
				t.Fatalf("Shutdown with an observer stream open: %v", err)
			}
			if _, err := io.Copy(io.Discard, resp.Body); err != nil {
				t.Fatalf("the stream did not end cleanly: %v", err)
			}
		})
	}
}

// cancelledTurnRunner's turn yields nothing until its context ends: a model still thinking
// when the daemon is told to stop. Then it does what the runner does on a cancel. It waits
// until the test has seen the observer stream end, spends a while on bookkeeping that runs
// on a context which outlives the turn's (the end rows, an operation claim), returns the
// cancellation as the turn's error, and closes ended.
type cancelledTurnRunner struct {
	scriptedRunner
	proceed <-chan struct{}
	ended   chan struct{}
}

func (r *cancelledTurnRunner) Turn(ctx context.Context, _ string, _ *string) iter.Seq2[*agent.Event, error] {
	return r.turn(ctx)
}

func (r *cancelledTurnRunner) TurnBranch(ctx context.Context, _ string, _ int) iter.Seq2[*agent.Event, error] {
	return r.turn(ctx)
}

func (r *cancelledTurnRunner) turn(ctx context.Context) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		defer close(r.ended)
		<-ctx.Done()
		<-r.proceed
		time.Sleep(200 * time.Millisecond)
		yield(nil, ctx.Err())
	}
}

// TestShutdownCancelsATurnAndWaitsForIt pins the turn half of finding 6, as amended after
// validation: the pool closes after the drain, so a turn's stream must not end before the
// turn has written its records. Shutdown cancels the turn, the stream carries its terminal
// frame, and Shutdown returns only after the turn has finished. An observer on the same
// server ends without waiting for it.
func TestShutdownCancelsATurnAndWaitsForIt(t *testing.T) {
	cases := []struct {
		name, method, path, body string
	}{
		{"request-scoped run", http.MethodPost, "/agent/run", runPayload(goodID)},
		{"branch rerun", http.MethodPost, "/api/conversations/" + goodID + "/edit", `{"diverge_seq":5,"content":"edited"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observed := make(chan struct{})
			var once sync.Once
			release := func() { once.Do(func() { close(observed) }) }
			defer release()
			run := &cancelledTurnRunner{proceed: observed, ended: make(chan struct{})}
			store := &fakeConvStore{known: map[string]bool{goodID: true}}
			s := NewServer(run, store, ServerConfig{})
			s.SetBrowserRelay(&echoRelay{})
			ts := startShutdownServer(t, s)
			turn := openStream(t, ts, tc.method, tc.path, tc.body)
			observer := openStream(t, ts, http.MethodGet, "/api/browser/sessions/s1/stream", "")

			shut := make(chan error, 1)
			go func() { shut <- shutdown(ts) }()
			observerEnded := make(chan error, 1)
			go func() {
				_, err := io.Copy(io.Discard, observer.Body)
				observerEnded <- err
			}()
			select {
			case err := <-observerEnded:
				if err != nil {
					t.Fatalf("the observer stream did not end cleanly: %v", err)
				}
			case err := <-shut:
				t.Fatalf("Shutdown returned (%v) while the observer stream was still open", err)
			}
			release()

			if err := <-shut; err != nil {
				t.Fatalf("Shutdown with a turn streaming: %v", err)
			}
			select {
			case <-run.ended:
			default:
				t.Fatal("Shutdown returned before the cancelled turn had finished")
			}
			body, err := io.ReadAll(turn.Body)
			if err != nil {
				t.Fatalf("read the turn's stream: %v", err)
			}
			if types, _ := sseFrames(t, string(body)); len(types) == 0 || types[len(types)-1] != "RUN_ERROR" {
				t.Fatalf("the turn's stream ended with %v, want its terminal RUN_ERROR", types)
			}
		})
	}
}

// TestShutdownSignalIsNilSafe: a Server literal, as many tests build, has no signal. Its
// streams then end only with their request, and BeginShutdown does nothing.
func TestShutdownSignalIsNilSafe(t *testing.T) {
	s := &Server{}
	s.BeginShutdown()
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := s.untilShutdown(parent)
	defer cancel()
	if ctx.Err() != nil {
		t.Fatalf("a Server literal ended the stream at once: %v", ctx.Err())
	}
	cancelParent()
	<-ctx.Done()
}
