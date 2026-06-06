package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"pgregory.net/rapid"
)

func mustV7(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid.NewV7: %v", err)
	}
	return id
}

// fullEvent builds an Event exercising every populated field: a parent span, an
// LLM turn with a reused llm.ToolCall, and nested StateDelta.
func fullEvent(t *testing.T) Event {
	t.Helper()
	parent := [8]byte{1, 2, 3, 4, 5, 6, 7, 8}
	var span [8]byte
	copy(span[:], []byte{9, 10, 11, 12, 13, 14, 15, 16})
	var tc llm.ToolCall
	tc.ID = "call_abc123"
	tc.Type = "function"
	tc.Function.Name = "web_search"
	tc.Function.Arguments = `{"q":"aura"}`
	return Event{
		RequestID:    mustV7(t),
		SpanID:       span,
		ParentSpanID: &parent,
		Author:       "InterviewLoop",
		Branch:       "root.iter-2.worker-3",
		ThreadID:     "thread-7",
		MessageID:    mustV7(t),
		LLMResponse: &LLMResponse{
			Content:      "thinking",
			ToolCalls:    []llm.ToolCall{tc},
			FinishReason: "tool_calls",
		},
		Actions: Actions{
			Escalate: true,
			StateDelta: map[string]any{
				"termination_reason": "budget_exhausted",
				"limit_hit":          "max_steps",
				"nested":             map[string]any{"a": "b"},
			},
		},
		Timestamp: time.Date(2026, 5, 29, 21, 38, 25, 123456789, time.UTC),
	}
}

func TestEvent_FullShapeMarshalsToJSON_RoundTrips(t *testing.T) {
	orig := fullEvent(t)

	first, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip not byte-identical:\n first=%s\nsecond=%s", first, second)
	}
	if decoded.LLMResponse == nil || len(decoded.LLMResponse.ToolCalls) != 1 {
		t.Fatalf("LLMResponse/ToolCalls lost in round-trip: %+v", decoded.LLMResponse)
	}
	if decoded.LLMResponse.ToolCalls[0].ID != "call_abc123" {
		t.Fatalf("ToolCallID (llm.ToolCall.ID) lost: %q", decoded.LLMResponse.ToolCalls[0].ID)
	}
	if nested, ok := decoded.Actions.StateDelta["nested"].(map[string]any); !ok || nested["a"] != "b" {
		t.Fatalf("nested StateDelta map[string]any not preserved: %#v", decoded.Actions.StateDelta["nested"])
	}
}

func TestEvent_AwaitingInput_RoundTripsByteIdentical(t *testing.T) {
	ev := Event{
		RequestID: mustV7(t),
		Author:    "aura",
		Actions: Actions{
			AwaitingInput: &AwaitingInput{
				Question:    "approve deploy?",
				Options:     []PauseOption{{Label: "yes", Value: "y"}, {Label: "no", Value: "n"}},
				Kind:        "choice",
				Priority:    80,
				ToolCallID:  "call_pause_1",
				OriginAgent: "aura",
			},
		},
		Timestamp: time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}

	first, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(first, []byte("awaiting_input")) {
		t.Fatalf("set AwaitingInput must appear on the wire, got: %s", first)
	}

	var decoded Event
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Actions.AwaitingInput == nil {
		t.Fatalf("AwaitingInput lost in round-trip")
	}
	if decoded.Actions.AwaitingInput.ToolCallID != "call_pause_1" ||
		decoded.Actions.AwaitingInput.Kind != "choice" ||
		decoded.Actions.AwaitingInput.Priority != 80 ||
		len(decoded.Actions.AwaitingInput.Options) != 2 {
		t.Fatalf("AwaitingInput payload mangled: %+v", decoded.Actions.AwaitingInput)
	}

	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip not byte-identical:\n first=%s\nsecond=%s", first, second)
	}
}

func TestEvent_NilAwaitingInput_OmitsKey(t *testing.T) {
	ev := Event{
		RequestID: mustV7(t),
		Author:    "aura",
		Timestamp: time.Now().UTC(),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("awaiting_input")) {
		t.Fatalf("nil AwaitingInput must be omitted from the wire, got: %s", b)
	}
}

