package idempotency

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/textproto"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// MaxOperationKeyBytes and the replay bounds are shared by ingress, store,
// and schema validation so every boundary applies the same limits.
const (
	MaxOperationKeyBytes    = 200
	MaxReplayBodyBytes      = 256 << 10
	MaxReplayPreviewBytes   = 4 << 10
	MaxSidecarRefBytes      = 512
	MaxReplayHeadersBytes   = 4 << 10
	MaxAuditToolCallIDBytes = 200
	MaxRetryAfter           = time.Minute
)

// ErrConflict and the other sentinels classify public operation outcomes and
// conditional-transition failures without embedding caller-controlled data.
var (
	ErrConflict           = errors.New("idempotency conflict")
	ErrInProgress         = errors.New("idempotency operation in progress")
	ErrIndeterminate      = errors.New("idempotency operation indeterminate")
	ErrStaleTransition    = errors.New("idempotency stale transition")
	ErrFingerprintPayload = errors.New("fingerprint payload is not JSON-marshalable")
)

// Scope is an Aura-owned operation namespace. The finite registry prevents
// model, remote-server, or payload data from inventing trusted scopes.
type Scope string

// ScopeHTTPMutation and the other constants are the complete trusted scope
// registry. Adapters choose a scope; callers cannot create one.
const (
	ScopeHTTPMutation Scope = "http.mutation"
	ScopeCLICommand   Scope = "cli.command"
	ScopeAgentTool    Scope = "agent.tool"
	ScopeSchedulerRun Scope = "scheduler.run"
	ScopeApproval     Scope = "approval.resume"
	ScopeMCPTool      Scope = "mcp.tool"
)

var validScopes = map[Scope]struct{}{
	ScopeHTTPMutation: {},
	ScopeCLICommand:   {},
	ScopeAgentTool:    {},
	ScopeSchedulerRun: {},
	ScopeApproval:     {},
	ScopeMCPTool:      {},
}

// Validate rejects scopes outside Aura's finite registry.
func (s Scope) Validate() error {
	if _, ok := validScopes[s]; !ok {
		return fmt.Errorf("invalid idempotency operation scope")
	}
	return nil
}

// State is the durable lifecycle of a public operation. Completed, rejected,
// and indeterminate are terminal; ambiguous work is never reacquired.
type State string

// StateInProgress and the terminal states are the only durable registry states.
const (
	StateInProgress    State = "in_progress"
	StateCompleted     State = "completed"
	StateIndeterminate State = "indeterminate"
	StateRejected      State = "rejected"
)

// ParseState accepts only the exact durable state vocabulary.
func ParseState(raw string) (State, error) {
	state := State(raw)
	switch state {
	case StateInProgress, StateCompleted, StateIndeterminate, StateRejected:
		return state, nil
	default:
		return "", fmt.Errorf("invalid idempotency operation state")
	}
}

// CanTransitionTo permits only an owned in-progress operation to become terminal.
func (s State) CanTransitionTo(next State) bool {
	return s == StateInProgress &&
		(next == StateCompleted || next == StateIndeterminate || next == StateRejected)
}

// Decision describes what a caller may do after Begin. Only acquired permits
// the external effect; all other values are observational outcomes.
type Decision string

// ClaimToken is the durable fencing generation assigned to acquired work.
// Terminal transitions must present the exact token returned by acquisition.
type ClaimToken int64

// DecisionAcquired and the other decisions are the complete Begin outcome set.
const (
	DecisionAcquired      Decision = "acquired"
	DecisionReplay        Decision = "replay"
	DecisionResultExpired Decision = "completed_result_expired"
	DecisionInProgress    Decision = "in_progress"
	DecisionIndeterminate Decision = "indeterminate"
	DecisionRejected      Decision = "rejected"
	DecisionConflict      Decision = "conflict"
)

// OperationKey is the trusted identity-scoped caller mutation identity.
type OperationKey struct {
	IdentityID string
	Scope      Scope
	Key        string
}

// Validate checks identity, scope membership, and bounded caller key syntax.
func (k OperationKey) Validate() error {
	if _, err := uuid.Parse(k.IdentityID); err != nil {
		return fmt.Errorf("invalid idempotency identity")
	}
	if err := k.Scope.Validate(); err != nil {
		return err
	}
	if err := validateBoundedText(k.Key, MaxOperationKeyBytes); err != nil {
		return fmt.Errorf("invalid idempotency operation key: %w", err)
	}
	return nil
}

