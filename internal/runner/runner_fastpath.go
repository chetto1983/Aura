package runner

import (
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/google/uuid"
)

const fastPathFinishReason = "fast_path"

func fastReplyFor(content string) (string, bool) {
	switch normalizeGreeting(content) {
	case "ciao", "salve", "buongiorno", "buon giorno", "buonasera", "buona sera":
		return "Ciao! Dimmi pure cosa vuoi fare.", true
	default:
		return "", false
	}
}

func normalizeGreeting(content string) string {
	s := strings.ToLower(strings.TrimSpace(content))
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.Trim(s, " \t\r\n.!?,;:")
	parts := strings.Fields(s)
	if len(parts) > 1 && parts[len(parts)-1] == "aura" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, " ")
}

func fastReplyEvent(convID string, requestID uuid.UUID, answer string) *agent.Event {
	return &agent.Event{
		RequestID: requestID,
		Author:    "aura",
		ThreadID:  convID,
		LLMResponse: &agent.LLMResponse{
			Content:      answer,
			FinishReason: fastPathFinishReason,
		},
		Actions: agent.Actions{StateDelta: map[string]any{
			"prompt_tokens":     0,
			"completion_tokens": 0,
			"cache_hit_tokens":  0,
		}},
		Timestamp: time.Now().UTC(),
	}
}

func fastReplyChunkEvent(final *agent.Event) *agent.Event {
	return &agent.Event{
		RequestID: final.RequestID,
		Author:    final.Author,
		ThreadID:  final.ThreadID,
		LLMResponse: &agent.LLMResponse{
			Content: final.LLMResponse.Content,
		},
		Timestamp: final.Timestamp,
	}
}
