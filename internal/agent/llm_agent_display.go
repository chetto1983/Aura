package agent

import "github.com/chetto1983/aura/internal/agent/display"

// deriveDisplay turns a completed web tool run into a typed display.Payload while
// accumulating its sources into the per-run registry (D-05). It is the live emit site
// for the source list + the aura.display CUSTOM event: it delegates to the SHARED
// display.NormalizeToolPreview, which decodes the model-visible result preview (the same
// preview the runner persists, so the D-06 replay re-derive at projectMessages is
// identical by construction — Pitfall 4) and runs the normalizer with the per-run
// registry.
//
// Only the source-bearing web tools are recognized; every other tool (and any decode
// failure) returns (nil, false) so the event keeps its raw card (D-FALLBACK) and the
// registry is untouched. The returned Payload is non-nil only when recognized.
func (a *LlmAgent) deriveDisplay(toolCallID, toolName, preview string) (*display.Payload, bool) {
	if a.sources == nil {
		return nil, false
	}
	p, ok := display.NormalizeToolPreview(toolCallID, toolName, preview, a.sources)
	if !ok {
		return nil, false
	}
	out := p
	return &out, true
}
