package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/idempotency"
)

func TestIdempotencyKeyValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value *string
	}{
		{name: "missing"},
		{name: "blank", value: ptrString("   ")},
		{name: "control", value: ptrString("key\nvalue")},
		{name: "over limit", value: ptrString(strings.Repeat("k", idempotency.MaxOperationKeyBytes+1))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/api/conversations", nil)
			if tt.value != nil {
				r.Header.Set("Idempotency-Key", *tt.value)
			}
			if _, err := parseIdempotencyKey(r); err == nil {
				t.Fatal("parseIdempotencyKey error = nil, want rejection")
			}
		})
	}

	r := httptest.NewRequest(http.MethodPost, "/api/conversations", nil)
	r.Header.Set("Idempotency-Key", "caller-stable-key")
	if got, err := parseIdempotencyKey(r); err != nil || got != "caller-stable-key" {
		t.Fatalf("parseIdempotencyKey = (%q, %v)", got, err)
	}
}

func TestWriteIdempotencyDecisionHTTPMapping(t *testing.T) {
	t.Parallel()

	replayBody := json.RawMessage(`{"status":"created","id":"conv-1"}`)
	tests := []struct {
		name       string
		decision   idempotency.BeginDecision
		wantHandle bool
		wantStatus int
		wantRetry  bool
		wantReplay bool
	}{
		{name: "acquired", decision: idempotency.BeginDecision{Decision: idempotency.DecisionAcquired}, wantStatus: http.StatusOK},
		{name: "replay", decision: idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: &idempotency.ReplayResult{Body: replayBody, ExpiresAt: time.Now().Add(time.Hour)}}, wantHandle: true, wantStatus: http.StatusOK, wantReplay: true},
		{name: "conflict", decision: idempotency.BeginDecision{Decision: idempotency.DecisionConflict}, wantHandle: true, wantStatus: http.StatusConflict},
		{name: "in progress", decision: idempotency.BeginDecision{Decision: idempotency.DecisionInProgress, RetryAfter: 1500 * time.Millisecond}, wantHandle: true, wantStatus: http.StatusConflict, wantRetry: true},
		{name: "indeterminate", decision: idempotency.BeginDecision{Decision: idempotency.DecisionIndeterminate}, wantHandle: true, wantStatus: http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rr := httptest.NewRecorder()
			handled := writeIdempotencyDecision(rr, tt.decision)
			if handled != tt.wantHandle {
				t.Fatalf("handled = %v, want %v", handled, tt.wantHandle)
			}
			if !handled {
				return
			}
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
			if got := rr.Header().Get("Retry-After"); (got != "") != tt.wantRetry {
				t.Fatalf("Retry-After = %q, want present=%v", got, tt.wantRetry)
			} else if got != "" {
				seconds, err := strconv.Atoi(got)
				if err != nil || seconds < 1 || seconds > int(idempotency.MaxRetryAfter/time.Second) {
					t.Fatalf("Retry-After = %q, want bounded integer seconds", got)
				}
			}
			if got := rr.Header().Get("Idempotency-Replayed"); (got == "true") != tt.wantReplay {
				t.Fatalf("Idempotency-Replayed = %q, want replay=%v", got, tt.wantReplay)
			}
			if tt.wantReplay && rr.Body.String() != string(replayBody) {
				t.Fatalf("replay body = %q, want %q", rr.Body.String(), replayBody)
			}
		})
	}
}

func ptrString(v string) *string { return &v }
