package agui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/runner"
)

const compactTestConversationID = "11111111-1111-4111-8111-111111111111"

type fakeCompactService struct {
	preview runner.CompactPreview
	request runner.CompactRequest
}

func (f *fakeCompactService) Preview(ctx context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	f.request = req
	return f.preview, nil
}
func (f *fakeCompactService) Restore(ctx context.Context, req runner.CompactRequest) (runner.CompactPreview, error) {
	f.request = req
	return runner.CompactPreview{OperationID: req.OperationID, Status: runner.CompactStatusRecovered}, nil
}

func TestPreviewOwnerGateAndSanitizedResponse(t *testing.T) {
	conv := &errConvStore{ownsGate: true}
	service := &fakeCompactService{preview: runner.CompactPreview{Status: runner.CompactStatusDisabled, Summary: "safe"}}
	s := &Server{conv: conv, compact: service}
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/conversations/"+compactTestConversationID+"/compact/preview", strings.NewReader(`{"operationId":"op-1"}`)), testLocalID)
	req.SetPathValue("id", compactTestConversationID)
	rec := httptest.NewRecorder()
	s.handleCompactPreview(rec, req)
	if rec.Code != http.StatusOK || service.request.ActorID != testLocalID || !strings.Contains(rec.Body.String(), `"status":"disabled"`) {
		t.Fatalf("response=%d %s req=%#v", rec.Code, rec.Body.String(), service.request)
	}
}

func TestRestoreRequiresSafePoint(t *testing.T) {
	conv := &errConvStore{ownsGate: true}
	service := &fakeCompactService{}
	s := &Server{conv: conv, compact: service}
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/conversations/"+compactTestConversationID+"/compact/restore", strings.NewReader(`{"operationId":"op-2","checkpointId":"cp-0","safePoint":false}`)), testLocalID)
	req.SetPathValue("id", compactTestConversationID)
	rec := httptest.NewRecorder()
	s.handleCompactRestore(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCompactOperationsUseIdenticalOwnerScopedCoordinatorOutcome(t *testing.T) {
	owner := "11111111-1111-1111-1111-111111111111"
	conversationID := "22222222-2222-2222-2222-222222222222"
	service := &fakeCompactService{preview: runner.CompactPreview{OperationID: "op", CheckpointID: "cp-1", Status: runner.CompactStatusDisabled}}
	s := &Server{conv: &errConvStore{ownsGate: true}, compact: service}

	for _, path := range []string{"/api/conversations/" + conversationID + "/compact", "/api/conversations/" + conversationID + "/compact/history", "/api/conversations/" + conversationID + "/compact/preview", "/api/conversations/" + conversationID + "/compact/diff"} {
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"operationId":"op","checkpointId":"cp-1"}`)), owner)
		s.Mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"disabled"`) {
			t.Fatalf("%s: code=%d body=%q", path, rec.Code, rec.Body.String())
		}
		if service.request.ActorID != owner || service.request.ConversationID != conversationID {
			t.Fatalf("%s escaped owner scope: %+v", path, service.request)
		}
	}
}
