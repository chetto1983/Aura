package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/wiki"
)

// maxWikiPageBytes caps the body returned by GET /wiki/page so a runaway
// page can't blow out the dashboard's fetch buffer.
const maxWikiPageBytes = 1 << 20 // 1 MiB

const (
	wikiSearchDefaultTopK   = 5
	wikiSearchMaxTopK       = 20
	wikiSearchQueryMaxBytes = 500
	wikiSearchTimeout       = 5 * time.Second
	wikiSearchSnippetBytes  = 240
)

func handleWikiPages(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summaries, err := loadWikiSummaries(deps.Wiki)
		if err != nil {
			deps.Logger.Warn("api: list wiki pages", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list wiki pages")
			return
		}
		// Sort by category then slug so the table is deterministic.
		sort.Slice(summaries, func(i, j int) bool {
			if summaries[i].Category != summaries[j].Category {
				return summaries[i].Category < summaries[j].Category
			}
			return summaries[i].Slug < summaries[j].Slug
		})
		writeJSON(w, deps.Logger, http.StatusOK, summaries)
	}
}

func handleWikiPage(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		slug := strings.TrimSpace(r.URL.Query().Get("slug"))
		if slug == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "slug query parameter required")
			return
		}
		// Defense in depth — wiki.Slug normalization keeps slugs to a-z 0-9 -.
		// Reject anything that wouldn't be returned by ListPages.
		if !isValidSlug(slug) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid slug")
			return
		}
		page, err := deps.Wiki.ReadPage(slug)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
				writeError(w, deps.Logger, http.StatusNotFound, "page not found")
				return
			}
			deps.Logger.Warn("api: read wiki page", "slug", slug, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read page")
			return
		}
		if len(page.Body) > maxWikiPageBytes {
			writeError(w, deps.Logger, http.StatusRequestEntityTooLarge, "page body exceeds 1MiB cap")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, WikiPage{
			Slug:        slug,
			Title:       page.Title,
			BodyMD:      page.Body,
			Frontmatter: pageFrontmatter(page),
		})
	}
}

func handleWikiGraph(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		graph, err := wiki.ReadGraphFile(deps.Wiki.Dir())
		if err != nil {
			graph, err = wiki.BuildGraphFromReader(deps.Wiki)
			if err != nil {
				deps.Logger.Warn("api: build wiki graph", "error", err)
				writeError(w, deps.Logger, http.StatusInternalServerError, "failed to build graph")
				return
			}
			if writeErr := wiki.WriteGraphFiles(deps.Wiki.Dir(), graph); writeErr != nil {
				deps.Logger.Warn("api: write wiki graph cache", "error", writeErr)
			}
		}
		writeJSON(w, deps.Logger, http.StatusOK, graph)
	}
}

