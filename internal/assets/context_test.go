package assets

import (
	"context"
	"strings"
	"testing"
)

// TestBuildTurnContextComposesAttachmentAndCatalogChannelAgnostic proves the single
// channel-agnostic seam both AG-UI and Telegram call: it details THIS turn's attachment
// and advertises the thread's OTHER searchable docs (minus the attached one and other
// threads), wrapping the user text — and degrades to the raw text when there is nothing
// to add.
func TestBuildTurnContextComposesAttachmentAndCatalogChannelAgnostic(t *testing.T) {
	store := newFakeAssetStore()
	store.assets["att"] = Asset{ID: "att", IdentityID: "u1", ThreadID: "t1", Status: StatusComplete, DocumentID: "doc-att", FileName: "attached.pdf"}
	store.assets["other"] = Asset{ID: "other", IdentityID: "u1", ThreadID: "t1", Status: StatusComplete, DocumentID: "doc-other", FileName: "other.pdf", Summary: "servo specs"}
	store.assets["library"] = Asset{ID: "library", IdentityID: "u1", Scope: ScopeLibrary, Status: StatusComplete, DocumentID: "doc-library", FileName: "library.pdf", Summary: "robot course"}
	store.assets["nope"] = Asset{ID: "nope", IdentityID: "u1", ThreadID: "t1", Status: StatusComplete, DocumentID: ""}
	store.assets["xthread"] = Asset{ID: "xthread", IdentityID: "u1", ThreadID: "t2", Status: StatusComplete, DocumentID: "doc-x", FileName: "x.pdf"}
	store.assets["other-user-library"] = Asset{ID: "other-user-library", IdentityID: "u2", Scope: ScopeLibrary, Status: StatusComplete, DocumentID: "doc-u2", FileName: "u2.pdf"}
	// A DocumentScope that holds everything: this test is about composition and thread/identity
	// scoping, not about which documents the index has. The catalog now asks the index instead
	// of reading the status column, so without one it would advertise nothing.
	svc := &Service{Store: store, DocumentScope: everythingIndexed{}}
	ctx := context.Background()

	withAttach := svc.BuildTurnContext(ctx, "u1", "t1", []Asset{store.assets["att"]}, nil, "what torque?")
	for _, want := range []string{"<attachments", "doc-att", "<knowledge_base", "doc-other", "User message:\nwhat torque?"} {
		if !strings.Contains(withAttach, want) {
			t.Fatalf("BuildTurnContext(attachment) missing %q:\n%s", want, withAttach)
		}
	}
	if strings.Contains(withAttach, "doc-x") {
		t.Fatalf("BuildTurnContext leaked another thread's doc:\n%s", withAttach)
	}
	kbStart, kbEnd := strings.Index(withAttach, "<knowledge_base"), strings.Index(withAttach, "</knowledge_base>")
	if catalog := withAttach[kbStart:kbEnd]; strings.Contains(catalog, "doc-att") {
		t.Fatalf("attached doc must not also appear in the catalog block:\n%s", catalog)
	}

	noAttach := svc.BuildTurnContext(ctx, "u1", "t1", nil, nil, "hello")
	for _, want := range []string{"<knowledge_base", "doc-att", "doc-other", "doc-library", "User message:\nhello"} {
		if !strings.Contains(noAttach, want) {
			t.Fatalf("BuildTurnContext(no attachment) missing %q:\n%s", want, noAttach)
		}
	}
	if strings.Contains(noAttach, "doc-u2") {
		t.Fatalf("BuildTurnContext leaked another identity's library doc:\n%s", noAttach)
	}
	if strings.Contains(noAttach, "<attachments") {
		t.Fatalf("no-attachment turn must not render an attachment block:\n%s", noAttach)
	}

	libraryOnly := svc.BuildTurnContext(ctx, "u1", "", nil, nil, "summarize library")
	for _, want := range []string{"<knowledge_base", "doc-library", "User message:\nsummarize library"} {
		if !strings.Contains(libraryOnly, want) {
			t.Fatalf("BuildTurnContext(library only) missing %q:\n%s", want, libraryOnly)
		}
	}

	if got := svc.BuildTurnContext(ctx, "", "", nil, nil, "plain"); got != "plain" {
		t.Fatalf("BuildTurnContext(no identity/thread/attachment) = %q, want raw text", got)
	}
}

