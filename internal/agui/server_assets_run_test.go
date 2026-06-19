package agui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
)

func TestServerRunPrependsAttachmentBlock(t *testing.T) {
	const tid = "11111111-1111-1111-1111-111111111111"
	run := &scriptedRunner{events: textTurn("ok")}
	assetSvc := &fakeAssetService{getResp: assets.Asset{
		ID:         "asset-1",
		IdentityID: assetAPIIdentityID,
		ThreadID:   tid,
		FileName:   "manual.pdf",
		Modality:   assets.ModalityDocument,
		Status:     assets.StatusSearchable,
		DocumentID: "doc-1",
		Summary:    "indexed",
	}}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	s.SetAssetService(assetSvc)

	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"summarize it"}],"aura":{"attachment_ids":["asset-1"]}}`
	rec := serveRunWithPrincipal(t, s, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if run.gotTurnUserMsg == nil {
		t.Fatal("Runner.Turn userMsg is nil, want attachment block + message")
	}
	got := *run.gotTurnUserMsg
	for _, want := range []string{`<attachments trust="untrusted_user_uploads">`, "document_search", `document_id="doc-1"`, "User message:\nsummarize it"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Turn userMsg missing %q:\n%s", want, got)
		}
	}
}

func TestServerRunStillRejectsStructuredMultimodalContent(t *testing.T) {
	const tid = "22222222-2222-2222-2222-222222222222"
	s := NewServer(&scriptedRunner{}, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})

	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":[{"type":"text","text":"hi"}]}]}`
	rec := serveRunWithPrincipal(t, s, body)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), errUnsupportedUserMessageContent.Error()) {
		t.Fatalf("body = %q, want unsupported-content error", rec.Body.String())
	}
}

func TestServerRunAttachmentFromAnotherThread404(t *testing.T) {
	const tid = "33333333-3333-3333-3333-333333333333"
	run := &scriptedRunner{events: textTurn("ok")}
	assetSvc := &fakeAssetService{getResp: assets.Asset{
		ID:         "asset-1",
		IdentityID: assetAPIIdentityID,
		ThreadID:   "44444444-4444-4444-4444-444444444444",
		FileName:   "manual.pdf",
	}}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	s.SetAssetService(assetSvc)

	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"x"}],"aura":{"attachment_ids":["asset-1"]}}`
	rec := serveRunWithPrincipal(t, s, body)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if run.turnCalled {
		t.Fatal("Runner.Turn was called for an attachment from another thread")
	}
}

func serveRunWithPrincipal(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, assetAPIIdentityID)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	_, _ = io.Copy(io.Discard, rec.Result().Body)
	return rec
}
