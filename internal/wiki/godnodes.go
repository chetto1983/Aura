package wiki

import (
	"slices"
	"sort"
)

// GodNode is a degree-centrality snapshot for one wiki page. The degree
// counts are computed from the in-memory GraphIndex — no disk scan.
type GodNode struct {
	Slug        string
	Title       string
	Category    string
	InDegree    int
	OutDegree   int
	TotalDegree int
}

// NodeSkipFunc decides whether a node should be excluded from a ranked
// degree-centrality view. Returning true skips the node before topK
// trimming so the caller still receives a full top-K window. Pass nil
// to disable filtering (raw graph behavior).
type NodeSkipFunc func(slug string, meta NodeMeta) bool

// IsAuxiliaryHubNode reports whether a slug is an auto-generated or
// operational page that should not surface in a god-node ranking.
// Mirrors graphify analyze.py _is_file_node + _is_concept_node intent:
// these nodes are connected by construction rather than because they
// represent meaningful abstractions.
//
// Filters:
//   - operational slugs (index, log, schema) — auto-generated catalogs
//   - category "uncategorized" — the rebuild emits a uncategorized.md
//     hub that links every orphan page
//   - frontmatter tag "hub" — explicit "I am a hub" marker
//   - empty Title — placeholder / never-written pages that survive as
//     dangling [[link]] targets
func IsAuxiliaryHubNode(slug string, meta NodeMeta) bool {
	if IsOperationalSlug(slug) {
		return true
	}
	if meta.Category == "uncategorized" {
		return true
	}
	if slices.Contains(meta.Tags, "hub") {
		return true
	}
	if meta.Title == "" {
		return true
	}
	return false
}

// TopByDegree returns up to topK nodes ranked by TotalDegree (InDegree +
// OutDegree) descending. Ties are broken alphabetically by slug for
// deterministic output. Returns nil when g is nil or topK ≤ 0. Returns
// all nodes when topK exceeds NodeCount.
//
// This is the raw graph view — operational slugs and auto-generated hubs
// are included. Use TopByDegreeFiltered with IsAuxiliaryHubNode for the
// user-facing god-node list.
func (g *GraphIndex) TopByDegree(topK int) []GodNode {
	return g.TopByDegreeFiltered(topK, nil)
}

// TopByDegreeFiltered returns up to topK nodes ranked by TotalDegree, with
// skip applied BEFORE the topK trim so callers receive a full window of
// non-skipped nodes. Pass nil skip to fall back to TopByDegree's raw view.
func (g *GraphIndex) TopByDegreeFiltered(topK int, skip NodeSkipFunc) []GodNode {
	if g == nil || topK <= 0 {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()

	if len(g.meta) == 0 {
		return nil
	}
	nodes := make([]GodNode, 0, len(g.meta))
	for slug, m := range g.meta {
		if skip != nil && skip(slug, m) {
			continue
		}
		in := len(g.inbound[slug])
		out := len(g.outbound[slug])
		nodes = append(nodes, GodNode{
			Slug:        slug,
			Title:       m.Title,
			Category:    m.Category,
			InDegree:    in,
			OutDegree:   out,
			TotalDegree: in + out,
		})
	}
	if len(nodes) == 0 {
		return nil
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].TotalDegree != nodes[j].TotalDegree {
			return nodes[i].TotalDegree > nodes[j].TotalDegree
		}
		return nodes[i].Slug < nodes[j].Slug
	})
	if topK < len(nodes) {
		return nodes[:topK]
	}
	return nodes
}

// TopNodes returns the top-K wiki pages by degree centrality from the
// in-memory graph index, filtered to user-facing entities (operational
// catalogs, hub pages, and the auto-generated uncategorized hub are
// excluded). Returns nil when the store has no graph index, topK ≤ 0,
// or every node was filtered out.
func (s *Store) TopNodes(topK int) []GodNode {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	return s.graphIndex.TopByDegreeFiltered(topK, IsAuxiliaryHubNode)
}

// WikiPath returns the shortest undirected path between two page slugs,
// forwarding to GraphIndex.ShortestPath. Returns nil when no path exists
// within maxHops or the store has no graph index.
func (s *Store) WikiPath(from, to string, maxHops int) []string {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	return s.graphIndex.ShortestPath(from, to, maxHops)
}

// PersonalizedPageRank runs PPR seeded on seeds with the supplied alpha and
// iter count. Returns nil when the store has no graph index.
func (s *Store) PersonalizedPageRank(seeds []string, alpha float64, iter int) map[string]float64 {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	return PersonalizedPageRank(s.graphIndex, seeds, alpha, iter)
}

// P99Degree returns the 99th-percentile total degree across wiki page nodes.
// Returns 50 as a safety floor when the store has no graph index.
func (s *Store) P99Degree() int {
	if s == nil || s.graphIndex == nil {
		return 50
	}
	return s.graphIndex.P99Degree()
}

// NeighborsHubAware returns slugs reachable from slug within depth hops,
// skipping expansion through hub nodes with total degree ≥ skipDegree.
// Returns nil when the store has no graph index.
func (s *Store) NeighborsHubAware(slug string, depth, skipDegree int) []string {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	return s.graphIndex.NeighborsHubAware(slug, depth, skipDegree)
}

// NodeMeta returns the cached NodeMeta for slug and an ok flag.
// Returns false when the store has no graph index or the slug is unknown.
func (s *Store) NodeMeta(slug string) (NodeMeta, bool) {
	if s == nil || s.graphIndex == nil {
		return NodeMeta{}, false
	}
	return s.graphIndex.Meta(slug)
}

// OutNeighbors returns the page-level outbound neighbors of slug.
// Returns nil when the store has no graph index.
func (s *Store) OutNeighbors(slug string) []string {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	return s.graphIndex.OutNeighbors(slug)
}
