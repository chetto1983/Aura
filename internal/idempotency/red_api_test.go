package idempotency

import (
	"encoding/json"
	"errors"
	"time"
)

// This test-only API shim keeps the RED commit compilable for the repository's
// pre-commit vet/lint hooks. Every behavior is deliberately wrong; the GREEN
// commit removes this file and supplies the production implementation.
type Scope string

const (
	ScopeHTTPMutation Scope = "http.mutation"
	ScopeCLICommand   Scope = "cli.command"
	ScopeAgentTool    Scope = "agent.tool"
	ScopeSchedulerRun Scope = "scheduler.run"
	ScopeApproval     Scope = "approval.resume"
	ScopeMCPTool      Scope = "mcp.tool"
)

type State string

const (
	StateInProgress    State = "in_progress"
	StateCompleted     State = "completed"
	StateIndeterminate State = "indeterminate"
)

type Decision string

const (
	DecisionAcquired      Decision = "acquired"
	DecisionReplay        Decision = "replay"
	DecisionInProgress    Decision = "in_progress"
	DecisionIndeterminate Decision = "indeterminate"
	DecisionConflict      Decision = "conflict"
)

const (
	MaxOperationKeyBytes  = 200
	MaxReplayBodyBytes    = 256 << 10
	MaxReplayPreviewBytes = 4 << 10
	MaxSidecarRefBytes    = 512
	MaxRetryAfter         = time.Minute
)

var (
	ErrConflict           = errors.New("idempotency conflict")
	ErrFingerprintPayload = errors.New("fingerprint payload is not JSON-marshalable")
)

type OperationKey struct {
	IdentityID string
	Scope      Scope
	Key        string
}

func (OperationKey) Validate() error { return nil }

func ParseState(raw string) (State, error) { return State(raw), nil }

func (State) CanTransitionTo(State) bool { return true }

type AuditLink struct {
	ConversationID string
	RequestID      string
	ToolCallID     string
}

func (AuditLink) Validate() error { return nil }

type ReplayResult struct {
	Body       json.RawMessage
	Preview    string
	SidecarRef string
	ExpiresAt  time.Time
}

func (ReplayResult) Validate() error { return nil }

type BeginRequest struct {
	Operation   OperationKey
	Fingerprint [32]byte
	Audit       *AuditLink
}

func (BeginRequest) Validate() error { return nil }

type BeginDecision struct {
	Decision   Decision
	Replay     *ReplayResult
	RetryAfter time.Duration
}

func (BeginDecision) Validate() error { return nil }

type CompleteRequest struct {
	Operation   OperationKey
	Fingerprint [32]byte
	Result      ReplayResult
}

func (CompleteRequest) Validate() error { return nil }

func FingerprintTyped(any) ([32]byte, error) { return [32]byte{}, nil }

func FingerprintHex([32]byte) string { return "" }
