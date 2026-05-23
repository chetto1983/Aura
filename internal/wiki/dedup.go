package wiki

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DedupHit is one neighbor returned by DedupSearcher.
type DedupHit struct {
	Slug     string
	Category string
	Score    float32
}

// DedupSearcher queries the vector index for nearest neighbors of an entity.
// The implementation is expected to embed entityText internally, query the
// backing vector store, and return hits with their cosine similarity scores.
// Production implementations wrap search.Searcher; tests use fakes.
type DedupSearcher interface {
	SearchNeighbors(ctx context.Context, entityText string, topK int) ([]DedupHit, error)
}

// DedupLLMCaller makes a single-turn YES/NO disambiguation query to an LLM.
// Used as the tiebreaker for cosine pairs in the ambiguous range.
type DedupLLMCaller interface {
	Ask(ctx context.Context, prompt string) (string, error)
}

// DedupOptions configures a DeduplicateEntities run.
type DedupOptions struct {
	DryRun bool
	// CosineAutoMerge is the minimum cosine score for automatic merge (default 0.92).
	CosineAutoMerge float32
	// CosineLLMRange is the [low, high) score band sent to the LLM tiebreaker (default [0.78, 0.92)).
	CosineLLMRange [2]float32
	// MaxLLMCalls caps tiebreaker spend per run (default 50).
	MaxLLMCalls int
}

// DedupCluster describes a set of near-duplicate pages identified by dedup.
// Method is one of "cosine_auto", "llm_yes", or "ambiguous".
type DedupCluster struct {
	Survivor  string
	Losers    []string
	Scores    []float32
	Method    string
	EdgeCount int // edges rewired to survivor (0 in dry-run)
}

// DedupReport summarises a DeduplicateEntities run.
type DedupReport struct {
	Clusters      []DedupCluster
	MergesApplied int
	EdgesRewired  int
	AuditPath     string
}

// uf is a private Union-Find for entity cluster detection.
type uf struct {
	parent map[string]string
	rank   map[string]int
}

func newUF() *uf {
	return &uf{parent: make(map[string]string), rank: make(map[string]int)}
}

func (u *uf) add(x string) {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
	}
}

func (u *uf) find(x string) string {
	if u.parent[x] == x {
		return x
	}
	u.parent[x] = u.find(u.parent[x]) // path compression
	return u.parent[x]
}

func (u *uf) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if u.rank[ra] < u.rank[rb] {
		ra, rb = rb, ra
	}
	u.parent[rb] = ra
	if u.rank[ra] == u.rank[rb] {
		u.rank[ra]++
	}
}

func (u *uf) groups() map[string][]string {
	g := make(map[string][]string)
	for x := range u.parent {
		root := u.find(x)
		g[root] = append(g[root], x)
	}
	return g
}

// SetDedupBackend wires the vector searcher for entity deduplication (US-GRAPH-03).
// Production code calls this after the search repository is available. When not
// called, DeduplicateEntities returns "embedding backend required for dedup".
func (s *Store) SetDedupBackend(searcher DedupSearcher) {
	s.dedupSearcher = searcher
}

// SetDedupLLM wires an optional LLM tiebreaker. When nil, cosine pairs in
// CosineLLMRange are flagged AMBIGUOUS instead of resolved.
func (s *Store) SetDedupLLM(llm DedupLLMCaller) {
	s.dedupLLM = llm
}

// dedupPairKey returns a canonical order-independent key for an entity pair.
func dedupPairKey(a, b string) string {
	if a <= b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

// dedupEntityText builds the search query text for an entity page.
func dedupEntityText(page *Page) string {
	body := page.Body
	if len(body) > 200 {
		body = body[:200]
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return page.Title
	}
	return page.Title + " " + body
}

// firstSentence returns the first sentence of body (≤200 chars) for LLM prompts.
func firstSentence(body string) string {
	for i, r := range body {
		if r == '.' || r == '!' || r == '?' || r == '\n' {
			return strings.TrimSpace(body[:i+1])
		}
	}
	if len(body) > 200 {
		return body[:200]
	}
	return strings.TrimSpace(body)
}

// mergeAliases appends elements of newAliases that don't already appear
// (case-fold dedup) in existing. Returns the extended slice.
func mergeAliases(existing, newAliases []string) []string {
	seen := make(map[string]bool, len(existing))
	for _, a := range existing {
		key := strings.ToLower(strings.TrimSpace(a))
		if key != "" {
			seen[key] = true
		}
	}
	result := append([]string(nil), existing...)
	for _, a := range newAliases {
		key := strings.ToLower(strings.TrimSpace(a))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, a)
	}
	return result
}

