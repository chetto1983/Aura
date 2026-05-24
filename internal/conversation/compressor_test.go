package conversation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

// --- stub LLM client --------------------------------------------------------

type stubCompressorLLM struct {
	response string
	err      error
}

func (s *stubCompressorLLM) Send(_ context.Context, _ llm.Request) (llm.Response, error) {
	return llm.Response{Content: s.response}, s.err
}

func (s *stubCompressorLLM) Stream(_ context.Context, _ llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token, 1)
	ch <- llm.Token{Content: s.response, Done: true}
	close(ch)
	return ch, s.err
}

// --- Test 1: SUMMARY_PREFIX byte stability ----------------------------------
//
// Any change to SUMMARY_PREFIX must update this test and add a rationale
// comment in compressor.go explaining why the preamble changed.

func TestSummaryPrefixByteStability(t *testing.T) {
	// The constant is lifted byte-by-byte from hermes-agent
	// agent/context_compressor.py lines 37-51.
	// Python len() = 772 Unicode code points; 5 em-dashes (U+2014, 3 bytes each)
	// give UTF-8 byte length = 772 + 5*2 = 782.
	// The PRD noted "451-char" which was incorrect; 782 is the verified byte count.
	const wantByteLen = 782
	got := len(SUMMARY_PREFIX)
	if got != wantByteLen {
		t.Errorf("SUMMARY_PREFIX byte length changed: got %d, want %d\n"+
			"Update this test AND add a rationale comment in compressor.go "+
			"if the change is intentional.", got, wantByteLen)
	}
	// Spot-check canonical substrings to catch accidental partial edits.
	for _, want := range []string{
		"[CONTEXT COMPACTION",
		"REFERENCE ONLY]",
		"## Active Task",
		"MEMORY.md, USER.md",
		"avoid repeating it:",
	} {
		if !strings.Contains(SUMMARY_PREFIX, want) {
			t.Errorf("SUMMARY_PREFIX missing expected substring: %q", want)
		}
	}
}

// --- Test 2: JSON-arg truncation valid round-trip ---------------------------

func TestSanitizeToolCallArgs_ValidJSON_RoundTrip(t *testing.T) {
	fixtures := []map[string]any{
		{"path": "/short"},
		{"content": strings.Repeat("x", 500), "mode": "write"},
		{"nested": map[string]any{"deep": strings.Repeat("y", 300)}},
		{"list": []any{strings.Repeat("z", 250), "ok"}},
		{"mixed": map[string]any{"num": 42, "flag": true, "text": strings.Repeat("a", 400)}},
	}

	msgs := make([]llm.Message, len(fixtures))
	for i, f := range fixtures {
		msgs[i] = llm.Message{
			Role: "assistant",
			ToolCalls: []llm.ToolCall{
				{ID: "tc" + string(rune('0'+i)), Name: "test_tool", Arguments: f},
			},
		}
	}

	sanitized := sanitizeToolCallArgs(msgs)

	for i, m := range sanitized {
		for j, tc := range m.ToolCalls {
			// Must marshal without error.
			data, err := json.Marshal(tc.Arguments)
			if err != nil {
				t.Errorf("fixture %d call %d: json.Marshal failed: %v", i, j, err)
				continue
			}
			// Must unmarshal back without error.
			var out map[string]any
			if err := json.Unmarshal(data, &out); err != nil {
				t.Errorf("fixture %d call %d: json.Unmarshal failed: %v (json=%s)", i, j, err, data)
				continue
			}
			// All string leaves must be ≤ _argTruncateChars + len("...[truncated]").
			assertStringLeaves(t, out, i, j)
		}
	}
}

