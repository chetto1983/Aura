package agui

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// echoRelay stands in for the in-box relay: it announces itself, then echoes every input line
// it receives on stdin back out, the way a browser answers a click with a new frame.
type echoRelay struct {
	openErr  error
	mu       sync.Mutex
	sessions []string
}

type echoHandle struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

func (h *echoHandle) Kill()              { h.once.Do(func() { close(h.stop) }) }
func (h *echoHandle) Wait() (int, error) { <-h.done; return 0, nil }

func (e *echoRelay) Open(ctx context.Context, session string, in io.ReadCloser, out io.Writer) (BrowserRelayHandle, error) {
	if e.openErr != nil {
		return nil, e.openErr
	}
	e.mu.Lock()
	e.sessions = append(e.sessions, scopedIdentityID(ctx)+"/"+session)
	e.mu.Unlock()
	h := &echoHandle{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(h.done)
		_, _ = io.WriteString(out, `{"type":"status","connected":true}`+"\n")
		read := make(chan string)
		go func() {
			defer close(read)
			sc := bufio.NewScanner(in)
			for sc.Scan() {
				read <- sc.Text()
			}
		}()
		for {
			select {
			case <-h.stop:
				_ = in.Close()
				for range read {
				}
				return
			case line, ok := <-read:
				if !ok {
					return
				}
				_, _ = io.WriteString(out, `{"type":"echo","got":`+line+"}\n")
			}
		}
	}()
	return h, nil
}

// browserTestServer serves the agui mux with the identity taken from a test header, the way
// RequireAuth plants the session's principal in production.
func browserTestServer(t *testing.T, relay BrowserRelay) *httptest.Server {
	t.Helper()
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	if relay != nil {
		s.SetBrowserRelay(relay)
	}
	mux := s.Mux()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, withPrincipal(r, r.Header.Get("X-Test-Identity")))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type sseReader struct {
	resp  *http.Response
	lines chan string
}

func openBrowserStream(t *testing.T, srv *httptest.Server, identity, session string) *sseReader {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/browser/sessions/"+session+"/stream", nil)
	req.Header.Set("X-Test-Identity", identity)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream status=%d type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	r := &sseReader{resp: resp, lines: make(chan string, 16)}
	go func() {
		defer close(r.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if data, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				r.lines <- data
			}
		}
	}()
	return r
}

func (r *sseReader) next(t *testing.T) string {
	t.Helper()
	select {
	case line, ok := <-r.lines:
		if !ok {
			t.Fatal("stream ended")
		}
		return line
	case <-time.After(5 * time.Second):
		t.Fatal("no SSE data within 5s")
	}
	return ""
}

func (r *sseReader) close() {
	_ = r.resp.Body.Close()
	for range r.lines {
	}
}