// pickSurvivor selects the canonical page from a cluster. Priority:
// most inbound links → shortest title → alphabetical slug.
func pickSurvivor(g *GraphIndex, slugs []string, readPage func(string) (*Page, error)) string {
	best := slugs[0]
	bestIn, _ := g.Degree(best)
	bestPage, _ := readPage(best)
	bestTitleLen := len(best)
	if bestPage != nil {
		bestTitleLen = len(bestPage.Title)
	}

	for _, slug := range slugs[1:] {
		inDeg, _ := g.Degree(slug)
		page, _ := readPage(slug)
		titleLen := len(slug)
		if page != nil {
			titleLen = len(page.Title)
		}
		switch {
		case inDeg > bestIn:
			best, bestIn, bestTitleLen = slug, inDeg, titleLen
		case inDeg == bestIn && titleLen < bestTitleLen:
			best, bestTitleLen = slug, titleLen
		case inDeg == bestIn && titleLen == bestTitleLen && slug < best:
			best = slug
		}
	}
	return best
}

// rewireLinks replaces all occurrences of [[from]] with [[to]] in every wiki
// page body and related frontmatter. Returns the count of pages updated.
func (s *Store) rewireLinks(ctx context.Context, from, to string) (int, error) {
	slugs, err := s.ListPages()
	if err != nil {
		return 0, fmt.Errorf("rewire links list: %w", err)
	}
	old, replacement := "[["+from+"]]", "[["+to+"]]"
	count := 0
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			continue
		}
		changed := false
		if strings.Contains(page.Body, old) {
			page.Body = strings.ReplaceAll(page.Body, old, replacement)
			changed = true
		}
		for i := range page.Related {
			if page.Related[i].Slug == from {
				page.Related[i].Slug = to
				changed = true
			}
		}
		if !changed {
			continue
		}
		page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.WritePage(ctx, page); err != nil {
			s.logger.Warn("dedup: rewire write failed", "slug", slug, "error", err)
			continue
		}
		count++
	}
	return count, nil
}

// mergeCluster merges losers into survivor: copies aliases, rewires [[loser]]
// links across the wiki, and deletes loser pages.
func (s *Store) mergeCluster(ctx context.Context, survivor string, losers []string) (rewired int, err error) {
	survivorPage, readErr := s.ReadPage(survivor)
	if readErr != nil {
		return 0, fmt.Errorf("read survivor %s: %w", survivor, readErr)
	}

	for _, loser := range losers {
		loserPage, readErr := s.ReadPage(loser)
		if readErr != nil {
			s.logger.Warn("dedup: read loser failed, skipping alias merge", "loser", loser, "error", readErr)
			continue
		}
		// Merge loser aliases + loser title into survivor.
		survivorPage.Aliases = mergeAliases(survivorPage.Aliases, loserPage.Aliases)
		survivorPage.Aliases = mergeAliases(survivorPage.Aliases, []string{loserPage.Title})
	}
	survivorPage.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := s.WritePage(ctx, survivorPage); err != nil {
		return 0, fmt.Errorf("write survivor %s: %w", survivor, err)
	}

	// Rewire [[loser]] links in every page body/related.
	for _, loser := range losers {
		n, rwErr := s.rewireLinks(ctx, loser, survivor)
		if rwErr != nil {
			s.logger.Warn("dedup: rewire links error", "loser", loser, "survivor", survivor, "error", rwErr)
		}
		rewired += n
	}

	// Delete loser pages.
	for _, loser := range losers {
		if delErr := s.DeletePage(ctx, loser); delErr != nil {
			s.logger.Warn("dedup: delete loser failed", "loser", loser, "error", delErr)
		}
	}
	return rewired, nil
}

