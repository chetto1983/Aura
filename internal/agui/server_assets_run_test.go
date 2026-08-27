package agui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
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
	if run.gotVisibleUserMsg == nil || *run.gotVisibleUserMsg != "summarize it" {
		t.Fatalf("visible userMsg = %v, want original message", run.gotVisibleUserMsg)
	}
	if run.gotModelUserMsg == nil {
		t.Fatal("model userMsg is nil, want attachment block + message")
	}
	got := *run.gotModelUserMsg
	for _, want := range []string{`<attachments trust="untrusted_user_uploads">`, "document_search", `document_id="doc-1"`, "User message:\nsummarize it"} {
		if !strings.Contains(got, want) {
			t.Fatalf("model userMsg missing %q:\n%s", want, got)
		}
	}
}

func TestServerRunInjectsKnowledgeCatalogWithoutAttachment(t *testing.T) {
	const tid = "55555555-5555-5555-5555-555555555555"
	run := &scriptedRunner{events: textTurn("ok")}
	assetSvc := &fakeAssetService{listResp: []assets.Asset{{
		ID:         "asset-7",
		IdentityID: assetAPIIdentityID,
		ThreadID:   tid,
		FileName:   "g220.pdf",
		Modality:   assets.ModalityDocument,
		Status:     assets.StatusSearchable,
		DocumentID: "doc-7",
		Summary:    "Servo Drive G220 datasheet",
	}}}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	s.SetAssetService(assetSvc)

	// No attachment_ids this turn: the catalog is the only signal that the doc exists.
	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"what is the rated torque?"}]}`
	rec := serveRunWithPrincipal(t, s, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if run.gotVisibleUserMsg == nil || *run.gotVisibleUserMsg != "what is the rated torque?" {
		t.Fatalf("visible userMsg = %v, want original message", run.gotVisibleUserMsg)
	}
	if run.gotModelUserMsg == nil {
		t.Fatal("model userMsg is nil, want knowledge catalog + message")
	}
	got := *run.gotModelUserMsg
	for _, want := range []string{"<knowledge_base", "document_search", "document_id=doc-7", "g220.pdf", "User message:\nwhat is the rated torque?"} {
		if !strings.Contains(got, want) {
			t.Fatalf("model userMsg missing %q:\n%s", want, got)
		}
	}
	if assetSvc.listThreadID != tid {
		t.Fatalf("ListForThread thread = %q, want %q", assetSvc.listThreadID, tid)
	}
}

func TestServerRunCarriesNormalizedGarageScopeOnlyInTheRunContext(t *testing.T) {
	const tid = "66666666-6666-4666-8666-666666666666"
	run := &scriptedRunner{events: textTurn("ok")}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"review @folder:\"/finance/2026\""}],"aura":{"document_scope":[{"kind":"folder","path":"/finance/2026/"}]}}`
	rec := serveRunWithPrincipal(t, s, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	scopes := documents.SourceScopesFromContext(run.turnCtx)
	if len(scopes) != 1 || scopes[0].Kind != documents.SourceScopeFolder ||
		scopes[0].Path != "finance/2026" {
		t.Fatalf("run scopes = %#v", scopes)
	}
	if run.gotTurnUserMsg == nil || *run.gotTurnUserMsg != `review @folder:"/finance/2026"` {
		t.Fatalf("visible message changed: %v", run.gotTurnUserMsg)
	}
}

func TestServerRunRejectsInvalidGarageScopeBeforeTheRunner(t *testing.T) {
	const tid = "77777777-7777-4777-8777-777777777777"
	run := &scriptedRunner{events: textTurn("should not run")}
	s := NewServer(run, &fakeConvStore{known: map[string]bool{tid: true}}, ServerConfig{})
	body := `{"threadId":"` + tid + `","messages":[{"id":"m1","role":"user","content":"x"}],"aura":{"document_scope":[{"kind":"folder","path":"../private"}]}}`
	rec := serveRunWithPrincipal(t, s, body)
	if rec.Code != http.StatusBadRequest || run.turnCalled {
		t.Fatalf("status = %d, turnCalled = %v", rec.Code, run.turnCalled)
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
