package wiki

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/storage/reindex"
)

// SetFreshnessStore wires an optional freshness.Store so WritePage bumps
// compact_memory_wiki pending_count after every successful page write.
// Nil-safe: passing nil disables the hook.
func (s *Store) SetFreshnessStore(fs *freshness.Store) {
	s.freshnessStore = fs
}

// ConflictError is returned by WritePage when the on-disk updated_at does
// not match the caller-supplied expectedUpdatedAt, OR when expectedUpdatedAt
// is "" (create-only sentinel) but a page with that slug already exists.
// The write_wiki_page tool turns this into a structured JSON tool RESULT
// (D-03) so the LLM can re-read and retry deterministically.
type ConflictError struct {
	Slug     string
	Expected string // "" means create-only sentinel (D-02)
	Actual   string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("page %s was modified since last read (expected %s, got %s)",
		e.Slug, e.Expected, e.Actual)
}

// WritePage atomically writes a wiki page to disk as .md, commits to git,
// and surfaces commit failures as page.Unversioned=true (GIT-01).
//
// The optional variadic expectedUpdatedAt enables optimistic concurrency
// (WIKI-02). Semantics (D-02):
//   - no variadic argument:        trust-caller (compat callers preserved, D-05)
//   - expectedUpdatedAt[0] == "":  create-only-if-absent (sentinel)
//   - expectedUpdatedAt[0] != "":  update-if-on-disk-updated_at-matches
//
// Conflicts return *ConflictError. The ETag check + the atomic write are
// both inside the per-slug fileMutex critical section (Pitfall #1 TOCTOU).
//
// After the critical section, WritePage runs bidirectional backlink
// maintenance (Wave 2 — GRAPH-01): every [[slug]] in page.Body that was
// not present in the previous version of this page gets `slug_of_writer`
// appended to the target page's `related` frontmatter array. This keeps
// the wiki graph bidirectional by construction so retrieval can rely on
// `related` as a real adjacency list. Backlink writes happen OUTSIDE the
// per-slug critical section to avoid cross-page deadlocks (lock targets
// in sorted slug order). The maintenance is additive only — removed
// [[links]] do NOT prune backlinks here; that belongs to the lint pass.
func (s *Store) WritePage(ctx context.Context, page *Page, expectedUpdatedAt ...string) error {
	if err := Validate(page); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	slug := Slug(page.Title)

	prevBodyLinks, err := s.writePageLocked(ctx, slug, page, expectedUpdatedAt)
	if err != nil {
		return err
	}

	// Phase 2 (GRAPH-01): bidirectional backlink maintenance.
	newBodyLinks := ExtractWikiLinks(page.Body)
	s.maintainBacklinks(ctx, slug, prevBodyLinks, newBodyLinks)

	// Rebuild TOC cache asynchronously so the next turn has the updated index.
	go s.RebuildTOC()
	return nil
}

