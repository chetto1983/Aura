package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/assets"
)

const assetAPIIdentityID = "00000000-0000-0000-0000-000000000001"

func TestAssetAPIUnavailableWithoutService(t *testing.T) {
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/assets/presign", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestAssetAPIPresignValidationError400(t *testing.T) {
	assetSvc := &fakeAssetService{presignErr: errors.New("bad request: token=secret")}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(assetSvc)

	req := httptest.NewRequest(http.MethodPost, "/api/assets/presign", strings.NewReader(`{"thread_id":"thread-1","file_name":"setup.exe","size_bytes":12}`))
	req = withPrincipal(req, assetAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("error body leaked secret: %q", rec.Body.String())
	}
	if assetSvc.presignReq.IdentityID != assetAPIIdentityID || assetSvc.presignReq.SourceKind != assets.SourceWeb {
		t.Fatalf("Presign request = %#v, want bound identity and web source", assetSvc.presignReq)
	}
}

func TestAssetPresignAcceptsLibraryScope(t *testing.T) {
	assetSvc := &fakeAssetService{}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(assetSvc)

	body := strings.NewReader(`{"file_name":"manual.pdf","mime_type":"application/pdf","size_bytes":128,"modality_hint":"document","scope":"library"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/assets/presign", body)
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, assetAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if assetSvc.presignReq.Scope != assets.ScopeLibrary {
		t.Fatalf("scope = %q, want %q", assetSvc.presignReq.Scope, assets.ScopeLibrary)
	}
}

func TestAssetAPIListUsesPrincipalAndThread(t *testing.T) {
	assetSvc := &fakeAssetService{
		listResp: []assets.Asset{{
			ID:         "asset-1",
			IdentityID: assetAPIIdentityID,
			ThreadID:   "thread-1",
			FileName:   "manual.pdf",
			Status:     assets.StatusSearchable,
		}},
	}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(assetSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/assets?thread_id=thread-1", nil)
	req = withPrincipal(req, assetAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if assetSvc.listIdentityID != assetAPIIdentityID || assetSvc.listThreadID != "thread-1" {
		t.Fatalf("ListForThread called with identity=%q thread=%q", assetSvc.listIdentityID, assetSvc.listThreadID)
	}
	var got []assets.Asset
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 || got[0].ID != "asset-1" {
		t.Fatalf("response = %#v, want asset-1", got)
	}
}

func TestAssetAPIRoutesAreRegisteredOnMux(t *testing.T) {
	assetSvc := &fakeAssetService{getResp: assets.Asset{ID: "asset-1", IdentityID: assetAPIIdentityID}}
	s := NewServer(&scriptedRunner{}, &fakeConvStore{}, ServerConfig{})
	s.SetAssetService(assetSvc)

	req := httptest.NewRequest(http.MethodGet, "/api/assets/asset-1", nil)
	req = withPrincipal(req, assetAPIIdentityID)
	rec := httptest.NewRecorder()

	s.Mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if assetSvc.getID != "asset-1" {
		t.Fatalf("GetForIdentity id = %q, want asset-1", assetSvc.getID)
	}
}

type fakeAssetService struct {
	presignReq PresignRequestAlias
	presignErr error
	listResp   []assets.Asset
	listErr    error
	getResp    assets.Asset
	getErr     error

	listIdentityID string
	listThreadID   string
	getID          string
	getIdentityID  string
}

type PresignRequestAlias = assets.PresignRequest

func (f *fakeAssetService) Presign(_ context.Context, req assets.PresignRequest) (assets.PresignResponse, error) {
	f.presignReq = req
	if f.presignErr != nil {
		return assets.PresignResponse{}, f.presignErr
	}
	return assets.PresignResponse{Asset: assets.Asset{ID: "asset-1"}}, nil
}

func (f *fakeAssetService) Finalize(context.Context, string, string) (assets.Asset, error) {
	return f.getResp, f.getErr
}

func (f *fakeAssetService) GetForIdentity(_ context.Context, id, identityID string) (assets.Asset, error) {
	f.getID = id
	f.getIdentityID = identityID
	if f.getErr != nil {
		return assets.Asset{}, f.getErr
	}
	return f.getResp, nil
}

func (f *fakeAssetService) ListForThread(_ context.Context, identityID, threadID string) ([]assets.Asset, error) {
	f.listIdentityID = identityID
	f.listThreadID = threadID
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResp, nil
}

// BuildTurnContext mirrors the real Service seam over the fake's data: it records the
// (identity, thread) the catalog is keyed by and composes the thread's searchable docs
// (listResp, minus the attached) + this turn's attachment block onto the user text.
func (f *fakeAssetService) BuildTurnContext(_ context.Context, identityID, threadID string, attachments []assets.Asset, userText string) string {
	f.listIdentityID = identityID
	f.listThreadID = threadID
	excluded := make(map[string]bool, len(attachments))
	for _, a := range attachments {
		excluded[a.ID] = true
	}
	var catalog string
	if identityID != "" && threadID != "" {
		catalog = assets.BuildKnowledgeCatalog(f.listResp, excluded)
	}
	return assets.WithContextBlocks(userText, catalog, assets.BuildAttachmentBlock(attachments))
}

func (f *fakeAssetService) Promote(context.Context, string, string) (assets.Asset, error) {
	return f.getResp, f.getErr
}

func (f *fakeAssetService) Delete(context.Context, string, string) (assets.Asset, error) {
	return f.getResp, f.getErr
}

func (f *fakeAssetService) Retry(context.Context, string, string) (assets.Asset, error) {
	return f.getResp, f.getErr
}
