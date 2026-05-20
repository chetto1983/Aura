package wiki

import "sort"

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

// TopByDegree returns up to topK nodes ranked by TotalDegree (InDegree +
// OutDegree) descending. Ties are broken alphabetically by slug for
// deterministic output. Returns nil when g is nil or topK ≤ 0. Returns
// all nodes when topK exceeds NodeCount.
func (g *GraphIndex) TopByDegree(topK int) []GodNode {
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
// in-memory graph index. Returns nil when the store has no graph index
// or topK ≤ 0.
func (s *Store) TopNodes(topK int) []GodNode {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	return s.graphIndex.TopByDegree(topK)
}
