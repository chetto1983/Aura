package conversations

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// When the transient block and the history cannot both fit, the BLOCK yields.
//
// The block is per-turn decoration -- the memory preload, the recalled evidence -- and
// the history is what the operator actually said. Dropping turns to keep the decoration
// would answer the current question with a shortened conversation and a paragraph about
// something else, which is the wrong trade in a way no error would reveal: both outcomes
// are a successful turn.
//
// Reaching this path needs a history that fits under the hard cap but NOT beside the
// tail, and a reduction that cannot help because everything left is protected.
func TestContextLadderDropsTheTransientBlockBeforeAnyTurn(t *testing.T) {
	enc := mustEncoderRaw(t)
	system := Turn{Seq: 1, Role: llm.RoleSystem, Content: "system prompt"}
	round := []Turn{
		{Seq: 2, Role: llm.RoleUser, Content: strings.Repeat("what did we decide about the schema ", 40)},
		{Seq: 3, Role: llm.RoleAssistant, Content: strings.Repeat("we decided to split the ranking ", 40)},
	}
	turns := append([]Turn{system}, round...)

	block := &TransientContext{
		Content:           strings.Repeat("recalled fact about the schema ", 30),
		BeforeCurrentUser: true,
	}
	historyTokens := totalTokens(enc, turns)
	blockTokens := countTokens(enc, block.Content)

	// The window admits the history alone and refuses it beside the block, which is the
	// only shape that forces the choice this test is about.
	cfg := windowFor(historyTokens)
	cfg.TransientContext = block
	if cap := cfg.hardCap(); historyTokens > cap || historyTokens <= cap-blockTokens {
		t.Fatalf("fixture does not force the choice: history %d, block %d, cap %d",
			historyTokens, blockTokens, cap)
	}

	emit := &fakeRotEmitter{}
	messages, err := applyContextLadder(context.Background(), "conv", turns, cfg, enc, emit)
	if err != nil {
		t.Fatalf("the ladder failed instead of dropping the block: %v", err)
	}

	for _, message := range messages {
		if strings.Contains(message.Content, "recalled fact about the schema") {
			t.Fatalf("the block survived at the expense of the budget: %+v", messages)
		}
	}
	// Every turn is still there: the block yielded, nothing else did.
	if len(messages) != len(turns) {
		t.Fatalf("got %d messages for %d turns -- a turn was dropped to keep the block",
			len(messages), len(turns))
	}
	if len(emit.calls) != 0 {
		t.Fatalf("dropping the block wrote %d rot events; nothing rotted", len(emit.calls))
	}
}
