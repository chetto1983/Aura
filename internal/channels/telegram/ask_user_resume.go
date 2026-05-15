package telegramadapter

import (
	"fmt"
	"strconv"
	"strings"
)

// formatAskUserQuestion formats an ask_user question for Telegram delivery.
// With options: numbered list + free-text hint.
// Without options (approval or open-ended): plain text prompt.
func formatAskUserQuestion(question string, options []string, _ string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		return ""
	}
	if len(options) == 0 {
		return "❓ " + question + "\n\n(reply with text)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "❓ %s", question)
	for i, opt := range options {
		fmt.Fprintf(&sb, "\n\n%d. %s", i+1, opt)
	}
	sb.WriteString("\n\n(reply with number, or text for free input)")
	return sb.String()
}

// parseAskUserReply interprets the user's textual reply to an ask_user question.
//   - Numeric 1..N → options[N-1] as the resolved content.
//   - Numeric out-of-range or zero → rejected; caller should send rejectMsg back.
//   - Non-numeric (including empty) → use raw text as the resolved content.
//
// rejected=true means the pending question is NOT consumed; the caller must
// send rejectMsg to the user and keep the run in waiting_for_user state.
func parseAskUserReply(reply string, options []string) (content string, rejected bool, rejectMsg string) {
	reply = strings.TrimSpace(reply)
	if len(options) == 0 {
		return reply, false, ""
	}
	n, err := strconv.Atoi(reply)
	if err != nil {
		// Non-numeric → free-text content.
		return reply, false, ""
	}
	if n < 1 || n > len(options) {
		return "", true, fmt.Sprintf("please reply with 1..%d or free text", len(options))
	}
	return options[n-1], false, ""
}

// extractStringSlice converts an any value (as stored in event payloads) to
// []string. Handles []string and []any (JSON-decoded arrays).
func extractStringSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		out := make([]string, 0, len(s))
		for _, item := range s {
			if str, ok := item.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}