// TestEvent_LLMResponseReasoning_RoundTripsByteIdentical (amendment #57): an Event
// whose LLMResponse carries only Reasoning marshals with "reasoning" and no
// "content" key, and decode(encode(ev)) is byte-identical.
func TestEvent_LLMResponseReasoning_RoundTripsByteIdentical(t *testing.T) {
	ev := Event{
		RequestID: mustV7(t),
		Author:    "aura",
		LLMResponse: &LLMResponse{
			Reasoning: "let me think",
		},
		Timestamp: time.Date(2026, 6, 6, 12, 0, 0, 0, time.UTC),
	}

	first, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(first, []byte("\"reasoning\":\"let me think\"")) {
		t.Fatalf("set Reasoning must appear on the wire, got: %s", first)
	}
	if bytes.Contains(first, []byte("\"content\"")) {
		t.Fatalf("a reasoning-only LLMResponse must omit the content key, got: %s", first)
	}

	var decoded Event
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.LLMResponse == nil || decoded.LLMResponse.Reasoning != "let me think" {
		t.Fatalf("Reasoning lost in round-trip: %+v", decoded.LLMResponse)
	}

	second, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("round-trip not byte-identical:\n first=%s\nsecond=%s", first, second)
	}
}

// TestEvent_EmptyReasoning_OmitsKey: an LLMResponse with empty Reasoning omits the
// `reasoning` key on the wire (omitempty), keeping the wire byte-symmetric.
func TestEvent_EmptyReasoning_OmitsKey(t *testing.T) {
	ev := Event{
		RequestID:   mustV7(t),
		Author:      "aura",
		LLMResponse: &LLMResponse{Content: "ciao"},
		Timestamp:   time.Now().UTC(),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("reasoning")) {
		t.Fatalf("empty Reasoning must be omitted from the wire, got: %s", b)
	}
}

func TestEvent_NilLLMResponse_OmitsObject(t *testing.T) {
	ev := Event{
		RequestID: mustV7(t),
		Author:    "user",
		Timestamp: time.Now().UTC(),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte("llm_response")) {
		t.Fatalf("nil LLMResponse must be omitted, got: %s", b)
	}
	if bytes.Contains(b, []byte("parent_span_id")) {
		t.Fatalf("nil ParentSpanID must be omitted, got: %s", b)
	}
	// WR-01 regression: a zero-valued MessageID must NOT leak an all-zero UUID.
	// uuid.UUID is [16]byte, so the source-struct omitempty tag is a no-op; the
	// eventWire *uuid.UUID projection is what actually omits it.
	if bytes.Contains(b, []byte("message_id")) {
		t.Fatalf("zero MessageID must be omitted from the wire, got: %s", b)
	}
}

func TestEvent_MessageID_PresentWhenSet(t *testing.T) {
	mid := mustV7(t)
	ev := Event{
		RequestID: mustV7(t),
		MessageID: mid,
		Author:    "agent",
		Timestamp: time.Now().UTC(),
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte("\"message_id\":\""+mid.String()+"\"")) {
		t.Fatalf("set MessageID must appear on the wire, got: %s", b)
	}
}

