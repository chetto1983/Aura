package conversation

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/aura/aura/internal/llm"
)

const compactedToolResultPrefix = "[tool result compacted]"

type ToolResultCompactionPolicy struct {
	MaxChars       int
	KeepRecentFull int
}

func (c *Context) CompactCompletedToolResults(policy ToolResultCompactionPolicy) int {
	if c == nil || len(c.messages) == 0 {
		return 0
	}
	maxChars := policy.MaxChars
	if maxChars <= 0 {
		maxChars = 1200
	}
	keepRecentFull := policy.KeepRecentFull
	if keepRecentFull < 0 {
		keepRecentFull = 0
	}

	toolNames := toolCallNames(c.messages)
	candidates := compactableToolResultIndexes(c.messages)
	if keepRecentFull > 0 && len(candidates) > keepRecentFull {
		candidates = candidates[:len(candidates)-keepRecentFull]
	} else if keepRecentFull > 0 {
		candidates = nil
	}

	changed := 0
	for _, idx := range candidates {
		msg := c.messages[idx]
		if strings.HasPrefix(msg.Content, compactedToolResultPrefix) {
			continue
		}
		if len(msg.Content) <= maxChars && !containsSecretLikeLine(msg.Content) {
			continue
		}
		toolName := toolNames[msg.ToolCallID]
		if toolName == "" {
			toolName = "unknown"
		}
		c.messages[idx].Content = CompactToolResultContent(toolName, msg.Content, maxChars)
		changed++
	}
	return changed
}

func toolCallNames(messages []llm.Message) map[string]string {
	names := make(map[string]string)
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, call := range msg.ToolCalls {
			if call.ID != "" && call.Name != "" {
				names[call.ID] = call.Name
			}
		}
	}
	return names
}

func compactableToolResultIndexes(messages []llm.Message) []int {
	var out []int
	for i, msg := range messages {
		if msg.Role != "tool" || msg.ToolCallID == "" {
			continue
		}
		if hasLaterAssistantMessage(messages, i+1) {
			out = append(out, i)
		}
	}
	return out
}

func hasLaterAssistantMessage(messages []llm.Message, from int) bool {
	for i := from; i < len(messages); i++ {
		if messages[i].Role == "assistant" {
			return true
		}
	}
	return false
}

// CompactToolResultContent returns the same compact preview used for historical
// tool-result messages. It is intentionally small and redacts secret-shaped
// lines before truncating so API probes can exercise the production compactor.
func CompactToolResultContent(toolName, content string, maxChars int) string {
	previewBudget := maxChars - 96 - len(toolName)
	if previewBudget < 80 {
		previewBudget = 80
	}
	preview := redactToolResultPreview(content)
	if len(preview) > previewBudget {
		preview = truncateToolResultPreview(preview, previewBudget)
	}
	preview = strings.TrimSpace(preview)
	return fmt.Sprintf("%s\ntool: %s\noriginal_chars: %d\npreview:\n%s", compactedToolResultPrefix, toolName, len(content), preview)
}

func truncateToolResultPreview(content string, maxBytes int) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	marker := toolResultPreviewMarker(len(content))
	if maxBytes <= len(marker)+2 {
		return safePreviewHead(content, maxBytes)
	}
	for {
		remaining := maxBytes - len(marker)
		headBudget := remaining / 2
		tailBudget := remaining - headBudget
		head := safePreviewHead(content, headBudget)
		tail := safePreviewTail(content, tailBudget)
		nextMarker := toolResultPreviewMarker(len(content) - len(head) - len(tail))
		if len(nextMarker) == len(marker) {
			return head + nextMarker + tail
		}
		marker = nextMarker
	}
}

func toolResultPreviewMarker(omittedBytes int) string {
	if omittedBytes < 1 {
		omittedBytes = 1
	}
	return fmt.Sprintf("\n\n[... %d bytes omitted from tool preview ...]\n\n", omittedBytes)
}

func safePreviewHead(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}
	if cut == 0 {
		_, size := utf8.DecodeRuneInString(content)
		cut = size
	}
	return content[:cut]
}

func safePreviewTail(content string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(content) <= maxBytes {
		return content
	}
	start := len(content) - maxBytes
	for start < len(content) && !utf8.RuneStart(content[start]) {
		start++
	}
	return content[start:]
}

func redactToolResultPreview(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if lineLooksSecret(line) {
			lines[i] = "[redacted secret-like line]"
		}
	}
	return strings.Join(lines, "\n")
}

func containsSecretLikeLine(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if lineLooksSecret(line) {
			return true
		}
	}
	return false
}

func lineLooksSecret(line string) bool {
	lower := strings.ToLower(line)
	for _, marker := range []string{"api_key", "apikey", "token", "password", "authorization", "bearer "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
