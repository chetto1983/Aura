package tools

import (
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

// RenderToolDefs maps the stable-ordered manifest to the []llm.ToolDef wire shape
// LlmAgent.Run puts in req.Tools. It REUSES the alphabetical ordering of Render()
// (cache-stability-load-bearing, manifest.go sort — feedback_aura_cache_poisoning_sites);
// it does NOT re-sort. Deferred tools contribute only Name + Summary (their full
// Description + Parameters schema stay hidden until tool_search loads them, per the
// deferred-tool pattern), so a deferred entry's wire Description falls back to its
// Summary and its Parameters are empty until promoted. Non-deferred tools carry
// their full Description + Parameters JSON schema.
func (r *Registry) RenderToolDefs() []llm.ToolDef {
	entries := r.Render()
	out := make([]llm.ToolDef, 0, len(entries))
	for _, e := range entries {
		var def llm.ToolDef
		def.Type = "function"
		def.Function.Name = e.Name
		if e.Description != "" {
			def.Function.Description = e.Description
		} else {
			def.Function.Description = e.Summary
		}
		if len(e.Parameters) > 0 {
			def.Function.Parameters = append([]byte(nil), e.Parameters...)
		}
		out = append(out, def)
	}
	return out
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
