package assets

import (
	"context"
	"strings"
	"testing"
)

// Re-ingesting a document must land on the SAME object, because that is the only thing the
// reconciler can follow. Measured 2026-09-09 against the live stack: an object overwritten
// in place at Documenti/realign-garage.md was re-indexed under its existing document id with
// the new sha256, and the superseded text stopped being findable -- CocoIndex reconciles
// correctly. But every ingest minted a fresh key, so the same file re-ingested became a
// SECOND document and version one stayed searchable for good: probe v1 "ZANZIBAR-4417" was
// still answering after v2 landed under its own chat/<uuid>.
//
// The key still must not carry the file name -- see PlaceAsset -- so the stable part is a
// digest of the document's identity, not the name itself.
func TestLibraryReingestOverwritesTheSameObject(t *testing.T) {
	t.Parallel()
	ingest := func(svc *Service, body string) Asset {
		t.Helper()
		asset, err := svc.IngestDocument(context.Background(), DocumentIngestRequest{
			IdentityID: serviceIdentityID,
			SourceKind: SourceWorkspace,
			SourceRef:  "sha256:workspace-locator",
			FileName:   "report.pdf",
			MIMEType:   "application/pdf",
			SizeBytes:  int64(len(body)),
			Reader:     strings.NewReader(body),
		})
		if err != nil {
			t.Fatalf("IngestDocument() error = %v", err)
		}
		return asset
	}
	limits := Limits{MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100}

	svc, _ := newAssetServiceTestRig(t, limits)
	first := ingest(svc, "%PDF version one")
	second := ingest(svc, "%PDF version two")

	if first.ObjectKey != second.ObjectKey {
		t.Fatalf("re-ingest wrote a new object:\n first  = %s\n second = %s", first.ObjectKey, second.ObjectKey)
	}
	if strings.Contains(first.ObjectKey, "report") {
		t.Fatalf("object key leaks the file name: %s", first.ObjectKey)
	}

	other, _ := newAssetServiceTestRig(t, limits)
	distinct, err := other.IngestDocument(context.Background(), DocumentIngestRequest{
		IdentityID: serviceIdentityID, SourceKind: SourceWorkspace,
		SourceRef: "sha256:another-locator", FileName: "report.pdf",
		MIMEType: "application/pdf", SizeBytes: 4, Reader: strings.NewReader("%PDF"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if distinct.ObjectKey == first.ObjectKey {
		t.Fatalf("two different documents share one object key: %s", distinct.ObjectKey)
	}
}

// Chat, Telegram and the document tools are one pipeline into one library, so the rule that
// keeps a re-ingest on its object cannot live in only one of the three ingresses. Presign is
// the web upload's half: with library scope it must place the same document on the same
// object, and with thread scope it must not -- two chats can each send their own foto.jpg.
func TestPresignAppliesTheSameKeyRuleAsIngest(t *testing.T) {
	t.Parallel()
	svc, _ := newAssetServiceTestRig(t, Limits{
		MaxDocumentBytes: 100, MaxImageBytes: 100, MaxAudioBytes: 100,
	})
	presign := func(scope Scope) string {
		t.Helper()
		out, err := svc.Presign(context.Background(), PresignRequest{
			IdentityID: serviceIdentityID, SourceKind: SourceWeb, Scope: scope,
			FileName: "report.pdf", MIMEType: "application/pdf", DeclaredSizeBytes: 4,
		})
		if err != nil {
			t.Fatalf("Presign(%s) error = %v", scope, err)
		}
		return out.Asset.ObjectKey
	}
	if first, second := presign(ScopeLibrary), presign(ScopeLibrary); first != second {
		t.Fatalf("library upload wrote a new object:\n first  = %s\n second = %s", first, second)
	}
	if first, second := presign(ScopeThread), presign(ScopeThread); first == second {
		t.Fatalf("two thread attachments collapsed onto one object: %s", first)
	}
}
