package telegramadapter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	tgtelegram "github.com/aura/aura/internal/telegram"
	tele "gopkg.in/telebot.v4"
)

type askUserResumeInput struct {
	CallID            string
	Options           []string
	Kind              string
	HasPendingCall    bool
	Content           string
	SelectedOptionIDs []string
	Rejected          bool
	RejectMessage     string
}

func prepareAskUserResumeInput(userText string, messages []llm.Message, durableOptions []string, durableKind string, hasDurablePending bool) (askUserResumeInput, bool) {
	callID, options, kind, hasPendingCall := agent.PendingAskUserCall(messages)
	if !hasPendingCall && !hasDurablePending {
		return askUserResumeInput{}, false
	}
	if len(options) == 0 && len(durableOptions) > 0 {
		options = durableOptions
	}
	if kind == "" {
		kind = durableKind
	}
	options = askUserDisplayOptions(options, kind)
	content, rejected, rejectMsg := parseAskUserReply(userText, options)
	return askUserResumeInput{
		CallID:            callID,
		Options:           options,
		Kind:              kind,
		HasPendingCall:    hasPendingCall,
		Content:           content,
		SelectedOptionIDs: askUserSelectedOptionIDs(userText, options),
		Rejected:          rejected,
		RejectMessage:     rejectMsg,
	}, true
}

var canonicalApprovalOptions = []string{"approve_once", "approve_session", "approve_persist", "deny", "cancel"}

var approvalButtonLabels = map[string]string{
	"approve_once":    "Approve once",
	"approve_session": "Approve session",
	"approve_persist": "Approve persist",
	"deny":            "Deny",
	"cancel":          "Cancel",
}

// formatAskUserQuestion formats an ask_user question for Telegram delivery.
// With options: numbered list + free-text hint.
// Approval requests get canonical choices when the model omits options.
func formatAskUserQuestion(question string, options []string, kind string) string {
	question = strings.TrimSpace(question)
	if question == "" {
		return ""
	}
	options = askUserDisplayOptions(options, kind)
	if len(options) == 0 {
		return "❓ " + question + "\n\n(reply with text)"
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "❓ %s", question)
	for i, opt := range options {
		fmt.Fprintf(&sb, "\n\n%d. %s", i+1, opt)
	}
	sb.WriteString("\n\n(tap a button, reply with number, or text for free input)")
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

func askUserDisplayOptions(options []string, kind string) []string {
	if len(options) == 0 && kind == "approval" {
		return append([]string(nil), canonicalApprovalOptions...)
	}
	return append([]string(nil), options...)
}

func askUserQuestionMarkup(options []string, kind string) *tele.ReplyMarkup {
	options = askUserDisplayOptions(options, kind)
	if len(options) == 0 {
		return nil
	}
	markup := &tele.ReplyMarkup{}
	rows := make([]tele.Row, 0, len(options))
	for i, opt := range options {
		selection := strconv.Itoa(i + 1)
		rows = append(rows, markup.Row(markup.Data(askUserButtonLabel(selection, opt), tgtelegram.AskUserCallbackUnique, selection)))
	}
	markup.Inline(rows...)
	return markup
}

func askUserButtonLabel(selection string, option string) string {
	option = strings.TrimSpace(option)
	if label, ok := approvalButtonLabels[option]; ok {
		option = label
	}
	if option == "" {
		return selection
	}
	return truncateAskUserButtonLabel(selection + ". " + option)
}

func truncateAskUserButtonLabel(label string) string {
	const maxRunes = 48
	runes := []rune(strings.TrimSpace(label))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	const suffix = "..."
	keep := maxRunes - len([]rune(suffix))
	if keep <= 0 {
		return string(runes[:maxRunes])
	}
	return strings.TrimSpace(string(runes[:keep])) + suffix
}

func askUserSelectedOptionIDs(reply string, options []string) []string {
	reply = strings.TrimSpace(reply)
	if len(options) == 0 {
		return nil
	}
	n, err := strconv.Atoi(reply)
	if err != nil || n < 1 || n > len(options) {
		return nil
	}
	return []string{strconv.Itoa(n)}
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
