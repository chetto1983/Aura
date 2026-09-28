package display

import (
	"strings"
	"testing"
)

func TestPatchDiffPreview(t *testing.T) {
	const result = "--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,2 @@\n-old\n+new\n keep\n"
	for _, args := range []string{
		`{"path":"a.txt","mode":"replace","old_string":"old","new_string":"new"}`,
		`{"path":"a.txt","mode":"patch","patch":"@@ -1 +1 @@\n-old\n+new\n"}`,
	} {
		p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "p1", ToolName: "patch", Arguments: args, ResultPreview: result}, NewRegistry())
		if !ok || p.Type != KindDiff || p.Diff == nil || p.Diff.Filename != "a.txt" || p.Diff.Additions != 1 || p.Diff.Deletions != 1 || len(p.Diff.Lines) != 3 {
			t.Fatalf("diff = %+v, ok=%v", p, ok)
		}
	}
	boxResult := strings.ReplaceAll(result, "a.txt", "/workspace/a.txt")
	if p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "p-box", ToolName: "patch", Arguments: `{"path":"a.txt","old_string":"old","new_string":"new"}`, ResultPreview: boxResult}, NewRegistry()); !ok || p.Diff == nil {
		t.Fatalf("sandbox path diff = %+v, ok=%v", p, ok)
	}
}

func TestPatchDiffContentStartingWithHeaderMarkers(t *testing.T) {
	in := PreviewInput{ToolCallID: "p2", ToolName: "patch", Arguments: `{"path":"a.txt","mode":"replace","old_string":"x","new_string":"+++ hello"}`, ResultPreview: "--- a/a.txt\n+++ b/a.txt\n@@ -1,1 +1,1 @@\n-x\n++++ hello\n"}
	p, ok := NormalizeToolPreview(in, NewRegistry())
	if !ok || p.Diff == nil || len(p.Diff.Lines) != 2 || p.Diff.Lines[1].Kind != "added" || p.Diff.Lines[1].Text != "+++ hello" {
		t.Fatalf("header-like content was misparsed: %+v, ok=%v", p, ok)
	}
}

func TestPatchDiffFallback(t *testing.T) {
	args := `{"path":"a.txt","mode":"replace","old_string":"x","new_string":"y"}`
	valid := "--- a/a.txt\n+++ b/a.txt\n@@ -1,1 +1,1 @@\n-x\n+y\n"
	for _, tc := range []struct{ name, args, result string }{
		{"no-op", args, "File already contains the target text — no write performed"},
		{"bad hunk count", args, "--- a/a.txt\n+++ b/a.txt\n@@ -1,2 +1,1 @@\n-x\n+y\n"},
		{"two files", args, valid + valid},
		{"wrong path", args, strings.ReplaceAll(valid, "a.txt", "b.txt")},
		{"malformed", args, "--- a/a.txt\n+++ b/a.txt\n@@ broken @@\n-x\n+y\n"},
		{"huge", args, valid + strings.Repeat("x", 65536)},
		{"missing path", `{}`, valid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if p, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "p3", ToolName: "patch", Arguments: tc.args, ResultPreview: tc.result}, NewRegistry()); ok {
				t.Fatalf("unexpected rich diff: %+v", p)
			}
		})
	}
}