func TestBuildAttachmentBlockGuidesDocumentSearchAndSanitizesLines(t *testing.T) {
	block := BuildAttachmentBlock([]Asset{{
		ID:           "asset-1",
		FileName:     "bad\nname.pdf",
		Modality:     ModalityDocument,
		Status:       StatusComplete,
		DocumentID:   "doc-1",
		Summary:      "first line\nsecond line",
		ErrorMessage: "warning\r\ncontinued",
	}})

	for _, want := range []string{
		`<attachments trust="untrusted_user_uploads">`,
		"document_search",
		`document_id="doc-1"`,
		`document_open with document_id="asset-1"`,
		"bad name.pdf",
		"first line second line",
		"warning continued",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("BuildAttachmentBlock() missing %q:\n%s", want, block)
		}
	}
	for _, forbidden := range []string{"bad\nname", "first line\nsecond", "warning\r"} {
		if strings.Contains(block, forbidden) {
			t.Fatalf("BuildAttachmentBlock() did not sanitize %q:\n%s", forbidden, block)
		}
	}
}

func TestNoAttachmentsLeaveTheUserTextUnchanged(t *testing.T) {
	const userText = "please summarize this"
	if got := WithContextBlocks(userText, BuildAttachmentBlock(nil)); got != userText {
		t.Fatalf("WithContextBlocks(no attachments) = %q, want original text", got)
	}
}

func TestBuildKnowledgeCatalogListsSearchableDocsExcludingAttached(t *testing.T) {
	items := []Asset{
		{ID: "a1", FileName: "manual.pdf", Status: StatusComplete, DocumentID: "doc-1", Summary: "G220 servo drive datasheet"},
		{ID: "a2", FileName: "draft.docx", Status: StatusComplete, DocumentID: ""},         // not searchable -> excluded
		{ID: "a3", FileName: "attached.xlsx", Status: StatusComplete, DocumentID: "doc-3"}, // attached this turn -> excluded
		{ID: "a4", FileName: "photo.png", Status: StatusComplete, DocumentID: "doc-4", Summary: "control panel"},
	}
	// Every id is indexed here: this test is about exclusion and rendering, not about which
	// documents the index holds -- context_indexed_test.go owns that.
	catalog := BuildKnowledgeCatalog(items, map[string]bool{"a3": true}, allIndexed)

	for _, want := range []string{"<knowledge_base", "document_search", "document_id=doc-1", "manual.pdf", "document_id=doc-4", "photo.png"} {
		if !strings.Contains(catalog, want) {
			t.Fatalf("catalog missing %q:\n%s", want, catalog)
		}
	}
	for _, forbidden := range []string{"doc-3", "attached.xlsx", "draft.docx"} {
		if strings.Contains(catalog, forbidden) {
			t.Fatalf("catalog should not include excluded/non-searchable %q:\n%s", forbidden, catalog)
		}
	}
}

func TestBuildKnowledgeCatalogEmptyWhenNothingSearchable(t *testing.T) {
	if got := BuildKnowledgeCatalog(nil, nil, allIndexed); got != "" {
		t.Fatalf("empty input catalog = %q, want empty", got)
	}
	// Both lack a document id, so neither can be advertised however the index answers.
	items := []Asset{{ID: "a1", Status: StatusComplete}, {ID: "a2", Status: StatusComplete, DocumentID: ""}}
	if got := BuildKnowledgeCatalog(items, nil, allIndexed); got != "" {
		t.Fatalf("no-searchable catalog = %q, want empty", got)
	}
}

// allIndexed stands in for an index that holds everything it is asked about.
func allIndexed(ids []string) map[string]bool {
	indexed := make(map[string]bool, len(ids))
	for _, id := range ids {
		indexed[id] = true
	}
	return indexed
}

// everythingIndexed answers that every document asked about is in the index.
type everythingIndexed struct{}

func (everythingIndexed) ResolveDocumentScope(_ context.Context, _ string, ids []string) ([]string, error) {
	return ids, nil
}

// An attachment the processing has not reached yet has no document id, and a photo or a clip
// never gets one; its asset id still opens it, so the block says how to get the file either way.
func TestBuildAttachmentBlockOpensAnAttachmentByItsAssetID(t *testing.T) {
	block := BuildAttachmentBlock([]Asset{{
		ID: "6ea35fff-df6d-46ef-9927-e74dce08c64b", FileName: "fcffginzu.xls",
		Modality: ModalityDocument, Status: StatusAccepted,
	}, {
		ID: "asset-photo", FileName: "ponte.jpg", Modality: ModalityImage, Status: StatusComplete,
	}})
	for _, want := range []string{
		`document_open with document_id="6ea35fff-df6d-46ef-9927-e74dce08c64b"`,
		`document_open with document_id="asset-photo"`,
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("BuildAttachmentBlock() missing %q:\n%s", want, block)
		}
	}
}
