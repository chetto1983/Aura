package conversation

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestNewContext(t *testing.T) {
	cfg := Config{
		MaxTokens: 4000,
		Logger:    slog.Default(),
	}
	ctx := NewContext(cfg)
	if ctx == nil {
		t.Fatal("NewContext returned nil")
	}
	if ctx.maxTokens != 4000 {
		t.Errorf("maxTokens = %d, want 4000", ctx.maxTokens)
	}
}

func TestNewContextDefaultTokens(t *testing.T) {
	cfg := Config{
		MaxTokens: 0,
		Logger:    slog.Default(),
	}
	ctx := NewContext(cfg)
	if ctx.maxTokens != 4000 {
		t.Errorf("maxTokens = %d, want 4000 (default)", ctx.maxTokens)
	}
}

func TestAddUserAndAssistantMessages(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.AddUserMessage("Hello")
	ctx.AddAssistantMessage("Hi there!")

	msgs := ctx.Messages()
	if len(msgs) != 2 {
		t.Fatalf("Messages() length = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "Hello" {
		t.Errorf("first message = %v, want role=user content=Hello", msgs[0])
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "Hi there!" {
		t.Errorf("second message = %v, want role=assistant content=Hi there!", msgs[1])
	}
}

func TestSetSystemMessage(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.SetSystemMessage("You are Aura.")
	ctx.AddUserMessage("Hello")

	msgs := ctx.Messages()
	if len(msgs) != 2 {
		t.Fatalf("Messages() length = %d, want 2", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "You are Aura." {
		t.Errorf("system message = %v, want role=system content=You are Aura.", msgs[0])
	}

	// Replace system message
	ctx.SetSystemMessage("You are helpful.")
	msgs = ctx.Messages()
	if msgs[0].Content != "You are helpful." {
		t.Errorf("replaced system message = %q, want %q", msgs[0].Content, "You are helpful.")
	}
}

func TestSetSearchContext(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.SetSystemMessage("You are Aura.")

	ctx.SetSearchContext("Relevant wiki knowledge:\n- test page")

	msgs := ctx.Messages()
	if msgs[0].Role != "system" {
		t.Errorf("system message role = %q, want system", msgs[0].Role)
	}
	expected := "You are Aura.\n\nRelevant wiki knowledge:\n- test page"
	if msgs[0].Content != expected {
		t.Errorf("system message with search = %q, want %q", msgs[0].Content, expected)
	}

	// Search context is replaced (not accumulated) on each message
	ctx.SetSearchContext("New search results:\n- different page")
	msgs = ctx.Messages()
	wantNew := "You are Aura.\n\nNew search results:\n- different page"
	if msgs[0].Content != wantNew {
		t.Errorf("refreshed search context = %q, want %q", msgs[0].Content, wantNew)
	}

	// SetSearchContext without prior system message creates one
	ctx2 := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx2.SetSearchContext("Wiki context here")
	msgs2 := ctx2.Messages()
	if msgs2[0].Content != "Wiki context here" {
		t.Errorf("search context without base = %q, want %q", msgs2[0].Content, "Wiki context here")
	}
}

func TestEstimatedTokens(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.AddUserMessage("Hello world")

	// 11 chars / 4 ≈ 2 tokens
	est := ctx.EstimatedTokens()
	if est <= 0 {
		t.Errorf("EstimatedTokens = %d, want > 0", est)
	}
}

func TestShouldSummarize(t *testing.T) {
	// Use a very small max tokens to trigger summarization
	ctx := NewContext(Config{MaxTokens: 20, Logger: slog.Default()})
	ctx.AddUserMessage("Short")

	if ctx.ShouldSummarize() {
		t.Error("ShouldSummarize() = true for short context, want false")
	}

	// Add enough messages to exceed 80% of 20 tokens
	for i := 0; i < 20; i++ {
		ctx.AddUserMessage("This is a somewhat longer message to fill context")
	}

	if !ctx.ShouldSummarize() {
		t.Error("ShouldSummarize() = false for long context, want true")
	}
}

func TestTrackTokens(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.TrackTokens(llm.TokenUsage{TotalTokens: 100})
	ctx.TrackTokens(llm.TokenUsage{TotalTokens: 50})

	if ctx.TotalTokensUsed() != 150 {
		t.Errorf("TotalTokensUsed = %d, want 150", ctx.TotalTokensUsed())
	}
}

func TestTranscript(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.AddUserMessage("Hello")
	ctx.AddAssistantMessage("Hi")

	transcript := ctx.Transcript()
	if len(transcript) != 2 {
		t.Fatalf("Transcript() length = %d, want 2", len(transcript))
	}
	if transcript[0] != "user: Hello" {
		t.Errorf("transcript[0] = %q, want %q", transcript[0], "user: Hello")
	}
	if transcript[1] != "assistant: Hi" {
		t.Errorf("transcript[1] = %q, want %q", transcript[1], "assistant: Hi")
	}
}

func TestSummarizeWithMockLLM(t *testing.T) {
	mock := &mockLLMClient{
		response: llm.Response{Content: "Summarized content", Usage: llm.TokenUsage{TotalTokens: 50}},
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:  50,
		Summarizer: mock,
		Logger:     logger,
	})

	// Add enough messages to trigger summarization
	for i := 0; i < 10; i++ {
		ctx.AddUserMessage("This is message number " + string(rune('0'+i)) + " with some content")
		ctx.AddAssistantMessage("Response number " + string(rune('0'+i)))
	}

	err := ctx.Summarize(context.Background())
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}

	if ctx.summary == "" {
		t.Error("summary is empty after summarization")
	}
}

func TestSummarizeWithoutLLM(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:  50,
		Summarizer: nil,
		Logger:     logger,
	})

	for i := 0; i < 10; i++ {
		ctx.AddUserMessage("Message " + string(rune('0'+i)))
		ctx.AddAssistantMessage("Reply " + string(rune('0'+i)))
	}

	err := ctx.Summarize(context.Background())
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}

	// Without LLM, it should have trimmed messages
	if len(ctx.messages) == 20 {
		t.Error("messages should have been trimmed without LLM")
	}
}

