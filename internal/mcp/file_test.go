package mcp

import (
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestNameFromURI(t *testing.T) {
	for uri, want := range map[string]string{
		"attachment://stash/abc123":                             "abc123",
		"whatsapp-media://393331234567@s.whatsapp.net/3EB0C767": "3EB0C767",
		"file:///tmp/report.pdf":                                "report.pdf",
		"https://example.com/a/b.pdf?sig=x#frag":                "b.pdf",
		"notes://dir/":                                          "dir",
		"":                                                      "",
	} {
		if got := NameFromURI(uri); got != want {
			t.Errorf("NameFromURI(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestFileFromContents(t *testing.T) {
	blob, ok := FileFromContents(&sdkmcp.ResourceContents{URI: "attachment://a/invoice.pdf", MIMEType: "application/pdf", Blob: []byte("%PDF")})
	if !ok || blob.Name != "invoice.pdf" || blob.MIMEType != "application/pdf" || string(blob.Data) != "%PDF" {
		t.Fatalf("blob contents = %+v, %v", blob, ok)
	}
	text, ok := FileFromContents(&sdkmcp.ResourceContents{URI: "notes://today.md", MIMEType: "text/markdown", Text: "# hi"})
	if !ok || string(text.Data) != "# hi" {
		t.Fatalf("text contents = %+v, %v", text, ok)
	}
	for _, empty := range []*sdkmcp.ResourceContents{nil, {URI: "empty://x"}} {
		if _, ok := FileFromContents(empty); ok {
			t.Fatalf("contents %+v carry no bytes, yet made a file", empty)
		}
	}
}

func TestFilePartNotMaterializedKeepsTheFirstReason(t *testing.T) {
	part := FilePart{Name: "a.pdf", MIMEType: "application/pdf", Data: []byte("abc")}
	want := FileOutcome{Name: "a.pdf", MIMEType: "application/pdf", SizeBytes: 3, NotMaterialized: "sandbox unavailable: down"}
	if got := part.NotMaterialized("sandbox unavailable: down"); got != want {
		t.Fatalf("outcome = %+v, want %+v", got, want)
	}
	unread := FilePart{Name: "old.pdf", Unavailable: "read failed: expired"}
	if got := unread.NotMaterialized("sandbox unavailable: down"); got.NotMaterialized != "read failed: expired" {
		t.Fatalf("the reason the bytes were missing must win: %+v", got)
	}
}

func TestFileCapExceeded(t *testing.T) {
	if got := FileCapExceeded(MaxFileBytes + 1); got != "26214401 bytes exceeds the 26214400-byte file cap" {
		t.Fatalf("FileCapExceeded = %q", got)
	}
}
