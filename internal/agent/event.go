package agent

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
//
// WR-04 — SPAN MINTING IS DEFERRED, BY DESIGN, to the future OTel-integration slice
// (SPEC §"OTel dep transitive": Phase 2 ships the OTel-compatible SHAPE without the
// OTel dep; populating real per-node spans is out of Phase-2 scope). Phase 2 therefore
// leaves SpanID at its zero value [8]byte{} on every Event, which serializes as the
// constant "span_id":"0000000000000000". This all-zero value is the documented
// not-yet-minted sentinel, NOT a bug: the wire field exists now so the OTel slice
// fans out an existing field rather than retrofitting one. When that slice lands it
// mints a crypto/rand SpanID per child InvocationContext and chains ParentSpanID.
//
// The struct stores SpanID/ParentSpanID as fixed-size byte arrays, but the wire
// form (see eventWire) is the OTel/W3C-idiomatic LOWER-HEX string, NOT the JSON
// number array a [N]byte array would default to. MessageID is value-typed here
// but omitted on the wire until set (eventWire uses *uuid.UUID so omitempty fires).
type Event struct {
	RequestID    uuid.UUID    `json:"request_id"`               // UUIDv7 TraceID/run_id, shared tree-wide
	SpanID       [8]byte      `json:"span_id"`                  // 8-byte OTel SpanID, per-node; wire = lower-hex
	ParentSpanID *[8]byte     `json:"parent_span_id,omitempty"` // nil at root; wire = lower-hex when set
	Author       string       `json:"author"`                   // workflow agent name, "user", or LLM agent name (D-14)
	Branch       string       `json:"branch,omitempty"`         // hierarchical label only (D-15)
	ThreadID     string       `json:"thread_id,omitempty"`      // AG-UI conversation thread (Phase 4 / Slice 1.8), forward-compat
	MessageID    uuid.UUID    `json:"message_id,omitempty"`     // AG-UI message correlation (UUIDv7); omitted on wire until set
	LLMResponse  *LLMResponse `json:"llm_response,omitempty"`   // nil when this Event is not an LLM turn
	Actions      Actions      `json:"actions"`                  // control signals (escalate, state/artifact deltas)
	Timestamp    time.Time    `json:"timestamp"`                // emit time, serialized RFC3339Nano UTC
}

// LLMResponse carries the model output for an Event that IS an LLM turn. It is
// referenced via a pointer on Event so non-LLM events omit it entirely.
type LLMResponse struct {
	Content      string         `json:"content,omitempty"`       // assistant text
	Reasoning    string         `json:"reasoning,omitempty"`     // stream-only CoT delta (amendment #57); never persisted to conversation_turns, additive D-17 forward-compat
	ToolCalls    []llm.ToolCall `json:"tool_calls,omitempty"`    // reuses llm.ToolCall (D-17); its ID is the AG-UI tool_call_id
	FinishReason string         `json:"finish_reason,omitempty"` // provider finish reason on the final turn
}

// Actions are the control signals an Event carries. Escalate is the canonical
// termination/cancellation signal (D-04: budget exhaustion is Event-only, never
// the error slot). StateDelta/ArtifactDelta accept nested map[string]any and map
// to the AG-UI STATE_DELTA stream in Phase 12. AwaitingInput is the HITL pause
// signal (D-A1-03): a sibling to Escalate, set by the pause-detection seam
// (llm_agent_pause.go) when ask_user fires — the pause travels as this Event,
// NEVER the iter.Seq2 error slot. It is a pointer so an unset pause omits the
// `awaiting_input` key on the wire (mirrors the MessageID *pointer omitempty
// trick), keeping decode(encode())==identity.
type Actions struct {
	Escalate       bool            `json:"escalate,omitempty"`       // true → stop this branch / cancel siblings
	StateDelta     map[string]any  `json:"state_delta,omitempty"`    // termination_reason/limit_hit/steps_consumed, etc.
	ArtifactDelta  map[string]any  `json:"artifact_delta,omitempty"` // produced-artifact deltas (forward-compat)
	AwaitingInput  *AwaitingInput  `json:"awaiting_input,omitempty"` // HITL pause payload (Slice 1.5); nil unless ask_user fired
	ToolInvocation *ToolInvocation `json:"tool_invocation,omitempty"`
	// DiscardStreamed is the mid-stream-retry repudiation signal (B-12). When a
	// stream fails mid-way and the loop retries, the partial chunk Events the failed
	// attempt already streamed are stale; the agent emits ONE Event carrying this
	// flag BEFORE the retry's fresh chunks so a streaming consumer (the REPL renderer,
	// the AG-UI gateway) drops what it has shown so far and renders the retry over a
	// blank slate — instead of the user seeing partial+answer. It fires ONLY on a
	// retry, so live token-by-token streaming on the common no-retry path is
	// untouched (no buffering). Omitted on the wire unless set.
	DiscardStreamed bool `json:"discard_streamed,omitempty"`
}