func postBrowserInput(t *testing.T, srv *httptest.Server, identity, session, contentType, body string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/browser/sessions/"+session+"/input", strings.NewReader(body))
	req.Header.Set("X-Test-Identity", identity)
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post input: %v", err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

const keyEvent = `{"type":"input_keyboard","eventType":"keyDown","key":"a","text":"a"}`

func TestBrowserLiveRelaysFramesAndInput(t *testing.T) {
	srv := browserTestServer(t, &echoRelay{})
	stream := openBrowserStream(t, srv, "alice", "login")
	defer stream.close()

	if got := stream.next(t); got != `{"type":"status","connected":true}` {
		t.Fatalf("first frame = %s, want the relay's status line", got)
	}
	if code := postBrowserInput(t, srv, "alice", "login", "application/json", keyEvent); code != http.StatusNoContent {
		t.Fatalf("input status = %d, want 204", code)
	}
	if got := stream.next(t); !strings.Contains(got, `"got":{"eventType":"keyDown"`) {
		t.Fatalf("echo = %s, want the key event re-encoded and relayed to the box", got)
	}
}

func TestBrowserLiveInputIsRefusedUnlessItIsTheCallersOwnViewer(t *testing.T) {
	srv := browserTestServer(t, &echoRelay{})
	stream := openBrowserStream(t, srv, "alice", "login")
	defer stream.close()
	stream.next(t)

	for name, tc := range map[string]struct {
		identity, session, contentType, body string
		want                                 int
	}{
		"another identity cannot type into alice's browser": {"mallory", "login", "application/json", keyEvent, http.StatusConflict},
		"no viewer on that session":                         {"alice", "other", "application/json", keyEvent, http.StatusConflict},
		"a non-input command never reaches the box":         {"alice", "login", "application/json", `{"type":"navigate","url":"https://evil.test"}`, http.StatusBadRequest},
		"a form post is refused":                            {"alice", "login", "application/x-www-form-urlencoded", keyEvent, http.StatusUnsupportedMediaType},
		"malformed json":                                    {"alice", "login", "application/json", `{"type":`, http.StatusBadRequest},
		"a traversal-shaped session":                        {"alice", "..%2Fx", "application/json", keyEvent, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			if got := postBrowserInput(t, srv, tc.identity, tc.session, tc.contentType, tc.body); got != tc.want {
				t.Fatalf("status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestBrowserLiveSecondViewerTakesOver(t *testing.T) {
	srv := browserTestServer(t, &echoRelay{})
	first := openBrowserStream(t, srv, "alice", "login")
	defer first.close()
	first.next(t)
	second := openBrowserStream(t, srv, "alice", "login")
	defer second.close()
	second.next(t)

	select {
	case _, ok := <-first.lines:
		if ok {
			t.Fatal("the first viewer kept receiving after a second one took the session")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the first viewer's stream never ended")
	}
	if code := postBrowserInput(t, srv, "alice", "login", "application/json", keyEvent); code != http.StatusNoContent {
		t.Fatalf("input to the new viewer = %d, want 204", code)
	}
	if got := second.next(t); !strings.Contains(got, `"echo"`) {
		t.Fatalf("second viewer got %s, want the echo", got)
	}
}

func TestBrowserLiveRefusesWhatItCannotServe(t *testing.T) {
	get := func(srv *httptest.Server, session string) int {
		resp, err := http.Get(srv.URL + "/api/browser/sessions/" + session + "/stream")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if got := get(browserTestServer(t, nil), "login"); got != http.StatusServiceUnavailable {
		t.Fatalf("no relay wired: status = %d, want 503", got)
	}
	if got := get(browserTestServer(t, &echoRelay{openErr: errors.New("box down")}), "login"); got != http.StatusServiceUnavailable {
		t.Fatalf("relay cannot open: status = %d, want 503", got)
	}
	if got := get(browserTestServer(t, &echoRelay{}), strings.Repeat("x", 49)); got != http.StatusNotFound {
		t.Fatalf("overlong session: status = %d, want 404", got)
	}
}

func TestBrowserLineSinkKeepsTheViewerLive(t *testing.T) {
	out := make(chan []byte, 1)
	sink := &browserLineSink{ctx: context.Background(), out: out}
	if _, err := io.WriteString(sink, `{"type":"frame","seq":1}`+"\n"+`{"type":"fr`); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := io.WriteString(sink, `ame","seq":2}`+"\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := string(<-out); got != `{"type":"frame","seq":1}` {
		t.Fatalf("first line = %s", got)
	}
	select {
	case extra := <-out:
		t.Fatalf("a frame arriving while the viewer was behind was queued instead of dropped: %s", extra)
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	blocked := &browserLineSink{ctx: ctx, out: make(chan []byte)}
	cancel()
	if _, err := io.WriteString(blocked, `{"type":"relay_error","reason":"no_such_session"}`+"\n"); !errors.Is(err, context.Canceled) {
		t.Fatalf("relay_error with no room: err = %v, want it to wait and give up only on ctx", err)
	}

	big := &browserLineSink{ctx: context.Background(), out: out}
	_, _ = big.Write(make([]byte, browserLineMax+1))
	if len(big.pending) != 0 {
		t.Fatalf("an unterminated line past %d bytes is kept (%d bytes)", browserLineMax, len(big.pending))
	}
}
