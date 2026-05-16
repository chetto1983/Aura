package attempts

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// LessonCandidate is an aggregated view of repeated tool failures eligible
// for promotion into operational memory via the Phase-N lesson promotion pipeline.
type LessonCandidate struct {
	ToolName         string
	ToolKind         string
	ErrorClass       string
	Reason           string
	AttemptCount     int
	FirstSeen        time.Time
	LastSeen         time.Time
	SampleAttemptIDs []string
	SignatureHash    string
}

// LessonSignatureHash returns a 16-char hex fingerprint for a
// (tool_name, tool_kind, error_class, reason) group:
// sha256(tool_name + tool_kind + error_class + reason)[:8] → 16 hex chars.
func LessonSignatureHash(toolName, toolKind, errorClass, reason string) string {
	h := sha256.Sum256([]byte(toolName + toolKind + errorClass + reason))
	return fmt.Sprintf("%x", h[:8])
}
