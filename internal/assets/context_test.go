package assets

import (
	"strings"
	"testing"
)

func TestBuildAttachmentBlockGuidesDocumentSearchAndSanitizesLines(t *testing.T) {
	block := BuildAttachmentBlock([]Asset{{
		ID:           "asset-1",
		FileName:     "bad\nname.pdf",
		Modality:     ModalityDocument,
		Status:       StatusSearchable,
		DocumentID:   "doc-1",
		Summary:      "first line\nsecond line",
		ErrorMessage: "warning\r\ncontinued",
	}})

	for _, want := range []string{
		`<attachments trust="untrusted_user_uploads">`,
		"document_search",
		`document_id="doc-1"`,
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

func TestWithAttachmentBlockReturnsOriginalTextWhenNoAssets(t *testing.T) {
	const userText = "please summarize this"
	if got := WithAttachmentBlock(userText, nil); got != userText {
		t.Fatalf("WithAttachmentBlock(no assets) = %q, want original text", got)
	}
}

func TestBuildKnowledgeCatalogListsSearchableDocsExcludingAttached(t *testing.T) {
	items := []Asset{
		{ID: "a1", FileName: "manual.pdf", Status: StatusSearchable, DocumentID: "doc-1", Summary: "G220 servo drive datasheet"},
		{ID: "a2", FileName: "draft.docx", Status: StatusComplete, DocumentID: ""},           // not searchable -> excluded
		{ID: "a3", FileName: "attached.xlsx", Status: StatusSearchable, DocumentID: "doc-3"}, // attached this turn -> excluded
		{ID: "a4", FileName: "photo.png", Status: StatusSearchable, DocumentID: "doc-4", Summary: "control panel"},
	}
	catalog := BuildKnowledgeCatalog(items, map[string]bool{"a3": true})

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
	if got := BuildKnowledgeCatalog(nil, nil); got != "" {
		t.Fatalf("empty input catalog = %q, want empty", got)
	}
	items := []Asset{{ID: "a1", Status: StatusComplete}, {ID: "a2", Status: StatusSearchable, DocumentID: ""}}
	if got := BuildKnowledgeCatalog(items, nil); got != "" {
		t.Fatalf("no-searchable catalog = %q, want empty", got)
	}
}
