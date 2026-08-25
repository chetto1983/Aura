package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

// ErrResumeDecisionNotAllowed is the HTTP-boundary classification for a decision
// excluded by the server-authored policy persisted with the pause.
var ErrResumeDecisionNotAllowed = runner.ErrResumeDecisionNotAllowed

// server_project.go holds the pure wire-shape mapping helpers split out of server.go
// (refactor-on-touch / ≤600 LOC, CLAUDE.md): the AG-UI Resume[]→Runner resume mapping, the
// last-user-message extraction, and the persisted-history→MESSAGES_SNAPSHOT projection +
// the RUN_ERROR redaction belt-and-suspenders. No behavior change from the prior in-file
// definitions; they are package-private and referenced only from server.go.

// resumeAnswers maps the AG-UI protocol-native Resume[] onto the Runner's three-action
// resume model and rejects malformed answers before they cross the wire boundary.
func resumeAnswers(entries []types.ResumeEntry) (map[string]runner.ResponseInput, error) {
	out := make(map[string]runner.ResponseInput, len(entries))
	for _, e := range entries {
		action := askuser.ActionAccept
		if e.Status == types.ResumeStatusCancelled {
			action = askuser.ActionCancel
		}
		answer := runner.ResponseInput{Action: action, Content: payloadString(e.Payload)}
		if err := askuser.ValidateResumeAnswer(askuser.ResumeAnswer(answer)); err != nil {
			return nil, fmt.Errorf("resume %q: %w", e.InterruptID, err)
		}
		out[e.InterruptID] = answer
	}
	return out, nil
}

func (s *Server) validateResumeEntries(ctx context.Context, entries []types.ResumeEntry) (map[string]runner.ResponseInput, error) {
	answers, err := resumeAnswers(entries)
	if err != nil {
		return nil, err
	}
	if err := s.run.ValidateResumeAnswers(ctx, answers); err != nil {
		return nil, err
	}
	return answers, nil
}

func resumeErrorStatus(err error) int {
	if errors.Is(err, ErrResumeDecisionNotAllowed) {
		return http.StatusForbidden
	}
	return http.StatusBadRequest
}

// payloadString renders a resume payload as the answer content: a string payload is
// used verbatim; any other shape is JSON-encoded so structured answers survive. A nil
// payload yields the empty string.
func payloadString(payload any) string {
	switch v := payload.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// lastUserMessage extracts the final user message from the RunAgentInput history to
// drive the turn (OQ3). It returns nil when there is no user message (a resume-only
// run continues over the rehydrated history without a fresh user turn).
func lastUserMessage(msgs []types.Message) (*string, error) {
	for _, v := range slices.Backward(msgs) {
		if string(v.Role) != llm.RoleUser {
			continue
		}
		if content, ok := v.Content.(string); ok {
			if content != "" {
				return &content, nil
			}
			continue
		}
		if v.Content != nil {
			// The runner currently accepts only text. Reject structured multimodal
			// user content explicitly instead of silently replaying old history.
			return nil, errUnsupportedUserMessageContent
		}
	}
	return nil, nil
}

// msgID is the stable 1-based snapshot message id, shared by the plain projection and
// the D-06 display-aware projection so both number messages identically.
func msgID(i int) string { return fmt.Sprintf("msg-%d", i+1) }

// redactEvent is the server-side belt-and-suspenders for T-12-10: the pure translator
// forwards a runner error as a RUN_ERROR event carrying the raw err.Error() string. The
// server sanitizes that message in-flight (before it reaches the wire) so a tool/infra
// error embedding a DSN/key never leaks, without reaching into the boundary-tested
// translator. Non-RUN_ERROR events pass through unchanged.
func redactEvent(ev events.Event) events.Event {
	re, ok := ev.(*events.RunErrorEvent)
	if !ok {
		return ev
	}
	re.Message = SanitizeString(re.Message)
	return re
}
