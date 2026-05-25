package wiki

import (
	"fmt"
	"sort"
	"strings"
)

const suggestQuestionsMaxTopK = 7

type Question struct {
	Type    string   `json:"type"`
	Text    string   `json:"question"`
	Why     string   `json:"why"`
	Related []string `json:"related_slugs,omitempty"`
}

type questionCandidate struct {
	Question
	priority int
	score    int
	key      string
}

func (s *Store) SuggestQuestions(topK int) []Question {
	if s == nil || s.graphIndex == nil {
		return nil
	}
	if topK <= 0 || topK > suggestQuestionsMaxTopK {
		topK = suggestQuestionsMaxTopK
	}
	meta, degree, outbound := questionSnapshot(s.graphIndex)
	if len(meta) == 0 {
		return nil
	}
	p99Degree := effectiveQuestionP99Degree(degree, s.graphIndex.P99Degree())
	var candidates []questionCandidate
	candidates = append(candidates, s.ambiguousEdgeQuestions(meta, degree)...)
	candidates = append(candidates, bridgeNodeQuestions(meta, degree, outbound, p99Degree)...)
	candidates = append(candidates, s.verifyInferredQuestions(meta, degree)...)
	candidates = append(candidates, isolatedNodeQuestions(s.graphIndex)...)
	candidates = dedupeQuestionCandidates(candidates)
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority < candidates[j].priority
		}
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].key < candidates[j].key
	})
	if topK < len(candidates) {
		candidates = candidates[:topK]
	}
	out := make([]Question, len(candidates))
	for i, candidate := range candidates {
		out[i] = candidate.Question
	}
	return out
}

func questionSnapshot(g *GraphIndex) (map[string]NodeMeta, map[string]int, map[string][]string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	meta := make(map[string]NodeMeta, len(g.meta))
	degree := make(map[string]int, len(g.meta))
	outbound := make(map[string][]string, len(g.outbound))
	for slug, m := range g.meta {
		if isSubnodeID(slug) {
			continue
		}
		meta[slug] = m
		degree[slug] = len(g.inbound[slug]) + len(g.outbound[slug])
	}
	for slug, targets := range g.outbound {
		if _, ok := meta[slug]; !ok {
			continue
		}
		for target := range targets {
			if _, ok := meta[target]; ok {
				outbound[slug] = append(outbound[slug], target)
			}
		}
		sort.Strings(outbound[slug])
	}
	return meta, degree, outbound
}

func effectiveQuestionP99Degree(degree map[string]int, p99Degree int) int {
	var maxDegree int
	for _, d := range degree {
		if d > maxDegree {
			maxDegree = d
		}
	}
	if maxDegree == 0 {
		return p99Degree
	}
	if p99Degree <= 0 || p99Degree > maxDegree {
		return maxDegree
	}
	return p99Degree
}

func (s *Store) ambiguousEdgeQuestions(meta map[string]NodeMeta, degree map[string]int) []questionCandidate {
	var out []questionCandidate
	for _, slug := range sortedMetaSlugs(meta) {
		srcMeta := meta[slug]
		if IsAuxiliaryHubNode(slug, srcMeta) {
			continue
		}
		page, err := s.ReadPage(slug)
		if err != nil || page == nil {
			continue
		}
		refs := append([]RelatedRef(nil), page.Related...)
		RelatedSortBySlugs(refs)
		for _, ref := range refs {
			target := Slug(ref.Slug)
			tgtMeta, ok := meta[target]
			if !ok || IsAuxiliaryHubNode(target, tgtMeta) || normalizeConfidence(ref.Confidence) != ConfidenceAmbiguous {
				continue
			}
			out = append(out, questionCandidate{
				Question: Question{
					Type:    "ambiguous_edge",
					Text:    fmt.Sprintf("What is the exact relationship between [[%s]] and [[%s]]?", slug, target),
					Why:     fmt.Sprintf("edge tagged AMBIGUOUS in [[%s]] frontmatter", slug),
					Related: []string{slug, target},
				},
				priority: 1,
				score:    degree[slug] + degree[target],
				key:      slug + "\x00" + target,
			})
		}
	}
	return out
}

