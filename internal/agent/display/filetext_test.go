package display

import (
	"strings"
	"testing"
)

func TestReadFilePreview(t *testing.T) {
	in := PreviewInput{ToolCallID: "r1", ToolName: "read_file", Arguments: `{"path":"a.txt","offset":3,"limit":2}`, ResultPreview: "3|hello\n4|world\n\n[Truncated: showing lines 3-4 of 10. Use offset=5 to continue reading.]"}
	p, ok := NormalizeToolPreview(in, NewRegistry())
	if !ok || p.Code == nil || p.Code.Filename != "a.txt" || p.Code.FirstLine != 3 || p.Code.Body != "hello\nworld" || p.Code.Notice == "" {
		t.Fatalf("read preview = %+v, ok=%v", p, ok)
	}
	in.ResultPreview = "[extracted from a document format — this is rendered text, not the raw file bytes]\n3|hello\n4|world"
	p, ok = NormalizeToolPreview(in, NewRegistry())
	if !ok || p.Code == nil || !p.Code.Extracted || p.Code.Body != "hello\nworld" {
		t.Fatalf("document text = %+v, ok=%v", p, ok)
	}
}

func TestReadFileFallback(t *testing.T) {
	args := `{"path":"a.txt","offset":3,"limit":2}`
	for _, result := range []string{
		"3|hello\n5|skipped",
		"3|hello\n\n[Truncated: made up]",
		"File not found: a.txt",
		"cannot read binary file",
		strings.Repeat("x", 65537),
	} {
		if p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "r2", ToolName: "read_file", Arguments: args, ResultPreview: result}, NewRegistry()); ok {
			t.Fatalf("unexpected rich read for %q: %+v", result[:min(len(result), 30)], p)
		}
	}
}

func TestWriteFilePreview(t *testing.T) {
	args := `{"path":"a.txt","content":"hello\n"}`
	result := "wrote 6 bytes to /workspace/a.txt\nverified:true — the on-disk content hash was confirmed; do not re-read the file to check the write landed"
	p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "w1", ToolName: "write_file", Arguments: args, ResultPreview: result}, NewRegistry())
	if !ok || p.Code == nil || p.Code.Filename != "a.txt" || p.Code.FirstLine != 1 || p.Code.Body != "hello\n" {
		t.Fatalf("write preview = %+v, ok=%v", p, ok)
	}
	for _, bad := range []string{
		strings.Replace(result, "6 bytes", "5 bytes", 1),
		strings.Replace(result, "verified:true", "verified:false", 1),
		strings.Replace(result, "/workspace/a.txt", "/workspace/b.txt", 1),
	} {
		if p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "w2", ToolName: "write_file", Arguments: args, ResultPreview: bad}, NewRegistry()); ok {
			t.Fatalf("mismatching receipt became rich: %+v", p)
		}
	}
}
