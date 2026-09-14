// Package mediagen is OpenRouter image and video generation for the generation
// tools and the video watcher: the Catalog of image/video models and the clamp
// that fits a request to what a model declares, the Client that submits, polls
// and downloads through the openai-go SDK, the Store of durable,
// identity-scoped video jobs in aura.media_job, and the Watcher that carries
// those jobs to a terminal status.
//
// What it needs from the daemon arrives through ports the composition root
// (cmd/aura) implements over infrastructure it already owns: Settings over
// aura.settings, MediaCredentials over the per-identity LLM resolver
// (CRED-01/CRED-05), and ReferenceReader and VideoAssets over the assets
// service.
package mediagen

import "errors"

// Error is a media-generation refusal a caller can act on: Code is machine-
// readable (no_key, no_credit, ...), Message is the human-readable copy. Its JSON
// form is the stable {code,message} a job's error column stores.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
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
