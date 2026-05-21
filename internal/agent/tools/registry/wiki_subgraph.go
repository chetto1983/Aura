package tools

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/wiki"
)

const (
	wikiSubgraphDefaultDepth        = 2
	wikiSubgraphMaxDepth            = 3
	wikiSubgraphDefaultBudgetTokens = 1500
	wikiSubgraphMaxBudgetTokens     = 4000
	wikiSubgraphTopK                = 10 // initial hybrid search width
	wikiSubgraphSeedTopK            = 5  // seeds passed to PPR
	wikiSubgraphPPRTopK             = 5  // top PPR nodes used as BFS seeds
	wikiSubgraphSnippetMaxChars     = 200
	wikiSubgraphCharsPerToken       = 3 // ~3 chars/token for IT+EN mixed content
	wikiSubgraphGapRatio            = 0.2
)

// WikiSubgraphTool returns a token-budgeted wiki subgraph for a query.
// It chains: hybrid FTS+vector search → PPR re-ranking → hub-aware BFS →
// byte-range snippet rendering, replacing the 7-call navigation pattern
// with a single tool call.
type WikiSubgraphTool struct {
	store    *wiki.Store
	searcher search.Searcher
}

// NewWikiSubgraphTool creates the tool. Returns nil when either dep is nil.
func NewWikiSubgraphTool(store *wiki.Store, searcher search.Searcher) *WikiSubgraphTool {
	if store == nil || searcher == nil {
		return nil
	}
	return &WikiSubgraphTool{store: store, searcher: searcher}
}

func (t *WikiSubgraphTool) Name() string { return "wiki_subgraph" }

func (t *WikiSubgraphTool) Description() string {
	return "Retrieve a token-budgeted subgraph of the wiki for a query. Uses hybrid search + Personalised PageRank seeding + hub-aware BFS to return the most relevant sections of the wiki in one call. Prefer this over multiple search_memory + file action=read round-trips when exploring a connected topic."
}

func (t *WikiSubgraphTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural-language query to seed the subgraph retrieval.",
			},
			"depth": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("BFS expansion depth from PPR seed nodes (default %d, max %d).", wikiSubgraphDefaultDepth, wikiSubgraphMaxDepth),
				"minimum":     1,
				"maximum":     wikiSubgraphMaxDepth,
			},
			"budget_tokens": map[string]any{
				"type":        "integer",
				"description": fmt.Sprintf("Approximate token budget for the rendered subgraph (default %d, max %d).", wikiSubgraphDefaultBudgetTokens, wikiSubgraphMaxBudgetTokens),
				"minimum":     1,
				"maximum":     wikiSubgraphMaxBudgetTokens,
			},
		},
		"required": []string{"query"},
	}
}

func (t *WikiSubgraphTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: VisibilityActiveTurn,
	}
}

