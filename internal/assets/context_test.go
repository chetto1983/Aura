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
