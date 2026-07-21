package idempotency

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	testIdentityID     = "00000000-0000-0000-0000-000000000001"
	testConversationID = "00000000-0000-0000-0000-000000000002"
	testRequestID      = "00000000-0000-0000-0000-000000000003"
)

func TestOperationKeyValidation(t *testing.T) {
	t.Parallel()

	valid := OperationKey{IdentityID: testIdentityID, Scope: ScopeAgentTool, Key: "caller-key-1"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid OperationKey: %v", err)
	}

	tests := []struct {
		name string
		key  OperationKey
	}{
		{name: "blank identity", key: OperationKey{Scope: ScopeAgentTool, Key: "key"}},
		{name: "malformed identity", key: OperationKey{IdentityID: "identity-1", Scope: ScopeAgentTool, Key: "key"}},
		{name: "blank scope", key: OperationKey{IdentityID: testIdentityID, Key: "key"}},
		{name: "scope outside registry", key: OperationKey{IdentityID: testIdentityID, Scope: Scope("agent.dynamic"), Key: "key"}},
		{name: "blank key", key: OperationKey{IdentityID: testIdentityID, Scope: ScopeAgentTool}},
		{name: "whitespace key", key: OperationKey{IdentityID: testIdentityID, Scope: ScopeAgentTool, Key: " \t "}},
		{name: "control character", key: OperationKey{IdentityID: testIdentityID, Scope: ScopeAgentTool, Key: "key\nvalue"}},
		{name: "over 200 bytes", key: OperationKey{IdentityID: testIdentityID, Scope: ScopeAgentTool, Key: strings.Repeat("k", MaxOperationKeyBytes+1)}},
		{name: "multibyte over 200 bytes", key: OperationKey{IdentityID: testIdentityID, Scope: ScopeAgentTool, Key: strings.Repeat("é", 101)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.key.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestOperationScopesAreFinite(t *testing.T) {
	t.Parallel()

	valid := []Scope{
		ScopeHTTPMutation,
		ScopeCLICommand,
		ScopeAgentTool,
		ScopeSchedulerRun,
		ScopeApproval,
		ScopeMCPTool,
	}
	for _, scope := range valid {
		key := OperationKey{IdentityID: testIdentityID, Scope: scope, Key: "key"}
		if err := key.Validate(); err != nil {
			t.Errorf("scope %q rejected: %v", scope, err)
		}
	}
}

func TestOperationStateParsingAndTransitions(t *testing.T) {
	t.Parallel()

	for _, want := range []State{StateInProgress, StateCompleted, StateIndeterminate} {
		got, err := ParseState(string(want))
		if err != nil {
			t.Fatalf("ParseState(%q): %v", want, err)
		}
		if got != want {
			t.Fatalf("ParseState(%q) = %q, want %q", want, got, want)
		}
	}
	for _, raw := range []string{"", "completed ", "COMPLETED", "failed", "acquired"} {
		if _, err := ParseState(raw); err == nil {
			t.Errorf("ParseState(%q) error = nil, want rejection", raw)
		}
	}

	tests := []struct {
		name string
		from State
		to   State
		want bool
	}{
		{name: "complete acquired work", from: StateInProgress, to: StateCompleted, want: true},
		{name: "mark ambiguous work", from: StateInProgress, to: StateIndeterminate, want: true},
		{name: "completed is terminal", from: StateCompleted, to: StateIndeterminate},
		{name: "indeterminate is terminal", from: StateIndeterminate, to: StateCompleted},
		{name: "cannot reacquire indeterminate", from: StateIndeterminate, to: StateInProgress},
		{name: "no self transition", from: StateInProgress, to: StateInProgress},
		{name: "unknown source", from: State("unknown"), to: StateCompleted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.from.CanTransitionTo(tt.to); got != tt.want {
				t.Fatalf("CanTransitionTo(%q, %q) = %v, want %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestOperationAuditLinkValidation(t *testing.T) {
	t.Parallel()

	valid := AuditLink{ConversationID: testConversationID, RequestID: testRequestID, ToolCallID: "call-1"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid AuditLink: %v", err)
	}

	tests := []struct {
		name string
		link AuditLink
	}{
		{name: "missing conversation", link: AuditLink{RequestID: testRequestID, ToolCallID: "call-1"}},
		{name: "malformed conversation", link: AuditLink{ConversationID: "bad", RequestID: testRequestID, ToolCallID: "call-1"}},
		{name: "missing request", link: AuditLink{ConversationID: testConversationID, ToolCallID: "call-1"}},
		{name: "malformed request", link: AuditLink{ConversationID: testConversationID, RequestID: "bad", ToolCallID: "call-1"}},
		{name: "missing tool call", link: AuditLink{ConversationID: testConversationID, RequestID: testRequestID}},
		{name: "control in tool call", link: AuditLink{ConversationID: testConversationID, RequestID: testRequestID, ToolCallID: "call\r1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.link.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestOperationReplayResultBounds(t *testing.T) {
	t.Parallel()

	expiresAt := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	valid := ReplayResult{
		Body:       json.RawMessage(`{"status":"ok"}`),
		Preview:    "created",
		SidecarRef: "tool-results/sidecar-1",
		StatusCode: 202,
		Headers:    map[string]string{"Location": "/operations/one"},
		ExpiresAt:  expiresAt,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid ReplayResult: %v", err)
	}

	tests := []struct {
		name   string
		result ReplayResult
	}{
		{name: "invalid JSON", result: ReplayResult{Body: json.RawMessage(`{"status":`), ExpiresAt: expiresAt}},
		{name: "oversized JSON", result: ReplayResult{Body: json.RawMessage(`"` + strings.Repeat("x", MaxReplayBodyBytes) + `"`), ExpiresAt: expiresAt}},
		{name: "oversized preview", result: ReplayResult{Preview: strings.Repeat("p", MaxReplayPreviewBytes+1), ExpiresAt: expiresAt}},
		{name: "preview control", result: ReplayResult{Preview: "created\x00", ExpiresAt: expiresAt}},
		{name: "oversized sidecar ref", result: ReplayResult{SidecarRef: strings.Repeat("s", MaxSidecarRefBytes+1), ExpiresAt: expiresAt}},
		{name: "sidecar control", result: ReplayResult{SidecarRef: "sidecar\nref", ExpiresAt: expiresAt}},
		{name: "missing expiry", result: ReplayResult{Body: json.RawMessage(`{}`)}},
		{name: "invalid status", result: ReplayResult{StatusCode: 99, ExpiresAt: expiresAt}},
		{name: "unapproved header", result: ReplayResult{StatusCode: 200, Headers: map[string]string{"Set-Cookie": "secret"}, ExpiresAt: expiresAt}},
		{name: "empty result", result: ReplayResult{ExpiresAt: expiresAt}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := tt.result.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want rejection")
			}
		})
	}
}

func TestOperationBeginDecisionValidation(t *testing.T) {
	t.Parallel()

	replay := ReplayResult{Body: json.RawMessage(`{"status":"ok"}`), ExpiresAt: time.Now().UTC().Add(time.Hour)}
	valid := []BeginDecision{
		{Decision: DecisionAcquired},
		{Decision: DecisionReplay, Replay: &replay},
		{Decision: DecisionInProgress, RetryAfter: time.Second},
		{Decision: DecisionIndeterminate},
		{Decision: DecisionConflict},
		{Decision: DecisionResultExpired},
	}
	for _, decision := range valid {
		if err := decision.Validate(); err != nil {
			t.Errorf("valid decision %q: %v", decision.Decision, err)
		}
	}

	tests := []BeginDecision{
		{},
		{Decision: Decision("unknown")},
		{Decision: DecisionReplay},
		{Decision: DecisionAcquired, RetryAfter: time.Second},
		{Decision: DecisionInProgress},
		{Decision: DecisionInProgress, RetryAfter: MaxRetryAfter + time.Nanosecond},
		{Decision: DecisionConflict, Replay: &replay},
	}
	for _, decision := range tests {
		if err := decision.Validate(); err == nil {
			t.Errorf("decision %+v Validate() error = nil, want rejection", decision)
		}
	}
}

func TestOperationRequestsValidateNestedContracts(t *testing.T) {
	t.Parallel()

	fingerprint := [32]byte{1}
	key := OperationKey{IdentityID: testIdentityID, Scope: ScopeAgentTool, Key: "caller-key"}
	audit := AuditLink{ConversationID: testConversationID, RequestID: testRequestID, ToolCallID: "call-1"}
	replay := ReplayResult{Body: json.RawMessage(`{"status":"ok"}`), ExpiresAt: time.Now().UTC().Add(time.Hour)}

	if err := (BeginRequest{Operation: key, Fingerprint: fingerprint, Audit: &audit}).Validate(); err != nil {
		t.Fatalf("valid BeginRequest: %v", err)
	}
	if err := (CompleteRequest{Operation: key, Fingerprint: fingerprint, Result: replay}).Validate(); err != nil {
		t.Fatalf("valid CompleteRequest: %v", err)
	}

	if err := (BeginRequest{Operation: key}).Validate(); err == nil {
		t.Error("zero BeginRequest fingerprint accepted")
	}
	if err := (BeginRequest{Operation: key, Fingerprint: fingerprint, Audit: &AuditLink{}}).Validate(); err == nil {
		t.Error("empty non-nil AuditLink accepted")
	}
	if err := (CompleteRequest{Operation: key, Fingerprint: fingerprint}).Validate(); err == nil {
		t.Error("invalid CompleteRequest result accepted")
	}
	if !errors.Is(ErrConflict, ErrConflict) {
		t.Error("ErrConflict must support errors.Is")
	}
}
