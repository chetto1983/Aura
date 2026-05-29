package agent

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// Event is the single type every runtime- and LLM-emitted signal flows through.
// The shape is full / forward-compat so the Phase-12 AG-UI gateway is a fan-out
// adapter, not a refactor (D-17): MessageID/ThreadID/ToolCallID are present now
// even though nothing consumes them yet.
//
// Trace IDs are OTel/W3C-correct widths (D-16): RequestID is a 16-byte UUIDv7
// (TraceID/run_id), SpanID is 8 random bytes, ParentSpanID is nil at the root.
// Storing a 16-byte UUID in the SpanID slot would force lossy truncation when a
// future OTel slice maps these — 8 bytes makes that mapping drop-in.
type Event struct {
	RequestID    uuid.UUID    `json:"request_id"`              // UUIDv7 TraceID/run_id, shared tree-wide
	SpanID       [8]byte      `json:"span_id"`                 // 8-byte OTel SpanID, per-node
	ParentSpanID *[8]byte     `json:"parent_span_id,omitempty"` // nil at root
	Author       string       `json:"author"`                  // workflow agent name, "user", or LLM agent name (D-14)
	Branch       string       `json:"branch,omitempty"`        // hierarchical label only (D-15)
	ThreadID     string       `json:"thread_id,omitempty"`     // AG-UI conversation thread (Phase 4 / Slice 1.8), forward-compat
	MessageID    uuid.UUID    `json:"message_id,omitempty"`    // AG-UI message correlation (UUIDv7), forward-compat
	LLMResponse  *LLMResponse `json:"llm_response,omitempty"`  // nil when this Event is not an LLM turn
	Actions      Actions      `json:"actions"`                 // control signals (escalate, state/artifact deltas)
	Timestamp    time.Time    `json:"timestamp"`               // emit time, serialized RFC3339Nano UTC
}

// LLMResponse carries the model output for an Event that IS an LLM turn. It is
// referenced via a pointer on Event so non-LLM events omit it entirely.
type LLMResponse struct {
	Content      string         `json:"content,omitempty"`      // assistant text
	ToolCalls    []llm.ToolCall `json:"tool_calls,omitempty"`   // reuses llm.ToolCall (D-17); its ID is the AG-UI tool_call_id
	FinishReason string         `json:"finish_reason,omitempty"` // provider finish reason on the final turn
}

// Actions are the control signals an Event carries. Escalate is the canonical
// termination/cancellation signal (D-04: budget exhaustion is Event-only, never
// the error slot). StateDelta/ArtifactDelta accept nested map[string]any and map
// to the AG-UI STATE_DELTA stream in Phase 12.
type Actions struct {
	Escalate      bool           `json:"escalate,omitempty"`       // true → stop this branch / cancel siblings
	StateDelta    map[string]any `json:"state_delta,omitempty"`    // termination_reason/limit_hit/steps_consumed, etc.
	ArtifactDelta map[string]any `json:"artifact_delta,omitempty"` // produced-artifact deltas (forward-compat)
}

// SetAuthorIfEmpty stamps the author on an LLM-emitted Event that has not set one
// (D-14). Aura's open interface has no base-struct embed hook, so each workflow
// agent sets Author explicitly; this helper covers events minted deeper down.
func (e *Event) SetAuthorIfEmpty(name string) {
	if e.Author == "" {
		e.Author = name
	}
}

// eventWire is the on-the-wire projection of Event with Timestamp forced to a
// canonical RFC3339Nano UTC string so MarshalJSON is byte-stable.
type eventWire struct {
	RequestID    uuid.UUID    `json:"request_id"`
	SpanID       [8]byte      `json:"span_id"`
	ParentSpanID *[8]byte     `json:"parent_span_id,omitempty"`
	Author       string       `json:"author"`
	Branch       string       `json:"branch,omitempty"`
	ThreadID     string       `json:"thread_id,omitempty"`
	MessageID    uuid.UUID    `json:"message_id,omitempty"`
	LLMResponse  *LLMResponse `json:"llm_response,omitempty"`
	Actions      Actions      `json:"actions"`
	Timestamp    string       `json:"timestamp"`
}

// MarshalJSON is the SINGLE user-facing serialization path for an Event (W7):
// the Plan-07 dry-run prints Events through json.NewEncoder(...).SetEscapeHTML(false)
// honoring this method. canonicaljson is for hashing/fingerprinting only, never
// for Event output. HTML escaping is disabled and Timestamp is normalized to
// RFC3339Nano UTC so decode(encode(ev)) round-trips byte-identical (D-21).
func (e Event) MarshalJSON() ([]byte, error) {
	w := eventWire{
		RequestID:    e.RequestID,
		SpanID:       e.SpanID,
		ParentSpanID: e.ParentSpanID,
		Author:       e.Author,
		Branch:       e.Branch,
		ThreadID:     e.ThreadID,
		MessageID:    e.MessageID,
		LLMResponse:  e.LLMResponse,
		Actions:      e.Actions,
		Timestamp:    e.Timestamp.UTC().Format(time.RFC3339Nano),
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(w); err != nil {
		return nil, err
	}
	// json.Encoder.Encode appends a trailing newline; trim it for byte-identity.
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// UnmarshalJSON decodes through eventWire (UseNumber for any nested StateDelta
// numbers, RFC3339Nano timestamp) so decode(encode(ev)) is symmetric.
func (e *Event) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var w eventWire
	if err := dec.Decode(&w); err != nil {
		return err
	}
	ts, err := time.Parse(time.RFC3339Nano, w.Timestamp)
	if err != nil {
		return err
	}
	e.RequestID = w.RequestID
	e.SpanID = w.SpanID
	e.ParentSpanID = w.ParentSpanID
	e.Author = w.Author
	e.Branch = w.Branch
	e.ThreadID = w.ThreadID
	e.MessageID = w.MessageID
	e.LLMResponse = w.LLMResponse
	e.Actions = w.Actions
	e.Timestamp = ts.UTC()
	return nil
}
