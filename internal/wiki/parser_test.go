package wiki

import (
	"testing"
)

func TestParseHeadings_BasicH2H3(t *testing.T) {
	body := []byte("# Page Title\n\n## First Section\n\nSome text here.\n\n### Subsection\n\nMore detail.\n\n## Second Section\n\nEnd content.\n")
	headings := ParseHeadings(body)
	if len(headings) != 3 {
		t.Fatalf("expected 3 headings (H2+H3 only), got %d: %v", len(headings), headings)
	}
	if headings[0].Level != 2 || headings[0].Text != "First Section" {
		t.Errorf("headings[0] = %+v, want Level=2 Text='First Section'", headings[0])
	}
	if headings[1].Level != 3 || headings[1].Text != "Subsection" {
		t.Errorf("headings[1] = %+v, want Level=3 Text='Subsection'", headings[1])
	}
	if headings[2].Level != 2 || headings[2].Text != "Second Section" {
		t.Errorf("headings[2] = %+v, want Level=2 Text='Second Section'", headings[2])
	}
	// ByteStart of first heading must be > 0 (after the H1 + blank line).
	if headings[0].ByteStart == 0 {
		t.Errorf("headings[0].ByteStart = 0, expected > 0 (H1 lines precede it)")
	}
	// ByteStart values must be strictly increasing.
	for i := 1; i < len(headings); i++ {
		if headings[i].ByteStart <= headings[i-1].ByteStart {
			t.Errorf("ByteStart not strictly increasing at index %d: %d <= %d",
				i, headings[i].ByteStart, headings[i-1].ByteStart)
		}
	}
}

func TestParseHeadings_SkipsFencedCode(t *testing.T) {
	body := []byte("## Real Heading\n\nSome prose.\n\n```bash\n## Fake Heading Inside Fence\n### Another Fake\n```\n\n## Another Real Heading\n")
	headings := ParseHeadings(body)
	for _, h := range headings {
		if h.Text == "Fake Heading Inside Fence" || h.Text == "Another Fake" {
			t.Errorf("fenced-code heading included: %q", h.Text)
		}
	}
	if len(headings) != 2 {
		t.Fatalf("expected 2 real headings, got %d: %v", len(headings), headings)
	}
	if headings[0].Text != "Real Heading" || headings[1].Text != "Another Real Heading" {
		t.Errorf("unexpected heading texts: %v", headings)
	}
}

func TestParseHeadings_EmptyDoc(t *testing.T) {
	if got := ParseHeadings(nil); got != nil {
		t.Errorf("nil body: expected nil, got %v", got)
	}
	if got := ParseHeadings([]byte("")); got != nil {
		t.Errorf("empty body: expected nil, got %v", got)
	}
	// Doc with only H1 should return nil (H1 excluded).
	if got := ParseHeadings([]byte("# Only Title\n\nJust paragraph text.\n")); got != nil {
		t.Errorf("H1-only doc: expected nil, got %v", got)
	}
	// Doc with H4+ only should also return nil.
	if got := ParseHeadings([]byte("#### Deep Heading\n\nContent.\n")); got != nil {
		t.Errorf("H4-only doc: expected nil, got %v", got)
	}
}