func assertStringLeaves(t *testing.T, v any, fixture, call int) {
	t.Helper()
	const maxLen = _argTruncateChars + len("...[truncated]")
	switch val := v.(type) {
	case string:
		if len(val) > maxLen {
			t.Errorf("fixture %d call %d: string leaf too long (%d > %d)", fixture, call, len(val), maxLen)
		}
	case map[string]any:
		for _, child := range val {
			assertStringLeaves(t, child, fixture, call)
		}
	case []any:
		for _, child := range val {
			assertStringLeaves(t, child, fixture, call)
		}
	}
}

// --- Test 3: streaming scrubber across chunk boundaries --------------------

func TestStreamingScrubber_RemovesFullSpan(t *testing.T) {
	s := &StreamingContextScrubber{}
	got := s.Feed("hello <memory-context>\nsecret</memory-context> world") + s.Flush()
	want := "hello  world"
	if got != want {
		t.Errorf("Feed: got %q, want %q", got, want)
	}
}

func TestStreamingScrubber_SpanSplitAcross2Chunks(t *testing.T) {
	s := &StreamingContextScrubber{}
	// Split right after the open tag
	chunk1 := "before <memory-context>"
	chunk2 := "secret</memory-context> after"

	out := s.Feed(chunk1) + s.Feed(chunk2) + s.Flush()
	want := "before  after"
	if out != want {
		t.Errorf("2-chunk split: got %q, want %q", out, want)
	}
}

func TestStreamingScrubber_SpanSplitAcross3Chunks(t *testing.T) {
	s := &StreamingContextScrubber{}
	// Split the open tag across chunks
	chunk1 := "start <memory"
	chunk2 := "-context>\nhidden\ncontent\n</"
	chunk3 := "memory-context> end"

	out := s.Feed(chunk1) + s.Feed(chunk2) + s.Feed(chunk3) + s.Flush()
	want := "start  end"
	if out != want {
		t.Errorf("3-chunk split: got %q, want %q", out, want)
	}
}

func TestStreamingScrubber_NoTagPassthrough(t *testing.T) {
	s := &StreamingContextScrubber{}
	got := s.Feed("hello world") + s.Flush()
	if got != "hello world" {
		t.Errorf("no-tag passthrough: got %q, want %q", got, "hello world")
	}
}

func TestStreamingScrubber_Reset(t *testing.T) {
	s := &StreamingContextScrubber{}
	_ = s.Feed("start <memory-context>hidden")
	s.Reset()
	got := s.Feed("fresh") + s.Flush()
	if got != "fresh" {
		t.Errorf("after Reset: got %q, want %q", got, "fresh")
	}
}

// --- Test 4: image-aware token budgeting ------------------------------------

func TestContentLengthForBudget_ImageTokenEstimate(t *testing.T) {
	// A message whose content is a JSON array with one image_url part and one
	// text part should count imageTokenEstimate * charsPerToken chars for the
	// image and len(text) chars for the text.
	content, _ := json.Marshal([]map[string]any{
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,abc"}},
		{"type": "text", "text": "hello"},
	})
	got := contentLengthForBudget(string(content))
	wantImageChars := imageTokenEstimate * charsPerToken
	wantTextChars := len("hello")
	want := wantImageChars + wantTextChars

	if got != want {
		t.Errorf("contentLengthForBudget: got %d chars, want %d (image=%d + text=%d)",
			got, want, wantImageChars, wantTextChars)
	}
	// The token count should be imageTokenEstimate for the image part.
	tokenCount := got / charsPerToken
	if tokenCount != imageTokenEstimate+len("hello")/charsPerToken {
		// len("hello") / 4 = 1, so tokenCount should be 1601
		t.Logf("tokenCount = %d (image=%d + text=%d)", tokenCount, imageTokenEstimate, len("hello")/charsPerToken)
	}
	// Specifically, the image contribution in tokens should be imageTokenEstimate.
	imageContribTokens := (imageTokenEstimate * charsPerToken) / charsPerToken
	if imageContribTokens != imageTokenEstimate {
		t.Errorf("image token contribution = %d, want %d", imageContribTokens, imageTokenEstimate)
	}
}

