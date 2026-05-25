package wiki

import "sort"

// KnowledgeGap is a page-level graph health signal for wiki pages with few
// edges. Subnodes and auxiliary/operational hubs are excluded before ranking.
type KnowledgeGap struct {
	Slug        string
	Title       string
	Category    string
	InDegree    int
	OutDegree   int
	TotalDegree int
}

// KnowledgeGaps returns wiki pages with one or fewer graph connections,
// sorted by least-connected first and then slug for stable tool output.
func (g *GraphIndex) KnowledgeGaps() []KnowledgeGap {
	if g == nil {
		return nil
	}
	g.mu.RLock()
	defer g.mu.RUnlock()

	gaps := make([]KnowledgeGap, 0)
	for slug, meta := range g.meta {
		if isSubnodeID(slug) || IsAuxiliaryHubNode(slug, meta) {
			continue
		}
		in := len(g.inbound[slug])
		out := len(g.outbound[slug])
		total := in + out
		if total > 1 {
			continue
		}
		gaps = append(gaps, KnowledgeGap{
			Slug:        slug,
			Title:       meta.Title,
			Category:    meta.Category,
			InDegree:    in,
			OutDegree:   out,
			TotalDegree: total,
		})
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].TotalDegree != gaps[j].TotalDegree {
			return gaps[i].TotalDegree < gaps[j].TotalDegree
		}
		return gaps[i].Slug < gaps[j].Slug
	})
	return gaps
}

// KnowledgeGaps returns the current store's graph health gaps. Returns nil
// when the store has no graph index.
func (s *Store) KnowledgeGaps() []KnowledgeGap {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	return s.graphIndex.KnowledgeGaps()
}
