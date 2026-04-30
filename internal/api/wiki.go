package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/wiki"
)

// maxWikiPageBytes caps the body returned by GET /wiki/page so a runaway
// page can't blow out the dashboard's fetch buffer.
const maxWikiPageBytes = 1 << 20 // 1 MiB

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
		slugs, err := deps.Wiki.ListPages()
		if err != nil {
			deps.Logger.Warn("api: list wiki pages for graph", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list pages")
			return
		}
		known := make(map[string]bool, len(slugs))
		for _, s := range slugs {
			known[s] = true
		}
		nodes := make([]GraphNode, 0, len(slugs))
		var edges []GraphEdge
		for _, slug := range slugs {
			page, err := deps.Wiki.ReadPage(slug)
			if err != nil {
				deps.Logger.Warn("api: read page for graph", "slug", slug, "error", err)
				continue
			}
			nodes = append(nodes, GraphNode{ID: slug, Title: page.Title, Category: page.Category})
			seen := make(map[string]bool)
			for _, target := range wiki.ExtractWikiLinks(page.Body) {
				if target == slug || !known[target] || seen[target] {
					continue
				}
				seen[target] = true
				edges = append(edges, GraphEdge{Source: slug, Target: target, Type: "wikilink"})
			}
			for _, target := range page.Related {
				if target == slug || !known[target] || seen[target] {
					continue
				}
				seen[target] = true
				edges = append(edges, GraphEdge{Source: slug, Target: target, Type: "related"})
			}
		}
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
		sort.Slice(edges, func(i, j int) bool {
			if edges[i].Source != edges[j].Source {
				return edges[i].Source < edges[j].Source
			}
			return edges[i].Target < edges[j].Target
		})
		writeJSON(w, deps.Logger, http.StatusOK, Graph{Nodes: nodes, Edges: edges})
	}
}

func loadWikiSummaries(store WikiStore) ([]WikiPageSummary, error) {
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
	}
	if p.Category != "" {
		fm["category"] = p.Category
	}
	if len(p.Tags) > 0 {
		fm["tags"] = slices.Clone(p.Tags)
	}
	if len(p.Related) > 0 {
		fm["related"] = slices.Clone(p.Related)
	}
	if len(p.Sources) > 0 {
		fm["sources"] = slices.Clone(p.Sources)
	}
	return fm
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