func TestMessagesWithSummary(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.AddUserMessage("Hello")

	msgs := ctx.Messages()
	if len(msgs) != 1 {
		t.Fatalf("Messages() without summary = %d, want 1", len(msgs))
	}

	// Simulate having a summary
	ctx.summary = "Previous conversation was about greetings."
	msgs = ctx.Messages()

	// Should include summary as a system message
	hasSummary := false
	for _, m := range msgs {
		if m.Role == "system" && m.Content == "Summary of earlier conversation:\nPrevious conversation was about greetings." {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Error("Messages() should include summary system message")
	}
}

// mockLLMClient implements llm.Client for testing.
type mockLLMClient struct {
	response llm.Response
	err      error
}

func (m *mockLLMClient) Send(ctx context.Context, req llm.Request) (llm.Response, error) {
	if m.err != nil {
		return llm.Response{}, m.err
	}
	return m.response, nil
}

func (m *mockLLMClient) Stream(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token, 1)
	if m.err != nil {
		ch <- llm.Token{Err: m.err, Done: true}
	} else {
		ch <- llm.Token{Content: m.response.Content, Done: true}
	}
	close(ch)
	return ch, nil
}

func TestIsOverLimit(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 20, Logger: slog.Default()})
	ctx.AddUserMessage("Hi")
	if ctx.IsOverLimit() {
		t.Error("IsOverLimit() = true for short context, want false")
	}

	for i := 0; i < 20; i++ {
		ctx.AddUserMessage("This is a longer message that adds tokens")
	}
	if !ctx.IsOverLimit() {
		t.Error("IsOverLimit() = false for context over maxTokens, want true")
	}
}

func TestMaxTokens(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	if ctx.MaxTokens() != 4000 {
		t.Errorf("MaxTokens() = %d, want 4000", ctx.MaxTokens())
	}
}

func TestEnforceLimitNoActionNeeded(t *testing.T) {
	ctx := NewContext(Config{MaxTokens: 4000, Logger: slog.Default()})
	ctx.AddUserMessage("Short message")

	beforeCount := len(ctx.messages)
	err := ctx.EnforceLimit(context.Background())
	if err != nil {
		t.Fatalf("EnforceLimit() error = %v", err)
	}
	if len(ctx.messages) != beforeCount {
		t.Error("EnforceLimit should not modify context when under limits")
	}
}

func TestEnforceLimitSummarizesWhenOver80Percent(t *testing.T) {
	mock := &mockLLMClient{
		response: llm.Response{Content: "Summary of conversation", Usage: llm.TokenUsage{TotalTokens: 10}},
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:  20,
		Summarizer: mock,
		Logger:     logger,
	})

	// Add enough messages to exceed 80% of 20 tokens
	for i := 0; i < 20; i++ {
		ctx.AddUserMessage("This is a message with enough content to fill context")
	}

	msgCountBefore := len(ctx.messages)
	err := ctx.EnforceLimit(context.Background())
	if err != nil {
		t.Fatalf("EnforceLimit() error = %v", err)
	}

	// Messages should have been reduced (summarized)
	if len(ctx.messages) >= msgCountBefore {
		t.Errorf("EnforceLimit should have reduced messages: before=%d, after=%d", msgCountBefore, len(ctx.messages))
	}

	// Summary should have been created
	if ctx.summary == "" {
		t.Error("EnforceLimit should have created a summary")
	}
}

