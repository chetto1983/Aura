package wiki

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/reindex"
)

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
//   - no variadic argument:        trust-caller (legacy callers preserved, D-05)
//   - expectedUpdatedAt[0] == "":  create-only-if-absent (sentinel)
//   - expectedUpdatedAt[0] != "":  update-if-on-disk-updated_at-matches
//
// Conflicts return *ConflictError. The ETag check + the atomic write are
// both inside the per-slug fileMutex critical section (Pitfall #1 TOCTOU).
func (s *Store) WritePage(ctx context.Context, page *Page, expectedUpdatedAt ...string) error {
	if err := Validate(page); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}
	slug := Slug(page.Title)
	filename := slug + ".md"
	path := filepath.Join(s.dir, filename)

	mu := s.fileMutex(slug)
	mu.Lock()
	defer mu.Unlock()

	// ETag check INSIDE the critical section (Pitfall #1 prevention).
	if len(expectedUpdatedAt) > 0 {
		expected := expectedUpdatedAt[0]
		existing, readErr := s.readPageLocked(slug)
		switch {
		case expected == "" && readErr == nil:
			// create-only sentinel, but page already exists
			return &ConflictError{Slug: slug, Expected: "", Actual: existing.UpdatedAt}
		case expected != "" && readErr != nil:
			// update expected but page not found
			return fmt.Errorf("page %s not found for ETag update: %w", slug, readErr)
		case expected != "" && existing.UpdatedAt != expected:
			// on-disk timestamp doesn't match what caller expected
			return &ConflictError{Slug: slug, Expected: expected, Actual: existing.UpdatedAt}
		}
	}

	// Remove legacy .yaml if it exists.
	yamlPath := filepath.Join(s.dir, slug+".yaml")
	if _, err := os.Stat(yamlPath); err == nil {
		os.Remove(yamlPath)
		s.gitCommit(ctx, slug+".yaml", "delete")
	}

	// Serialize markdown.
	data, err := MarshalMD(page)
	if err != nil {
		return fmt.Errorf("marshaling markdown: %w", err)
	}

	// Atomic temp+rename.
	if err := writeAtomic(s.dir, slug, path, data); err != nil {
		return err
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
		for i, rel := range page.Related {
			if rel == brokenSlug {
				page.Related[i] = fixedSlug
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
