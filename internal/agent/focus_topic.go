package agent

import "github.com/aura/aura/internal/llm"

const focusTopicMaxRunes = 500

// lastUserMessageText returns the latest user-role message text, capped so a
// compaction focus hint cannot become another oversized prompt payload.
func lastUserMessageText(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return truncateRunes(msgs[i].Content, focusTopicMaxRunes)
		}
	}
	return ""
}

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