func TestEnforceLimitTrimsWhenOverHardLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:  200,
		Summarizer: nil, // no LLM, will force trimming
		Logger:     logger,
	})

	// Add many messages to exceed the hard limit
	for i := 0; i < 40; i++ {
		ctx.AddUserMessage("Message with enough content to exceed the token limit when many are added")
	}

	err := ctx.EnforceLimit(context.Background())
	if err != nil {
		t.Fatalf("EnforceLimit() error = %v", err)
	}

	// Context should be reduced after enforcement
	if ctx.IsOverLimit() {
		est := ctx.EstimatedTokens()
		t.Errorf("EnforceLimit should bring context under limit: estimated=%d, max=%d", est, ctx.maxTokens)
	}
}

func TestEnforceLimitMessageCapDropsOldestNoLLMCall(t *testing.T) {
	// Mock that fails if invoked — proves no summarization round-trip.
	mock := &mockLLMClient{err: errMockSummarizerCalled}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:   1_000_000, // way above any real usage
		MaxMessages: 5,
		Summarizer:  mock,
		Logger:      logger,
	})
	ctx.SetSystemMessage("system identity")

	for i := 0; i < 12; i++ {
		ctx.AddUserMessage("user " + string(rune('a'+i)))
		ctx.AddAssistantMessage("asst " + string(rune('a'+i)))
	}

	if err := ctx.EnforceLimit(context.Background()); err != nil {
		t.Fatalf("EnforceLimit error = %v", err)
	}

	msgs := ctx.Messages()
	// system + 5 capped messages = 6
	if len(msgs) != 6 {
		t.Fatalf("got %d messages, want 6 (system + 5 capped)", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("first message role = %q, want system", msgs[0].Role)
	}
	// Last message should be the last assistant we added.
	if msgs[len(msgs)-1].Content != "asst l" {
		t.Errorf("last message content = %q, want last asst", msgs[len(msgs)-1].Content)
	}
	// Transcript still has the full history.
	if len(ctx.Transcript()) != 24 {
		t.Errorf("transcript length = %d, want 24", len(ctx.Transcript()))
	}
}

func TestEnforceLimitMessageCapPreservesToolCallPairs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:   1_000_000,
		MaxMessages: 3,
		Logger:      logger,
	})

	ctx.AddUserMessage("u1")
	ctx.AddAssistantMessage("a1")
	ctx.AddUserMessage("u2")
	// Assistant emits 2 tool calls — both tool results must remain attached.
	ctx.AddAssistantToolCallMessage("", []llm.ToolCall{
		{ID: "t1", Name: "x"},
		{ID: "t2", Name: "y"},
	})
	ctx.AddToolResultMessage("t1", "r1")
	ctx.AddToolResultMessage("t2", "r2")
	ctx.AddAssistantMessage("a-final")

	if err := ctx.EnforceLimit(context.Background()); err != nil {
		t.Fatalf("EnforceLimit error = %v", err)
	}

	// We should never see an orphaned tool result. Walk and check invariant:
	// every "tool" message must be preceded (within history) by an
	// assistant tool-call message.
	msgs := ctx.Messages()
	sawAssistantWithCalls := false
	for _, m := range msgs {
		switch m.Role {
		case "assistant":
			sawAssistantWithCalls = len(m.ToolCalls) > 0
		case "tool":
			if !sawAssistantWithCalls {
				t.Fatalf("orphan tool message after cap: %+v", m)
			}
		default:
			sawAssistantWithCalls = false
		}
	}
}

func TestEnforceLimitMessageCapDisabledWhenZero(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:   1_000_000,
		MaxMessages: 0, // disabled
		Logger:      logger,
	})

	for i := 0; i < 100; i++ {
		ctx.AddUserMessage("msg")
	}

	if err := ctx.EnforceLimit(context.Background()); err != nil {
		t.Fatalf("EnforceLimit error = %v", err)
	}
	if len(ctx.messages) != 100 {
		t.Errorf("with cap disabled expected 100 messages kept, got %d", len(ctx.messages))
	}
}

var errMockSummarizerCalled = errMock("summarizer should not be called when message cap is sufficient")

type errMock string

func (e errMock) Error() string { return string(e) }

func TestEnforceLimitPreservesTranscript(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := NewContext(Config{
		MaxTokens:  10,
		Summarizer: nil,
		Logger:     logger,
	})

	for i := 0; i < 20; i++ {
		ctx.AddUserMessage("Message with content")
	}

	transcriptLen := len(ctx.Transcript())
	_ = ctx.EnforceLimit(context.Background())

	// Transcript should be preserved even after trimming active messages
	if len(ctx.Transcript()) != transcriptLen {
		t.Errorf("Transcript should be preserved: before=%d, after=%d", transcriptLen, len(ctx.Transcript()))
	}
}
