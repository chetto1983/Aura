package governance_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent/governance"
	"github.com/aura/aura/internal/llm"
)

// mockSummaryClient is a test-double for llm.Client that tracks call count and
// returns a configurable response or error.
type mockSummaryClient struct {
	response string
	err      error
	calls    int
}

func (m *mockSummaryClient) Send(_ context.Context, _ llm.Request) (llm.Response, error) {
	m.calls++
	if m.err != nil {
		return llm.Response{}, m.err
	}
	return llm.Response{Content: m.response}, nil
}

func (m *mockSummaryClient) Stream(_ context.Context, _ llm.Request) (<-chan llm.Token, error) {
	panic("Stream not used by SubagentPayloadSummarizer")
}

// bigRaw returns a string of n bytes (above payload threshold when n/4 > threshold).
func bigRaw(n int) string { return strings.Repeat("x", n) }

// TestPayloadSummarizer_BelowThreshold_SkipsLLM verifies guard 1: a result
// below the token threshold passes through without any LLM call.
// This covers the "TokenJuice rule MATCH succeeds AND output ≤ threshold →
// summarizer NOT called (layer-1 win, no layer-2 cost)" AC.
func TestPayloadSummarizer_BelowThreshold_SkipsLLM(t *testing.T) {
	mock := &mockSummaryClient{response: "short summary"}
	// threshold = 100 tokens; raw = 80 chars = ~20 tokens — well below threshold
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "", 100, 65536)
	result := s.MaybeSummarize(context.Background(), "search", "", bigRaw(80))
	if result != nil {
		t.Fatalf("expected nil (below threshold), got %+v", result)
	}
	if mock.calls != 0 {
		t.Errorf("LLM should not be called below threshold, got %d calls", mock.calls)
	}
}

// TestPayloadSummarizer_AboveMax_SkipsLLM verifies guard 2: a result above the
// max_payload_tokens threshold is skipped and passed to spill/truncation.
func TestPayloadSummarizer_AboveMax_SkipsLLM(t *testing.T) {
	mock := &mockSummaryClient{response: "summary"}
	// maxTokens = 100; raw = 1000 chars = 250 tokens — above max
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "", 10, 100)
	result := s.MaybeSummarize(context.Background(), "search", "", bigRaw(1000))
	if result != nil {
		t.Fatalf("expected nil (above max), got %+v", result)
	}
	if mock.calls != 0 {
		t.Errorf("LLM should not be called above max tokens, got %d calls", mock.calls)
	}
}

// TestPayloadSummarizer_Success_ReturnsSummarizedPayload verifies the happy path:
// a result that is ≥ threshold AND has a smaller LLM summary returns a
// *SummarizedPayload with the summary content.
// This covers the "tool result ≥ threshold AND TokenJuice fallback → summarizer
// fires; result replaces inline" AC.
func TestPayloadSummarizer_Success_ReturnsSummarizedPayload(t *testing.T) {
	raw := bigRaw(4000) // 4000 bytes = ~1000 tokens; threshold=100 triggers
	summaryText := "short"
	mock := &mockSummaryClient{response: summaryText}
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "system", 100, 65536)

	result := s.MaybeSummarize(context.Background(), "web", "find thing", raw)
	if result == nil {
		t.Fatal("expected *SummarizedPayload, got nil")
	}
	if result.Summary != summaryText {
		t.Errorf("Summary = %q, want %q", result.Summary, summaryText)
	}
	if result.OriginalBytes != len(raw) {
		t.Errorf("OriginalBytes = %d, want %d", result.OriginalBytes, len(raw))
	}
	if result.SummaryBytes != len(summaryText) {
		t.Errorf("SummaryBytes = %d, want %d", result.SummaryBytes, len(summaryText))
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.calls)
	}
}

// TestPayloadSummarizer_SummaryGTERaw_RejectsAndRecordsFailure verifies guard 6:
// when the LLM returns a summary ≥ raw length, the summarizer rejects it and
// records a failure (incrementing the circuit-breaker counter).
func TestPayloadSummarizer_SummaryGTERaw_RejectsAndRecordsFailure(t *testing.T) {
	raw := bigRaw(400) // 400 bytes = ~100 tokens; threshold=10 triggers
	// Summary is same length as raw — should be rejected.
	mock := &mockSummaryClient{response: bigRaw(400)}
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "", 10, 65536)

	result := s.MaybeSummarize(context.Background(), "search", "", raw)
	if result != nil {
		t.Fatalf("expected nil (summary >= raw), got %+v", result)
	}
	// 1 failure should NOT trip the breaker (need 3 consecutive).
	if s.IsDisabled() {
		t.Error("breaker should not be tripped after 1 failure")
	}
}

