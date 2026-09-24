package mcp

import (
	"encoding/json"
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
		"file:///tmp/my%20report.pdf":                           "my report.pdf",
		"x://a/b%zz":                                            "b%zz",
	} {
		if got := NameFromURI(uri); got != want {
			t.Errorf("NameFromURI(%q) = %q, want %q", uri, got, want)
		}
	}
}

func TestFileFromContents(t *testing.T) {
	blob, ok := FileFromContents(&sdkmcp.ResourceContents{URI: "file:///srv/invoice.pdf", MIMEType: "application/pdf", Blob: []byte("%PDF")})
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

// TestFileFromContentsEmptyBlobOnTheWireIsNotAFile pins encoding/json's decode of a
// present-but-empty "blob" field: it yields a non-nil, zero-length []byte, which the
// old `rc.Blob != nil` check would have accepted as a file.
func TestFileFromContentsEmptyBlobOnTheWireIsNotAFile(t *testing.T) {
	var rc sdkmcp.ResourceContents
	if err := json.Unmarshal([]byte(`{"uri":"x","blob":""}`), &rc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := FileFromContents(&rc); ok {
		t.Fatalf("an empty blob on the wire must not become a file: %+v", rc)
	}
}

func TestFilePartNotMaterializedKeepsTheFirstReason(t *testing.T) {
	part := FilePart{Name: "a.pdf", MIMEType: "application/pdf", Data: []byte("abc")}
	want := FileOutcome{Name: "a.pdf", MIMEType: "application/pdf", SizeBytes: 3, NotMaterialized: "sandbox unavailable: down"}
	if got := part.NotMaterialized("sandbox unavailable: down"); got != want {
		t.Fatalf("outcome = %+v, want %+v", got, want)
	}
	unread := FilePart{Name: "old.pdf", Size: 8, Unavailable: "read failed: expired"}
	got := unread.NotMaterialized("sandbox unavailable: down")
	if got.NotMaterialized != "read failed: expired" {
		t.Fatalf("the reason the bytes were missing must win: %+v", got)
	}
	if got.SizeBytes != 8 {
		t.Fatalf("SizeBytes = %d, want the part's known Size standing in for the missing bytes: %+v", got.SizeBytes, got)
	}
}

func TestFileCapExceeded(t *testing.T) {
	if got := FileCapExceeded(MaxFileBytes + 1); got != "26214401 bytes exceeds the 26214400-byte file cap" {
		t.Fatalf("FileCapExceeded = %q", got)
	}
}

func TestFilePartCallBytes(t *testing.T) {
	cases := []struct {
		name string
		part FilePart
		want int
	}{
		{"a file within the cap counts its bytes", FilePart{Data: make([]byte, 10)}, 10},
		{"a file of exactly the file cap counts", FilePart{Data: make([]byte, MaxFileBytes)}, MaxFileBytes},
		{"a file over the file cap is refused alone and counts none", FilePart{Data: make([]byte, MaxFileBytes+1)}, 0},
		{"an unavailable part counts none, whatever size it advertises", FilePart{Unavailable: "read failed", Size: MaxFileBytes}, 0},
		{"an unavailable part counts none even if it holds bytes", FilePart{Unavailable: "read failed", Data: []byte("abc")}, 0},
		{"an empty part counts none", FilePart{}, 0},
	}
	for _, c := range cases {
		if got := c.part.CallBytes(); got != c.want {
			t.Errorf("%s: CallBytes = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestCallCapExceeded(t *testing.T) {
	if got := CallCapExceeded(); got != "the call's files exceed the 52428800-byte cap" {
		t.Fatalf("CallCapExceeded = %q", got)
	}
}