func TestEvent_TraceID16Bytes_SpanID8Bytes(t *testing.T) {
	ev := Event{
		RequestID: mustV7(t),
		SpanID:    [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		Timestamp: time.Now().UTC(),
	}

	if len(ev.RequestID) != 16 {
		t.Fatalf("RequestID must be 16 bytes (OTel TraceID), got %d", len(ev.RequestID))
	}
	if got := len(ev.SpanID); got != 8 {
		t.Fatalf("SpanID must be 8 bytes (OTel SpanID), got %d", got)
	}
	if ev.ParentSpanID != nil {
		t.Fatalf("ParentSpanID must be nil at root, got %v", ev.ParentSpanID)
	}

	// UUIDv7 version nibble must be 7 (D-16).
	if ev.RequestID.Version() != 7 {
		t.Fatalf("RequestID must be UUIDv7, got version %d", ev.RequestID.Version())
	}

	// WR-02: span_id is serialized as a lower-hex STRING (OTel/W3C-idiomatic),
	// NOT the JSON number array a raw [8]byte field would emit. An 8-byte SpanID
	// is exactly 16 hex chars.
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if !bytes.Contains(b, []byte("\"span_id\":\"0102030405060708\"")) {
		t.Fatalf("span_id must be lower-hex string %q, got: %s", "0102030405060708", b)
	}

	// Round-trip the hex wire form back into the fixed array.
	var decoded Event
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.SpanID != ev.SpanID {
		t.Fatalf("SpanID lost in hex round-trip: want %v, got %v", ev.SpanID, decoded.SpanID)
	}
}

func TestEvent_Property_JSONRoundTripByteIdentical(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		span := rapid.SliceOfN(rapid.Byte(), 8, 8).Draw(rt, "span")
		var sp [8]byte
		copy(sp[:], span)

		hasParent := rapid.Bool().Draw(rt, "hasParent")
		var parent *[8]byte
		if hasParent {
			pb := rapid.SliceOfN(rapid.Byte(), 8, 8).Draw(rt, "parent")
			var p [8]byte
			copy(p[:], pb)
			parent = &p
		}

		author := rapid.StringMatching(`[a-zA-Z0-9._-]{0,24}`).Draw(rt, "author")
		branch := rapid.StringMatching(`[a-zA-Z0-9._-]{0,24}`).Draw(rt, "branch")
		escalate := rapid.Bool().Draw(rt, "escalate")
		// nanosecond precision within RFC3339Nano round-trips exactly.
		nanos := rapid.Int64Range(0, 1_900_000_000_000_000_000).Draw(rt, "nanos")

		hasLLM := rapid.Bool().Draw(rt, "hasLLM")
		var llmResp *LLMResponse
		if hasLLM {
			llmResp = &LLMResponse{
				Content:      rapid.StringMatching(`[a-zA-Z0-9 ]{0,32}`).Draw(rt, "content"),
				FinishReason: rapid.SampledFrom([]string{"", "stop", "tool_calls"}).Draw(rt, "finish"),
			}
		}

		ev := Event{
			RequestID:    mustV7Rapid(rt),
			SpanID:       sp,
			ParentSpanID: parent,
			Author:       author,
			Branch:       branch,
			MessageID:    mustV7Rapid(rt),
			LLMResponse:  llmResp,
			Actions:      Actions{Escalate: escalate},
			Timestamp:    time.Unix(0, nanos).UTC(),
		}

		first, err := json.Marshal(ev)
		if err != nil {
			rt.Fatalf("marshal: %v", err)
		}
		var decoded Event
		if err := json.Unmarshal(first, &decoded); err != nil {
			rt.Fatalf("unmarshal: %v (json=%s)", err, first)
		}
		second, err := json.Marshal(decoded)
		if err != nil {
			rt.Fatalf("re-marshal: %v", err)
		}
		if !bytes.Equal(first, second) {
			rt.Fatalf("not byte-identical:\n first=%s\nsecond=%s", first, second)
		}
	})
}

func mustV7Rapid(rt *rapid.T) uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		rt.Fatalf("uuid.NewV7: %v", err)
	}
	return id
}

func TestErrBudgetExhausted_IsComparable(t *testing.T) {
	wrapped := errors.Join(errors.New("loop hit cap"), ErrBudgetExhausted)
	if !errors.Is(wrapped, ErrBudgetExhausted) {
		t.Fatal("errors.Is must match the exported ErrBudgetExhausted sentinel")
	}
}

func TestEvent_SetAuthorIfEmpty(t *testing.T) {
	ev := &Event{}
	ev.SetAuthorIfEmpty("LlmAgent")
	if ev.Author != "LlmAgent" {
		t.Fatalf("want author LlmAgent, got %q", ev.Author)
	}
	ev.SetAuthorIfEmpty("override")
	if ev.Author != "LlmAgent" {
		t.Fatalf("SetAuthorIfEmpty must not overwrite, got %q", ev.Author)
	}
}
