package conversations

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestDynamicTailPlacementPreservesStableAlwaysBlock(t *testing.T) {
	t.Parallel()
	enc, err := encoder()
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	tests := []struct {
		name       string
		beforeUser bool
		wantIndex  int
	}{
		{name: "fresh turn before current user", beforeUser: true, wantIndex: 4},
		{name: "resumed turn at tail", beforeUser: false, wantIndex: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			tail := DynamicTail{
				ID:                "placement-tail",
				Content:           "<memory>exact recalled bytes</memory>",
				BeforeCurrentUser: test.beforeUser,
			}
			cfg := windowFor(1_000)
			cfg.AlwaysBlock = "SKILL-BLOCK-BYTE-IDENTICAL"
			cfg.DynamicTail = &tail
			turns := []Turn{
				{Seq: 1, Role: llm.RoleSystem, Content: "system"},
				{Seq: 2, Role: llm.RoleUser, Content: "old question"},
				{Seq: 3, Role: llm.RoleAssistant, Content: "old answer"},
				{Seq: 4, Role: llm.RoleUser, Content: "current question"},
			}

			got, err := applyContextLadder(
				context.Background(), "conversation", turns, cfg, enc, &fakeRotEmitter{},
			)
			if err != nil {
				t.Fatalf("applyContextLadder: %v", err)
			}
			if got[1].Content != cfg.AlwaysBlock {
				t.Fatalf("messages[1] = %q, want byte-identical skills block", got[1].Content)
			}
			if got[test.wantIndex].Content != tail.Content {
				t.Fatalf("dynamic tail index = %d, want %d: %#v", messageIndex(got, tail.Content), test.wantIndex, got)
			}
			if countMessageContent(got, tail.Content) != 1 {
				t.Fatalf("dynamic tail count = %d, want 1", countMessageContent(got, tail.Content))
			}
		})
	}
}

func TestDynamicTailEvictsOrdinaryHistoryBeforeWholeItem(t *testing.T) {
	t.Parallel()
	enc, err := encoder()
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	tail := DynamicTail{
		ID:                "eviction-tail",
		Content:           "<memory>" + strings.Repeat("recalled ", 25) + "</memory>",
		BeforeCurrentUser: true,
	}
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "system"},
		{Seq: 2, Role: llm.RoleUser, Content: strings.Repeat("old question ", 35)},
		{Seq: 3, Role: llm.RoleAssistant, Content: strings.Repeat("old answer ", 35)},
		{Seq: 4, Role: llm.RoleUser, Content: "current question"},
	}
	protected := []Turn{turns[0], turns[3]}
	hardCap := totalTokens(enc, protected) + countTokens(enc, tail.Content) + 4
	cfg := windowFor(hardCap)
	cfg.DynamicTail = &tail
	emit := &fakeRotEmitter{}

	got, err := applyContextLadder(
		context.Background(), "conversation", turns, cfg, enc, emit,
	)
	if err != nil {
		t.Fatalf("applyContextLadder: %v", err)
	}
	if messageIndex(got, tail.Content) < 0 {
		t.Fatal("dynamic tail was omitted before ordinary history")
	}
	if messageIndex(got, "current question") < 0 {
		t.Fatal("current user message was evicted")
	}
	if messageIndex(got, turns[1].Content) >= 0 || messageIndex(got, turns[2].Content) >= 0 {
		t.Fatal("old ordinary pair survived despite reserved dynamic-tail budget")
	}
	if len(emit.calls) != 1 {
		t.Fatalf("rot events = %d, want one ordinary-history eviction", len(emit.calls))
	}
}

func TestDynamicTailOversizeIsOmittedWhole(t *testing.T) {
	t.Parallel()
	enc, err := encoder()
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	tail := DynamicTail{
		ID:                "oversize-tail",
		Content:           "<memory>" + strings.Repeat("oversize ", 500) + "</memory>",
		BeforeCurrentUser: true,
	}
	cfg := windowFor(50)
	cfg.DynamicTail = &tail
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "system"},
		{Seq: 2, Role: llm.RoleUser, Content: "current question"},
	}

	got, err := applyContextLadder(
		context.Background(), "conversation", turns, cfg, enc, &fakeRotEmitter{},
	)
	if err != nil {
		t.Fatalf("applyContextLadder: %v", err)
	}
	if messageIndex(got, tail.Content) >= 0 {
		t.Fatal("oversize dynamic tail was partially or fully included")
	}
	if messageIndex(got, "current question") < 0 {
		t.Fatal("ordinary current user message was lost with oversize tail")
	}
}