// writePageLocked is the per-slug critical section of WritePage. It
// returns the [[wiki-links]] present in the page's BODY before the write
// (empty slice if the page didn't exist), which the caller uses to
// compute the backlink delta after releasing the mutex.
func (s *Store) writePageLocked(ctx context.Context, slug string, page *Page, expectedUpdatedAt []string) ([]string, error) {
	filename := slug + ".md"
	path := filepath.Join(s.dir, filename)

	mu := s.fileMutex(slug)
	mu.Lock()
	defer mu.Unlock()

	// Read existing once — used for both the ETag check below AND the
	// prev-body-links capture returned to the caller. Trust-caller writes
	// (no variadic) still benefit from the read so backlink maintenance
	// can compute an accurate delta.
	var prevBodyLinks []string
	existing, readErr := s.readPageLocked(slug)
	if readErr == nil {
		prevBodyLinks = ExtractWikiLinks(existing.Body)
	}

	// ETag check INSIDE the critical section (Pitfall #1 TOCTOU prevention).
	if len(expectedUpdatedAt) > 0 {
		expected := expectedUpdatedAt[0]
		switch {
		case expected == "" && readErr == nil:
			// create-only sentinel, but page already exists
			return nil, &ConflictError{Slug: slug, Expected: "", Actual: existing.UpdatedAt}
		case expected != "" && readErr != nil:
			// update expected but page not found
			return nil, fmt.Errorf("page %s not found for ETag update: %w", slug, readErr)
		case expected != "" && existing.UpdatedAt != expected:
			// on-disk timestamp doesn't match what caller expected
			return nil, &ConflictError{Slug: slug, Expected: expected, Actual: existing.UpdatedAt}
		}
	}

	// Remove old .yaml if it exists.
	yamlPath := filepath.Join(s.dir, slug+".yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		os.Remove(yamlPath)
		s.gitCommit(ctx, slug+".yaml", "delete")
	}

	// Serialize markdown.
	data, err := MarshalMD(page)
	if err != nil {
		return nil, fmt.Errorf("marshaling markdown: %w", err)
	}

	// Atomic temp+rename.
	if err := writeAtomic(s.dir, slug, path, data); err != nil {
		return nil, err
	}

	if s.freshnessStore != nil {
		if bErr := s.freshnessStore.BumpPending(ctx, "compact_memory_wiki", 1); bErr != nil {
			s.logger.Warn("wiki: freshness bump failed", "slug", slug, "error", bErr)
		}
	}

	s.logger.Info("wiki page written", "slug", slug, "path", path)
	s.updateIndex(ctx)
	s.appendLog(ctx, "update", slug)

	// gitCommit and Unversioned set/clear (GIT-01).
	commitErr := s.gitCommit(ctx, filename, "update")
	if commitErr != nil {
		s.logger.Error("git commit failed for wiki page", "slug", slug, "error", commitErr)
		// D-17: re-read the just-written page, run Validate, set Unversioned=true,
		// atomic re-write. NO recursive commit (would just fail again).
		// WARNING 14 (2026-05-10 revision): Validate(reread) runs BEFORE re-marshal
		// so a corrupted on-disk page does not propagate the Unversioned flag write.
		if reread, rerr := s.readPageLocked(slug); rerr == nil && !reread.Unversioned {
			if vErr := Validate(reread); vErr != nil {
				s.logger.Warn("skipping Unversioned set: re-read page failed validation",
					"slug", slug, "error", vErr)
			} else {
				reread.Unversioned = true
				if newData, mErr := MarshalMD(reread); mErr == nil {
					_ = writeAtomic(s.dir, slug, path, newData)
				}
			}
		}
	} else {
		// D-18: commit succeeded — if the page was Unversioned, clear and atomic re-write.
		// WARNING 14: Validate runs BEFORE re-marshal here too. NO recursive commit
		// (avoids loop-back).
		if reread, rerr := s.readPageLocked(slug); rerr == nil && reread.Unversioned {
			if vErr := Validate(reread); vErr != nil {
				s.logger.Warn("skipping Unversioned clear: re-read page failed validation",
					"slug", slug, "error", vErr)
			} else {
				reread.Unversioned = false
				if newData, mErr := MarshalMD(reread); mErr == nil {
					_ = writeAtomic(s.dir, slug, path, newData)
				}
			}
		}
	}

	// D-14: Enqueue reindex AFTER file write succeeds, regardless of git commit
	// outcome. Submission is non-blocking and drop-newest; the worker re-reads
	// from disk so dropped signals are safe.
	if s.reindexSubmitter != nil {
		_ = s.reindexSubmitter.Submit(reindex.Job{Slug: slug, Op: reindex.OpUpsert})
	}

	// GRAPH-02: refresh the in-memory adjacency index so retrieval sees
	// the new outbound edges immediately. Refresh happens INSIDE the
	// critical section so concurrent writes to other slugs serialize
	// against the index's own RWMutex, not against this slug's file
	// mutex. The index's per-call locking is cheap.
	if s.graphIndex != nil {
		s.graphIndex.RefreshPage(slug, page)
	}
	return prevBodyLinks, nil
}

// maintainBacklinks (Wave 2 — GRAPH-01) keeps `related:` arrays in sync
// with the actual [[wiki-link]] graph. For each link that newly appeared
// in the page's body, we append the writer slug to the target page's
// `related` array (idempotent). Targets are processed in sorted slug
// order so concurrent writes on cycles (A↔B) cannot deadlock — both
// acquire mutexes in the same order.
//
// Removed links are NOT pruned here. Pruning would require distinguishing
// automatically-added entries from user-curated `related` entries, which
// is impossible without provenance metadata. The lint pass (Lint) handles
// stale-backlink detection.
func (s *Store) maintainBacklinks(ctx context.Context, writerSlug string, prevLinks, newLinks []string) {
	prevSet := make(map[string]bool, len(prevLinks))
	for _, link := range prevLinks {
		prevSet[link] = true
	}
	var added []string
	for _, link := range newLinks {
		if link == writerSlug {
			continue // skip self-references
		}
		if prevSet[link] {
			continue // already linked in prev body
		}
		added = append(added, link)
	}
	if len(added) == 0 {
		return
	}
	sort.Strings(added) // canonical lock acquisition order

	for _, targetSlug := range added {
		if err := s.addBacklink(ctx, targetSlug, writerSlug); err != nil {
			// Log and continue — one failed backlink target should not
			// abort the others, and the writer's main page is already
			// committed so the agent loop has made progress.
			s.logger.Warn("backlink update failed",
				"writer", writerSlug, "target", targetSlug, "error", err)
		}
	}
}

