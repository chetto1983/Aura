package display

import (
	"fmt"
	"net/url"
	"strings"
)

// Registry is the per-turn source accumulator (D-05). Code — never the model —
// owns the [n]->source map: a unique normalized URL gets a stable RefID and a
// stable 1-based Index the first time it is seen, and the same URL returns the
// same RefID/Index on every later call in the turn. The model can therefore only
// cite a number the registry rendered (it cannot reference a hallucinated source).
//
// A Registry is NOT safe for concurrent use; each turn owns one and feeds it
// sequentially as tools complete (the live path and the replay path each build a
// fresh Registry, so live and replay are identical by construction — Pitfall 4).
type Registry struct {
	byURL map[string]int // normalized URL -> slice index in order
	items []Source
}

// NewRegistry returns an empty per-turn source registry.
func NewRegistry() *Registry { return &Registry{byURL: map[string]int{}} }

// Add records a consulted source for rawURL, assigning a stable RefID + 1-based
// Index on first sight and returning the existing entry's RefID on a repeat. An
// empty/unparseable URL is skipped (returns ""). cited marks the source as
// referenced; a later cited=true upgrades a previously-consulted entry (a source
// the model cited is also one it consulted, never the reverse).
func (r *Registry) Add(srcType Kind, title, rawURL, snippet string, cited bool) string {
	key := normalizeURL(rawURL)
	if key == "" {
		return ""
	}
	if idx, ok := r.byURL[key]; ok {
		if cited {
			r.items[idx].Cited = true
		}
		return r.items[idx].RefID
	}
	idx := len(r.items)
	refID := fmt.Sprintf("src-%d", idx+1)
	r.items = append(r.items, Source{
		RefID:   refID,
		Index:   idx + 1,
		Type:    srcType,
		Title:   title,
		URL:     rawURL,
		Snippet: snippet,
		Cited:   cited,
	})
	r.byURL[key] = idx
	return refID
}

// Sources returns the accumulated registry entries in stable Index order (a copy
// so the caller cannot mutate the registry's backing slice).
func (r *Registry) Sources() []Source {
	out := make([]Source, len(r.items))
	copy(out, r.items)
	return out
}

// RenderSourceList renders the numbered "[n] Title — url" block the model sees in
// the tool-result preview (D-05). It is the VOLATILE per-turn data that must ride
// the tail-inject copy path (prompt.Budget), never messages[0] — putting it in the
// cached prefix trips the AG-031 KV-drift guard. Returns "" for an empty registry.
func (r *Registry) RenderSourceList() string {
	if len(r.items) == 0 {
		return ""
	}
	var b strings.Builder
	for i := range r.items {
		s := &r.items[i]
		title := s.Title
		if title == "" {
			title = s.URL
		}
		fmt.Fprintf(&b, "[%d] %s — %s\n", s.Index, title, s.URL)
	}
	return strings.TrimRight(b.String(), "\n")
}

// normalizeURL is the stable registry key: lower-cased scheme+host+path with the
// fragment and a trailing slash dropped, so the same page reached via "#section"
// or a trailing "/" collapses to one source. An unparseable URL keys empty (it is
// not registered — a source must have a stable identity to be citable).
func normalizeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Host)
	path := strings.TrimRight(u.Path, "/")
	key := scheme + "://" + host + path
	if u.RawQuery != "" {
		key += "?" + u.RawQuery
	}
	return key
}
