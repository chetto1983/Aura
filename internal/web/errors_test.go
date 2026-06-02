//go:build !web_integration

package web

import "testing"

func TestWebError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *WebError
		want string
	}{
		{"code only", &WebError{Code: CodeTimeout}, "timeout"},
		{"code+reason", &WebError{Code: CodeBlockedURL, Reason: ReasonPrivateOrMetadata},
			"blocked_url (private_or_metadata_target)"},
		{"code+status", &WebError{Code: CodeHTTPError, StatusCode: 404}, "http_error status 404"},
		{"code+reason+status", &WebError{Code: CodeHTTPError, Reason: ReasonInvalidTarget, StatusCode: 503},
			"http_error (invalid_target) status 503"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInternalError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *internalError
		want string
	}{
		{"code only", &internalError{code: CodeBlockedURL}, "blocked_url"},
		{"code+reason", &internalError{code: CodeBlockedURL, reason: ReasonPrivateOrMetadata},
			"blocked_url (private_or_metadata_target)"},
		{"with sensitive fields", &internalError{
			code: CodeBlockedURL, reason: ReasonRedirectToBlocked,
			host: "metadata.test", resolvedIP: "169.254.169.254", redirectFrom: "public.test",
		}, "blocked_url (redirect_to_blocked_target) host=metadata.test ip=169.254.169.254 redirect=public.test"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}