// writeDedupAudit writes a markdown audit table to the wiki directory and
// commits it to git. Returns the absolute file path.
func (s *Store) writeDedupAudit(ctx context.Context, report *DedupReport) (string, error) {
	ts := time.Now().UTC().Format("20060102-150405")
	filename := "dedup-audit-" + ts + ".md"
	auditPath := filepath.Join(s.dir, filename)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# Dedup Audit\n\nRun at: %s\n\n", time.Now().UTC().Format(time.RFC3339))
	sb.WriteString("| survivor | losers | scores | method | edges_rewired |\n")
	sb.WriteString("|---|---|---|---|---|\n")
	for _, c := range report.Clusters {
		if c.Method == "ambiguous" {
			continue
		}
		scores := make([]string, len(c.Scores))
		for i, sc := range c.Scores {
			scores[i] = fmt.Sprintf("%.3f", sc)
		}
		fmt.Fprintf(&sb, "| [[%s]] | %s | %s | %s | %d |\n",
			c.Survivor,
			strings.Join(c.Losers, ", "),
			strings.Join(scores, ", "),
			c.Method,
			c.EdgeCount,
		)
	}
	fmt.Fprintf(&sb, "\nTotal merges: %d, edges rewired: %d\n", report.MergesApplied, report.EdgesRewired)

	if err := os.WriteFile(auditPath, []byte(sb.String()), 0644); err != nil {
		return "", fmt.Errorf("write dedup audit: %w", err)
	}
	if err := s.gitCommit(ctx, filename, "update"); err != nil {
		s.logger.Warn("dedup audit: git commit failed", "file", filename, "error", err)
	}
	return auditPath, nil
}

