package agui

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/llm"
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

// A clip travels the same seam as a still: the gateway arms the projection for video too, and
// the provider client decides from the model's declared input modalities whether the bytes are
// actually sent. Both run the same flow, so they share it — two copies would have to be kept
// in step by hand.
func TestServerRunCarriesVerifiedGarageMediaProjection(t *testing.T) {
	for _, tc := range []struct {
		name     string
		threadID string
		asset    assets.Asset
		body     string
		prompt   string
	}{
		{
			name:     "image",
			threadID: "88888888-8888-4888-8888-888888888888",
			body:     "real-png-bytes",
			prompt:   "describe it",
			asset: assets.Asset{
				ID:       "asset-image",
				FileName: "panel.png",
				MIMEType: "image/png",
				Modality: assets.ModalityImage,
				Summary:  "control panel",
			},
		},
		{
			name:     "video",
			threadID: "99999999-9999-4999-8999-999999999999",
			body:     "real-mp4-bytes",
			prompt:   "what happens in the clip?",
			asset: assets.Asset{
				ID:       "asset-video",
				FileName: "clip.mp4",
				MIMEType: "video/mp4",
				Modality: assets.ModalityVideo,
				Summary:  "a servo panel, filmed",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			digest := sha256.Sum256(body)
			asset := tc.asset
			asset.IdentityID = assetAPIIdentityID
			asset.ThreadID = tc.threadID
			asset.Status = assets.StatusComplete
			asset.SizeBytes = int64(len(body))
			asset.ContentHash = hex.EncodeToString(digest[:])

			run := &scriptedRunner{events: textTurn("ok")}
			assetSvc := &fakeAssetService{
				getResp:   asset,
				openAsset: asset,
				openResp:  io.NopCloser(strings.NewReader(tc.body)),
			}
			s := NewServer(run, &fakeConvStore{known: map[string]bool{tc.threadID: true}}, ServerConfig{})
			s.SetAssetService(assetSvc)

			requestBody := `{"threadId":"` + tc.threadID + `","messages":[{"id":"m1","role":"user","content":"` +
				tc.prompt + `"}],"aura":{"attachment_ids":["` + asset.ID + `"]}}`
			rec := serveRunWithPrincipal(t, s, requestBody)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d: %s", rec.Code, rec.Body)
			}
			projection, ok := llm.ContentProjectionFromContext(run.turnCtx)
			if !ok {
				t.Fatal("run context has no content projection")
			}
			if projection.Principal.OwnerID != assetAPIIdentityID ||
				len(projection.ReferenceIDs) != 1 || projection.ReferenceIDs[0] != asset.ID {
				t.Fatalf("projection = %+v", projection)
			}
			part, err := projection.Loader.LoadContentPart(context.Background(), "", projection.Principal.OwnerID, asset.ID)
			if err != nil {
				t.Fatalf("LoadContentPart: %v", err)
			}
			if string(part.Bytes) != tc.body || part.MIMEType != asset.MIMEType || part.FallbackText != asset.Summary {
				t.Fatalf("verified part = %+v", part)
			}
			if assetSvc.openIdentityID != assetAPIIdentityID {
				t.Fatalf("OpenForIdentity owner = %q", assetSvc.openIdentityID)
			}
		})
	}
}

func TestGarageMediaProjectionRejectsDigestDrift(t *testing.T) {
	// An IMAGE, deliberately: audio is refused a step earlier (modality), so an audio
	// fixture here would make this test green without ever reaching the digest compare.
	assetSvc := &fakeAssetService{
		openAsset: assets.Asset{
			ID: "a1", ThreadID: "t1", MIMEType: "image/png", Modality: assets.ModalityImage,
			SizeBytes: 3, ContentHash: strings.Repeat("0", 64),
		},
		openResp: io.NopCloser(strings.NewReader("png")),
	}
	loader := assets.TurnMediaLoader{Opener: assetSvc, ThreadID: "t1", Allowed: map[string]bool{"a1": true}}
	_, err := loader.LoadContentPart(context.Background(), "", assetAPIIdentityID, "a1")
	if err == nil {
		t.Fatal("digest drift was accepted")
	}
	if !strings.Contains(err.Error(), "digest changed") {
		t.Fatalf("LoadContentPart error = %v, want the digest check", err)
	}
}

// Speech reaches the model as WORDS, never as bytes: an audio asset is refused by the
// native-media loader, so a voice turn carries its STT transcript (Asset.Summary,
// rendered into the attachment block) and nothing else.
func TestGarageMediaProjectionRefusesAudioAsNativeMedia(t *testing.T) {
	body := []byte("ogg-bytes")
	digest := sha256.Sum256(body)
	assetSvc := &fakeAssetService{
		openAsset: assets.Asset{
			ID: "a1", ThreadID: "t1", MIMEType: "audio/ogg", Modality: assets.ModalityAudio,
			SizeBytes: int64(len(body)), ContentHash: hex.EncodeToString(digest[:]),
			Summary: "ciao, che coppia eroga il servo?",
		},
		openResp: io.NopCloser(bytes.NewReader(body)),
	}
	loader := assets.TurnMediaLoader{Opener: assetSvc, ThreadID: "t1", Allowed: map[string]bool{"a1": true}}
	_, err := loader.LoadContentPart(context.Background(), "", assetAPIIdentityID, "a1")
	if err == nil {
		t.Fatal("an audio asset was projected as native media")
	}
	if !strings.Contains(err.Error(), "not native media") {
		t.Fatalf("LoadContentPart error = %v, want the modality refusal", err)
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
	return serveRunAs(t, s, assetAPIIdentityID, body)
}

// serveRunAs posts a run carrying identity as the authenticated principal. The principal is
// what the pinned-skill lookup resolves against since amendment #214, so a run test that
// cares WHOSE skill is pinned names the identity here rather than taking the default.
func serveRunAs(t *testing.T, s *Server, identity, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/agent/run", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, identity)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	_, _ = io.Copy(io.Discard, rec.Result().Body)
	return rec
}
