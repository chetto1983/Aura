package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *Store) planOpaqueSourceRenames(pages []memoryPage, known map[string]*Page) []PageRename {
	planned := make([]PageRename, 0)
	reserved := make(map[string]bool)
	for slug := range known {
		reserved[slug] = true
	}
	for _, item := range pages {
		if !isOpaqueSourceSlug(item.slug) || item.page == nil || !hasString(item.page.Tags, "source") {
			continue
		}
		sourceID := pageSourceID(item.page)
		if sourceID == "" {
			continue
		}
		heading := s.semanticSourceHeading(sourceID)
		if heading == "" {
			continue
		}
		title := truncateTitle("Source: " + heading)
		nextSlug := Slug(title)
		if nextSlug == "" || nextSlug == item.slug || IsOperationalSlug(nextSlug) {
			continue
		}
		if reserved[nextSlug] && nextSlug != item.slug {
			continue
		}
		reserved[nextSlug] = true
		planned = append(planned, PageRename{
			From:   item.slug,
			To:     nextSlug,
			Title:  title,
			Reason: "opaque source slug replaced with heading from extracted source markdown",
		})
	}
	sort.Slice(planned, func(i, j int) bool {
		return planned[i].From < planned[j].From
	})
	return planned
}

func (s *Store) applyPageRenames(ctx context.Context, renames []PageRename) error {
	renameMap := make(map[string]string, len(renames))
	for _, rename := range renames {
		if rename.From == "" || rename.To == "" || rename.From == rename.To {
			continue
		}
		page, err := s.ReadPage(rename.From)
		if err != nil {
			return fmt.Errorf("rename wiki page read %s: %w", rename.From, err)
		}
		page.Title = rename.Title
		if page.Title == "" {
			page.Title = titleFromSlug(rename.To)
		}
		stripAutoSourcePreview(page)
		page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.WritePage(ctx, page); err != nil {
			return fmt.Errorf("rename wiki page write %s -> %s: %w", rename.From, rename.To, err)
		}
		if err := s.DeletePage(ctx, rename.From); err != nil {
			return fmt.Errorf("rename wiki page delete %s: %w", rename.From, err)
		}
		if sourceID := pageSourceID(page); sourceID != "" {
			if err := s.updateSourceWikiPages(sourceID, rename.From, rename.To); err != nil {
				return fmt.Errorf("rename wiki source metadata %s: %w", sourceID, err)
			}
		}
		renameMap[rename.From] = rename.To
	}
	if len(renameMap) == 0 {
		return nil
	}

	pages, err := s.memoryPages()
	if err != nil {
		return err
	}
	for _, item := range pages {
		changed := false
		for oldSlug, newSlug := range renameMap {
			oldLink := "[[" + oldSlug + "]]"
			newLink := "[[" + newSlug + "]]"
			if strings.Contains(item.page.Body, oldLink) {
				item.page.Body = strings.ReplaceAll(item.page.Body, oldLink, newLink)
				changed = true
			}
			if replaceRelated(item.page, oldSlug, newSlug) {
				changed = true
			}
		}
		if !changed {
			continue
		}
		item.page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.WritePage(ctx, item.page); err != nil {
			return fmt.Errorf("rename wiki backlinks write %s: %w", item.slug, err)
		}
	}
	return nil
}

func isOpaqueSourceSlug(slug string) bool {
	const prefix = "source-"
	if !strings.HasPrefix(slug, prefix) {
		return false
	}
	rest := strings.TrimPrefix(slug, prefix)
	parts := strings.Split(rest, "-")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func pageSourceID(page *Page) string {
	if page == nil {
		return ""
	}
	for _, source := range page.Sources {
		source = strings.TrimSpace(source)
		if id, ok := strings.CutPrefix(source, "source:"); ok {
			return id
		}
	}
	return ""
}

func (s *Store) semanticSourceHeading(sourceID string) string {
	for _, name := range []string{"extract.md", "ocr.md"} {
		data, err := os.ReadFile(filepath.Join(s.sourceFilesRoot(), sourceID, name))
		if err != nil {
			continue
		}
		if heading := firstSemanticHeading(string(data)); heading != "" {
			return heading
		}
	}
	return ""
}

// sourceFilesRoot returns the directory where ingested source artifacts
// live. After Phase-FS-LAYOUT (2026-05-23) this is configured via
// SetSourcesDir from cfg.SourcesPath; pre-split deployments fall back to
// the legacy <wikiDir>/raw layout so existing fixtures keep working
// until the migration in cmd/aura.migrateLegacyWikiRaw runs.
func (s *Store) sourceFilesRoot() string {
	if s.sourcesDir != "" {
		return s.sourcesDir
	}
	return filepath.Join(s.dir, "raw")
}

// SetSourcesDir wires the external sources path the wiki store reads
// from when resolving source artifacts. Called once at boot by
// cmd/aura.app wiring. Empty value leaves the store on the legacy
// <wikiDir>/raw fallback.
func (s *Store) SetSourcesDir(dir string) {
	s.sourcesDir = dir
}

func truncateTitle(title string) string {
	title = strings.TrimSpace(title)
	if len(title) <= 200 {
		return title
	}
	cut := title[:200]
	if idx := strings.LastIndex(cut, " "); idx >= 20 {
		cut = cut[:idx]
	}
	return strings.TrimSpace(cut)
}

func (s *Store) updateSourceWikiPages(sourceID, oldSlug, newSlug string) error {
	path := filepath.Join(s.sourceFilesRoot(), sourceID, "source.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	pages := make([]string, 0, 1)
	seen := make(map[string]bool)
	if rawPages, ok := meta["wiki_pages"].([]any); ok {
		for _, rawPage := range rawPages {
			slug, ok := rawPage.(string)
			if !ok {
				continue
			}
			if slug == oldSlug {
				slug = newSlug
			}
			if slug == "" || seen[slug] {
				continue
			}
			seen[slug] = true
			pages = append(pages, slug)
		}
	}
	if !seen[newSlug] {
		pages = append(pages, newSlug)
	}
	sort.Strings(pages)
	meta["wiki_pages"] = pages

	updated, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	updated = append(updated, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), "source.*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(updated); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
