package agui

import (
	"strings"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

// TestDefaultIDGenerator exercises the production uuid-v4 IDGenerator (the one
// NewServer wires into `aura serve`; unit tests otherwise inject fixedIDGen, so the
// default minter is unreached). It asserts the PRD-mandated prefixes and the
// tool-result correlation to the originating tool call id.
func TestDefaultIDGenerator(t *testing.T) {
	g := NewIDGenerator()

	mid := g.NewMessageID()
	if !strings.HasPrefix(mid, "msg-") {
		t.Errorf("NewMessageID = %q, want msg- prefix", mid)
	}
	rid := g.NewReasoningID()
	if !strings.HasPrefix(rid, "rsn-") {
		t.Errorf("NewReasoningID = %q, want rsn- prefix", rid)
	}
	tid := g.NewToolResultID("call-7")
	if tid != "msg-tool-call-7" {
		t.Errorf("NewToolResultID = %q, want msg-tool-call-7 (correlated to the tool call id)", tid)
	}
	// Distinct calls mint distinct ids (the uuid body is fresh each time).
	if g.NewMessageID() == mid {
		t.Error("NewMessageID returned the same id twice; want fresh uuids")
	}
}

// TestPayloadString covers the resume-payload encoder across its three branches: nil →
// "", a string verbatim, and a structured (non-string) payload JSON-encoded so a
// structured answer survives onto the Runner's resume content.
func TestPayloadString(t *testing.T) {
	if got := payloadString(nil); got != "" {
		t.Errorf("payloadString(nil) = %q, want empty", got)
	}
	if got := payloadString("yes"); got != "yes" {
		t.Errorf("payloadString(string) = %q, want verbatim", got)
	}
	got := payloadString(map[string]any{"choice": "a"})
	if got != `{"choice":"a"}` {
		t.Errorf("payloadString(map) = %q, want JSON-encoded", got)
	}
}

// TestResumeAnswersActions maps resolved→accept and cancelled→cancel, keyed on the
// interrupt token, with the payload carried as the answer content.
func TestResumeAnswersActions(t *testing.T) {
	out, err := resumeAnswers([]types.ResumeEntry{
		{InterruptID: "tok-a", Status: types.ResumeStatusResolved, Payload: "ok"},
		{InterruptID: "tok-b", Status: types.ResumeStatusCancelled},
	})
	if err != nil {
		t.Fatalf("resumeAnswers: %v", err)
	}
	if a := out["tok-a"]; a.Action != askuser.ActionAccept || a.Content != "ok" {
		t.Errorf("tok-a = %+v, want accept/ok", a)
	}
	if a := out["tok-b"]; a.Action != askuser.ActionCancel {
		t.Errorf("tok-b action = %q, want cancel", a.Action)
	}
}

// TestLastUserMessage returns the final non-empty user message and nil when none is
// present (a resume-only run continues over the rehydrated history with no fresh turn).
func TestLastUserMessage(t *testing.T) {
	msgs := []types.Message{
		{Role: types.Role(llm.RoleUser), Content: "first"},
		{Role: types.Role(llm.RoleAssistant), Content: "reply"},
		{Role: types.Role(llm.RoleUser), Content: "second"},
	}
	got, err := lastUserMessage(msgs)
	if err != nil {
		t.Fatalf("lastUserMessage returned error: %v", err)
	}
	if got == nil || *got != "second" {
		t.Errorf("lastUserMessage = %v, want second", got)
	}
	// No user message → nil.
	got, err = lastUserMessage([]types.Message{{Role: types.Role(llm.RoleAssistant), Content: "x"}})
	if err != nil {
		t.Fatalf("lastUserMessage(no user) error: %v", err)
	}
	if got != nil {
		t.Errorf("lastUserMessage(no user) = %v, want nil", got)
	}
	// Empty user content is skipped (not a fresh turn).
	got, err = lastUserMessage([]types.Message{{Role: types.Role(llm.RoleUser), Content: ""}})
	if err != nil {
		t.Fatalf("lastUserMessage(empty user) error: %v", err)
	}
	if got != nil {
		t.Errorf("lastUserMessage(empty user) = %v, want nil", got)
	}
	// Multimodal content is rejected explicitly rather than silently replaying
	// old history with no fresh user turn.
	_, err = lastUserMessage([]types.Message{{
		Role: types.Role(llm.RoleUser),
		Content: []types.InputContent{{
			Type: types.InputContentTypeText,
			Text: "hi",
		}},
	}})
	if err == nil {
		t.Fatal("lastUserMessage(multimodal) error = nil, want explicit rejection")
	}
}

// TestBufferCapFallback: a non-positive configured cap falls back to the fanout
// default; a positive cap is honored.
func TestBufferCapFallback(t *testing.T) {
	if got := (&Server{cfg: ServerConfig{BufferCap: 0}}).bufferCap(); got != fanoutBuffer {
		t.Errorf("bufferCap(0) = %d, want fanout default %d", got, fanoutBuffer)
	}
	if got := (&Server{cfg: ServerConfig{BufferCap: 7}}).bufferCap(); got != 7 {
		t.Errorf("bufferCap(7) = %d, want 7", got)
	}
}