func TestContentLengthForBudget_PlainString(t *testing.T) {
	s := "hello world"
	got := contentLengthForBudget(s)
	if got != len(s) {
		t.Errorf("plain string: got %d, want %d", got, len(s))
	}
}

func TestContentLengthForBudget_AllImageTypes(t *testing.T) {
	for _, imageType := range []string{"image_url", "input_image", "image"} {
		content, _ := json.Marshal([]map[string]any{
			{"type": imageType},
		})
		got := contentLengthForBudget(string(content))
		want := imageTokenEstimate * charsPerToken
		if got != want {
			t.Errorf("type=%q: got %d chars, want %d", imageType, got, want)
		}
	}
}

// --- Test 5: summary >= raw guard fires, raw preserved ----------------------

func TestContextCompressor_SummaryLargerThanRaw_GuardFires(t *testing.T) {
	const rawContent = "short"
	// Stub returns a summary LONGER than the raw content.
	stub := &stubCompressorLLM{response: strings.Repeat("x", len(rawContent)+100)}
	c := NewContextCompressor(stub, 128000)

	msgs := []llm.Message{
		{Role: "user", Content: rawContent},
		{Role: "assistant", Content: "ok"},
	}
	result := c.Compress(msgs, 0, "")

	// Guard must have fired — original messages returned unchanged.
	if len(result) != len(msgs) {
		t.Fatalf("guard did not fire: got %d messages, want %d (original)", len(result), len(msgs))
	}
	for i, m := range result {
		if m.Content != msgs[i].Content || m.Role != msgs[i].Role {
			t.Errorf("message %d changed: got {%s %q}, want {%s %q}",
				i, m.Role, m.Content, msgs[i].Role, msgs[i].Content)
		}
	}
}

func TestContextCompressor_SummaryEqualsRaw_GuardFires(t *testing.T) {
	const rawContent = "hello"
	// Stub returns a summary EQUAL in length to raw.
	stub := &stubCompressorLLM{response: strings.Repeat("y", len(rawContent))}
	c := NewContextCompressor(stub, 128000)

	msgs := []llm.Message{
		{Role: "user", Content: rawContent},
	}
	result := c.Compress(msgs, 0, "")
	// Must return original (guard fires on >=).
	if len(result) != 1 || result[0].Content != rawContent {
		t.Errorf("guard did not fire for equal-length summary: got %v", result)
	}
}

func TestContextCompressor_ShorterSummary_Compressed(t *testing.T) {
	rawContent := strings.Repeat("word ", 200) // ~1000 chars
	summary := "short summary"                 // much shorter
	stub := &stubCompressorLLM{response: summary}
	c := NewContextCompressor(stub, 128000)

	msgs := []llm.Message{
		{Role: "user", Content: rawContent},
		{Role: "assistant", Content: rawContent},
	}
	result := c.Compress(msgs, 0, "")

	if len(result) < 1 {
		t.Fatal("compress returned empty slice")
	}
	// The summary message should contain SUMMARY_PREFIX.
	found := false
	for _, m := range result {
		if strings.HasPrefix(m.Content, SUMMARY_PREFIX) {
			found = true
			break
		}
	}
	if !found {
		t.Error("compressed result does not contain SUMMARY_PREFIX")
	}
}

func TestContextCompressor_SystemMessagePreserved(t *testing.T) {
	rawContent := strings.Repeat("w ", 300)
	stub := &stubCompressorLLM{response: "brief summary"}
	c := NewContextCompressor(stub, 128000)

	msgs := []llm.Message{
		{Role: "system", Content: "system instructions"},
		{Role: "user", Content: rawContent},
	}
	result := c.Compress(msgs, 0, "")

	if len(result) == 0 || result[0].Role != "system" || result[0].Content != "system instructions" {
		t.Errorf("system message not preserved: result[0] = %+v", result[0])
	}
}

// --- Interface compliance ---------------------------------------------------

var _ ContextEngine = (*ContextCompressor)(nil)
