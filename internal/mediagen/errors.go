// Package mediagen defines the ports image/video generation tools and the video
// watcher consume: a live-model/wait settings port over aura.settings and an
// identity-scoped credential port over the daemon's existing per-identity LLM
// resolver (CRED-01/CRED-05). It has no implementation of its own — the
// composition root (cmd/aura) implements both ports over concrete infrastructure
// it already owns, the same split as internal/llm's Client interface.
package mediagen

import "errors"

// Error is a media-generation refusal a caller can act on: Code is machine-
// readable (no_key, no_credit, ...), Message is the human-readable copy.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// ErrorCode returns "" for a nil error, the Code carried by a *Error, or
// "job_failed" for any other error — an infrastructure failure with no
// specific refusal code, never fabricated as one of the named codes above.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if mediaErr, ok := errors.AsType[*Error](err); ok {
		return mediaErr.Code
	}
	return "job_failed"
}
