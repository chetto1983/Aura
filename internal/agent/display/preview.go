package display

import (
	"encoding/json"

	"github.com/chetto1983/aura/internal/web"
)

// NormalizeToolPreview is the SINGLE decode+normalize site shared by the live agent
// loop and the replay snapshot projection (D-06, "one normalizer for live + replay").
// It reverses a source-bearing web tool's model-visible result preview — the exact
// string the runner persists as the RoleTool turn Content AND streams as the live
// TOOL_CALL_RESULT — back into the concrete value Normalize switches on, then runs the
// shared normalizer with the caller-owned per-turn registry. Because live and replay
// both consume the SAME preview shape, the re-derived Payload is identical by
// construction (Pitfall 4: no preview-vs-full-bytes drift).
//
// Only the source-bearing web tools this protocol wires are recognized:
//   - web_search → []web.Result (the adapter marshals {"results":[…]}, web_search.go)
//   - web_fetch  → web.Page     (the adapter marshals the Page directly)
//
// Every other tool, an empty preview, an inline {error,…} preview, or malformed JSON
// returns (Payload{}, false) so the caller keeps the raw escaped card (D-FALLBACK) and
// the registry is untouched — replay D-FALLBACK == live D-FALLBACK.
func NormalizeToolPreview(toolCallID, toolName, preview string, reg *Registry) (Payload, bool) {
	if preview == "" || reg == nil {
		return Payload{}, false
	}
	result, ok := decodeToolPreview(toolName, preview)
	if !ok {
		return Payload{}, false
	}
	return NormalizeWithRegistry(toolCallID, toolName, result, reg)
}

// decodeToolPreview decodes a source-bearing web tool's result preview into the
// concrete value NormalizeWithRegistry expects. A non-web tool, an error-shaped
// preview, or malformed JSON returns ok=false.
func decodeToolPreview(toolName, preview string) (any, bool) {
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
