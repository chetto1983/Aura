package conversations

import (
	"errors"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/pkoukk/tiktoken-go"
)

// DynamicTail is one indivisible, non-persisted model-visible reference item.
type DynamicTail struct {
	ID                string
	Content           string
	BeforeCurrentUser bool
}

func usableDynamicTail(
	tail *DynamicTail,
	enc *tiktoken.Tiktoken,
	hardCap int,
) (*DynamicTail, int) {
	if tail == nil || tail.ID == "" || strings.TrimSpace(tail.ID) != tail.ID ||
		strings.TrimSpace(tail.Content) == "" || hardCap <= 0 {
		return nil, 0
	}
	tokens := countTokens(enc, tail.Content)
	if tokens <= 0 || tokens > hardCap {
		return nil, 0
	}
	copy := *tail
	return &copy, tokens
}

func injectDynamicTail(messages []llm.Message, tail *DynamicTail) []llm.Message {
	if tail == nil {
		return messages
	}
	index := len(messages)
	if tail.BeforeCurrentUser {
		for candidate := len(messages) - 1; candidate >= 0; candidate-- {
			if messages[candidate].Role == llm.RoleUser {
				index = candidate
				break
			}
		}
	}
	item := llm.Message{
		Role: llm.RoleUser, Content: tail.Content, DynamicTailID: tail.ID,
	}
	out := make([]llm.Message, 0, len(messages)+1)
	out = append(out, messages[:index]...)
	out = append(out, item)
	out = append(out, messages[index:]...)
	return out
}

// ValidateDynamicTailMessages enforces the final byte, cardinality, placement,
// and hard-cap invariants immediately before a model request is exposed.
func ValidateDynamicTailMessages(
	messages []llm.Message,
	tail DynamicTail,
	hardCap int,
) error {
	if tail.ID == "" || strings.TrimSpace(tail.ID) != tail.ID {
		return errors.New("dynamic tail identity is invalid")
	}
	if strings.TrimSpace(tail.Content) == "" {
		return errors.New("dynamic tail content is empty")
	}
	found := -1
	for index, message := range messages {
		if message.DynamicTailID == "" {
			continue
		}
		if message.DynamicTailID != tail.ID {
			return errors.New("unexpected dynamic tail identity")
		}
		if message.Content != tail.Content || message.Role != llm.RoleUser ||
			message.ToolCallID != "" ||
			len(message.ToolCalls) != 0 || message.Name != "" {
			return errors.New("dynamic tail wire shape changed")
		}
		if found >= 0 {
			return errors.New("dynamic tail is duplicated")
		}
		found = index
	}
	if found < 0 {
		return errors.New("dynamic tail is missing or changed")
	}
	if tail.BeforeCurrentUser {
		if found+1 >= len(messages) || messages[found+1].Role != llm.RoleUser {
			return errors.New("dynamic tail is not immediately before the current user")
		}
	} else if found != len(messages)-1 {
		return errors.New("dynamic tail is not at the request tail")
	}
	if hardCap > 0 {
		enc, err := encoder()
		if err != nil {
			return fmt.Errorf("dynamic tail token encoder: %w", err)
		}
		if tokens := messagesTokenCount(enc, messages); tokens > hardCap {
			return fmt.Errorf("%w (%d tokens, cap %d)", ErrContextWindowExceeded, tokens, hardCap)
		}
	}
	return nil
}

func messagesTokenCount(enc *tiktoken.Tiktoken, messages []llm.Message) int {
	total := 0
	for index, message := range messages {
		if index == 0 && message.Role == llm.RoleSystem {
			continue
		}
		total += countTokens(enc, message.Content)
		total += countTokens(enc, message.ToolCallID)
		for _, call := range message.ToolCalls {
			total += countTokens(enc, call.ID)
			total += countTokens(enc, call.Function.Name)
			total += countTokens(enc, call.Function.Arguments)
		}
	}
	return total
}
