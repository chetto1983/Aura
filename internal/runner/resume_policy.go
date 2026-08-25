package runner

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/chetto1983/aura/internal/askuser"
)

const allowedDecisionsField = "allowed_decisions"

// ErrResumeDecisionNotAllowed reports a decision outside the pause's persisted policy.
var ErrResumeDecisionNotAllowed = errors.New("resume decision not allowed")

func allResumeDecisions() []string {
	return []string{askuser.ActionAccept, askuser.ActionDecline, askuser.ActionCancel}
}

// resumeContextWithDecisionPolicy records the server-authored per-pause policy in the
// existing resume_context object. The canonical action order makes persisted JSON stable,
// while preserving every caller-owned context field used by resume hooks.
func resumeContextWithDecisionPolicy(raw json.RawMessage, allowed []string) (json.RawMessage, error) {
	normalized, err := normalizeAllowedDecisions(allowed)
	if err != nil {
		return nil, err
	}
	fields := make(map[string]json.RawMessage)
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null")) {
		if err := json.Unmarshal(trimmed, &fields); err != nil || fields == nil {
			return nil, fmt.Errorf("approval resume_context must be a JSON object")
		}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal approval decision policy: %w", err)
	}
	fields[allowedDecisionsField] = encoded
	out, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("marshal approval resume_context: %w", err)
	}
	return out, nil
}

func normalizeAllowedDecisions(allowed []string) ([]string, error) {
	selected := make(map[string]bool, len(allowed))
	for _, action := range allowed {
		if !knownResumeAction(action) {
			return nil, fmt.Errorf("invalid approval decision %q", action)
		}
		selected[action] = true
	}
	normalized := make([]string, 0, len(selected))
	for _, action := range allResumeDecisions() {
		if selected[action] {
			normalized = append(normalized, action)
		}
	}
	return normalized, nil
}

func validatePendingResumeDecision(pending askuser.Pending, action string) error {
	if !knownResumeAction(action) {
		return fmt.Errorf("%w: action %q must be accept|decline|cancel", askuser.ErrInvalidAnswer, action)
	}
	allowed, err := persistedAllowedDecisions(pending.ResumeContext)
	if err != nil {
		return fmt.Errorf("%w: invalid persisted policy", ErrResumeDecisionNotAllowed)
	}
	if slices.Contains(allowed, action) {
		return nil
	}
	return fmt.Errorf("%w: action %q", ErrResumeDecisionNotAllowed, action)
}

func persistedAllowedDecisions(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("allowed_decisions is required")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, errors.New("resume_context must be an object")
	}
	encoded, present := fields[allowedDecisionsField]
	if !present {
		return nil, errors.New("allowed_decisions is required")
	}
	var allowed []string
	if err := json.Unmarshal(encoded, &allowed); err != nil || allowed == nil {
		return nil, errors.New("allowed_decisions must be an array")
	}
	normalized, err := normalizeAllowedDecisions(allowed)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func knownResumeAction(action string) bool {
	switch action {
	case askuser.ActionAccept, askuser.ActionDecline, askuser.ActionCancel:
		return true
	default:
		return false
	}
}
