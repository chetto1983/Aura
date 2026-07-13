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
