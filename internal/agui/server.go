package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/google/uuid"
)

// maxRunBodyBytes caps the POST /agent/run request body (Tampering/DoS guard,
// T-12-12). The RunAgentInput payload is small (a thread id + a short message
// history); 1 MiB is generous headroom while bounding a hostile body before it
// reaches the SDK decoder.
const maxRunBodyBytes = 1 << 20

// ServerConfig carries the AG-UI server knobs the daemon resolves from config
// (AURA_AGUI_*). CORSPermissive gates the `Access-Control-Allow-Origin: *` header
// (default restrictive, T-12-13); BufferCap is the cap of the per-connection SSE
// pump channel (drop-on-full, never block the Loop, T-12-09). A non-positive
// BufferCap falls back to the fanout default.
type ServerConfig struct {
	CORSPermissive bool
	BufferCap      int
}

// Runner is the narrow agent-driver surface the server consumes (D-A2-02; *runner.Runner
// satisfies it implicitly). Turn drives one round over a thread and yields the agent
// Event stream; SubmitAnswers resolves protocol-native HITL resume entries (resolved→accept,
// cancelled→cancel) before the Turn. Declared consumer-side so the server depends only on
// the two methods it calls, not the whole Runner — and so unit tests pass scripted fakes.
type Runner interface {
	Turn(ctx context.Context, convID string, userMsg *string) iter.Seq2[*agent.Event, error]
	SubmitAnswers(ctx context.Context, answers map[string]runner.ResponseInput) (int, error)
}

// Server is the minimal AG-UI HTTP gateway (Slice 8b): POST /agent/run streams a
// translated agent turn as SSE, GET /threads/{id}/messages returns the persisted
// history as a MESSAGES_SNAPSHOT JSON body. It is the thinnest glue over EXISTING
// seams — the narrow Runner + ConversationStore, the translator, and the SDK SSE
// writer. The bind is hardcoded loopback by the daemon (auth deferred this phase,
// amendment #35); the loopback bind IS the compensating control (T-12-08).
type Server struct {
	run   Runner
	conv  ConversationStore
	idgen IDGenerator
	cfg   ServerConfig
}

// NewServer builds the gateway over the supplied driver + store + config. The
// IDGenerator is the default uuid-v4 minter; tests inject a deterministic one via
// the exported field for stable frame ids.
func NewServer(run Runner, conv ConversationStore, cfg ServerConfig) *Server {
	return &Server{run: run, conv: conv, idgen: NewIDGenerator(), cfg: cfg}
}

// Mux registers the two routes using Go 1.22+ method-pattern routing (no chi/gorilla
// — matches the no-router codebase posture).
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agent/run", s.handleRun)
	mux.HandleFunc("GET /threads/{id}/messages", s.handleMessages)
	return mux
}

// handleRun parses RunAgentInput, resolves the thread (404), applies any protocol-native
// resume entries, drives Runner.Turn over the last user message, and streams the translated
// AG-UI events as SSE. The body is size-capped (T-12-12); a malformed/empty payload is a 400.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var in types.RunAgentInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := ValidateRunInput(in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	// A syntactically-invalid thread id can never identify an existing conversation:
	// resolve it to 404 BEFORE the store round-trip so a malformed id (e.g. a non-UUID
	// "does-not-exist") returns a clean 404 instead of leaking the store's parse error
	// as a 500 (T-12-11 chokepoint; caught by the live agui smoke).
	if _, err := uuid.Parse(in.ThreadID); err != nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	if _, err := s.conv.Get(ctx, in.ThreadID); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}
	if len(in.Resume) > 0 {
		if _, err := s.run.SubmitAnswers(ctx, resumeAnswers(in.Resume)); err != nil {
			http.Error(w, sanitizeErr(err), http.StatusBadRequest)
			return
		}
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	if s.cfg.CORSPermissive {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}

	runID := "run-" + uuid.NewString()
	userMsg := lastUserMessage(in.Messages)
	turn := s.run.Turn(ctx, in.ThreadID, userMsg)
	s.streamSSE(ctx, w, Translate(in.ThreadID, runID, s.idgen, turn))
}

