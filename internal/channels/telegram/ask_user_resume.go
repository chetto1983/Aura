package telegramadapter

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/channels/askuser"
	"github.com/aura/aura/internal/llm"
	tgtelegram "github.com/aura/aura/internal/telegram"
	tele "gopkg.in/telebot.v4"
)

type askUserResumeInput = askuser.ResumeInput

func prepareAskUserResumeInput(userText string, messages []llm.Message, durableOptions []string, durableKind string, hasDurablePending bool) (askUserResumeInput, bool) {
	return askuser.PrepareResumeInput(userText, messages, durableOptions, durableKind, hasDurablePending)
}

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
	return askuser.ParseReply(reply, options)
}

func askUserDisplayOptions(options []string, kind string) []string {
	return askuser.DisplayOptions(options, kind)
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
	return askuser.SelectedOptionIDs(reply, options)
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