// DeduplicateEntities detects near-duplicate entity/concept pages via
// embedding cosine similarity + optional LLM tiebreaker, then merges
// clusters into canonical survivors.
//
// SAFETY: acquires a Store-level dedup lock to prevent concurrent dedup runs.
// Concurrent WritePage calls are NOT blocked by this lock — operators MUST run
// DeduplicateEntities only during maintenance windows when no concurrent writes
// are expected. Running dedup under concurrent writes may produce inconsistent
// alias merges or broken links.
func (s *Store) DeduplicateEntities(ctx context.Context, opts DedupOptions) (*DedupReport, error) {
	// Apply defaults.
	if opts.CosineAutoMerge == 0 {
		opts.CosineAutoMerge = 0.92
	}
	if opts.CosineLLMRange == ([2]float32{}) {
		opts.CosineLLMRange = [2]float32{0.78, 0.92}
	}
	if opts.MaxLLMCalls == 0 {
		opts.MaxLLMCalls = 50
	}

	if s.dedupSearcher == nil {
		return nil, fmt.Errorf("embedding backend required for dedup")
	}

	// Prevent concurrent dedup runs.
	s.dedupMu.Lock()
	defer s.dedupMu.Unlock()

	slugs, err := s.ListPages()
	if err != nil {
		return nil, fmt.Errorf("dedup: list pages: %w", err)
	}

	// Load all entity/concept pages.
	type entityEntry struct {
		slug string
		page *Page
		text string
	}
	var entities []entityEntry
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil || page == nil {
			continue
		}
		if page.Category != "entity" && page.Category != "concept" {
			continue
		}
		entities = append(entities, entityEntry{slug: slug, page: page, text: dedupEntityText(page)})
	}

	if len(entities) < 2 {
		return &DedupReport{}, nil
	}

	// Build candidate pairs from per-entity top-K Qdrant searches.
	// Cosine scores come from Qdrant (Cosine distance collection → scores ARE
	// cosine similarities). No extra round-trips needed for pairwise scores.
	type pairScore struct {
		a, b  string
		score float32
	}
	seen := make(map[string]bool)
	pairScores := make(map[string]float32)
	pairMethod := make(map[string]string) // "cosine_auto" | "llm_range"

	var autoMerge, llmRange []pairScore

	for _, e := range entities {
		hits, err := s.dedupSearcher.SearchNeighbors(ctx, e.text, 10)
		if err != nil {
			s.logger.Warn("dedup: search neighbors failed", "slug", e.slug, "error", err)
			continue
		}
		for _, hit := range hits {
			if hit.Slug == e.slug {
				continue
			}
			if hit.Category != "entity" && hit.Category != "concept" {
				continue
			}
			k := dedupPairKey(e.slug, hit.Slug)
			if seen[k] {
				// Update score to max of the two search directions.
				if hit.Score > pairScores[k] {
					pairScores[k] = hit.Score
					old := autoMerge
					autoMerge = old[:0]
					for _, p := range old {
						if dedupPairKey(p.a, p.b) == k {
							autoMerge = append(autoMerge, pairScore{p.a, p.b, hit.Score})
						} else {
							autoMerge = append(autoMerge, p)
						}
					}
				}
				continue
			}
			seen[k] = true
			pairScores[k] = hit.Score

			p := pairScore{a: e.slug, b: hit.Slug, score: hit.Score}
			switch {
			case hit.Score >= opts.CosineAutoMerge:
				autoMerge = append(autoMerge, p)
				pairMethod[k] = "cosine_auto"
			case hit.Score >= opts.CosineLLMRange[0] && hit.Score < opts.CosineLLMRange[1]:
				llmRange = append(llmRange, p)
				pairMethod[k] = "llm_range"
			}
		}
	}

	// LLM tiebreaker for llmRange pairs (capped at MaxLLMCalls).
	llmUsed := 0
	var mergedPairs []pairScore
	mergedPairs = append(mergedPairs, autoMerge...)

	var ambiguous []pairScore
	for _, p := range llmRange {
		if llmUsed >= opts.MaxLLMCalls || s.dedupLLM == nil {
			ambiguous = append(ambiguous, p)
			continue
		}
		pageA, _ := s.ReadPage(p.a)
		pageB, _ := s.ReadPage(p.b)
		descA, descB := "", ""
		if pageA != nil {
			descA = firstSentence(pageA.Body)
		}
		if pageB != nil {
			descB = firstSentence(pageB.Body)
		}
		prompt := fmt.Sprintf(
			"Are these two wiki entities the same concept? Reply only YES or NO.\nEntity A: %s. %s\nEntity B: %s. %s",
			p.a, descA, p.b, descB,
		)
		resp, llmErr := s.dedupLLM.Ask(ctx, prompt)
		llmUsed++
		if llmErr != nil {
			s.logger.Warn("dedup: LLM tiebreaker error", "a", p.a, "b", p.b, "error", llmErr)
			ambiguous = append(ambiguous, p)
			continue
		}
		if strings.Contains(strings.ToUpper(strings.TrimSpace(resp)), "YES") {
			mergedPairs = append(mergedPairs, p)
			pairMethod[dedupPairKey(p.a, p.b)] = "llm_yes"
		}
		// NO → distinct, not added to mergedPairs
	}

	// Build clusters via Union-Find.
	union := newUF()
	for _, e := range entities {
		union.add(e.slug)
	}
	for _, p := range mergedPairs {
		union.union(p.a, p.b)
	}

	report := &DedupReport{}

	for _, members := range union.groups() {
		if len(members) < 2 {
			continue
		}
		survivor := pickSurvivor(s.graphIndex, members, s.ReadPage)
		losers := make([]string, 0, len(members)-1)
		for _, m := range members {
			if m != survivor {
				losers = append(losers, m)
			}
		}
		sort.Strings(losers)

		scores := make([]float32, len(losers))
		for i, loser := range losers {
			scores[i] = pairScores[dedupPairKey(survivor, loser)]
		}

		// Determine method for this cluster.
		method := "cosine_auto"
		for _, loser := range losers {
			if pairMethod[dedupPairKey(survivor, loser)] == "llm_yes" {
				method = "llm_yes"
				break
			}
		}

		cluster := DedupCluster{
			Survivor: survivor,
			Losers:   losers,
			Scores:   scores,
			Method:   method,
		}

		if !opts.DryRun {
			rwCount, mergeErr := s.mergeCluster(ctx, survivor, losers)
			if mergeErr != nil {
				s.logger.Warn("dedup: merge cluster failed", "survivor", survivor, "error", mergeErr)
			} else {
				cluster.EdgeCount = rwCount
				report.MergesApplied += len(losers)
				report.EdgesRewired += rwCount
			}
		}

		report.Clusters = append(report.Clusters, cluster)
	}

	// Append AMBIGUOUS pairs as informational clusters (no merge).
	for _, p := range ambiguous {
		report.Clusters = append(report.Clusters, DedupCluster{
			Survivor: p.a,
			Losers:   []string{p.b},
			Scores:   []float32{p.score},
			Method:   "ambiguous",
		})
	}

	// Write audit log on real runs.
	if !opts.DryRun && len(report.Clusters) > 0 {
		if auditPath, auditErr := s.writeDedupAudit(ctx, report); auditErr != nil {
			s.logger.Warn("dedup: audit write failed", "error", auditErr)
		} else {
			report.AuditPath = auditPath
		}
	}

	return report, nil
}
