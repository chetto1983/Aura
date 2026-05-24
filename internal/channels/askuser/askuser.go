package askuser

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
)

// ResumeInput is the channel-neutral interpretation of a user's answer to an
// outstanding ask_user call.
type ResumeInput struct {
	CallID            string
	Options           []string
	Kind              string
	HasPendingCall    bool
	Content           string
	SelectedOptionIDs []string
	Rejected          bool
	RejectMessage     string
}

// PrepareResumeInput resolves userText against the pending ask_user call in
// messages and, when present, durable question state from chat_questions.
func PrepareResumeInput(userText string, messages []llm.Message, durableOptions []string, durableKind string, hasDurablePending bool) (ResumeInput, bool) {
	callID, options, kind, hasPendingCall := agent.PendingAskUserCall(messages)
	if !hasPendingCall && !hasDurablePending {
		return ResumeInput{}, false
	}
	if len(options) == 0 && len(durableOptions) > 0 {
		options = durableOptions
	}
	if kind == "" {
		kind = durableKind
	}
	options = DisplayOptions(options, kind)
	content, rejected, rejectMsg := ParseReply(userText, options)
	return ResumeInput{
		CallID:            callID,
		Options:           options,
		Kind:              kind,
		HasPendingCall:    hasPendingCall,
		Content:           content,
		SelectedOptionIDs: SelectedOptionIDs(userText, options),
		Rejected:          rejected,
		RejectMessage:     rejectMsg,
	}, true
}

var canonicalApprovalOptions = []string{"approve_once", "approve_session", "approve_persist", "deny", "cancel"}

// DisplayOptions returns the concrete options shown to the user. Approval
// questions get canonical choices even when the model omitted options.
func DisplayOptions(options []string, kind string) []string {
	if len(options) == 0 && kind == "approval" {
		return append([]string(nil), canonicalApprovalOptions...)
	}
	return append([]string(nil), options...)
}

// ParseReply interprets a textual reply. Numeric 1..N maps to that option;
// an out-of-range number is rejected so the question remains pending.
func ParseReply(reply string, options []string) (content string, rejected bool, rejectMsg string) {
	reply = strings.TrimSpace(reply)
	if len(options) == 0 {
		return reply, false, ""
	}
	n, err := strconv.Atoi(reply)
	if err != nil {
		return reply, false, ""
	}
	if n < 1 || n > len(options) {
		return "", true, fmt.Sprintf("please reply with 1..%d or free text", len(options))
	}
	return options[n-1], false, ""
}

// SelectedOptionIDs returns the stable selection index used by chat_questions.
func SelectedOptionIDs(reply string, options []string) []string {
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
