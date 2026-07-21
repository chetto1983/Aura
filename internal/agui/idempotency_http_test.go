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
	"github.com/chetto1983/aura/internal/identityctx"
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
		wantHeader string
	}{
		{name: "acquired", decision: idempotency.BeginDecision{Decision: idempotency.DecisionAcquired}, wantStatus: http.StatusOK},
		{name: "replay", decision: idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: &idempotency.ReplayResult{Body: replayBody, StatusCode: http.StatusAccepted, Headers: map[string]string{"Location": "/api/conversations/conv-1"}, ExpiresAt: time.Now().Add(time.Hour)}}, wantHandle: true, wantStatus: http.StatusAccepted, wantReplay: true, wantHeader: "/api/conversations/conv-1"},
		{name: "expired result", decision: idempotency.BeginDecision{Decision: idempotency.DecisionResultExpired}, wantHandle: true, wantStatus: http.StatusGone},
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
			if got := rr.Header().Get("Location"); got != tt.wantHeader {
				t.Fatalf("Location = %q, want %q", got, tt.wantHeader)
			}
			if tt.wantReplay && rr.Body.String() != string(replayBody) {
				t.Fatalf("replay body = %q, want %q", rr.Body.String(), replayBody)
			}
		})
	}
}

func ptrString(v string) *string { return &v }

func TestApprovalResolveForwardsOriginalOperationContext(t *testing.T) {
	t.Parallel()

	run := &scriptedRunner{}
	s := NewServer(run, nil, ServerConfig{})
	body := strings.NewReader(`{"action":"accept","content":"approved"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/approvals/00000000-0000-0000-0000-000000000099/resolve", body)
	r.SetPathValue("token", "00000000-0000-0000-0000-000000000099")
	fingerprint, err := idempotency.FingerprintTyped(struct {
		Token   string `json:"token"`
		Action  string `json:"action"`
		Content string `json:"content"`
	}{Token: "00000000-0000-0000-0000-000000000099", Action: "accept", Content: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	op := idempotency.Operation{
		Key:         idempotency.OperationKey{IdentityID: identityctx.LocalOperatorIdentity, Scope: idempotency.ScopeApproval, Key: "approval-key"},
		Fingerprint: fingerprint,
	}
	ctx, err := idempotency.WithOperation(identityctx.WithIdentityID(r.Context(), identityctx.LocalOperatorIdentity), op)
	if err != nil {
		t.Fatal(err)
	}
	r = r.WithContext(ctx)

	rr := httptest.NewRecorder()
	s.handleResolveApproval(rr, r)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body=%q", rr.Code, rr.Body.String())
	}
	got, ok := idempotency.OperationFromContext(run.submitAnswersCtx)
	if !ok || got != op {
		t.Fatalf("runner received operation %+v/%v, want %+v", got, ok, op)
	}
}
