package conversation

import (
	"testing"

	"github.com/aura/aura/internal/llm"
)

// ---- interface compliance ----

var _ ContextEngine = (*DefaultContextEngine)(nil)

// Verify secondary interfaces can be type-asserted (compile-time checks only).
var _ ContextEnginePreflight = (*stubPreflightEngine)(nil)
var _ ContextEngineLifecycle = (*stubLifecycleEngine)(nil)

type stubPreflightEngine struct{ DefaultContextEngine }

func (s *stubPreflightEngine) ShouldCompressPreflight(_ []llm.Message) bool { return false }

type stubLifecycleEngine struct{ DefaultContextEngine }

func (s *stubLifecycleEngine) OnSessionStart() {}
func (s *stubLifecycleEngine) OnSessionEnd()   {}
func (s *stubLifecycleEngine) OnSessionReset() {}

// ---- DefaultContextEngine.Name ----

func TestDefaultContextEngine_Name(t *testing.T) {
	e := &DefaultContextEngine{}
	if e.Name() != "default" {
		t.Fatalf("Name() = %q, want 'default'", e.Name())
	}
}

// ---- DefaultContextEngine.UpdateFromResponse ----

func TestDefaultContextEngine_UpdateFromResponse_StoresFields(t *testing.T) {
	e := &DefaultContextEngine{}
	usage := llm.TokenUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	e.UpdateFromResponse(usage)
	if e.LastPromptTokens != 100 {
		t.Errorf("LastPromptTokens = %d, want 100", e.LastPromptTokens)
	}
	if e.LastCompletionTokens != 50 {
		t.Errorf("LastCompletionTokens = %d, want 50", e.LastCompletionTokens)
	}
	if e.LastTotalTokens != 150 {
		t.Errorf("LastTotalTokens = %d, want 150", e.LastTotalTokens)
	}
}

func TestDefaultContextEngine_UpdateFromResponse_OverwritesPreviousValues(t *testing.T) {
	e := &DefaultContextEngine{}
	e.UpdateFromResponse(llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15})
	e.UpdateFromResponse(llm.TokenUsage{PromptTokens: 200, CompletionTokens: 80, TotalTokens: 280})
	if e.LastPromptTokens != 200 {
		t.Errorf("LastPromptTokens = %d, want 200 (last call wins)", e.LastPromptTokens)
	}
}

// ---- DefaultContextEngine.ShouldCompress ----

func TestDefaultContextEngine_ShouldCompress_BelowCap_False(t *testing.T) {
	e := &DefaultContextEngine{} // cap = 50
	if e.ShouldCompress(50) {
		t.Error("ShouldCompress(50) = true, want false (equal to cap is NOT over limit)")
	}
	if e.ShouldCompress(0) {
		t.Error("ShouldCompress(0) = true, want false")
	}
	if e.ShouldCompress(49) {
		t.Error("ShouldCompress(49) = true, want false")
	}
}

func TestDefaultContextEngine_ShouldCompress_AboveCap_True(t *testing.T) {
	e := &DefaultContextEngine{} // cap = 50
	if !e.ShouldCompress(51) {
		t.Error("ShouldCompress(51) = false, want true")
	}
	if !e.ShouldCompress(100) {
		t.Error("ShouldCompress(100) = false, want true")
	}
}

func TestDefaultContextEngine_ShouldCompress_CustomCap(t *testing.T) {
	e := NewDefaultContextEngine(10)
	if e.ShouldCompress(10) {
		t.Error("ShouldCompress(10) with cap=10 = true, want false")
	}
	if !e.ShouldCompress(11) {
		t.Error("ShouldCompress(11) with cap=10 = false, want true")
	}
}

func TestDefaultContextEngine_ShouldCompress_ZeroCap_UsesDefault(t *testing.T) {
	e := NewDefaultContextEngine(0) // should default to MaxConversationMessages=50
	if e.ShouldCompress(50) {
		t.Error("ShouldCompress(50) with zero cap = true, want false (defaults to 50)")
	}
	if !e.ShouldCompress(51) {
		t.Error("ShouldCompress(51) with zero cap = false, want true (defaults to 50)")
	}
}

// ---- DefaultContextEngine.Compress ----

func makeMessages(roles ...string) []llm.Message {
	msgs := make([]llm.Message, len(roles))
	for i, r := range roles {
		msgs[i] = llm.Message{Role: r, Content: r + "_content"}
	}
	return msgs
}

func TestDefaultContextEngine_Compress_BelowCap_ReturnsSameSlice(t *testing.T) {
	e := &DefaultContextEngine{Cap: 5}
	msgs := makeMessages("user", "assistant", "user")
	got := e.Compress(msgs, 3, "")
	if len(got) != 3 {
		t.Errorf("Compress with 3 msgs and cap=5 returned %d messages, want 3", len(got))
	}
	// CompressionCount must NOT increment when no compression happened.
	if e.CompressionCount != 0 {
		t.Errorf("CompressionCount = %d, want 0 (no compression)", e.CompressionCount)
	}
}

