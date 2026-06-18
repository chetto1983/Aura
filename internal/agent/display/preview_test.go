package display

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/web"
)

// TestNormalizeToolPreviewWebSearch (D-06): the SHARED decode+normalize over a
// web_search result preview (the {"results":[…]} JSON the runner persists) re-derives
// the same web_result Payload the live path produces from the typed value, and
// accumulates into the registry.
func TestNormalizeToolPreviewWebSearch(t *testing.T) {
	results := []web.Result{
		{Title: "Rome", URL: "https://w.test/rome", Snippet: "sunny"},
		{Title: "Milan", URL: "https://w.test/milan", Snippet: "rain"},
	}
	preview, _ := json.Marshal(map[string]any{"results": results})

	reg := NewRegistry()
	p, ok := NormalizeToolPreview("c1", "web_search", string(preview), reg)
	if !ok {
		t.Fatalf("recognized=false, want true")
	}
	if p.Type != KindWebResult || p.ToolCallID != "c1" || len(p.WebResults) != 2 {
		t.Fatalf("payload mismatch: %+v", p)
	}
	if p.WebResults[0].RefID != "src-1" || p.WebResults[1].RefID != "src-2" {
		t.Fatalf("RefIDs not assigned: %+v", p.WebResults)
	}
	if len(reg.Sources()) != 2 {
		t.Fatalf("registry did not accumulate: %d", len(reg.Sources()))
	}
}

// TestNormalizeToolPreviewParityWithTypedNormalize (D-06 / Pitfall 4): re-deriving
// from the persisted preview yields the SAME Payload (sans live-only registry identity)
// the typed normalizer produces from the concrete value — replay == live by
// construction.
func TestNormalizeToolPreviewParityWithTypedNormalize(t *testing.T) {
	results := []web.Result{{Title: "A", URL: "https://a.test/x", Snippet: "s"}}
	preview, _ := json.Marshal(map[string]any{"results": results})

	live, okLive := NormalizeWithRegistry("c1", "web_search", results, NewRegistry())
	replay, okReplay := NormalizeToolPreview("c1", "web_search", string(preview), NewRegistry())
	if !okLive || !okReplay {
		t.Fatalf("recognized live=%v replay=%v", okLive, okReplay)
	}
	lb, _ := json.Marshal(live)
	rb, _ := json.Marshal(replay)
	if string(lb) != string(rb) {
		t.Fatalf("replay payload diverged from live:\n live=%s\nreplay=%s", lb, rb)
	}
}

// TestNormalizeToolPreviewWebFetch (D-06): a web_fetch document preview re-derives the
// document Payload.
func TestNormalizeToolPreviewWebFetch(t *testing.T) {
	page := web.Page{Title: "Doc", URL: "https://d.test/p", ContentMD: "# Doc"}
	preview, _ := json.Marshal(page)
	reg := NewRegistry()
	p, ok := NormalizeToolPreview("c2", "web_fetch", string(preview), reg)
	if !ok || p.Type != KindDocument || p.Document == nil || p.Document.URL != "https://d.test/p" {
		t.Fatalf("web_fetch preview not re-derived: ok=%v p=%+v", ok, p)
	}
}

// TestNormalizeToolPreviewFallback (D-FALLBACK): an unknown tool, an empty preview, a
// nil registry, and malformed/non-source JSON all return recognized=false so the raw
// card stands on replay exactly as live.
func TestNormalizeToolPreviewFallback(t *testing.T) {
	reg := NewRegistry()
	cases := []struct {
		name, tool, preview string
		reg                 *Registry
	}{
		{"unknown tool", "shell_exec", `{"results":[]}`, reg},
		{"empty preview", "web_search", "", reg},
		{"nil registry", "web_search", `{"results":[{"title":"A","url":"https://a.test"}]}`, nil},
		{"malformed json", "web_search", "{not json", reg},
		{"error-shaped preview", "web_search", `{"error":"blocked_url"}`, reg},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := NormalizeToolPreview("c", tc.tool, tc.preview, tc.reg); ok {
				t.Fatalf("recognized=true, want false (D-FALLBACK)")
			}
		})
	}
}
