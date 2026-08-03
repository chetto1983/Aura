package documents

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

const (
	testCatalogID  = "10000000-0000-0000-0000-000000000001"
	testIdentityID = "00000000-0000-0000-0000-000000000001"
	testAssetID    = "30000000-0000-0000-0000-000000000001"
)

type fakeSearchResolver struct {
	askedIdentity string
	asked         string
	id            string
	err           error
}

func (f *fakeSearchResolver) CatalogIDForSearchDocument(
	_ context.Context, identityID, searchDocumentID string,
) (string, error) {
	f.askedIdentity, f.asked = identityID, searchDocumentID
	return f.id, f.err
}

type fakeAssetOpener struct {
	identityID string
	assetID    string
	body       string
	err        error
}

func (f *fakeAssetOpener) OpenDocumentAsset(_ context.Context, identityID, assetID string) (io.ReadCloser, error) {
	f.identityID, f.assetID = identityID, assetID
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(f.body)), nil
}

func openServiceWith(detail DocumentDetail) (*OpenService, *fakeCatalogStore, *fakeAssetOpener, *fakeSearchResolver) {
	store := &fakeCatalogStore{detail: detail}
	assets := &fakeAssetOpener{body: "original bytes"}
	resolver := &fakeSearchResolver{id: testCatalogID}
	return &OpenService{
		Catalog:  &CatalogService{Store: store},
		Resolver: resolver,
		Assets:   assets,
	}, store, assets, resolver
}

func oneVersionDetail() DocumentDetail {
	return DocumentDetail{
		Document: Document{ID: testCatalogID, IdentityID: testIdentityID, Title: "Clienti.xlsx"},
		Versions: []DocumentVersion{{
			ID: "20000000-0000-0000-0000-000000000001", DocumentID: testCatalogID,
			AssetID: testAssetID, VersionNumber: 1, SHA256: "abc", SizeBytes: 331239,
			ContentType: "application/vnd.ms-excel",
		}},
	}
}

