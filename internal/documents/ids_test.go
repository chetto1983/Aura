package documents

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestSearchDocumentIDIsStableAcrossVersions(t *testing.T) {
	key, err := SourceKey("connector://folder/report.pdf", "")
	if err != nil {
		t.Fatal(err)
	}
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

func TestSourceKeyDoesNotExposeLocator(t *testing.T) {
	key, err := SourceKey(`D:\private\Clienti 2026.xlsx`, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "sha256:") || strings.Contains(key, "Clienti") {
		t.Fatalf("source key = %q", key)
	}
	if _, err := SourceKey("", ""); err == nil {
		t.Fatal("empty source locator accepted")
	}
}

func TestContentHashesReaderReturnsSHA1AndSHA256(t *testing.T) {
	hashes, err := ContentHashesReader(strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	sha1Sum := sha1.Sum([]byte("payload"))
	sha256Sum := sha256.Sum256([]byte("payload"))
	if hashes.SHA1 != hex.EncodeToString(sha1Sum[:]) {
		t.Fatalf("SHA1 = %q, want sha1(payload)", hashes.SHA1)
	}
	if hashes.SHA256 != hex.EncodeToString(sha256Sum[:]) {
		t.Fatalf("SHA256 = %q, want sha256(payload)", hashes.SHA256)
	}
}