// addBacklink acquires the target's fileMutex, reads the page, ensures
// writerSlug is present in the target's `related` array, and writes
// atomically with a "backlink" git commit. It is idempotent — calling
// twice with the same args is a no-op.
//
// Skipped silently when:
//   - the target page does not exist on disk (no-op, no error)
//   - writerSlug is already in target.Related (idempotency)
//
// Index/log regeneration is skipped because a backlink-only edit does
// not change a page's category or carry narrative content; the next
// real write will pick up the related-array change via the index
// rebuild.
func (s *Store) addBacklink(ctx context.Context, targetSlug, writerSlug string) error {
	mu := s.fileMutex(targetSlug)
	mu.Lock()
	defer mu.Unlock()

	target, err := s.readPageLocked(targetSlug)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // target doesn't exist yet — broken ref, lint will catch
		}
		return fmt.Errorf("read backlink target: %w", err)
	}

	// Idempotency: if writerSlug already related, nothing to do.
	if RelatedContainsSlug(target.Related, writerSlug) {
		return nil
	}

	target.Related = RelatedAppendSlug(target.Related, writerSlug)
	target.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	if err := Validate(target); err != nil {
		return fmt.Errorf("backlink target failed validation: %w", err)
	}

	data, err := MarshalMD(target)
	if err != nil {
		return fmt.Errorf("marshal backlink target: %w", err)
	}
	path := filepath.Join(s.dir, targetSlug+".md")
	if err := writeAtomic(s.dir, targetSlug, path, data); err != nil {
		return err
	}

	s.logger.Info("backlink added", "target", targetSlug, "writer", writerSlug)
	s.appendLog(ctx, "backlink", targetSlug)

	if commitErr := s.gitCommit(ctx, targetSlug+".md", "backlink"); commitErr != nil {
		s.logger.Warn("git commit for backlink failed",
			"target", targetSlug, "error", commitErr)
	}
	if s.reindexSubmitter != nil {
		_ = s.reindexSubmitter.Submit(reindex.Job{Slug: targetSlug, Op: reindex.OpUpsert})
	}
	// GRAPH-02: refresh the index for the backlink target so its
	// outbound (Related-derived) edge to the writer is reflected at
	// runtime. Without this the index would track only the writer's
	// forward edge and miss the now-mirrored back edge.
	if s.graphIndex != nil {
		s.graphIndex.RefreshPage(targetSlug, target)
	}
	return nil
}

// DeletePage removes a wiki page by slug and commits the deletion to git.
func (s *Store) DeletePage(ctx context.Context, slug string) error {
	mu := s.fileMutex(slug)
	mu.Lock()
	defer mu.Unlock()

	// Try .md first, then .yaml
	var removed bool
	var filename string
	for _, ext := range []string{".md", ".yaml"} {
		path := filepath.Join(s.dir, slug+ext)
		if err := os.Remove(path); err == nil {
			removed = true
			filename = slug + ext
			break
		}
	}

	if !removed {
		return fmt.Errorf("deleting wiki page %s: file not found", slug)
	}

	s.logger.Info("wiki page deleted", "slug", slug)
	s.updateIndex(ctx)
	s.appendLog(ctx, "delete", slug)

	if err := s.gitCommit(ctx, filename, "delete"); err != nil {
		s.logger.Error("git commit failed for wiki page deletion", "slug", slug, "error", err)
	}

	// D-14: Enqueue reindex delete AFTER file removal succeeds, regardless of git commit outcome.
	if s.reindexSubmitter != nil {
		_ = s.reindexSubmitter.Submit(reindex.Job{Slug: slug, Op: reindex.OpDelete})
	}

	// GRAPH-02: purge the slug from the in-memory adjacency index so
	// downstream retrieval doesn't traverse into a deleted node. Pages
	// that still reference the deleted slug retain a body [[wikilink]]
	// that will surface as a broken ref in the materialized graph but
	// are dropped from the in-mem outbound set (the link goes nowhere
	// at traversal time).
	if s.graphIndex != nil {
		s.graphIndex.RemoveNode(slug)
	}
	go s.RebuildTOC()
	return nil
}

