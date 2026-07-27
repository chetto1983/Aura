package tools

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// ManifestEntry is one row in the LLM-visible tool manifest. Deferred tools
// contribute only Name + Summary; non-deferred contribute the full Description
// + Parameters schema.
type ManifestEntry struct {
	Name        string
	Summary     string
	Description string
	Parameters  []byte
	Deferred    bool
}

// Render returns the manifest as a stable-ordered slice — alphabetical by Name
// for cache stability. Stable ordering matters: any reshuffle invalidates the
// provider-side prompt cache. See [[feedback_aura_cache_poisoning_sites_2026-05-27]].
func (r *Registry) Render() []ManifestEntry {
	out := make([]ManifestEntry, 0, len(r.tools))
	for _, t := range r.tools {
		s := t.Spec()
		entry := ManifestEntry{
			Name:     s.Name,
			Summary:  s.Summary,
			Deferred: s.Deferred,
		}
		if !s.Deferred {
			entry.Description = s.Description
			entry.Parameters = []byte(s.Parameters)
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RenderToolDefs maps the registry to the []llm.ToolDef wire shape LlmAgent.Run
// puts in req.Tools, applying the full-promotion deferred-tool filter (Claude
// Code / OpenAI Agents parity): a deferred tool is EXCLUDED from the callable set
// entirely until tool_search loads its schema — its name must be a key in the
// per-run `activated` set to appear. This is the fix for the empty-schema footgun:
// the old code emitted every deferred tool as a callable function with an EMPTY
// parameter schema, which made the model hallucinate arguments (e.g. send_file
// {"file":...} instead of {"path":...}). A callable function with no schema is a
// trap; keeping deferred tools hidden until their schema is loaded mirrors Claude
// Code ("remain hidden until the model explicitly requests them") and matches
// Aura's own system prompt, which already tells the model deferred tools are
// "absent from the current list… discover with tool_search first".
//
// It iterates the registry directly (NOT via Render(), which strips a deferred
// tool's Description + Parameters) so a PROMOTED deferred tool ships its FULL
// Description + Parameters schema. `activated` is nil-safe: a nil map makes every
// lookup a miss, so all deferred tools are omitted. The final alphabetical sort
// is cache-stability-load-bearing (manifest.go — feedback_aura_cache_poisoning_sites):
// any reshuffle invalidates the provider-side prompt cache, so the order is fixed
// regardless of which tools are promoted.
func (r *Registry) RenderToolDefs(activated map[string]struct{}) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(r.tools))
	for _, t := range r.tools {
		s := t.Spec()
		if s.Deferred {
			if _, ok := activated[s.Name]; !ok {
				continue // hidden until tool_search promotes it (schema-load-before-call)
			}
		}
		var def llm.ToolDef
		def.Type = "function"
		def.Function.Name = s.Name
		if s.Description != "" {
			def.Function.Description = s.Description
		} else {
			def.Function.Description = s.Summary
		}
		if len(s.Parameters) > 0 {
			def.Function.Parameters = append([]byte(nil), s.Parameters...)
		}
		out = append(out, def)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Function.Name < out[j].Function.Name })
	return out
}

// ManifestEntryJSON is one tool as the offline tooling needs it: the exact fields
// searchDocument feeds to BM25 and to the embedding bank (Name, Description,
// Parameters), plus Deferred so a consumer can reproduce the frozen candidate set.
// Summary rides along for human reading only.
type ManifestEntryJSON struct {
	Name        string          `json:"name"`
	Deferred    bool            `json:"deferred"`
	Summary     string          `json:"summary,omitempty"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// RenderJSON dumps the full specs of the live manifest.
//
// It exists because the adaptive tool-discovery dataset must derive its expected
// labels FROM the running registry instead of having them typed by hand. The frozen
// benchmark dataset was hand-written, and 8 of its 12 tool_discovery scenarios name
// tools Aura has never had (`weather_now`, `send_email`, `finance_quote`) or use the
// un-namespaced form of ones it does (`memory_add_preference` vs
// `memory__memory_add_preference`). The evaluator compares by exact string equality,
// so those scenarios can never pass under ANY ranking strategy — every arm scores the
// same zero and the benchmark cannot tell them apart. A label generated from this
// dump cannot make that mistake.
//
// Name/Description/Parameters are exactly what searchDocument consumes, so a consumer
// can rebuild a byte-identical ranking document offline.
func (r *Registry) RenderJSON() ([]byte, error) {
	entries := make([]ManifestEntryJSON, 0, len(r.tools))
	for _, t := range r.tools {
		s := t.Spec()
		entries = append(entries, ManifestEntryJSON{
			Name:        s.Name,
			Deferred:    s.Deferred,
			Summary:     s.Summary,
			Description: s.Description,
			Parameters:  s.Parameters,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return json.MarshalIndent(entries, "", "  ")
}

// RenderText is a human-readable rendering of the manifest, useful for boot
// logs and `aura tools` CLI subcommand.
func (r *Registry) RenderText() string {
	var b strings.Builder
	for _, e := range r.Render() {
		if e.Deferred {
			b.WriteString("[deferred] ")
		} else {
			b.WriteString("[active]   ")
		}
		b.WriteString(e.Name)
		b.WriteString(" — ")
		b.WriteString(e.Summary)
		b.WriteByte('\n')
	}
	return b.String()
}
