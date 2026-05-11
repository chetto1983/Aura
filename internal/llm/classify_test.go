package llm

import (
	"context"
	"errors"
	"regexp"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name          string
		in            error
		wantBucket    Bucket
		wantRetryable bool
	}{
		{
			name:          "nil_error",
			in:            nil,
			wantBucket:    BucketPermanent,
			wantRetryable: false,
		},
		{
			name:          "context_canceled",
			in:            context.Canceled,
			wantBucket:    BucketPermanent,
			wantRetryable: false,
		},
		{
			name:          "context_deadline",
			in:            context.DeadlineExceeded,
			wantBucket:    BucketTransient,
			wantRetryable: true,
		},
		{
			name:          "schema_validation",
			in:            ErrSchemaValidation,
			wantBucket:    BucketContent,
			wantRetryable: true,
		},
		{
			name:          "empty_output",
			in:            ErrEmptyOutput,
			wantBucket:    BucketContent,
			wantRetryable: true,
		},
		{
			name:          "malformed_toolcall",
			in:            ErrMalformedToolCall,
			wantBucket:    BucketContent,
			wantRetryable: true,
		},
		{
			name:          "api_429",
			in:            &APIError{StatusCode: 429},
			wantBucket:    BucketTransient,
			wantRetryable: true,
		},
		{
			name:          "api_503",
			in:            &APIError{StatusCode: 503},
			wantBucket:    BucketTransient,
			wantRetryable: true,
		},
		{
			name:          "api_401",
			in:            &APIError{StatusCode: 401},
			wantBucket:    BucketPermanent,
			wantRetryable: false,
		},
		{
			name:          "api_400_model_not_found",
			in:            &APIError{StatusCode: 400, Body: "model not found"},
			wantBucket:    BucketPermanent,
			wantRetryable: false,
		},
		{
			name:          "msg_rate_limit",
			in:            errors.New("rate limit exceeded"),
			wantBucket:    BucketTransient,
			wantRetryable: true,
		},
		{
			name:          "msg_quota",
			in:            errors.New("monthly quota reached"),
			wantBucket:    BucketPermanent,
			wantRetryable: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, retryable, _ := Classify(tt.in)
			if bucket != tt.wantBucket {
				t.Errorf("Classify(%v) bucket = %v, want %v", tt.in, bucket, tt.wantBucket)
			}
			if retryable != tt.wantRetryable {
				t.Errorf("Classify(%v) retryable = %v, want %v", tt.in, retryable, tt.wantRetryable)
			}
		})
	}
}

func TestRedactor_NoSecretLeak(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		noMatch *regexp.Regexp // pattern that must NOT appear in cleaned output
	}{
		{
			name:    "url_with_token",
			input:   "https://api.example.com/?token=abc12345xyz",
			noMatch: regexp.MustCompile(`abc12345xyz`),
		},
		{
			name:    "bearer_header",
			input:   "Authorization: Bearer sk_live_abc123",
			noMatch: regexp.MustCompile(`sk_live`),
		},
		{
			name:    "basic_auth_url",
			input:   "https://user:pass@host/x",
			noMatch: regexp.MustCompile(`user:pass`),
		},
		{
			name:    "base64_long",
			input:   "xxx aGVsbG8gd29ybGQgaGVsbG8gd29ybGQgaGVsbG8 yyy",
			noMatch: regexp.MustCompile(`aGVsbG8gd29ybGQgaGVsbG8gd29ybGQgaGVsbG8`),
		},
		{
			name:    "authorization_header_bare",
			input:   "Authorization: foo",
			noMatch: regexp.MustCompile(`Authorization: foo`),
		},
		{
			name:    "openrouter_key",
			input:   "sk-or-v1-abc123def456",
			noMatch: regexp.MustCompile(`sk-or-v1-`),
		},
		{
			name:    "jwt",
			input:   "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyMSJ9.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			noMatch: regexp.MustCompile(`eyJ`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cleaned := redact(tt.input)
			if tt.noMatch.MatchString(cleaned) {
				t.Errorf("redact(%q) = %q, still contains secret pattern %q", tt.input, cleaned, tt.noMatch.String())
			}
		})
	}
}
