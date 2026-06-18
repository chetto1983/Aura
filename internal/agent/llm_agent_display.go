package agent

import (
	"encoding/json"

	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/web"
)

// deriveDisplay turns a completed web tool run into a typed display.Payload while
// accumulating its sources into the per-run registry (D-05). It is the SINGLE live
// decode site for the source list + the aura.display CUSTOM event: it decodes the
// model-visible result preview back to the concrete shape display.Normalize expects
// (the same preview the runner persists, so the D-06 replay re-derive at
// projectMessages is identical by construction — Pitfall 4) and runs the shared
// normalizer with the caller-owned registry.
//
// It recognizes only the source-bearing web tools this plan wires; every other tool
// (and any decode failure) returns (nil, ok=false) so the event keeps its raw card
// (D-FALLBACK) and the registry is untouched. The returned Payload is non-nil only
// when the normalizer recognized the result.
func (a *LlmAgent) deriveDisplay(toolCallID, toolName, preview string) (*display.Payload, bool) {
	if a.sources == nil || preview == "" {
		return nil, false
	}
	result, ok := decodeWebToolResult(toolName, preview)
	if !ok {
		return nil, false
	}
	p, recognized := display.NormalizeWithRegistry(toolCallID, toolName, result, a.sources)
	if !recognized {
		return nil, false
	}
	out := p
	return &out, true
}

// decodeWebToolResult reverses the web tool adapters' result rendering back into the
// concrete value display.Normalize switches on:
//   - web_search → []web.Result (the adapter marshals {"results":[…]}, web_search.go:101)
//   - web_fetch  → web.Page     (the adapter marshals the Page directly)
//
// A preview carrying the inline {error,reason,message} sanitized WebError shape (D-41)
// is NOT decoded here — it is not a source-bearing success and an error display is out
// of this plan's scope (system_event live emit is a later wiring), so it returns
// ok=false and the raw card stands. Any non-web tool or malformed JSON returns ok=false.
func decodeWebToolResult(toolName, preview string) (any, bool) {
	switch toolName {
	case "web_search":
		var wrap struct {
			Results []web.Result `json:"results"`
		}
		if err := json.Unmarshal([]byte(preview), &wrap); err != nil || wrap.Results == nil {
			return nil, false
		}
		return wrap.Results, true
	case "web_fetch":
		var page web.Page
		if err := json.Unmarshal([]byte(preview), &page); err != nil || page.URL == "" {
			return nil, false
		}
		return page, true
	default:
		return nil, false
	}
}
