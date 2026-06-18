package display

import (
	"testing"

	"github.com/chetto1983/aura/internal/web"
)

// TestNormalizeWebSearch (DISP-01): each web.Result becomes a WebItem with Domain
// derived from the host, the metadata tier lifted from ResultMetadata, and a stable
// URL-keyed RefID; the registry carries the consulted sources.
func TestNormalizeWebSearch(t *testing.T) {
	published := "2026-06-18"
	results := []web.Result{
		{Title: "Forecast", URL: "https://weather.example.com/today", Snippet: "sunny",
			Metadata: &web.ResultMetadata{Score: 0.91, PublishedAt: &published, Thumbnail: "https://weather.example.com/t.png"}},
		{Title: "News", URL: "https://news.example.org/a", Snippet: "headline"},
	}
	reg := NewRegistry()
	p, ok := normalizeWebSearch("call-1", results, reg)
	if !ok {
		t.Fatalf("normalizeWebSearch recognized=false, want true")
	}
	if p.Type != KindWebResult || p.ToolCallID != "call-1" {
		t.Fatalf("payload type/id = %v/%q, want web_result/call-1", p.Type, p.ToolCallID)
	}
	if len(p.WebResults) != 2 {
		t.Fatalf("WebResults = %d, want 2", len(p.WebResults))
	}
	w0 := p.WebResults[0]
	if w0.Domain != "weather.example.com" {
		t.Fatalf("Domain = %q, want weather.example.com", w0.Domain)
	}
	if w0.Score != 0.91 || w0.PublishedAt == nil || *w0.PublishedAt != published || w0.Thumbnail == "" {
		t.Fatalf("metadata not lifted from ResultMetadata: %+v", w0)
	}
	if w0.RefID != "src-1" || p.WebResults[1].RefID != "src-2" {
		t.Fatalf("RefIDs = %q,%q, want src-1,src-2", w0.RefID, p.WebResults[1].RefID)
	}
	if len(p.Sources) != 2 {
		t.Fatalf("Sources = %d, want 2", len(p.Sources))
	}
	// Tier-1 result (no metadata) keeps the metadata fields zero.
	if p.WebResults[1].Score != 0 || p.WebResults[1].PublishedAt != nil {
		t.Fatalf("tier-1 result leaked metadata: %+v", p.WebResults[1])
	}
}

// TestNormalizeWebSearchEmpty: an empty result set is still a recognized web_result
// (a "no hits" typed display), not a fallback.
func TestNormalizeWebSearchEmpty(t *testing.T) {
	p, ok := normalizeWebSearch("call-1", nil, NewRegistry())
	if !ok || p.Type != KindWebResult || len(p.WebResults) != 0 {
		t.Fatalf("empty search = (%+v, %v), want recognized empty web_result", p, ok)
	}
}

// TestNormalizeWebSearchUnparseableURLDomain: a hit whose URL cannot yield a host
// gets an empty Domain (a display hint, never a security decision) and is not
// registered as a source.
func TestNormalizeWebSearchUnparseableURLDomain(t *testing.T) {
	reg := NewRegistry()
	p, ok := normalizeWebSearch("c", []web.Result{{Title: "x", URL: "::bad::"}}, reg)
	if !ok {
		t.Fatalf("recognized=false")
	}
	if p.WebResults[0].Domain != "" {
		t.Fatalf("Domain = %q, want empty for an unparseable URL", p.WebResults[0].Domain)
	}
	if len(p.Sources) != 0 {
		t.Fatalf("unparseable URL registered as a source: %+v", p.Sources)
	}
}

// TestNormalizeWebFetch (DISP-01): a web.Page becomes a document payload carrying
// the markdown, and the page is registered as a consulted source.
func TestNormalizeWebFetch(t *testing.T) {
	page := web.Page{Title: "Doc Title", URL: "https://docs.example.com/p", ContentMD: "# Heading\n\nbody"}
	reg := NewRegistry()
	p, ok := normalizeWebFetch("call-2", page, reg)
	if !ok || p.Type != KindDocument {
		t.Fatalf("normalizeWebFetch = (%v, %v), want document/true", p.Type, ok)
	}
	if p.Document == nil || p.Document.ContentMD != "# Heading\n\nbody" || p.Document.URL != page.URL {
		t.Fatalf("document payload wrong: %+v", p.Document)
	}
	if len(p.Sources) != 1 || p.Sources[0].URL != page.URL {
		t.Fatalf("fetched page not registered as a source: %+v", p.Sources)
	}
}