func handleWikiSearch(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := strings.TrimSpace(r.URL.Query().Get("q"))
		if query == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "q query parameter required")
			return
		}
		if len(query) > wikiSearchQueryMaxBytes {
			writeError(w, deps.Logger, http.StatusBadRequest, "q query parameter too long")
			return
		}
		topK := wikiSearchDefaultTopK
		if raw := strings.TrimSpace(r.URL.Query().Get("top_k")); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 1 || n > wikiSearchMaxTopK {
				writeError(w, deps.Logger, http.StatusBadRequest, "top_k must be an integer between 1 and 20")
				return
			}
			topK = n
		}
		if deps.WikiSearcher == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "wiki search unavailable")
			return
		}
		if !deps.WikiSearcher.IsIndexed() {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "wiki index not ready")
			return
		}

		searchCtx, cancel := context.WithTimeout(r.Context(), wikiSearchTimeout)
		defer cancel()
		start := time.Now()
		var (
			results []search.Result
			err     error
		)
		if hybrid, ok := deps.WikiSearcher.(search.HybridSearcher); ok {
			results, err = hybrid.SearchHybrid(searchCtx, query, topK)
		} else {
			results, err = deps.WikiSearcher.Search(searchCtx, query, topK)
		}
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(searchCtx.Err(), context.DeadlineExceeded) {
				writeError(w, deps.Logger, http.StatusGatewayTimeout, "wiki search timed out")
				return
			}
			deps.Logger.Warn("api: wiki search", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "wiki search failed")
			return
		}

		hits := make([]WikiSearchHit, 0, len(results))
		for i, result := range results {
			kind := strings.TrimSpace(result.Kind)
			if kind == "" {
				kind = "wiki_page"
			}
			hits = append(hits, WikiSearchHit{
				Rank:        i + 1,
				Kind:        kind,
				Slug:        result.Slug,
				Title:       result.Title,
				Snippet:     wikiSearchSnippet(result.Content, query, wikiSearchSnippetBytes),
				Score:       result.Score,
				ScoreExact:  result.ScoreExact,
				ScoreFTS:    result.ScoreFTS,
				ScoreVector: result.ScoreVector,
				FilePath:    result.FilePath,
				Category:    result.Category,
				Tags:        result.Tags,
				Sources:     result.Sources,
			})
		}
		writeJSON(w, deps.Logger, http.StatusOK, WikiSearchResponse{
			Query:     query,
			TopK:      topK,
			Indexed:   true,
			ElapsedMS: elapsed,
			Results:   hits,
		})
	}
}

func loadWikiSummaries(store wiki.PageReader) ([]WikiPageSummary, error) {
	slugs, err := store.ListPages()
	if err != nil {
		return nil, err
	}
	out := make([]WikiPageSummary, 0, len(slugs))
	for _, slug := range slugs {
		page, err := store.ReadPage(slug)
		if err != nil {
			// Skip unreadable pages — they shouldn't kill the whole list.
			continue
		}
		out = append(out, WikiPageSummary{
			Slug:      slug,
			Title:     page.Title,
			Category:  page.Category,
			Tags:      page.Tags,
			UpdatedAt: parseTime(page.UpdatedAt),
		})
	}
	return out, nil
}

func pageFrontmatter(p *wiki.Page) map[string]any {
	fm := map[string]any{
		"title":          p.Title,
		"schema_version": p.SchemaVersion,
		"prompt_version": p.PromptVersion,
		"created_at":     p.CreatedAt,
		"updated_at":     p.UpdatedAt,
		"unversioned":    p.Unversioned, // GIT-01 — always present (true or false) for TS strict typing
	}
	if p.Category != "" {
		fm["category"] = p.Category
	}
	if len(p.Tags) > 0 {
		fm["tags"] = slices.Clone(p.Tags)
	}
	if len(p.Related) > 0 {
		fm["related"] = wiki.RelatedSlugs(p.Related)
	}
	if len(p.Sources) > 0 {
		fm["sources"] = slices.Clone(p.Sources)
	}
	return fm
}

func wikiSearchSnippet(content, query string, limit int) string {
	content = stripMarkdownFrontmatter(content)
	content = strings.Join(strings.Fields(content), " ")
	if content == "" {
		return ""
	}
	if limit <= 0 || len(content) <= limit {
		return content
	}
	lower := strings.ToLower(content)
	offset := -1
	phrase := strings.ToLower(strings.TrimSpace(query))
	if phrase != "" {
		offset = strings.Index(lower, phrase)
	}
	if offset < 0 {
		for _, term := range wikiSearchTerms(query) {
			if idx := strings.Index(lower, term); idx >= 0 {
				offset = idx
				break
			}
		}
	}
	if offset < 0 {
		offset = 0
	}
	start := offset - limit/3
	if start < 0 {
		start = 0
	}
	end := start + limit
	if end > len(content) {
		end = len(content)
		start = end - limit
		if start < 0 {
			start = 0
		}
	}
	snippet := strings.TrimSpace(content[start:end])
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet += "..."
	}
	return snippet
}

func stripMarkdownFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---") {
		return content
	}
	rest := strings.TrimPrefix(content, "---")
	if strings.HasPrefix(rest, "\r\n") {
		rest = strings.TrimPrefix(rest, "\r\n")
	} else if strings.HasPrefix(rest, "\n") {
		rest = strings.TrimPrefix(rest, "\n")
	}
	for _, sep := range []string{"\n---\n", "\n---\r\n"} {
		if idx := strings.Index(rest, sep); idx >= 0 {
			return rest[idx+len(sep):]
		}
	}
	return content
}

func wikiSearchTerms(query string) []string {
	fields := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	seen := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) < 2 || seen[field] {
			continue
		}
		seen[field] = true
		out = append(out, field)
	}
	return out
}

// parseTime returns the parsed RFC3339 time or zero if the input is empty
// or malformed. Caller decides what to do with the zero value.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// isValidSlug mirrors wiki.Slug's allowed character set: lowercase
// alphanumerics + hyphen.
func isValidSlug(s string) bool {
	if s == "" || len(s) > 200 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-':
		default:
			return false
		}
	}
	return true
}

func handleWikiGodNodes(deps Deps) http.HandlerFunc {
	type godNodesProvider interface {
		TopNodes(topK int) []wiki.GodNode
	}
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := deps.Wiki.(godNodesProvider)
		if !ok {
			writeError(w, deps.Logger, http.StatusNotImplemented, "god nodes not available")
			return
		}
		topK := 10
		if s := r.URL.Query().Get("top_k"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > 50 {
				writeError(w, deps.Logger, http.StatusBadRequest, "top_k must be an integer between 1 and 50")
				return
			}
			topK = n
		}
		nodes := provider.TopNodes(topK)
		type nodeDTO struct {
			Slug     string `json:"slug"`
			Title    string `json:"title"`
			Category string `json:"category,omitempty"`
			In       int    `json:"in"`
			Out      int    `json:"out"`
			Total    int    `json:"total"`
		}
		dtos := make([]nodeDTO, len(nodes))
		for i, n := range nodes {
			dtos[i] = nodeDTO{
				Slug:     n.Slug,
				Title:    n.Title,
				Category: n.Category,
				In:       n.InDegree,
				Out:      n.OutDegree,
				Total:    n.TotalDegree,
			}
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"nodes": dtos})
	}
}

func handleWikiPath(deps Deps) http.HandlerFunc {
	type pathProvider interface {
		WikiPath(from, to string, maxHops int) []string
	}
	return func(w http.ResponseWriter, r *http.Request) {
		provider, ok := deps.Wiki.(pathProvider)
		if !ok {
			writeError(w, deps.Logger, http.StatusNotImplemented, "wiki path not available")
			return
		}
		from := strings.TrimSpace(r.URL.Query().Get("from"))
		to := strings.TrimSpace(r.URL.Query().Get("to"))
		if from == "" || to == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "from and to query parameters required")
			return
		}
		if !isValidSlug(from) || !isValidSlug(to) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid slug")
			return
		}
		maxHops := 5
		if s := r.URL.Query().Get("max_hops"); s != "" {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > 20 {
				writeError(w, deps.Logger, http.StatusBadRequest, "max_hops must be an integer between 1 and 20")
				return
			}
			maxHops = n
		}
		path := provider.WikiPath(from, to, maxHops)
		if len(path) == 0 {
			writeJSON(w, deps.Logger, http.StatusOK, map[string]any{
				"path":   nil,
				"reason": strconv.Itoa(maxHops) + " hops limit reached — no path found",
			})
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{
			"path": path,
			"hops": len(path) - 1,
		})
	}
}

// latestWikiMTime walks dir non-recursively and returns the newest .md
// modification time, or zero if no pages exist.
func latestWikiMTime(dir string) (time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}, err
	}
	var latest time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".md" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
	}
	return latest.UTC(), nil
}
