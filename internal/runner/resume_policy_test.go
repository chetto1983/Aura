package runner

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/askuser"
)

func TestValidatePendingResumeDecision(t *testing.T) {
	tests := []struct {
		name   string
		ctx    string
		action string
		want   error
	}{
		{name: "missing policy fails closed", ctx: `{"type":"approval"}`, action: askuser.ActionAccept, want: ErrResumeDecisionNotAllowed},
		{name: "non-object context fails closed", ctx: `"invalid"`, action: askuser.ActionDecline, want: ErrResumeDecisionNotAllowed},
		{name: "restricted policy permits decline", ctx: `{"allowed_decisions":["decline"]}`, action: askuser.ActionDecline},
		{name: "restricted policy rejects accept", ctx: `{"allowed_decisions":["decline"]}`, action: askuser.ActionAccept, want: ErrResumeDecisionNotAllowed},
		{name: "empty policy rejects cancel", ctx: `{"allowed_decisions":[]}`, action: askuser.ActionCancel, want: ErrResumeDecisionNotAllowed},
		{name: "null policy fails closed", ctx: `{"allowed_decisions":null}`, action: askuser.ActionDecline, want: ErrResumeDecisionNotAllowed},
		{name: "unknown persisted decision fails closed", ctx: `{"allowed_decisions":["approve"]}`, action: askuser.ActionAccept, want: ErrResumeDecisionNotAllowed},
		{name: "unknown submitted action is invalid", ctx: `{}`, action: "approve", want: askuser.ErrInvalidAnswer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePendingResumeDecision(askuser.Pending{ResumeContext: json.RawMessage(tc.ctx)}, tc.action)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("validatePendingResumeDecision() error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidatePendingResumeDecisionReportsInvalidPersistedPolicy(t *testing.T) {
	err := validatePendingResumeDecision(
		askuser.Pending{ResumeContext: json.RawMessage(`{"type":"approval"}`)},
		askuser.ActionDecline,
	)
	const want = "resume decision not allowed: invalid persisted policy"
	if err == nil || err.Error() != want {
		t.Fatalf("validatePendingResumeDecision() error = %v, want %q", err, want)
	}
}

func TestResumeContextWithDecisionPolicy(t *testing.T) {
	raw, err := resumeContextWithDecisionPolicy(
		json.RawMessage(`{"type":"scheduled","nonce":"keep"}`),
		[]string{askuser.ActionCancel, askuser.ActionDecline, askuser.ActionDecline},
	)
	if err != nil {
		t.Fatalf("resumeContextWithDecisionPolicy: %v", err)
	}
	var got struct {
		Type             string   `json:"type"`
		Nonce            string   `json:"nonce"`
		AllowedDecisions []string `json:"allowed_decisions"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode policy context: %v", err)
	}
	if got.Type != "scheduled" || got.Nonce != "keep" {
		t.Fatalf("context fields not preserved: %s", raw)
	}
	if want := []string{askuser.ActionDecline, askuser.ActionCancel}; !slices.Equal(got.AllowedDecisions, want) {
		t.Fatalf("allowed_decisions = %v, want %v", got.AllowedDecisions, want)
	}
}

func TestResumeContextWithDecisionPolicyRejectsInvalidInput(t *testing.T) {
	if _, err := resumeContextWithDecisionPolicy(json.RawMessage(`[]`), allResumeDecisions()); err == nil {
		t.Fatal("non-object resume_context must be rejected at mint")
	}
	if _, err := resumeContextWithDecisionPolicy(nil, []string{"approve"}); err == nil {
		t.Fatal("unknown decision must be rejected at mint")
	}
}

func TestPersistedAllowedDecisions(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []string
		wantErr string
	}{
		{name: "empty", wantErr: "allowed_decisions is required"},
		{name: "null context", raw: `null`, wantErr: "allowed_decisions is required"},
		{name: "non-object", raw: `"invalid"`, wantErr: "resume_context must be an object"},
		{name: "missing field", raw: `{}`, wantErr: "allowed_decisions is required"},
		{name: "null field", raw: `{"allowed_decisions":null}`, wantErr: "allowed_decisions must be an array"},
		{name: "wrong field type", raw: `{"allowed_decisions":"decline"}`, wantErr: "allowed_decisions must be an array"},
		{name: "unknown decision", raw: `{"allowed_decisions":["approve"]}`, wantErr: `invalid approval decision "approve"`},
		{
			name: "canonicalizes and deduplicates",
			raw:  `{"allowed_decisions":["cancel","decline","cancel"]}`,
			want: []string{askuser.ActionDecline, askuser.ActionCancel},
		},
		{name: "explicit empty policy", raw: `{"allowed_decisions":[]}`, want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := persistedAllowedDecisions(json.RawMessage(tc.raw))
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("persistedAllowedDecisions() error = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("persistedAllowedDecisions() error = %v", err)
			}
			if got == nil || !slices.Equal(got, tc.want) {
				t.Fatalf("persistedAllowedDecisions() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestKnownResumeAction(t *testing.T) {
	for _, action := range allResumeDecisions() {
		if !knownResumeAction(action) {
			t.Errorf("knownResumeAction(%q) = false", action)
		}
	}
	for _, action := range []string{"", "approve", "ACCEPT", " decline "} {
		if knownResumeAction(action) {
			t.Errorf("knownResumeAction(%q) = true", action)
		}
	}
}