// handleMessages resolves the thread (404) and returns the persisted history as a
// MESSAGES_SNAPSHOT JSON body (one-shot read, NOT SSE — OQ2). Each persisted
// llm.Message is projected to an events.Message.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	// A malformed thread id (non-UUID) is definitionally not an existing thread —
	// 404 before the store round-trip rather than a 500 from the id parse failure
	// (T-12-11; the live smoke's `does-not-exist` chokepoint).
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	if _, err := s.conv.Get(ctx, id); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}
	hist, err := s.conv.LoadHistory(ctx, id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(events.NewMessagesSnapshotEvent(projectMessages(hist))); err != nil {
		slog.Warn("agui: encode messages snapshot", "err", err)
	}
}

// streamSSE pumps the translated event stream to the client over SSE through a
// cap-N buffered channel: the producer goroutine ranges the stream (the sole sender,
// drop+WARN on a full buffer so it never blocks the Loop, T-12-09) while the handler
// goroutine drains the channel onto the wire via the SDK writer. On client disconnect
// (ctx.Done) both unwind — the producer stops yielding, the channel closes, the drain
// loop returns (goleak-clean, Pitfall 4). A translated RUN_ERROR is sanitized at the
// translator boundary already; sanitizeErr is the belt-and-suspenders for the pump's
// own error frame.
func (s *Server) streamSSE(ctx context.Context, w http.ResponseWriter, stream iter.Seq2[events.Event, error]) {
	writer := sse.NewSSEWriter()
	out := make(chan events.Event, s.bufferCap())
	go func() {
		defer close(out)
		for ev, err := range stream {
			if err != nil {
				s.pumpSend(ctx, out, events.NewRunErrorEvent(sanitizeErr(err)))
				return
			}
			if !s.pumpSend(ctx, out, ev) {
				return
			}
		}
	}()

	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-out:
			if !ok {
				return
			}
			ev = redactEvent(ev)
			if err := writer.WriteEventWithType(ctx, w, ev, string(ev.Type())); err != nil {
				return // client gone — let the producer drain via ctx (Pitfall 4)
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// pumpSend delivers one event to the SSE channel without ever blocking the producer
// indefinitely: deliver if there is room, abort on ctx-cancel. A run-lifecycle frame
// (RUN_STARTED/RUN_FINISHED/RUN_ERROR) that cannot fit falls back to a blocking send
// (still abortable on ctx-cancel) so the terminal frame is never dropped — an AG-UI
// consumer waits on RUN_FINISHED, and silently dropping it is a protocol violation, not
// graceful degradation (WR-01). A non-lifecycle delta that cannot fit is DROPPED with a
// WARN (T-12-09: the Loop must never stall on a slow client). Returns false only on
// ctx-cancel so the producer unwinds and closes the channel.
func (s *Server) pumpSend(ctx context.Context, out chan events.Event, ev events.Event) bool {
	if isLifecycleFrame(ev.Type()) {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	default:
		slog.Warn("agui server: SSE client slow, dropping event", "type", ev.Type())
		return true
	}
}

// isLifecycleFrame reports whether an event is a run-lifecycle terminal/boundary frame
// (RUN_STARTED/RUN_FINISHED/RUN_ERROR). These are non-droppable: dropping the terminal
// RUN_FINISHED/RUN_ERROR hangs a consumer that waits for it; dropping RUN_STARTED breaks
// the protocol prologue. Shared by the SSE pump and the in-process fanout (WR-01).
func isLifecycleFrame(t events.EventType) bool {
	switch t {
	case events.EventTypeRunStarted, events.EventTypeRunFinished, events.EventTypeRunError:
		return true
	default:
		return false
	}
}

// bufferCap resolves the per-connection SSE channel cap, falling back to the fanout
// default when the config knob is non-positive.
func (s *Server) bufferCap() int {
	if s.cfg.BufferCap > 0 {
		return s.cfg.BufferCap
	}
	return fanoutBuffer
}

// resumeAnswers maps the AG-UI protocol-native Resume[] onto the Runner's three-action
// resume model: a resolved interrupt accepts (carrying any payload string as the answer),
// a cancelled interrupt cancels. The InterruptID is the pause token the Runner keys on.
func resumeAnswers(entries []types.ResumeEntry) map[string]runner.ResponseInput {
	out := make(map[string]runner.ResponseInput, len(entries))
	for _, e := range entries {
		action := askuser.ActionAccept
		if e.Status == types.ResumeStatusCancelled {
			action = askuser.ActionCancel
		}
		out[e.InterruptID] = runner.ResponseInput{Action: action, Content: payloadString(e.Payload)}
	}
	return out
}

// payloadString renders a resume payload as the answer content: a string payload is
// used verbatim; any other shape is JSON-encoded so structured answers survive. A nil
// payload yields the empty string.
func payloadString(payload any) string {
	switch v := payload.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// lastUserMessage extracts the final user message from the RunAgentInput history to
// drive the turn (OQ3). It returns nil when there is no user message (a resume-only
// run continues over the rehydrated history without a fresh user turn).
func lastUserMessage(msgs []types.Message) *string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if string(msgs[i].Role) != llm.RoleUser {
			continue
		}
		if content, ok := msgs[i].Content.(string); ok && content != "" {
			return &content
		}
	}
	return nil
}

// projectMessages projects the persisted llm.Message history onto the AG-UI
// events.Message shape for the MESSAGES_SNAPSHOT body. The id is a stable
// 1-based index; the role string is converted to the SDK Role type.
func projectMessages(hist []llm.Message) []events.Message {
	msgs := make([]events.Message, 0, len(hist))
	for i, m := range hist {
		msgs = append(msgs, events.Message{
			ID:         fmt.Sprintf("msg-%d", i+1),
			Role:       types.Role(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		})
	}
	return msgs
}

// secretPattern collapses a whole DB DSN (scheme + userinfo + host path) to a
// scheme + "[redacted]" marker — the password AND the host path both leak operational
// detail, so the entire connection string after the scheme is dropped (T-12-10 / V7).
var secretPattern = regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb|redis|amqp)://[^\s"']*`)

// urlUserinfoPattern matches `scheme://user:password@` for ANY URL scheme (an HTTP MCP
// server, webhook, or proxy URL — not just the five DB DSNs above). Only the userinfo is
// collapsed; the rest of the URL is left intact so the error stays diagnosable (WR-03).
// The DSN pass runs first and already consumes its schemes, so this never double-matches
// them.
var urlUserinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^/\s@]+@`)

// tokenPattern matches common credential tokens embedded in free-form error strings:
// `Bearer <token>`, `api_key=<...>`, `api-key=<...>`, `apikey=<...>`, `token=<...>`. The
// token body is collapsed, the prefix kept so the redaction is legible (WR-03).
var tokenPattern = regexp.MustCompile(`(?i)(bearer\s+|api[_-]?key=|token=)\S+`)

// sanitizeErr redacts credential-bearing substrings from an error string before it is
// surfaced over the wire (RUN_ERROR / 4xx body). The agent path already structurally
// redacts the OpenRouter key (D-28); this is the server-side belt-and-suspenders for the
// tool/infra error strings the translator forwards (T-12-10).
func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	return sanitizeString(err.Error())
}

// sanitizeString strips credential-bearing substrings from an arbitrary string: whole DB
// DSNs collapse to a scheme marker, generic URL userinfo collapses to `scheme://[redacted]@`,
// and bearer/api-key/token shapes collapse to a `prefix[redacted]` marker (WR-03). The DSN
// pass runs first so the generic userinfo pass never reaches the five DB schemes.
func sanitizeString(msg string) string {
	out := secretPattern.ReplaceAllStringFunc(msg, func(match string) string {
		scheme := match
		if idx := strings.Index(match, "://"); idx >= 0 {
			scheme = match[:idx]
		}
		return scheme + "://[redacted]"
	})
	out = urlUserinfoPattern.ReplaceAllString(out, "${1}[redacted]@")
	return tokenPattern.ReplaceAllStringFunc(out, func(match string) string {
		prefix := match
		if idx := strings.IndexAny(match, " =\t"); idx >= 0 {
			prefix = match[:idx+1]
		}
		return prefix + "[redacted]"
	})
}

// redactEvent is the server-side belt-and-suspenders for T-12-10: the pure translator
// forwards a runner error as a RUN_ERROR event carrying the raw err.Error() string. The
// server sanitizes that message in-flight (before it reaches the wire) so a tool/infra
// error embedding a DSN/key never leaks, without reaching into the boundary-tested
// translator. Non-RUN_ERROR events pass through unchanged.
func redactEvent(ev events.Event) events.Event {
	re, ok := ev.(*events.RunErrorEvent)
	if !ok {
		return ev
	}
	re.Message = sanitizeString(re.Message)
	return re
}
