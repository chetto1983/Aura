package agent

import (
	"strings"
	"unicode/utf8"

	"github.com/aura/aura/internal/llm"
)

const parentTaskHintMaxChars = 512

func parentTaskHintFromMessages(messages []llm.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return normalizeParentTaskHint(messages[i].Content)
		}
	}
	return ""
}

func normalizeParentTaskHint(raw string) string {
	hint := strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(hint) <= parentTaskHintMaxChars {
		return hint
	}
	var b strings.Builder
	b.Grow(parentTaskHintMaxChars)
	count := 0
	for _, r := range hint {
		if count >= parentTaskHintMaxChars {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}