// TestPayloadSummarizer_ThreeConsecutiveFailures_TripsBreaker verifies the
// 3-strike circuit breaker: after 3 consecutive MaybeSummarize failures,
// the summarizer is disabled for the rest of the session and subsequent
// calls return nil without hitting the LLM.
func TestPayloadSummarizer_ThreeConsecutiveFailures_TripsBreaker(t *testing.T) {
	raw := bigRaw(400) // above threshold=10
	mock := &mockSummaryClient{err: errors.New("LLM unavailable")}
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "", 10, 65536)

	for i := range 3 {
		result := s.MaybeSummarize(context.Background(), "search", "", raw)
		if result != nil {
			t.Fatalf("failure %d: expected nil, got %+v", i+1, result)
		}
	}
	if !s.IsDisabled() {
		t.Fatal("breaker should be tripped after 3 consecutive failures")
	}

	// Subsequent call — breaker is tripped, LLM must NOT be called.
	prevCalls := mock.calls
	result := s.MaybeSummarize(context.Background(), "search", "", raw)
	if result != nil {
		t.Fatalf("post-breaker: expected nil, got %+v", result)
	}
	if mock.calls != prevCalls {
		t.Error("post-breaker: LLM should not be called when circuit breaker is tripped")
	}
}

// TestPayloadSummarizer_SuccessResetsFailCounter verifies that a success between
// failures resets the consecutive counter so 3 failures in a row are required
// to trip the breaker (2 failures → success → 2 failures does NOT trip it).
func TestPayloadSummarizer_SuccessResetsFailCounter(t *testing.T) {
	raw := bigRaw(400) // above threshold=10
	shortSummary := "ok"
	mock := &mockSummaryClient{}
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "", 10, 65536)

	// Two failures.
	mock.err = errors.New("fail")
	for range 2 {
		s.MaybeSummarize(context.Background(), "search", "", raw)
	}

	// One success — resets counter.
	mock.err = nil
	mock.response = shortSummary
	result := s.MaybeSummarize(context.Background(), "search", "", raw)
	if result == nil {
		t.Fatal("expected success after mock reset")
	}

	// Two more failures — should NOT trip (counter was reset by the success).
	mock.err = errors.New("fail again")
	for range 2 {
		s.MaybeSummarize(context.Background(), "search", "", raw)
	}
	if s.IsDisabled() {
		t.Error("breaker should NOT be tripped: success reset counter mid-sequence")
	}
}

// TestPayloadSummarizer_NilClient_ReturnsNil verifies that a non-nil
// SubagentPayloadSummarizer with a client that always errors returns nil
// without panicking (nil-safe error handling).
func TestPayloadSummarizer_NilClient_ReturnsNil(t *testing.T) {
	raw := bigRaw(400)
	mock := &mockSummaryClient{err: errors.New("client error")}
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "", 10, 65536)

	result := s.MaybeSummarize(context.Background(), "search", "", raw)
	if result != nil {
		t.Fatalf("expected nil on client error, got %+v", result)
	}
}

// TestPayloadSummarizer_EmptySummary_RejectsAndRecordsFailure verifies guard 5:
// when the LLM returns an empty (or whitespace-only) summary, the call is
// rejected and a failure is recorded.
func TestPayloadSummarizer_EmptySummary_RejectsAndRecordsFailure(t *testing.T) {
	raw := bigRaw(400)
	mock := &mockSummaryClient{response: "   "} // whitespace only
	s := governance.NewSubagentPayloadSummarizer(mock, "test-model", "", 10, 65536)

	result := s.MaybeSummarize(context.Background(), "search", "", raw)
	if result != nil {
		t.Fatalf("expected nil (empty summary), got %+v", result)
	}
	if mock.calls != 1 {
		t.Errorf("expected 1 LLM call, got %d", mock.calls)
	}
}
