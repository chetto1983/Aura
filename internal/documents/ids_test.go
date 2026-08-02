package documents

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestDocumentIDStableForSameContentAndSource(t *testing.T) {
	a := DocumentID("hash-a", "source-a")
	b := DocumentID("hash-a", "source-a")
	if a != b {
		t.Fatalf("document id should be stable: %q != %q", a, b)
	}
	if !strings.HasPrefix(a, "doc_") || len(a) != len("doc_")+32 {
		t.Fatalf("unexpected document id format: %q", a)
	}
}

func TestDocumentIDDifferentForDifferentSource(t *testing.T) {
	a := DocumentID("hash-a", "source-a")
	b := DocumentID("hash-a", "source-b")
	if a == b {
		t.Fatalf("document id should include source id: %q", a)
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

func TestContentHashReaderRemainsSHA256Canonical(t *testing.T) {
	got, err := ContentHashReader(strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	hashes, err := ContentHashesReader(strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if got != hashes.SHA256 {
		t.Fatalf("ContentHashReader = %q, want SHA256 %q", got, hashes.SHA256)
	}
}
