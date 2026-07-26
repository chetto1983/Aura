package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identityctx"
)

type memoryHTTPRegistry struct {
	request *idempotency.BeginRequest
	replay  *idempotency.ReplayResult
	marked  int
}

func (m *memoryHTTPRegistry) Begin(_ context.Context, request idempotency.BeginRequest) (idempotency.BeginDecision, error) {
	if m.request == nil {
		copyRequest := request
		m.request = &copyRequest
		return idempotency.BeginDecision{
			Decision: idempotency.DecisionAcquired, ClaimToken: 1,
		}, nil
	}
	if m.request.Operation != request.Operation || m.request.Fingerprint != request.Fingerprint {
		return idempotency.BeginDecision{Decision: idempotency.DecisionConflict}, idempotency.ErrConflict
	}
	if m.replay == nil {
		return idempotency.BeginDecision{Decision: idempotency.DecisionInProgress, RetryAfter: time.Second}, nil
	}
	return idempotency.BeginDecision{Decision: idempotency.DecisionReplay, Replay: m.replay}, nil
}

func (m *memoryHTTPRegistry) Complete(_ context.Context, request idempotency.CompleteRequest) error {
	if m.request == nil || m.request.Operation != request.Operation ||
		m.request.Fingerprint != request.Fingerprint ||
		request.ClaimToken != 1 {
		return errors.New("unexpected completion")
	}
	result := request.Result
	m.replay = &result
	return nil
}

func (m *memoryHTTPRegistry) MarkIndeterminate(
	context.Context,
	idempotency.OperationKey,
	[32]byte,
	idempotency.ClaimToken,
) error {
	m.marked++
	return nil
}

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
		{name: "acquired", decision: idempotency.BeginDecision{Decision: idempotency.DecisionAcquired, ClaimToken: 1}, wantStatus: http.StatusOK},
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

func TestMutationMuxAcquiresAndReplaysBeforeHandler(t *testing.T) {
	t.Parallel()

	run := &scriptedRunner{newConversationID: goodID}
	store := &fakeConvStore{known: map[string]bool{goodID: true}}
	registry := &memoryHTTPRegistry{}
	server := NewServer(run, store, ServerConfig{})
	server.SetOperationRegistry(registry)
	mux := server.Mux()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := identityctx.WithIdentityID(r.Context(), identityctx.LocalOperatorIdentity)
		mux.ServeHTTP(w, r.WithContext(ctx))
	})

	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/conversations", strings.NewReader(`{"title":"Stable"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "create-conversation-39")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, r)
		return rr
	}
	first := request()
	second := request()

	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("statuses = %d/%d, want 201/201; bodies=%q/%q", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replay body = %q, want %q", second.Body.String(), first.Body.String())
	}
	if second.Header().Get("Idempotency-Replayed") != "true" {
		t.Fatal("second response did not advertise durable replay")
	}
	if run.newConversationCalls != 1 {
		t.Fatalf("NewConversation calls = %d, want one effect", run.newConversationCalls)
	}
	if registry.request == nil || registry.request.Operation.Scope != idempotency.ScopeHTTPMutation || registry.request.Operation.IdentityID != identityctx.LocalOperatorIdentity {
		t.Fatalf("registry request = %+v, want authenticated HTTP operation", registry.request)
	}
}

func TestMutationMuxRequiresKeyBeforeHandler(t *testing.T) {
	t.Parallel()

	run := &scriptedRunner{newConversationID: goodID}
	store := &fakeConvStore{known: map[string]bool{goodID: true}}
	server := NewServer(run, store, ServerConfig{})
	server.SetOperationRegistry(&memoryHTTPRegistry{})
	r := httptest.NewRequest(http.MethodPost, "/api/conversations", strings.NewReader(`{}`))
	r = r.WithContext(identityctx.WithIdentityID(r.Context(), identityctx.LocalOperatorIdentity))
	rr := httptest.NewRecorder()
	server.Mux().ServeHTTP(rr, r)
	if rr.Code != http.StatusBadRequest || run.newConversationCalls != 0 {
		t.Fatalf("status=%d effect calls=%d, want 400 and no effect", rr.Code, run.newConversationCalls)
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
	// 200 (was 204): the Phase A resolve returns the ResolveDirective. The operation-context
	// forwarding this test pins is untouched by that projection.
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%q", rr.Code, rr.Body.String())
	}
	got, ok := idempotency.OperationFromContext(run.submitAnswersCtx)
	if !ok || got != op {
		t.Fatalf("runner received operation %+v/%v, want %+v", got, ok, op)
	}
}