func (t *WikiSubgraphTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil {
		return "", fmt.Errorf("wiki_subgraph: tool unavailable")
	}
	query, err := requiredString(args, "query")
	if err != nil {
		return "", fmt.Errorf("wiki_subgraph: %w", err)
	}
	depth := intArg(args, "depth", wikiSubgraphDefaultDepth, 1, wikiSubgraphMaxDepth)
	budgetTokens := intArg(args, "budget_tokens", wikiSubgraphDefaultBudgetTokens, 1, wikiSubgraphMaxBudgetTokens)

	// ── Step 1: Hybrid search for seed candidates (topK = 10). ──────────────
	var seedResults []search.Result
	if hybrid, ok := t.searcher.(search.HybridSearcher); ok {
		seedResults, err = hybrid.SearchHybrid(ctx, query, wikiSubgraphTopK)
	} else {
		seedResults, err = t.searcher.Search(ctx, query, wikiSubgraphTopK)
	}
	if err != nil || len(seedResults) == 0 {
		return "wiki_subgraph: no wiki pages found for query", nil
	}

	// ── Step 2: Top-5 seeds with gap-ratio cutoff (0.2 × top_score). ────────
	topScore := float64(seedResults[0].Score)
	var seeds []string
	for _, r := range seedResults {
		if len(seeds) >= wikiSubgraphSeedTopK {
			break
		}
		if topScore > 0 && float64(r.Score) < wikiSubgraphGapRatio*topScore {
			break
		}
		seeds = append(seeds, r.Slug)
	}
	if len(seeds) == 0 {
		seeds = []string{seedResults[0].Slug}
	}
	seedSet := make(map[string]bool, len(seeds))
	for _, seed := range seeds {
		seedSet[seed] = true
	}
	selectedSearchResults := make([]search.Result, 0, len(seeds))
	for _, r := range seedResults {
		if seedSet[r.Slug] {
			selectedSearchResults = append(selectedSearchResults, r)
		}
	}

	// ── Step 3: Personalised PageRank (alpha=0.15, 30 iter). ────────────────
	pprScores := t.store.PersonalizedPageRank(seeds, 0.15, 30)

	// ── Step 4: Top-5 PPR-ranked nodes as BFS seeds. ─────────────────────────
	type scoredSlug struct {
		slug  string
		score float64
	}
	ranked := make([]scoredSlug, 0, len(pprScores))
	for slug, score := range pprScores {
		ranked = append(ranked, scoredSlug{slug, score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].slug < ranked[j].slug
	})
	pprSeeds := make([]string, 0, wikiSubgraphPPRTopK)
	for _, rs := range ranked {
		if len(pprSeeds) >= wikiSubgraphPPRTopK {
			break
		}
		pprSeeds = append(pprSeeds, rs.slug)
	}
	if len(pprSeeds) == 0 {
		pprSeeds = seeds
	}

	// ── Step 5: Hub-aware BFS from the PPR seeds. ────────────────────────────
	skipDegree := t.store.P99Degree()

	// BFS level-by-level so we can track depth-from-nearest-seed accurately.
	depthMap := make(map[string]int) // slug → min depth from any seed
	for _, seed := range pprSeeds {
		depthMap[seed] = 0
	}
	frontier := make([]string, len(pprSeeds))
	copy(frontier, pprSeeds)
	for hop := 1; hop <= depth; hop++ {
		var next []string
		for _, slug := range frontier {
			for _, nb := range t.store.NeighborsHubAware(slug, 1, skipDegree) {
				if _, ok := depthMap[nb]; !ok {
					depthMap[nb] = hop
					next = append(next, nb)
				}
			}
		}
		frontier = next
	}

	// ── Step 6: Render within the token budget. ──────────────────────────────
	budgetChars := budgetTokens * wikiSubgraphCharsPerToken

	// Sort: seeds first (depth=0), then ascending depth, then alphabetical.
	type visitedNode struct {
		slug  string
		depth int
	}
	nodes := make([]visitedNode, 0, len(depthMap))
	for slug, d := range depthMap {
		nodes = append(nodes, visitedNode{slug, d})
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].depth != nodes[j].depth {
			return nodes[i].depth < nodes[j].depth
		}
		return nodes[i].slug < nodes[j].slug
	})

	// Cache page reads for edge-confidence lookups.
	pageCache := make(map[string]*wiki.Page)
	readPage := func(slug string) *wiki.Page {
		if p, ok := pageCache[slug]; ok {
			return p
		}
		p, readErr := t.store.ReadPage(slug)
		if readErr != nil {
			return nil
		}
		pageCache[slug] = p
		return p
	}

	var sb strings.Builder
	sb.WriteString("CAPSULE wiki_subgraph\n")
	sb.WriteString(fmt.Sprintf("QUERY %s\n", wikiSubgraphOneLine(query)))
	sb.WriteString(fmt.Sprintf(
		"TRAVERSAL bfs depth=%d budget_tokens=%d nodes=%d hub_skip_degree=%d\n",
		depth,
		budgetTokens,
		len(nodes),
		skipDegree,
	))
	sb.WriteString("SEARCH_SEEDS\n")
	for _, r := range selectedSearchResults {
		sb.WriteString(wikiSubgraphSearchSeedLine(r))
	}
	sb.WriteString("PPR_SEEDS\n")
	for _, seed := range pprSeeds {
		sb.WriteString(wikiSubgraphPPRSeedLine(seed, pprScores[seed]))
	}
	sb.WriteString("CONTENT\n")

	if budgetChars <= sb.Len() {
		sb.WriteString("[truncated: budget_tokens too small for nodes/edges; increase budget_tokens or use a narrower query]\n")
		return sb.String(), nil
	}
	truncated := false
	appendWithinBudget := func(text string) bool {
		if sb.Len()+len(text) > budgetChars {
			truncated = true
			return false
		}
		sb.WriteString(text)
		return true
	}

	// NODE lines.
	for _, n := range nodes {
		if sb.Len() >= budgetChars {
			truncated = true
			break
		}
		meta, hasMeta := t.store.NodeMeta(n.slug)
		title := n.slug
		if hasMeta && meta.Title != "" {
			title = meta.Title
		}
		header := fmt.Sprintf("NODE %s [%s | depth=%d]\n", n.slug, title, n.depth)
		if !appendWithinBudget(header) {
			break
		}

		// For subnodes, add the byte-range snippet from B04.
		if wiki.IsSubnodeID(n.slug) {
			raw := t.store.SubgraphSnippet(n.slug)
			if len(raw) > 0 {
				snippet := strings.TrimSpace(string(raw))
				if len(snippet) > wikiSubgraphSnippetMaxChars {
					snippet = snippet[:wikiSubgraphSnippetMaxChars]
				}
				snippet = strings.TrimSpace(snippet)
				if snippet != "" {
					appendWithinBudget(snippet + "\n")
				}
			}
		}
	}

	// EDGE lines (page-level edges only, both endpoints in visited set).
	edgesSeen := make(map[string]bool)
	for _, n := range nodes {
		if sb.Len() >= budgetChars {
			truncated = true
			break
		}
		slug := n.slug
		if wiki.IsSubnodeID(slug) {
			continue
		}
		// Build confidence map from this page's related: entries.
		confMap := make(map[string]string)
		if p := readPage(slug); p != nil {
			for _, ref := range p.Related {
				confMap[wiki.Slug(ref.Slug)] = ref.Confidence
			}
		}
		for _, target := range t.store.OutNeighbors(slug) {
			if _, inVisited := depthMap[target]; !inVisited {
				continue
			}
			edgeKey := slug + "\x00" + target
			if edgesSeen[edgeKey] {
				continue
			}
			edgesSeen[edgeKey] = true
			conf, hasConf := confMap[target]
			if !hasConf {
				conf = "EXTRACTED" // body [[wiki-link]]
			}
			edge := fmt.Sprintf("EDGE %s --%s--> %s\n", slug, conf, target)
			appendWithinBudget(edge)
		}
	}

	result := sb.String()
	if len(result) > budgetChars {
		cut := result[:budgetChars]
		if idx := strings.LastIndex(cut, "\n"); idx > 0 {
			cut = cut[:idx+1]
		}
		result = cut
		truncated = true
	}
	if truncated {
		result += "[truncated: budget_tokens exceeded; increase budget_tokens or use a narrower query]\n"
	}
	return result, nil
}

func wikiSubgraphOneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func wikiSubgraphSearchSeedLine(r search.Result) string {
	return fmt.Sprintf(
		"SEARCH_SEED %s score=%.3f exact=%.3f fts=%.3f vector=%.3f title=%q\n",
		r.Slug,
		r.Score,
		r.ScoreExact,
		r.ScoreFTS,
		r.ScoreVector,
		wikiSubgraphOneLine(r.Title),
	)
}

func wikiSubgraphPPRSeedLine(slug string, score float64) string {
	return fmt.Sprintf("PPR_SEED %s score=%.6f\n", slug, score)
}

var _ Tool = (*WikiSubgraphTool)(nil)
