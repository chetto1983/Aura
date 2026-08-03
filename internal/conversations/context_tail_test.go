package conversations

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestTransientContextIsNonPersistedAndImmediatelyBeforeCurrentUser(t *testing.T) {
	enc := mustEncoderRaw(t)
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "system"},
		{Seq: 2, Role: llm.RoleUser, Content: "old question"},
		{Seq: 3, Role: llm.RoleAssistant, Content: "old answer"},
		{Seq: 4, Role: llm.RoleUser, Content: "where do I live?"},
	}
	cfg := windowFor(200)
	cfg.TransientContext = &TransientContext{
		Content:           "Davide located_in Caraglio",
		BeforeCurrentUser: true,
	}

	got, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, &fakeRotEmitter{})
	if err != nil {
		t.Fatalf("applyContextLadder: %v", err)
	}
	if len(got) != len(turns)+1 {
		t.Fatalf("messages = %d, want %d", len(got), len(turns)+1)
	}
	if got[len(got)-2].Content != cfg.TransientContext.Content || got[len(got)-1].Content != "where do I live?" {
		t.Fatalf("tail = %+v", got[len(got)-2:])
	}
	if len(turns) != 4 || turns[3].Content != "where do I live?" {
		t.Fatalf("persisted input was mutated: %+v", turns)
	}
}

func TestTransientContextIsOmittedWholeWhenItCannotFit(t *testing.T) {
	enc := mustEncoderRaw(t)
	content := strings.Repeat("memory ", 40)
	cfg := windowFor(5)
	cfg.TransientContext = &TransientContext{Content: content, BeforeCurrentUser: true}
	turns := []Turn{
		{Seq: 1, Role: llm.RoleSystem, Content: "s"},
		{Seq: 2, Role: llm.RoleUser, Content: "q"},
	}

	got, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, &fakeRotEmitter{})
	if err != nil {
		t.Fatalf("applyContextLadder: %v", err)
	}
	if len(got) != len(turns) {
		t.Fatalf("messages = %+v, want the original history without a partial memory item", got)
	}
	for _, msg := range got {
		if strings.Contains(msg.Content, "memory") {
			t.Fatalf("oversized memory was truncated into the prompt: %+v", got)
		}
	}
}