// AuditLink is optional correlation to the append-only Phase 35 execution
// tuple. It supplements public-operation ownership and never replaces it.
type AuditLink struct {
	ConversationID string
	RequestID      string
	ToolCallID     string
}

// Validate requires the complete, well-formed Phase 35 audit tuple.
func (a AuditLink) Validate() error {
	if _, err := uuid.Parse(a.ConversationID); err != nil {
		return fmt.Errorf("invalid idempotency audit conversation")
	}
	if _, err := uuid.Parse(a.RequestID); err != nil {
		return fmt.Errorf("invalid idempotency audit request")
	}
	if err := validateBoundedText(a.ToolCallID, MaxAuditToolCallIDBytes); err != nil {
		return fmt.Errorf("invalid idempotency audit tool call: %w", err)
	}
	return nil
}

// ReplayResult is the bounded replay material retained independently from the
// operation/audit metadata. Body is typed JSON and SidecarRef is an already
// trusted internal reference, never a caller-provided path.
type ReplayResult struct {
	Body       json.RawMessage
	Preview    string
	SidecarRef string
	StatusCode int
	Headers    map[string]string
	ExpiresAt  time.Time
}

var replayHeaderAllowlist = map[string]struct{}{
	"Content-Disposition": {},
	"Content-Type":        {},
	"Etag":                {},
	"Location":            {},
	"Retry-After":         {},
}

// ReplayHeaderAllowed reports whether a response header is safe and useful to
// persist in the bounded replay envelope.
func ReplayHeaderAllowed(name string) bool {
	_, ok := replayHeaderAllowlist[textproto.CanonicalMIMEHeaderKey(name)]
	return ok
}

// Validate enforces the replay-body, preview, sidecar, and expiry contract.
func (r ReplayResult) Validate() error {
	if r.ExpiresAt.IsZero() {
		return fmt.Errorf("idempotency replay expiry is required")
	}
	if len(r.Body) > MaxReplayBodyBytes {
		return fmt.Errorf("idempotency replay body exceeds limit")
	}
	if len(r.Body) > 0 && !json.Valid(r.Body) {
		return fmt.Errorf("idempotency replay body is not valid JSON")
	}
	if r.StatusCode != 0 && (r.StatusCode < 100 || r.StatusCode > 599) {
		return fmt.Errorf("idempotency replay status is invalid")
	}
	for name, value := range r.Headers {
		if !ReplayHeaderAllowed(name) || textproto.CanonicalMIMEHeaderKey(name) != name {
			return fmt.Errorf("idempotency replay header is not allowlisted")
		}
		if err := validateOptionalBoundedText(value, MaxReplayHeadersBytes); err != nil {
			return fmt.Errorf("invalid idempotency replay header: %w", err)
		}
	}
	encodedHeaders, err := json.Marshal(r.Headers)
	if err != nil || len(encodedHeaders) > MaxReplayHeadersBytes {
		return fmt.Errorf("idempotency replay headers exceed limit")
	}
	if err := validateReplayPreview(r.Preview, MaxReplayPreviewBytes); err != nil {
		return fmt.Errorf("invalid idempotency replay preview: %w", err)
	}
	if err := validateOptionalBoundedText(r.SidecarRef, MaxSidecarRefBytes); err != nil {
		return fmt.Errorf("invalid idempotency replay sidecar reference: %w", err)
	}
	if len(r.Body) == 0 && r.Preview == "" && r.SidecarRef == "" && r.StatusCode == 0 && len(r.Headers) == 0 {
		return fmt.Errorf("idempotency replay result is empty")
	}
	return nil
}

// BeginRequest carries a validated operation intent into atomic acquisition.
type BeginRequest struct {
	Operation   OperationKey
	Fingerprint [32]byte
	Audit       *AuditLink
}

