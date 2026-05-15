package tools

import (
	"crypto/sha256"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// Outcome classifies a tool-call result into one of 5 semantic buckets for
// the Phase-6 Tool Experience Loop (US-J01).
type Outcome uint8

const (
	OutcomeOK          Outcome = iota // tool returned a result without error
	OutcomeRecoverable                // failure may be resolved by different args/context
	OutcomeBlocked                    // caller must not retry (permission denied, SSRF guard)
	OutcomeFatal                      // hard failure; retry unlikely to help
	OutcomeCancelled                  // context cancelled or user-requested stop
)

func (o Outcome) String() string {
	switch o {
	case OutcomeOK:
		return "ok"
	case OutcomeRecoverable:
		return "recoverable"
	case OutcomeBlocked:
		return "blocked"
	case OutcomeFatal:
		return "fatal"
	case OutcomeCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// BucketOf maps the 10-label vocabulary from ClassifyToolError to 5 Outcome
// buckets:
//
//	ok:          "" (no error)
//	recoverable: validation, not_found, rate_limited, io
//	blocked:     permission, blocked
//	fatal:       error, timeout
//	cancelled:   cancelled
func BucketOf(class string) Outcome {
	switch class {
	case "":
		return OutcomeOK
	case "validation", "not_found", "rate_limited", "io":
		return OutcomeRecoverable
	case "permission", "blocked":
		return OutcomeBlocked
	case "error", "timeout":
		return OutcomeFatal
	case "cancelled":
		return OutcomeCancelled
	default:
		return OutcomeFatal // conservative: unrecognised class treated as fatal
	}
}

// ToolKindOf returns "mcp" when the tool name starts with the "mcp_<server>_"
// prefix (see mcp.go:26-27 for the naming convention) and "native" otherwise.
func ToolKindOf(name string) string {
	if strings.HasPrefix(name, "mcp_") {
		return "mcp"
	}
	return "native"
}

// ToolObservation captures the full context of one tool-call execution.
// It is the unit of data written to tool_attempts (US-J02) and consumed by
// the briefer (US-J04) and retry-budget checker (US-J05).
//
// All fields are exported so the struct round-trips cleanly through
// encoding/gob (required for the downstream persistence layer).
type ToolObservation struct {
	RunID         string
	ToolName      string
	ToolKind      string   // "native" or "mcp" — derived from ToolName at construction via ToolKindOf
	AttemptN      uint16
	Outcome       Outcome
	Class         string   // 10-label vocab from ClassifyToolError
	Reason        string   // closed-set refusal/retry-skip code; empty for normal outcomes
	ArgsHash      [32]byte // SHA-256(canonical sorted-keys JSON of args); values never stored separately
	ArgKeys       []string // key names only — never values (CLAUDE.md privacy rule)
	SchemaHash    [16]byte // first 16 bytes of SHA-256(ToolDefinition.Parameters); zero until US-J02
	ErrorRedacted string
	StartedAt     time.Time
	ElapsedMS     int64
}

// ObservationArgsHash computes the canonical SHA-256 hash of args (using
// encoding/json which sorts map keys) and returns the sorted key names.
// Values are consumed only for hashing and never returned.
func ObservationArgsHash(args map[string]any) ([32]byte, []string) {
	if len(args) == 0 {
		return sha256.Sum256([]byte("{}")), nil
	}
	b, err := json.Marshal(args) // encoding/json sorts map keys deterministically
	if err != nil {
		b = []byte("{}")
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return sha256.Sum256(b), keys
}