func TestOpenDocument_ResolvesSearchIDThroughTheCatalog(t *testing.T) {
	service, store, assets, resolver := openServiceWith(oneVersionDetail())

	body, meta, err := service.OpenDocument(context.Background(), testIdentityID, "doc_9f2c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = body.Close() }()

	if resolver.asked != "doc_9f2c" {
		t.Fatalf("resolver asked for %q, want the search id", resolver.asked)
	}
	// The resolution is a read of aura.documents, which fails closed without a principal
	// (migration 0087), so the identity has to reach the resolver too — not only the two
	// gates below.
	if resolver.askedIdentity != testIdentityID {
		t.Fatalf("resolver asked as %q, want the caller's identity", resolver.askedIdentity)
	}
	if store.getDocumentID != testCatalogID {
		t.Fatalf("catalog looked up %q, want the resolved uuid", store.getDocumentID)
	}
	// Both gates must carry the caller's identity, not the document's owner field.
	if store.getIdentityID != testIdentityID || assets.identityID != testIdentityID {
		t.Fatalf("identity lost: catalog=%q assets=%q", store.getIdentityID, assets.identityID)
	}
	if assets.assetID != testAssetID {
		t.Fatalf("opened asset %q, want %q", assets.assetID, testAssetID)
	}
	if meta.FileName != "Clienti.xlsx" || meta.SHA256 != "abc" || meta.SizeBytes != 331239 {
		t.Fatalf("metadata = %#v", meta)
	}
	if meta.DocumentID != "doc_9f2c" || meta.CatalogID != testCatalogID {
		t.Fatalf("ids = %q / %q, want the caller's id and the resolved uuid", meta.DocumentID, meta.CatalogID)
	}
	content, err := io.ReadAll(body)
	if err != nil || string(content) != "original bytes" {
		t.Fatalf("body = %q err = %v", content, err)
	}
}

func TestOpenDocument_UUIDSkipsTheResolver(t *testing.T) {
	service, store, _, resolver := openServiceWith(oneVersionDetail())
	body, _, err := service.OpenDocument(context.Background(), testIdentityID, testCatalogID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = body.Close()
	if resolver.asked != "" {
		t.Fatalf("a catalog uuid was sent through the search resolver (%q)", resolver.asked)
	}
	if store.getDocumentID != testCatalogID {
		t.Fatalf("catalog looked up %q", store.getDocumentID)
	}
}

func TestOpenDocument_IdentityGateRefusesAnotherOwnersDocument(t *testing.T) {
	service, store, assets, _ := openServiceWith(oneVersionDetail())
	store.getErr = errors.New("document not found")

	if _, _, err := service.OpenDocument(context.Background(), testIdentityID, testCatalogID); err == nil {
		t.Fatal("a non-owner lookup reported success")
	}
	// The object store must never be reached once the catalog gate refuses: that
	// ordering IS the IDOR guard, not an optimization.
	if assets.assetID != "" {
		t.Fatalf("asset %q was opened after the catalog refused", assets.assetID)
	}
}

func TestOpenDocument_PicksTheActiveVersion(t *testing.T) {
	detail := DocumentDetail{
		Document: Document{
			ID: testCatalogID, Title: "Clienti.xlsx",
			ActiveVersionID: "version-1",
		},
		Versions: []DocumentVersion{
			{ID: "version-1", AssetID: "asset-v1", VersionNumber: 1},
			{ID: "version-2", AssetID: "asset-v2", VersionNumber: 2},
		},
	}
	service, _, assets, _ := openServiceWith(detail)
	body, _, err := service.OpenDocument(context.Background(), testIdentityID, testCatalogID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = body.Close()
	// The catalog's active pointer wins over "highest number": a rolled-back
	// document still has the newer version row, and serving it would hand the
	// agent bytes the user no longer considers current.
	if assets.assetID != "asset-v1" {
		t.Fatalf("opened %q, want the catalog's active version", assets.assetID)
	}
}

func TestOpenDocument_FallsBackToTheNewestVersion(t *testing.T) {
	detail := DocumentDetail{
		Document: Document{ID: testCatalogID, Title: "Clienti.xlsx"},
		Versions: []DocumentVersion{
			{ID: "version-1", AssetID: "asset-v1", VersionNumber: 1},
			{ID: "version-3", AssetID: "asset-v3", VersionNumber: 3},
			{ID: "version-2", AssetID: "asset-v2", VersionNumber: 2},
		},
	}
	service, _, assets, _ := openServiceWith(detail)
	body, _, err := service.OpenDocument(context.Background(), testIdentityID, testCatalogID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = body.Close()
	if assets.assetID != "asset-v3" {
		t.Fatalf("opened %q, want the highest version number", assets.assetID)
	}
}

func TestOpenDocument_RefusesVersionsWithNoOriginal(t *testing.T) {
	cases := map[string]DocumentDetail{
		"no versions at all": {Document: Document{ID: testCatalogID, Title: "x.xlsx"}},
		"version without an asset": {
			Document: Document{ID: testCatalogID, Title: "x.xlsx"},
			Versions: []DocumentVersion{{ID: "version-1", VersionNumber: 1}},
		},
	}
	for name, detail := range cases {
		t.Run(name, func(t *testing.T) {
			service, _, _, _ := openServiceWith(detail)
			if _, _, err := service.OpenDocument(context.Background(), testIdentityID, testCatalogID); err == nil {
				t.Fatal("expected an error: there is no original to hand over")
			}
		})
	}
}

func TestOpenDocument_ValidatesItsInputsAndWiring(t *testing.T) {
	detail := oneVersionDetail()
	t.Run("blank ids", func(t *testing.T) {
		service, _, _, _ := openServiceWith(detail)
		if _, _, err := service.OpenDocument(context.Background(), testIdentityID, "  "); err == nil {
			t.Error("empty document_id accepted")
		}
		if _, _, err := service.OpenDocument(context.Background(), " ", testCatalogID); err == nil {
			t.Error("empty identity accepted")
		}
	})
	t.Run("unconfigured service", func(t *testing.T) {
		if _, _, err := (&OpenService{}).OpenDocument(context.Background(), testIdentityID, testCatalogID); err == nil {
			t.Error("a service with no catalog reported success")
		}
		if _, _, err := (&OpenService{Catalog: &CatalogService{Store: &fakeCatalogStore{}}}).
			OpenDocument(context.Background(), testIdentityID, testCatalogID); err == nil {
			t.Error("a service with no asset opener reported success")
		}
	})
	t.Run("search id with no resolver", func(t *testing.T) {
		service, _, _, _ := openServiceWith(detail)
		service.Resolver = nil
		if _, _, err := service.OpenDocument(context.Background(), testIdentityID, "doc_9f2c"); err == nil {
			t.Error("a search id was accepted with no way to resolve it")
		}
	})
	t.Run("resolver failure", func(t *testing.T) {
		service, _, _, resolver := openServiceWith(detail)
		resolver.err = errors.New("not catalogued")
		_, _, err := service.OpenDocument(context.Background(), testIdentityID, "doc_9f2c")
		if err == nil || !strings.Contains(err.Error(), "not catalogued") {
			t.Errorf("error = %v, want the resolver's reason preserved", err)
		}
	})
	t.Run("asset open failure", func(t *testing.T) {
		service, _, assets, _ := openServiceWith(detail)
		assets.err = errors.New("object missing")
		_, _, err := service.OpenDocument(context.Background(), testIdentityID, testCatalogID)
		if err == nil || !strings.Contains(err.Error(), "object missing") {
			t.Errorf("error = %v, want the storage reason preserved", err)
		}
	})
}

func TestDocumentFileName(t *testing.T) {
	// The title becomes a path component, so anything that could steer the write
	// elsewhere — or produce a dotfile — is replaced rather than trusted.
	cases := []struct {
		name  string
		title string
		want  string
	}{
		{"plain", "Clienti.xlsx", "Clienti.xlsx"},
		{"unix path", "/etc/passwd", "passwd"},
		{"windows path", `C:\Users\x\Clienti.xlsx`, "Clienti.xlsx"},
		{"traversal", "../../escape.xlsx", "escape.xlsx"},
		{"empty", "   ", "document-abcdef012345"},
		{"dotfile", ".bashrc", "document-abcdef012345"},
		{"dot", ".", "document-abcdef012345"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := documentFileName(
				Document{Title: tc.title},
				DocumentVersion{SHA256: "abcdef0123456789"},
			)
			if got != tc.want {
				t.Fatalf("documentFileName(%q) = %q, want %q", tc.title, got, tc.want)
			}
		})
	}
}

func TestShortHash(t *testing.T) {
	if got := shortHash(""); got != "unknown" {
		t.Errorf("shortHash(empty) = %q", got)
	}
	if got := shortHash("abc"); got != "abc" {
		t.Errorf("shortHash(short) = %q", got)
	}
	if got := shortHash("0123456789abcdef"); got != "0123456789ab" {
		t.Errorf("shortHash(long) = %q", got)
	}
}
