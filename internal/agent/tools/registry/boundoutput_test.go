package tools

import (
	"strings"
	"testing"
)

func TestBoundOutput_BelowCaps_NoChange(t *testing.T) {
	content := "short content\nline two"
	out, truncated := BoundOutput(content, 8192, 20)
	if truncated {
		t.Fatal("expected no truncation for content below both caps")
	}
	if out != content {
		t.Fatalf("content changed unexpectedly: got %q", out)
	}
}

func TestBoundOutput_AboveBytes_ClipsAndMarks(t *testing.T) {
	content := strings.Repeat("x", 10000) // 10000 bytes, 1 row
	out, truncated := BoundOutput(content, 8192, 1000)
	if !truncated {
		t.Fatal("expected truncation for content above byte cap")
	}
	if !strings.Contains(out, "[truncated:") {
		t.Fatal("truncation marker missing from output")
	}
	// Only one marker.
	if strings.Count(out, "[truncated:") > 1 {
		t.Fatal("double marker: BoundOutput applied marker twice")
	}
	// Clipped portion must be at most maxBytes.
	markerIdx := strings.Index(out, "\n\n[truncated:")
	if markerIdx < 0 {
		t.Fatal("marker prefix '\\n\\n[truncated:' missing")
	}
	if markerIdx > 8192 {
		t.Fatalf("clipped portion is %d bytes, want ≤ 8192", markerIdx)
	}
}

func TestBoundOutput_AboveRows_ClipsAndMarks(t *testing.T) {
	lines := make([]string, 50)
	for i := range lines {
		lines[i] = "row"
	}
	content := strings.Join(lines, "\n") // 50 rows, ~200 bytes
	out, truncated := BoundOutput(content, 8192, 20)
	if !truncated {
		t.Fatal("expected truncation for content above row cap")
	}
	markerIdx := strings.Index(out, "\n\n[truncated:")
	if markerIdx < 0 {
		t.Fatal("marker prefix '\\n\\n[truncated:' missing")
	}
	clipped := out[:markerIdx]
	rows := strings.Count(clipped, "\n") + 1
	if rows > 20 {
		t.Fatalf("clipped content has %d rows, want ≤ 20", rows)
	}
}

func TestBoundOutput_UTF8Safe(t *testing.T) {
	// 'à' is U+00E0, encoded as 2 bytes (0xC3 0xA0) in UTF-8.
	// 4097 repetitions = 8194 bytes. Capping at 8193 forces safeTruncateUTF8
	// to step back one byte (0xA0 is a continuation byte → not a rune start),
	// proving no rune is split.
	content := strings.Repeat("à", 4097) // 8194 bytes
	out, truncated := BoundOutput(content, 8193, 1000000)
	if !truncated {
		t.Fatal("expected truncation by byte cap")
	}
	markerIdx := strings.Index(out, "\n\n[truncated:")
	if markerIdx < 0 {
		t.Fatal("marker prefix '\\n\\n[truncated:' missing")
	}
	clipped := out[:markerIdx]
	for _, r := range clipped {
		if r == '�' {
			t.Fatal("broken rune detected: safeTruncateUTF8 split a multi-byte character")
		}
	}
}

func TestBoundOutput_MarkerIsLLMReadable(t *testing.T) {
	content := strings.Repeat("y", 10000)
	out, _ := BoundOutput(content, 8192, 1000)
	for _, keyword := range []string{"truncated", "narrower", "ask the user"} {
		if !strings.Contains(out, keyword) {
			t.Errorf("truncation marker missing required keyword %q", keyword)
		}
	}
}