// ToolInvocationStart and ToolInvocationEnd label tool invocation lifecycle events.
const (
	ToolInvocationStart = "start"
	ToolInvocationEnd   = "end"
)

// ToolInvocation is the typed audit payload emitted around real tool execution.
// It is intentionally runtime-only shape, not a DB type: the Runner projects it
// into the append-only tool invocation ledger.
type ToolInvocation struct {
	Event             string         `json:"event"`
	ToolCallID        string         `json:"tool_call_id"`
	ToolName          string         `json:"tool_name"`
	Arguments         string         `json:"arguments,omitempty"`
	ArgsBytes         int            `json:"args_bytes,omitempty"`
	BatchIndex        int            `json:"batch_index,omitempty"`
	BatchSize         int            `json:"batch_size,omitempty"`
	StartedAt         *time.Time     `json:"started_at,omitempty"`
	EndedAt           *time.Time     `json:"ended_at,omitempty"`
	DurationMS        int64          `json:"duration_ms,omitempty"`
	Status            string         `json:"status,omitempty"`
	Error             string         `json:"error,omitempty"`
	ResultPreview     string         `json:"result_preview,omitempty"`
	PreviewBytes      int            `json:"preview_bytes,omitempty"`
	ResultBytes       int            `json:"result_bytes,omitempty"`
	ResultTruncated   bool           `json:"result_truncated,omitempty"`
	ResultSidecarPath string         `json:"result_sidecar_path,omitempty"`
	ExitCode          *int           `json:"exit_code,omitempty"`
	Meta              map[string]any `json:"meta,omitempty"`
}

// AwaitingInput is the pause payload an ask_user Event carries (D-A1-03). It is
// the Event-side projection of tools.ErrAwaitingUserInput plus the dispatch-
// stamped ToolCallID and the originating-agent id for swarm forward-compat
// (D-A1-08: paused_states.proxied_* stay NULL for direct calls, populated by
// Phase 9 as the Event crosses the child→parent boundary). It carries no DB
// type — the askuser.Store reads these plain fields off the Event, never the
// other way round (the agent stays DB-free).
type AwaitingInput struct {
	Question           string          `json:"question"`
	Options            []PauseOption   `json:"options,omitempty"`
	Kind               string          `json:"kind"`
	Priority           int             `json:"priority,omitempty"`
	ToolCallID         string          `json:"tool_call_id"`
	ResumeContext      json.RawMessage `json:"resume_context,omitempty"`
	OriginAgent        string          `json:"origin_agent,omitempty"`          // emitting agent name (swarm proxy forward-compat, D-A1-08)
	ProxiedFromChildID string          `json:"proxied_from_child_id,omitempty"` // child id when this pause relays a child's needs_user_input report (D-05); empty on a direct call
	ProxiedToolCallID  string          `json:"proxied_tool_call_id,omitempty"`  // child's originating tool_call id for the relay (D-05); empty on a direct call
}

