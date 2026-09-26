package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"regexp"
	"sync"
	"time"
)

// browser_live.go serves the cockpit's live view of one agent-browser session in the caller's
// own box (prd.md §12, authenticated browsing). The viewport stream binds loopback inside the box
// netns and exec is Aura's only channel in, so a GET opens the image's relay over a stdin-capable
// exec and forwards its NDJSON lines as SSE, while a POST writes one viewer input event to that
// relay's stdin. The key is the caller's identity plus the session name, so a viewer can only
// ever reach its own box.

// BrowserRelayHandle is the running relay: Kill ends it, Wait blocks until it has ended.
type BrowserRelayHandle interface {
	Kill()
	Wait() (int, error)
}

// BrowserRelay opens the relay for session in the box of the identity ctx carries. in feeds the
// relay's stdin and is closed by the handle when the relay ends; out receives its stdout.
type BrowserRelay interface {
	Open(ctx context.Context, session string, in io.ReadCloser, out io.Writer) (BrowserRelayHandle, error)
}

var browserSessionPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

// browserInputTypes mirrors the relay's own allowlist (docker/aura-sandbox/browser-relay.mjs):
// only viewer input may reach the browser, and it is refused here before it reaches the box.
var browserInputTypes = map[string]bool{"input_mouse": true, "input_keyboard": true, "input_touch": true, "config": true}

const (
	browserInputMaxBytes = 16 << 10
	// browserLineMax bounds one relay line; a 1280x720 JPEG frame measured about 13 KB of base64,
	// so a line this long is not a frame and is dropped rather than buffered.
	browserLineMax = 4 << 20
	// browserLineBuffer frames may queue for a slow viewer; beyond it the newest frames are
	// dropped, because a live view wants the latest image, not every image.
	browserLineBuffer = 16
)

type browserViewers struct {
	mu sync.Mutex
	m  map[string]*browserViewer
}

type browserViewer struct {
	mu sync.Mutex // one input line at a time, never interleaved
	in *io.PipeWriter
}

func (s *Server) registerBrowserLiveRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/browser/sessions/{session}/stream", s.handleBrowserStream)
	mux.HandleFunc("POST /api/browser/sessions/{session}/input", s.handleBrowserInput)
}

func (s *Server) handleBrowserStream(w http.ResponseWriter, r *http.Request) {
	session := r.PathValue("session")
	if !browserSessionPattern.MatchString(session) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "invalid_session"})
		return
	}
	if s.browserRelay == nil {
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "browser_unavailable"})
		return
	}
	ctx := r.Context()
	key := scopedIdentityID(ctx) + "\x00" + session
	lines := make(chan []byte, browserLineBuffer)
	pr, pw := io.Pipe()
	h, err := s.browserRelay.Open(ctx, session, pr, &browserLineSink{ctx: ctx, out: lines})
	if err != nil {
		_ = pw.Close()
		writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "sandbox_unavailable"})
		return
	}
	ended := make(chan struct{})
	go func() { _, _ = h.Wait(); close(ended) }()
	viewer := s.browserViewers.claim(key, pw)
	defer func() {
		s.browserViewers.release(key, viewer)
		h.Kill()
		_ = pw.Close()
		<-ended
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	var heartbeatC <-chan time.Time
	if s.heartbeatInterval > 0 {
		ticker := time.NewTicker(s.heartbeatInterval)
		defer ticker.Stop()
		heartbeatC = ticker.C
	}
	for {
		var frame []byte
		select {
		case <-ctx.Done():
			return
		case <-ended:
			return
		case <-heartbeatC:
			frame = sseHeartbeatComment
		case line := <-lines:
			frame = append(append([]byte("data: "), line...), '\n', '\n')
		}
		if _, err := w.Write(frame); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (s *Server) handleBrowserInput(w http.ResponseWriter, r *http.Request) {
	session := r.PathValue("session")
	if !browserSessionPattern.MatchString(session) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "invalid_session"})
		return
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		writeJSONStatus(w, http.StatusUnsupportedMediaType, map[string]string{"error": "json_required"})
		return
	}
	var event map[string]any
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, browserInputMaxBytes)).Decode(&event); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_event"})
		return
	}
	if kind, _ := event["type"].(string); !browserInputTypes[kind] {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "input_type_not_allowed"})
		return
	}
	line, err := json.Marshal(event)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid_event"})
		return
	}
	viewer := s.browserViewers.get(scopedIdentityID(r.Context()) + "\x00" + session)
	if viewer == nil || viewer.send(append(line, '\n')) != nil {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "no_live_view"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// claim makes pw the session's input target. A second viewer replaces the first: closing the
// old pipe ends the old relay's input, so there is never more than one hand on the browser.
func (v *browserViewers) claim(key string, pw *io.PipeWriter) *browserViewer {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.m == nil {
		v.m = map[string]*browserViewer{}
	}
	if old := v.m[key]; old != nil {
		_ = old.in.Close()
	}
	viewer := &browserViewer{in: pw}
	v.m[key] = viewer
	return viewer
}

// release forgets viewer unless a newer one has already taken the session.
func (v *browserViewers) release(key string, viewer *browserViewer) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.m[key] == viewer {
		delete(v.m, key)
	}
}

func (v *browserViewers) get(key string) *browserViewer {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.m[key]
}

func (b *browserViewer) send(line []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, err := b.in.Write(line)
	return err
}

// browserLineSink splits the relay's stdout into lines for the SSE loop. It runs on the exec
// pump goroutine: frames are dropped when the viewer is behind, and a relay_error line, which is
// the viewer's only explanation for a stream that ends, always waits for room.
type browserLineSink struct {
	ctx     context.Context
	out     chan<- []byte
	pending []byte
}

func (b *browserLineSink) Write(p []byte) (int, error) {
	b.pending = append(b.pending, p...)
	for {
		i := bytes.IndexByte(b.pending, '\n')
		if i < 0 {
			break
		}
		line := bytes.Clone(b.pending[:i])
		b.pending = b.pending[i+1:]
		if len(line) == 0 {
			continue
		}
		if !bytes.Contains(line, []byte(`"relay_error"`)) {
			select {
			case b.out <- line:
			default:
			}
			continue
		}
		select {
		case b.out <- line:
		case <-b.ctx.Done():
			return len(p), b.ctx.Err()
		}
	}
	if len(b.pending) > browserLineMax {
		b.pending = nil
	}
	return len(p), nil
}
