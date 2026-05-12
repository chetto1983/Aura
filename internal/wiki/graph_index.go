package wiki

import (
	"sort"
	"sync"
)

// GraphIndex is an in-memory adjacency index over the wiki graph used by
// the retrieval layer (Wave 3) to traverse [[wiki-link]] + `related:`
// edges at query time without re-scanning disk. It is maintained by the
// Store: NewStore populates it at boot from BuildGraphFromReader, and
// WritePage / DeletePage refresh single-node deltas as pages mutate.
//
// Edges are stored undirected by convention — both [[wiki-link]] body
// references and `related:` frontmatter entries become bidirectional
// after the Wave 2.1 backlink maintenance pass. Retrieval expands a
// seed slug by either direction equally; the explicit Out/In accessors
// let callers stick to a single direction when they need to.
type GraphIndex struct {
	mu       sync.RWMutex
	outbound map[string]map[string]bool // slug -> set of slugs it links to
	inbound  map[string]map[string]bool // slug -> set of slugs that link to it
	meta     map[string]NodeMeta        // slug -> snapshot of node metadata
}

// NodeMeta carries the lightweight per-node info retrieval ranking needs
// without forcing a disk re-read. Title, category, tags are stable
// enough that the index can serve as the source of truth for any UI
// or scoring layer that doesn't need the page body.
type NodeMeta struct {
	Slug     string
	Title    string
	Category string
	Tags     []string
}

// NewGraphIndex returns an empty index ready for population.
func NewGraphIndex() *GraphIndex {
	return &GraphIndex{
		outbound: make(map[string]map[string]bool),
		inbound:  make(map[string]map[string]bool),
		meta:     make(map[string]NodeMeta),
	}
}

// LoadFromPages rebuilds the index from a full snapshot of pages.
// Called at boot (NewStore) and after disruptive maintenance like
// RebuildGraph. Replaces existing state atomically.
func (g *GraphIndex) LoadFromPages(pages map[string]*Page) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.outbound = make(map[string]map[string]bool, len(pages))
	g.inbound = make(map[string]map[string]bool, len(pages))
	g.meta = make(map[string]NodeMeta, len(pages))

	for slug, page := range pages {
		if page == nil {
			continue
		}
		g.upsertLocked(slug, page)
	}
}

// RefreshPage updates the index for a single slug after a successful
// WritePage. New outbound edges are added; outbound edges that
// disappeared (the body no longer contains [[target]] and `related:`
// no longer lists target) have their reverse-inbound entries cleaned
// up so the index stays consistent with on-disk reality.
func (g *GraphIndex) RefreshPage(slug string, page *Page) {
	if g == nil || page == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.upsertLocked(slug, page)
}

// RemoveNode purges a slug from the index after a DeletePage. Its
// outbound edges are dropped and every page that linked TO it loses
// the corresponding inbound edge. Broken-ref detection is left to
// the materialized graph layer (BuildGraph) — the in-mem index only
// tracks live nodes for fast traversal.
func (g *GraphIndex) RemoveNode(slug string) {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	// Remove this slug as a source of edges. Each target loses an
	// inbound entry pointing back at slug.
	for target := range g.outbound[slug] {
		if set := g.inbound[target]; set != nil {
			delete(set, slug)
			if len(set) == 0 {
				delete(g.inbound, target)
			}
		}
	}
	delete(g.outbound, slug)
	// Remove this slug as a target of edges. Each source loses an
	// outbound entry pointing at slug. We keep the source nodes — they
	// still exist on disk and may just have a now-broken ref.
	for source := range g.inbound[slug] {
		if set := g.outbound[source]; set != nil {
			delete(set, slug)
			if len(set) == 0 {
				delete(g.outbound, source)
			}
		}
	}
	delete(g.inbound, slug)
	delete(g.meta, slug)
}