// PauseOption mirrors a tools.Option on the wire. It is redeclared here (rather
// than importing tools.Option into the Event model) so internal/agent/event.go
// stays free of any tools dependency cycle; the pause seam copies the fields.
type PauseOption struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// SetAuthorIfEmpty stamps the author on an LLM-emitted Event that has not set one
// (D-14). Aura's open interface has no base-struct embed hook, so each workflow
// agent sets Author explicitly; this helper covers events minted deeper down.
func (e *Event) SetAuthorIfEmpty(name string) {
	if e.Author == "" {
		e.Author = name
	}
}

// eventWire is the on-the-wire projection of Event. It diverges from Event in
// three encodings that the source struct cannot express via tags:
//   - SpanID/ParentSpanID are hex strings (OTel/W3C-idiomatic, D-16), NOT the
//     verbose JSON number array a [8]byte field would otherwise emit; ParentSpanID
//     stays omitempty (nil at root) via the *string.
//   - MessageID is *uuid.UUID so omitempty actually fires: a value-type uuid.UUID
//     ([16]byte array) never triggers omitempty, leaking an all-zero UUID on every
//     line (WR-01); the pointer omits it until a real id is set.
//   - Timestamp is forced to a canonical RFC3339Nano UTC string so MarshalJSON is
//     byte-stable.
type eventWire struct {
	RequestID    uuid.UUID    `json:"request_id"`
	SpanID       string       `json:"span_id"`
	ParentSpanID *string      `json:"parent_span_id,omitempty"`
	Author       string       `json:"author"`
	Branch       string       `json:"branch,omitempty"`
	ThreadID     string       `json:"thread_id,omitempty"`
	MessageID    *uuid.UUID   `json:"message_id,omitempty"`
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
		SpanID:       hex.EncodeToString(e.SpanID[:]),
		ParentSpanID: hexPtr(e.ParentSpanID),
		Author:       e.Author,
		Branch:       e.Branch,
		ThreadID:     e.ThreadID,
		MessageID:    uuidPtrIfSet(e.MessageID),
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
	span, err := decodeSpan(w.SpanID)
	if err != nil {
		return fmt.Errorf("span_id: %w", err)
	}
	parent, err := decodeSpanPtr(w.ParentSpanID)
	if err != nil {
		return fmt.Errorf("parent_span_id: %w", err)
	}
	e.RequestID = w.RequestID
	e.SpanID = span
	e.ParentSpanID = parent
	e.Author = w.Author
	e.Branch = w.Branch
	e.ThreadID = w.ThreadID
	if w.MessageID != nil {
		e.MessageID = *w.MessageID
	} else {
		e.MessageID = uuid.Nil
	}
	e.LLMResponse = w.LLMResponse
	e.Actions = w.Actions
	e.Timestamp = ts.UTC()
	return nil
}

// hexPtr renders an optional 8-byte SpanID as a hex string pointer, preserving
// the nil-at-root omitempty contract (D-16).
func hexPtr(p *[8]byte) *string {
	if p == nil {
		return nil
	}
	s := hex.EncodeToString(p[:])
	return &s
}

// uuidPtrIfSet returns a pointer to id only when it is non-nil, so the wire
// message_id omitempty fires for an unset UUID (WR-01).
func uuidPtrIfSet(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// decodeSpan parses a hex SpanID back into the fixed 8-byte array.
func decodeSpan(s string) ([8]byte, error) {
	var span [8]byte
	raw, err := hex.DecodeString(s)
	if err != nil {
		return span, err
	}
	if len(raw) != len(span) {
		return span, fmt.Errorf("want %d bytes, got %d", len(span), len(raw))
	}
	copy(span[:], raw)
	return span, nil
}

// decodeSpanPtr parses an optional hex ParentSpanID; nil stays nil (root).
func decodeSpanPtr(s *string) (*[8]byte, error) {
	if s == nil {
		return nil, nil
	}
	span, err := decodeSpan(*s)
	if err != nil {
		return nil, err
	}
	return &span, nil
}
