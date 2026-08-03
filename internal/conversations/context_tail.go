package conversations

import (
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// TransientContext is one non-persisted, indivisible reference item inserted after
// persisted history has passed through the deterministic context ladder.
type TransientContext struct {
	Content           string
	BeforeCurrentUser bool
}

func usableTransientContext(
	item *TransientContext,
	enc *tiktoken.Tiktoken,
	hardCap int,
) (*TransientContext, int) {
	if item == nil || strings.TrimSpace(item.Content) == "" || hardCap <= 0 {
		return nil, 0
	}
	tokens := countTokens(enc, item.Content)
	if tokens <= 0 || tokens > hardCap {
		return nil, 0
	}
	copy := *item
	return &copy, tokens
}

func injectTransientContext(messages []llm.Message, item *TransientContext) []llm.Message {
	if item == nil {
		return messages
	}
	index := len(messages)
	if item.BeforeCurrentUser {
		for candidate, message := range slices.Backward(messages) {
			if message.Role == llm.RoleUser {
				index = candidate
				break
			}
		}
	}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, messages[:index]...)
	out = append(out, llm.Message{Role: llm.RoleUser, Content: item.Content})
	out = append(out, messages[index:]...)
	return out
}