func TestValidateDynamicTailMessagesRejectsTamperDuplicatePlacementAndCap(t *testing.T) {
	t.Parallel()
	tail := DynamicTail{
		ID: "validation-tail", Content: "<memory>exact</memory>",
		BeforeCurrentUser: true,
	}
	cfg := windowFor(1_000)
	base := []llm.Message{
		{Role: llm.RoleSystem, Content: "system"},
		{
			Role: llm.RoleUser, Content: tail.Content,
			DynamicTailID: tail.ID,
		},
		{Role: llm.RoleUser, Content: "current"},
	}
	if err := ValidateDynamicTailMessages(base, tail, cfg.HardCap()); err != nil {
		t.Fatalf("valid messages: %v", err)
	}
	tests := []struct {
		name     string
		messages []llm.Message
	}{
		{name: "wire shape", messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "system"},
			{
				Role: llm.RoleAssistant, Content: tail.Content,
				DynamicTailID: tail.ID,
			},
			{Role: llm.RoleUser, Content: "current"},
		}},
		{name: "tampered", messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "system"},
			{
				Role: llm.RoleUser, Content: tail.Content + "!",
				DynamicTailID: tail.ID,
			},
			{Role: llm.RoleUser, Content: "current"},
		}},
		{name: "duplicated", messages: append(append([]llm.Message(nil), base...), llm.Message{
			Role: llm.RoleUser, Content: tail.Content, DynamicTailID: tail.ID,
		})},
		{name: "wrong placement", messages: []llm.Message{
			{Role: llm.RoleSystem, Content: "system"},
			{
				Role: llm.RoleUser, Content: tail.Content,
				DynamicTailID: tail.ID,
			},
			{Role: llm.RoleAssistant, Content: "not current user"},
		}},
	}
	if err := ValidateDynamicTailMessages(
		base,
		DynamicTail{},
		cfg.HardCap(),
	); err == nil {
		t.Fatal("empty dynamic tail error = nil")
	}
	resumed := DynamicTail{ID: tail.ID, Content: tail.Content}
	if err := ValidateDynamicTailMessages(
		[]llm.Message{
			{Role: llm.RoleSystem, Content: "system"},
			{
				Role: llm.RoleUser, Content: tail.Content,
				DynamicTailID: tail.ID,
			},
		},
		resumed,
		0,
	); err != nil {
		t.Fatalf("valid resumed tail: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateDynamicTailMessages(test.messages, tail, cfg.HardCap()); err == nil {
				t.Fatal("ValidateDynamicTailMessages error = nil")
			}
		})
	}
	nonSystemTokens := countMessagesTokensForTest(t, base[1:])
	if err := ValidateDynamicTailMessages(
		[]llm.Message{
			{Role: llm.RoleSystem, Content: strings.Repeat("stable system ", 5_000)},
			{
				Role: llm.RoleUser, Content: tail.Content,
				DynamicTailID: tail.ID,
			},
			{Role: llm.RoleUser, Content: "current"},
		},
		tail,
		nonSystemTokens,
	); err != nil {
		t.Fatalf("reserved system prompt counted against history cap: %v", err)
	}
	tooSmall := nonSystemTokens - 1
	if err := ValidateDynamicTailMessages(base, tail, tooSmall); !errors.Is(err, ErrContextWindowExceeded) {
		t.Fatalf("hard-cap error = %v, want ErrContextWindowExceeded", err)
	}
	withPersistedDuplicates := append([]llm.Message{
		{Role: llm.RoleUser, Content: tail.Content},
		{Role: llm.RoleUser, Content: tail.Content},
	}, base...)
	if err := ValidateDynamicTailMessages(
		withPersistedDuplicates,
		tail,
		0,
	); err != nil {
		t.Fatalf("persisted duplicate content changed marker identity: %v", err)
	}
}

func TestDynamicTailMessageCountIncludesToolWireFields(t *testing.T) {
	t.Parallel()
	enc, err := encoder()
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	var call llm.ToolCall
	call.ID = "call-id"
	call.Function.Name = "tool-name"
	call.Function.Arguments = `{"field":"value"}`
	plain := []llm.Message{{Role: llm.RoleUser, Content: "content"}}
	wire := []llm.Message{
		{
			Role: llm.RoleAssistant, Content: "content",
			ToolCallID: "result-id", ToolCalls: []llm.ToolCall{call},
		},
	}
	if messagesTokenCount(enc, wire) <= messagesTokenCount(enc, plain) {
		t.Fatal("tool-call wire fields were not counted")
	}
}

func messageIndex(messages []llm.Message, content string) int {
	for index, message := range messages {
		if message.Content == content {
			return index
		}
	}
	return -1
}

func countMessageContent(messages []llm.Message, content string) int {
	count := 0
	for _, message := range messages {
		if message.Content == content {
			count++
		}
	}
	return count
}

func countMessagesTokensForTest(t *testing.T, messages []llm.Message) int {
	t.Helper()
	enc, err := encoder()
	if err != nil {
		t.Fatalf("encoder: %v", err)
	}
	turns := make([]Turn, len(messages))
	for index, message := range messages {
		turns[index] = Turn{
			Role: message.Role, Content: message.Content,
			ToolCallID: message.ToolCallID,
		}
	}
	return totalTokens(enc, turns)
}
