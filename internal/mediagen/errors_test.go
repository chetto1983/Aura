package mediagen

import (
	"errors"
	"testing"
)

func TestErrorCode(t *testing.T) {
	if got := ErrorCode(nil); got != "" {
		t.Fatalf("ErrorCode(nil) = %q, want empty", got)
	}
	mediaErr := &Error{Code: "no_credit", Message: "This identity has no generation credit."}
	if got := ErrorCode(mediaErr); got != "no_credit" {
		t.Fatalf("ErrorCode(media error) = %q, want no_credit", got)
	}
	if got := ErrorCode(errors.New("boom")); got != "job_failed" {
		t.Fatalf("ErrorCode(generic error) = %q, want job_failed", got)
	}
	if mediaErr.Error() != mediaErr.Message {
		t.Fatalf("Error() = %q, want the Message %q", mediaErr.Error(), mediaErr.Message)
	}
}

func TestErrorCodeUnwrapsWrappedMediaError(t *testing.T) {
	mediaErr := &Error{Code: "no_key", Message: "No identity credential is available."}
	wrapped := errors.Join(errors.New("context"), mediaErr)
	if got := ErrorCode(wrapped); got != "no_key" {
		t.Fatalf("ErrorCode(wrapped) = %q, want no_key", got)
	}
}