// Validate checks the operation, fingerprint, and optional audit linkage.
func (r BeginRequest) Validate() error {
	if err := r.Operation.Validate(); err != nil {
		return err
	}
	if isZeroFingerprint(r.Fingerprint) {
		return fmt.Errorf("idempotency payload fingerprint is required")
	}
	if r.Audit != nil {
		if err := r.Audit.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// BeginDecision is the typed result of an acquisition attempt.
type BeginDecision struct {
	Decision   Decision
	ClaimToken ClaimToken
	Replay     *ReplayResult
	RetryAfter time.Duration
}

// Validate rejects decision metadata inconsistent with its variant.
func (d BeginDecision) Validate() error {
	switch d.Decision {
	case DecisionAcquired:
		if d.ClaimToken <= 0 || d.Replay != nil || d.RetryAfter != 0 {
			return fmt.Errorf("idempotency acquired decision is incomplete")
		}
	case DecisionIndeterminate, DecisionConflict, DecisionResultExpired:
		if d.ClaimToken != 0 || d.Replay != nil || d.RetryAfter != 0 {
			return fmt.Errorf("idempotency decision carries incompatible metadata")
		}
	case DecisionRejected:
		if d.ClaimToken != 0 || d.RetryAfter != 0 {
			return fmt.Errorf("idempotency rejected decision carries incompatible metadata")
		}
		if d.Replay != nil {
			if err := d.Replay.Validate(); err != nil {
				return err
			}
		}
	case DecisionReplay:
		if d.ClaimToken != 0 || d.Replay == nil || d.RetryAfter != 0 {
			return fmt.Errorf("idempotency replay decision is incomplete")
		}
		if err := d.Replay.Validate(); err != nil {
			return err
		}
	case DecisionInProgress:
		if d.ClaimToken != 0 || d.Replay != nil || d.RetryAfter <= 0 || d.RetryAfter > MaxRetryAfter {
			return fmt.Errorf("idempotency in-progress retry guidance is invalid")
		}
	default:
		return fmt.Errorf("invalid idempotency begin decision")
	}
	return nil
}

// CompleteRequest carries the owned operation fingerprint and bounded replay result.
type CompleteRequest struct {
	Operation   OperationKey
	Fingerprint [32]byte
	ClaimToken  ClaimToken
	Result      ReplayResult
}

// Validate checks every completion field before persistence.
func (r CompleteRequest) Validate() error {
	if err := r.Operation.Validate(); err != nil {
		return err
	}
	if isZeroFingerprint(r.Fingerprint) {
		return fmt.Errorf("idempotency payload fingerprint is required")
	}
	if r.ClaimToken <= 0 {
		return fmt.Errorf("idempotency claim token is required")
	}
	return r.Result.Validate()
}

// RejectRequest records a deterministic no-effect error for safe error replay.
type RejectRequest struct {
	Operation   OperationKey
	Fingerprint [32]byte
	ClaimToken  ClaimToken
	Result      ReplayResult
}

// Validate applies the same ownership, fencing, and bounded replay contract as
// completion while retaining a distinct terminal state.
func (r RejectRequest) Validate() error {
	return CompleteRequest(r).Validate()
}

func validateBoundedText(value string, maxBytes int) error {
	if len(value) == 0 || len(bytes.TrimSpace([]byte(value))) == 0 {
		return fmt.Errorf("value is blank")
	}
	return validateOptionalBoundedText(value, maxBytes)
}

// validateReplayPreview bounds the replay preview. It is deliberately NOT
// validateOptionalBoundedText: that one rejects every unicode control character, which is
// correct for the two things it guards — an HTTP header value (CRLF injection) and the
// sidecar path — and wrong for this one, which is arbitrary TOOL OUTPUT.
//
// A shell result almost always ends in "\n", so the shared validator refused practically
// every shell_exec completion. The command had already run by then: the effect happened, the
// bookkeeping rejected its record, and the agent was handed an error for work that had
// succeeded. ANSI escapes from coloured output would have been rejected the same way.
//
// NUL is the one character genuinely rejected, and on a real constraint rather than taste:
// Postgres cannot store U+0000 in a text column, so a preview carrying one could never be
// persisted. Everything else is text the ledger is happy to hold.
func validateReplayPreview(value string, maxBytes int) error {
	if len(value) > maxBytes {
		return fmt.Errorf("value exceeds %d bytes", maxBytes)
	}
	if strings.ContainsRune(value, 0) {
		return fmt.Errorf("value contains a NUL byte, which a text column cannot store")
	}
	return nil
}

func validateOptionalBoundedText(value string, maxBytes int) error {
	if len(value) > maxBytes {
		return fmt.Errorf("value exceeds %d bytes", maxBytes)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("value contains control characters")
		}
	}
	return nil
}

func isZeroFingerprint(fingerprint [32]byte) bool {
	return fingerprint == [32]byte{}
}