// upsertLocked is the shared write path for LoadFromPages and
// RefreshPage. Caller MUST hold g.mu.Lock().
func (g *GraphIndex) upsertLocked(slug string, page *Page) {
	// Compute the canonical edge set: body wikilinks ∪ frontmatter related.
	newTargets := make(map[string]bool)
	for _, t := range ExtractWikiLinks(page.Body) {
		if t != "" && t != slug {
			newTargets[t] = true
		}
	}
	for _, t := range page.Related {
		t = Slug(t)
		if t != "" && t != slug {
			newTargets[t] = true
		}
	}

	// Diff against the previous outbound set so we can prune stale
	// inbound back-references on targets that are no longer linked.
	prev := g.outbound[slug]
	for target := range prev {
		if !newTargets[target] {
			if set := g.inbound[target]; set != nil {
				delete(set, slug)
				if len(set) == 0 {
					delete(g.inbound, target)
				}
			}
		}
	}

	// Install the new outbound set.
	if len(newTargets) == 0 {
		delete(g.outbound, slug)
	} else {
		g.outbound[slug] = newTargets
	}
	// Mirror into inbound on each target.
	for target := range newTargets {
		set := g.inbound[target]
		if set == nil {
			set = make(map[string]bool)
			g.inbound[target] = set
		}
		set[slug] = true
	}

	// Snapshot metadata. Tags is copied so later page mutations don't
	// leak into the index.
	tagsCopy := make([]string, len(page.Tags))
	copy(tagsCopy, page.Tags)
	g.meta[slug] = NodeMeta{
		Slug:     slug,
		Title:    page.Title,
		Category: page.Category,
		Tags:     tagsCopy,
	}
}

// Neighbors returns slugs reachable from `slug` within `depth` hops
// in either direction (outbound OR inbound edges), excluding `slug`
// itself. depth<=0 returns nil. Results are sorted alphabetically for
// deterministic test fixtures.
func (g *GraphIndex) Neighbors(slug string, depth int) []string {
	if g == nil || depth <= 0 {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()

	seen := map[string]bool{slug: true}
	frontier := []string{slug}
	for hop := 0; hop < depth; hop++ {
		var next []string
		for _, s := range frontier {
			for t := range g.outbound[s] {
				if !seen[t] {
					seen[t] = true
					next = append(next, t)
				}
			}
			for t := range g.inbound[s] {
				if !seen[t] {
					seen[t] = true
					next = append(next, t)
				}
			}
		}
		if len(next) == 0 {
			break
		}
		frontier = next
	}

	out := make([]string, 0, len(seen)-1)
	for s := range seen {
		if s == slug {
			continue
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// OutNeighbors returns directly-linked-FROM targets of `slug`.
// Useful when retrieval wants "what does this page reference" without
// the reverse "who references this page" expansion.
func (g *GraphIndex) OutNeighbors(slug string) []string {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	set := g.outbound[slug]
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// InNeighbors returns slugs that link TO `slug` — the backlink list.
func (g *GraphIndex) InNeighbors(slug string) []string {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	set := g.inbound[slug]
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Degree returns (inboundEdges, outboundEdges) for `slug`. Used for
// centrality-aware retrieval ranking (Wave 3) where hub pages with
// high degree get a relevance boost.
func (g *GraphIndex) Degree(slug string) (in, out int) {
	if g == nil {
		return 0, 0
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.inbound[slug]), len(g.outbound[slug])
}

// HasNode reports whether the index knows about `slug`. A node is
// "known" if it has metadata recorded (i.e. the page existed on disk
// when the index last saw it).
func (g *GraphIndex) HasNode(slug string) bool {
	if g == nil {
		return false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.meta[slug]
	return ok
}

// Meta returns a snapshot of the cached metadata for `slug` and an
// `ok` flag. Callers must not mutate the returned NodeMeta.Tags slice;
// the index returns the same backing array for performance.
func (g *GraphIndex) Meta(slug string) (NodeMeta, bool) {
	if g == nil {
		return NodeMeta{}, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	m, ok := g.meta[slug]
	return m, ok
}

// NodeCount returns the number of nodes currently in the index.
func (g *GraphIndex) NodeCount() int {
	if g == nil {
		return 0
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.meta)
}