func bridgeNodeQuestions(meta map[string]NodeMeta, degree map[string]int, outbound map[string][]string, p99Degree int) []questionCandidate {
	var out []questionCandidate
	minDegree := p99Degree - 1
	if minDegree < 2 {
		minDegree = 2
	}
	for _, slug := range sortedMetaSlugs(meta) {
		nodeMeta := meta[slug]
		if IsAuxiliaryHubNode(slug, nodeMeta) || degree[slug] < minDegree {
			continue
		}
		cats := distinctNeighborCategories(outbound[slug], meta)
		if len(cats) < 2 {
			continue
		}
		out = append(out, questionCandidate{
			Question: Question{
				Type:    "bridge_node",
				Text:    fmt.Sprintf("Why does [[%s]] connect %s to %s?", slug, cats[0], cats[1]),
				Why:     fmt.Sprintf("degree=%d (P99=%d); outbound spans %d distinct categories", degree[slug], p99Degree, len(cats)),
				Related: append([]string{slug}, outbound[slug]...),
			},
			priority: 2,
			score:    degree[slug] + len(cats),
			key:      slug,
		})
	}
	return out
}

func (s *Store) verifyInferredQuestions(meta map[string]NodeMeta, degree map[string]int) []questionCandidate {
	top := s.TopNodes(10)
	var out []questionCandidate
	for _, node := range top {
		nodeMeta, ok := meta[node.Slug]
		if !ok || IsAuxiliaryHubNode(node.Slug, nodeMeta) {
			continue
		}
		page, err := s.ReadPage(node.Slug)
		if err != nil || page == nil {
			continue
		}
		var inferred []string
		refs := append([]RelatedRef(nil), page.Related...)
		RelatedSortBySlugs(refs)
		for _, ref := range refs {
			target := Slug(ref.Slug)
			if _, ok := meta[target]; ok && normalizeConfidence(ref.Confidence) == ConfidenceInferred {
				inferred = append(inferred, target)
			}
		}
		if len(inferred) < 2 {
			continue
		}
		examples := inferred
		if len(examples) > 2 {
			examples = examples[:2]
		}
		out = append(out, questionCandidate{
			Question: Question{
				Type:    "verify_inferred",
				Text:    fmt.Sprintf("Are the %d inferred relationships involving [[%s]] (for example %s) actually correct?", len(inferred), node.Slug, joinWikiRefs(examples)),
				Why:     fmt.Sprintf("top-degree page with %d INFERRED-confidence outbound edges", len(inferred)),
				Related: append([]string{node.Slug}, inferred...),
			},
			priority: 3,
			score:    degree[node.Slug] + len(inferred),
			key:      node.Slug,
		})
	}
	return out
}

func isolatedNodeQuestions(g *GraphIndex) []questionCandidate {
	gaps := g.KnowledgeGaps()
	if len(gaps) == 0 {
		return nil
	}
	limit := min(3, len(gaps))
	slugs := make([]string, 0, limit)
	for _, gap := range gaps {
		if gap.TotalDegree > 1 {
			continue
		}
		slugs = append(slugs, gap.Slug)
		if len(slugs) == limit {
			break
		}
	}
	if len(slugs) == 0 {
		return nil
	}
	return []questionCandidate{{
		Question: Question{
			Type:    "isolated_nodes",
			Text:    fmt.Sprintf("What connects %s to the rest of the system?", joinWikiRefs(slugs)),
			Why:     fmt.Sprintf("%d wiki page(s) with InDegree+OutDegree <= 1", len(slugs)),
			Related: slugs,
		},
		priority: 4,
		score:    len(slugs),
		key:      strings.Join(slugs, "\x00"),
	}}
}

func distinctNeighborCategories(slugs []string, meta map[string]NodeMeta) []string {
	set := map[string]struct{}{}
	for _, slug := range slugs {
		category := strings.TrimSpace(meta[slug].Category)
		if category != "" {
			set[category] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for category := range set {
		out = append(out, category)
	}
	sort.Strings(out)
	return out
}

func sortedMetaSlugs(meta map[string]NodeMeta) []string {
	slugs := make([]string, 0, len(meta))
	for slug := range meta {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs
}

func dedupeQuestionCandidates(in []questionCandidate) []questionCandidate {
	seen := map[string]struct{}{}
	out := make([]questionCandidate, 0, len(in))
	for _, item := range in {
		if item.Text == "" {
			continue
		}
		if _, ok := seen[item.Text]; ok {
			continue
		}
		seen[item.Text] = struct{}{}
		out = append(out, item)
	}
	return out
}

func joinWikiRefs(slugs []string) string {
	refs := make([]string, len(slugs))
	for i, slug := range slugs {
		refs[i] = "[[" + slug + "]]"
	}
	return strings.Join(refs, ", ")
}
