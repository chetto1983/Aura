package documents

import (
	"strings"
	"testing"
)

func TestSearchDocumentIDIsStableAcrossVersions(t *testing.T) {
	const key = "sha256:report"
	first, err := SearchDocumentID("owner-1", "CLI", key)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SearchDocumentID("owner-1", "cli", key)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || !strings.HasPrefix(first, "doc_") || len(first) != len("doc_")+32 {
		t.Fatalf("stable ids = %q / %q", first, second)
	}
	otherOwner, err := SearchDocumentID("owner-2", "cli", key)
	if err != nil {
		t.Fatal(err)
	}
	if otherOwner == first {
		t.Fatal("search id must be owner scoped")
	}
}
