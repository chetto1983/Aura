package wiki

import (
	"fmt"
	"sort"
	"strings"
)

type Surprise struct {
	SrcSlug    string `json:"src_slug"`
	TgtSlug    string `json:"tgt_slug"`
	Score      int    `json:"score"`
	Confidence string `json:"confidence"`
	Why        string `json:"why"`
}

type surpriseEdge struct {
	source string
	target string
}

func (s *Store) SurprisingEdges(topK int) []Surprise {
	if s == nil || s.graphIndex == nil || topK <= 0 {
		return nil
	}
	edges, meta, degree := surpriseSnapshot(s.graphIndex)
	best := map[string]Surprise{}
	for _, edge := range edges {
		srcMeta, ok := meta[edge.source]
		if !ok || IsAuxiliaryHubNode(edge.source, srcMeta) {
			continue
		}
		tgtMeta, ok := meta[edge.target]
		if !ok || IsAuxiliaryHubNode(edge.target, tgtMeta) {
			continue
		}
		confidence := s.edgeConfidence(edge.source, edge.target)
		score, why := surpriseScore(srcMeta, tgtMeta, degree[edge.source], degree[edge.target], confidence)
		item := Surprise{
			SrcSlug:    edge.source,
			TgtSlug:    edge.target,
			Score:      score,
			Confidence: confidence,
			Why:        why,
		}
		key := surprisePairKey(edge.source, edge.target)
		if existing, ok := best[key]; !ok || surpriseLess(existing, item) {
			best[key] = item
		}
	}
	out := make([]Surprise, 0, len(best))
	for _, item := range best {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].SrcSlug != out[j].SrcSlug {
			return out[i].SrcSlug < out[j].SrcSlug
		}
		return out[i].TgtSlug < out[j].TgtSlug
	})
	if topK < len(out) {
		return out[:topK]
	}
	return out
}

func surpriseSnapshot(g *GraphIndex) ([]surpriseEdge, map[string]NodeMeta, map[string]int) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	meta := make(map[string]NodeMeta, len(g.meta))
	degree := make(map[string]int, len(g.meta))
	for slug, m := range g.meta {
		meta[slug] = m
		degree[slug] = len(g.inbound[slug]) + len(g.outbound[slug])
	}
	edges := make([]surpriseEdge, 0)
	for source, targets := range g.outbound {
		for target := range targets {
			edges = append(edges, surpriseEdge{source: source, target: target})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].source != edges[j].source {
			return edges[i].source < edges[j].source
		}
		return edges[i].target < edges[j].target
	})
	return edges, meta, degree
}

func (s *Store) edgeConfidence(source, target string) string {
	page, err := s.ReadPage(source)
	if err != nil || page == nil {
		return ConfidenceExtracted
	}
	for _, ref := range page.Related {
		if Slug(ref.Slug) == target {
			return normalizeConfidence(ref.Confidence)
		}
	}
	return ConfidenceExtracted
}

func surpriseScore(srcMeta, tgtMeta NodeMeta, srcDegree, tgtDegree int, confidence string) (int, string) {
	score := confidenceWeight(confidence)
	reasons := []string{strings.ToLower(confidence) + " confidence"}
	if srcMeta.Category != "" && tgtMeta.Category != "" && srcMeta.Category != tgtMeta.Category {
		score += 2
		reasons = append(reasons, fmt.Sprintf("crosses categories %s->%s", srcMeta.Category, tgtMeta.Category))
	}
	minDegree, maxDegree := srcDegree, tgtDegree
	if minDegree > maxDegree {
		minDegree, maxDegree = maxDegree, minDegree
	}
	if minDegree <= 2 && maxDegree >= 5 {
		score += 2
		reasons = append(reasons, fmt.Sprintf("peripheral-to-hub degree %d->%d", minDegree, maxDegree))
	}
	return score, strings.Join(reasons, "; ")
}

func confidenceWeight(confidence string) int {
	switch normalizeConfidence(confidence) {
	case ConfidenceAmbiguous:
		return 3
	case ConfidenceInferred:
		return 2
	default:
		return 1
	}
}

func normalizeConfidence(confidence string) string {
	switch strings.ToUpper(strings.TrimSpace(confidence)) {
	case ConfidenceAmbiguous:
		return ConfidenceAmbiguous
	case ConfidenceInferred:
		return ConfidenceInferred
	default:
		return ConfidenceExtracted
	}
}

func surprisePairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

func surpriseLess(a, b Surprise) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	if confidenceWeight(a.Confidence) != confidenceWeight(b.Confidence) {
		return confidenceWeight(a.Confidence) < confidenceWeight(b.Confidence)
	}
	if a.SrcSlug != b.SrcSlug {
		return a.SrcSlug > b.SrcSlug
	}
	return a.TgtSlug > b.TgtSlug
}