func TestDefaultContextEngine_Compress_DropsOldestToFitCap(t *testing.T) {
	e := &DefaultContextEngine{Cap: 4}
	msgs := makeMessages("user", "assistant", "user", "assistant", "user", "assistant")
	// 6 messages, cap=4 → keep last 4
	got := e.Compress(msgs, len(msgs), "")
	if len(got) != 4 {
		t.Errorf("Compress returned %d messages, want 4", len(got))
	}
	if got[0].Content != "user_content" {
		t.Errorf("first kept message content = %q, want user_content", got[0].Content)
	}
	if got[len(got)-1].Content != "assistant_content" {
		t.Errorf("last kept message content = %q, want assistant_content", got[len(got)-1].Content)
	}
	if e.CompressionCount != 1 {
		t.Errorf("CompressionCount = %d, want 1", e.CompressionCount)
	}
}

func TestDefaultContextEngine_Compress_PreservesSystemMessage(t *testing.T) {
	e := &DefaultContextEngine{Cap: 3}
	msgs := []llm.Message{
		{Role: "system", Content: "system_prompt"},
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
	}
	// body = 5 messages, cap=3 → keep last 3 body + system = 4 total
	got := e.Compress(msgs, len(msgs), "")
	if len(got) == 0 || got[0].Role != "system" {
		t.Fatalf("system message not preserved: got[0] = %+v", got[0])
	}
	if got[0].Content != "system_prompt" {
		t.Errorf("system content = %q, want system_prompt", got[0].Content)
	}
	// 3 body messages + 1 system = 4 total
	if len(got) != 4 {
		t.Errorf("Compress returned %d messages, want 4 (system + 3 body)", len(got))
	}
	if e.CompressionCount != 1 {
		t.Errorf("CompressionCount = %d, want 1", e.CompressionCount)
	}
}

func TestDefaultContextEngine_Compress_ToolSafeBoundaryRespected(t *testing.T) {
	// cap=3 but split point would land on a tool result — must advance to keep the pair.
	e := &DefaultContextEngine{Cap: 3}
	msgs := []llm.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1", ToolCalls: []llm.ToolCall{{ID: "tc1", Name: "x"}}},
		{Role: "tool", Content: "r1", ToolCallID: "tc1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}
	// body = 5, cap=3 → naïve split=2, but messages[2] is tool result of messages[1]
	// toolSafeBoundary advances split to include the pair.
	got := e.Compress(msgs, len(msgs), "")
	// Must not have a tool result without its preceding assistant-with-tool-calls.
	for i, m := range got {
		if m.Role == "tool" {
			if i == 0 {
				t.Fatal("orphan tool result at index 0")
			}
			prev := got[i-1]
			if prev.Role != "tool" && len(prev.ToolCalls) == 0 {
				t.Errorf("tool result at %d has no assistant-tool-call parent (prev role=%q)", i, prev.Role)
			}
		}
	}
}

func TestDefaultContextEngine_Compress_DoesNotMutateInputSlice(t *testing.T) {
	e := &DefaultContextEngine{Cap: 2}
	original := makeMessages("user", "assistant", "user", "assistant")
	snapshot := make([]llm.Message, len(original))
	copy(snapshot, original)
	e.Compress(original, len(original), "")
	for i, m := range original {
		if m.Role != snapshot[i].Role || m.Content != snapshot[i].Content {
			t.Errorf("input slice mutated at index %d: got %+v, want %+v", i, m, snapshot[i])
		}
	}
}

func TestDefaultContextEngine_Compress_Increments_CompressionCount(t *testing.T) {
	e := &DefaultContextEngine{Cap: 2}
	msgs := makeMessages("user", "assistant", "user", "assistant")
	e.Compress(msgs, len(msgs), "")
	e.Compress(msgs, len(msgs), "")
	if e.CompressionCount != 2 {
		t.Errorf("CompressionCount = %d, want 2", e.CompressionCount)
	}
}

// ---- secondary interface type-assertion at runtime ----

func TestContextEnginePreflight_TypeAssertion(t *testing.T) {
	var engine ContextEngine = &stubPreflightEngine{}
	if _, ok := engine.(ContextEnginePreflight); !ok {
		t.Error("stubPreflightEngine should satisfy ContextEnginePreflight")
	}
	// DefaultContextEngine does NOT implement ContextEnginePreflight.
	var plain ContextEngine = &DefaultContextEngine{}
	if _, ok := plain.(ContextEnginePreflight); ok {
		t.Error("DefaultContextEngine should NOT satisfy ContextEnginePreflight")
	}
}

func TestContextEngineLifecycle_TypeAssertion(t *testing.T) {
	var engine ContextEngine = &stubLifecycleEngine{}
	if _, ok := engine.(ContextEngineLifecycle); !ok {
		t.Error("stubLifecycleEngine should satisfy ContextEngineLifecycle")
	}
	// DefaultContextEngine does NOT implement ContextEngineLifecycle.
	var plain ContextEngine = &DefaultContextEngine{}
	if _, ok := plain.(ContextEngineLifecycle); ok {
		t.Error("DefaultContextEngine should NOT satisfy ContextEngineLifecycle")
	}
}