// readPageLocked reads a page from disk WITHOUT acquiring fileMutex.
// Caller MUST already hold s.fileMutex(slug).Lock(). Used by WritePage's
// ETag check (Pitfall #1) and Unversioned set/clear (D-17, D-18).
//
// ParseMD returns (*Page, error) — verified at internal/wiki/parser.go:144
// during the 2026-05-10 plan revision. The two-value form is canonical.
func (s *Store) readPageLocked(slug string) (*Page, error) {
	path := filepath.Join(s.dir, slug+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	page, err := ParseMD(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", slug, err)
	}
	return page, nil
}

// writeAtomic writes data to path via temp+rename. Caller MUST already
// hold the per-slug fileMutex. Factored from the existing inline shape
// in store.go to keep WritePage readable.
func writeAtomic(dir, slug, path string, data []byte) error {
	tmp, err := os.CreateTemp(dir, slug+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("writing temp file: %w", err)
	}
	tmp.Close()
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// MigrateYAMLToMD performs a one-time migration of all .yaml wiki pages to .md format.
// Returns the number of pages migrated.
func (s *Store) MigrateYAMLToMD(ctx context.Context) (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, fmt.Errorf("reading wiki dir for migration: %w", err)
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		slug := strings.TrimSuffix(entry.Name(), ".yaml")
		yamlPath := filepath.Join(s.dir, entry.Name())
		mdPath := filepath.Join(s.dir, slug+".md")

		// Skip if .md already exists
		if _, err := os.Stat(mdPath); err == nil {
			continue
		}

		data, err := os.ReadFile(yamlPath)
		if err != nil {
			s.logger.Warn("failed to read yaml for migration", "slug", slug, "error", err)
			continue
		}

		page, err := ParseYAML(data)
		if err != nil {
			s.logger.Warn("failed to parse yaml for migration", "slug", slug, "error", err)
			continue
		}

		// Force current schema version
		page.SchemaVersion = CurrentSchemaVersion

		mdData, err := MarshalMD(page)
		if err != nil {
			s.logger.Warn("failed to marshal md for migration", "slug", slug, "error", err)
			continue
		}

		if err := os.WriteFile(mdPath, mdData, 0644); err != nil {
			s.logger.Warn("failed to write md during migration", "slug", slug, "error", err)
			continue
		}

		os.Remove(yamlPath)
		count++

		s.logger.Info("migrated wiki page", "slug", slug, "from", "yaml", "to", "md")
	}

	if count > 0 {
		s.updateIndex(ctx)
		s.appendLog(ctx, "migrate", "batch")
		s.logger.Info("wiki migration complete", "pages_migrated", count)
	}

	return count, nil
}

// RepairLink replaces all occurrences of [[brokenSlug]] in page bodies and
// brokenSlug entries in related frontmatter with fixedSlug. Pages without the
// broken reference are not modified. Commits each repaired page to git.
//
// Per-page failures are accumulated rather than aborting the scan. That
// keeps a single malformed page from preventing later pages from being
// repaired, and guarantees the audit log records that an auto-fix pass ran.
func (s *Store) RepairLink(ctx context.Context, brokenSlug, fixedSlug string) error {
	slugs, err := s.ListPages()
	if err != nil {
		return fmt.Errorf("repair link list: %w", err)
	}
	old := "[[" + brokenSlug + "]]"
	replacement := "[[" + fixedSlug + "]]"
	var failures []error
	for _, slug := range slugs {
		page, err := s.ReadPage(slug)
		if err != nil {
			failures = append(failures, fmt.Errorf("read %s: %w", slug, err))
			continue
		}
		changed := false
		if strings.Contains(page.Body, old) {
			page.Body = strings.ReplaceAll(page.Body, old, replacement)
			changed = true
		}
		for i := range page.Related {
			if page.Related[i].Slug == brokenSlug {
				page.Related[i].Slug = fixedSlug
				changed = true
			}
		}
		if !changed {
			continue
		}
		page.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := s.WritePage(ctx, page); err != nil {
			failures = append(failures, fmt.Errorf("write %s: %w", slug, err))
			continue
		}
		s.logger.Info("auto-fixed broken link", "page", slug, "broken", brokenSlug, "fixed", fixedSlug)
	}
	s.AppendLog(ctx, "auto-fix", brokenSlug+"->"+fixedSlug)
	if len(failures) > 0 {
		return fmt.Errorf("repair link completed with %d failed page(s): %w", len(failures), errors.Join(failures...))
	}
	return nil
}
