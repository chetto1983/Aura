package display

import (
	"strings"
	"testing"
)

// TestRegistryStableURLKeyedRefIDs (DISP-05): the same URL returns the same RefID
// and Index across calls in a turn; a different URL gets the next number.
func TestRegistryStableURLKeyedRefIDs(t *testing.T) {
	reg := NewRegistry()
	a1 := reg.Add(KindWebResult, "A", "https://example.com/a", "snippet a", false)
	b1 := reg.Add(KindWebResult, "B", "https://example.com/b", "snippet b", false)
	a2 := reg.Add(KindWebResult, "A again", "https://example.com/a", "snippet a", false)

	if a1 != "src-1" || b1 != "src-2" {
		t.Fatalf("RefIDs = %q,%q, want src-1,src-2", a1, b1)
	}
	if a2 != a1 {
		t.Fatalf("same URL got a new RefID: first %q, second %q", a1, a2)
	}
	if got := reg.Sources(); len(got) != 2 {
		t.Fatalf("registry size = %d, want 2 (duplicate URL must not add an entry)", len(got))
	}
}

// TestRegistryURLNormalizationCollapses: fragment + trailing slash collapse to one
// source key, so "#section" and "/" variants of one page share a RefID.
func TestRegistryURLNormalizationCollapses(t *testing.T) {
	reg := NewRegistry()
	r1 := reg.Add(KindWebResult, "P", "https://example.com/page/", "", false)
	r2 := reg.Add(KindWebResult, "P", "https://example.com/page#section", "", false)
	if r1 != r2 {
		t.Fatalf("normalized URL variants got different RefIDs: %q vs %q", r1, r2)
	}
}

// TestRegistryCitedVsConsulted (DISP-05): Add(cited=false) records a consulted
// source; a later cited=true upgrades the SAME entry to cited (never the reverse).
func TestRegistryCitedVsConsulted(t *testing.T) {
	reg := NewRegistry()
	reg.Add(KindWebResult, "A", "https://example.com/a", "", false)
	if reg.Sources()[0].Cited {
		t.Fatalf("source marked cited before any citation")
	}
	reg.Add(KindWebResult, "A", "https://example.com/a", "", true)
	if !reg.Sources()[0].Cited {
		t.Fatalf("cited=true did not upgrade the consulted source")
	}
	// A re-add with cited=false must NOT downgrade a cited source.
	reg.Add(KindWebResult, "A", "https://example.com/a", "", false)
	if !reg.Sources()[0].Cited {
		t.Fatalf("cited source was downgraded by a later consulted add")
	}
}

// TestRegistryEmptyURLSkipped: an empty/unparseable URL is not registered (a source
// must have a stable identity to be citable).
func TestRegistryEmptyURLSkipped(t *testing.T) {
	reg := NewRegistry()
	if ref := reg.Add(KindWebResult, "x", "", "", false); ref != "" {
		t.Fatalf("empty URL got RefID %q, want empty", ref)
	}
	if ref := reg.Add(KindWebResult, "x", "::not a url::", "", false); ref != "" {
		t.Fatalf("unparseable URL got RefID %q, want empty", ref)
	}
	if len(reg.Sources()) != 0 {
		t.Fatalf("registry recorded a source with no stable URL identity")
	}
}

// TestRenderSourceListFormat (DISP-05): the numbered "[n] Title — url" block is
// emitted in Index order; an empty registry renders "".
func TestRenderSourceListFormat(t *testing.T) {
	if got := NewRegistry().RenderSourceList(); got != "" {
		t.Fatalf("empty registry rendered %q, want empty", got)
	}
	reg := NewRegistry()
	reg.Add(KindWebResult, "Alpha", "https://a.test/x", "", false)
	reg.Add(KindWebResult, "", "https://b.test/y", "", false) // empty title falls back to URL
	got := reg.RenderSourceList()
	want := "[1] Alpha — https://a.test/x\n[2] https://b.test/y — https://b.test/y"
	if got != want {
		t.Fatalf("RenderSourceList =\n%q\nwant\n%q", got, want)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("rendered list has a trailing newline")
	}
}

// On 2026-10-07 an answer cited [3] twice for claims its snippet never made: [3] was a
// SiePortal page whose fetch had answered 403, and the list gave no sign of it. A
// failed fetch now marks the source, so the model knows it holds only a snippet.
func TestRenderSourceListMarksASourceWhoseFetchFailed(t *testing.T) {
	reg := NewRegistry()
	reg.Add(KindWebResult, "SiePortal", "https://sieportal.test/detail", "16 documents", false)
	reg.Add(KindWebResult, "Datasheet", "https://rs.test/a.pdf", "", false)
	failFetch(t, reg, "https://sieportal.test/detail", `{"error":"http_error","message":"non-success status","status_code":403}`)
	failFetch(t, reg, "https://rs.test/a.pdf", `{"error":"extraction_failed","reason":"pdf_without_text","message":"scan"}`)

	want := "[1] SiePortal — https://sieportal.test/detail (not read: http_error status 403; only its search snippet was seen)\n" +
		"[2] Datasheet — https://rs.test/a.pdf (not read: extraction_failed (pdf_without_text); only its search snippet was seen)"
	if got := reg.RenderSourceList(); got != want {
		t.Fatalf("RenderSourceList =\n%q\nwant\n%q", got, want)
	}
	if got := reg.Sources()[0].Unread; got != "http_error status 403" {
		t.Errorf("Sources()[0].Unread = %q, want the failure for the source explorer too", got)
	}
}

func TestAFailedFetchOfAnUnlistedURLAddsNoSource(t *testing.T) {
	reg := NewRegistry()
	failFetch(t, reg, "https://guessed.test/page", `{"error":"http_error","status_code":404}`)
	if got := reg.RenderSourceList(); got != "" {
		t.Errorf("a URL nobody listed became source %q", got)
	}
}

func TestALaterSuccessfulFetchClearsTheMark(t *testing.T) {
	reg := NewRegistry()
	reg.Add(KindWebResult, "Page", "https://p.test/a", "", false)
	failFetch(t, reg, "https://p.test/a", `{"error":"timeout","message":"fetch timed out"}`)
	page := `{"title":"Page","url":"https://p.test/a","content_md":"read at last","links":null}`
	if _, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "c2", ToolName: "web_fetch",
		Arguments: `{"url":"https://p.test/a"}`, ResultPreview: page}, reg); !ok {
		t.Fatal("the successful fetch was not recognized")
	}
	if got := reg.RenderSourceList(); got != "[1] Page — https://p.test/a" {
		t.Errorf("RenderSourceList = %q, want the mark gone", got)
	}
}

// A failed fetch keeps its raw card: only the registry learns from it.
func failFetch(t *testing.T, reg *Registry, url, preview string) {
	t.Helper()
	args := `{"url":"` + url + `"}`
	if _, ok := NormalizeToolPreview(PreviewInput{ToolCallID: "c", ToolName: "web_fetch",
		Arguments: args, ResultPreview: preview}, reg); ok {
		t.Fatalf("a failed fetch rendered a typed payload")
	}
}
