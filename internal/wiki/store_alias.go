package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// warmAliasIndex reads every wiki page from disk and populates the alias index
// (US-GRAPH-01) in one shot. Pages are processed in mtime order (oldest first)
// so that first-write-wins conflict resolution is deterministic across restarts.
// Per-page failures are logged and skipped.
func (s *Store) warmAliasIndex() error {
	slugs, err := s.ListPages()
	if err != nil {
		return fmt.Errorf("list pages for alias index: %w", err)
	}

	type slugMtime struct {
		slug  string
		mtime int64
	}
	ordered := make([]slugMtime, 0, len(slugs))
	for _, slug := range slugs {
		path := filepath.Join(s.dir, slug+".md")
		fi, err := os.Stat(path)
		if err != nil {
			// Fall back to .yaml mtime if .md absent.
			path = filepath.Join(s.dir, slug+".yaml")
			fi, err = os.Stat(path)
			if err != nil {
				ordered = append(ordered, slugMtime{slug: slug, mtime: 0})
				continue
			}
		}
		ordered = append(ordered, slugMtime{slug: slug, mtime: fi.ModTime().UnixNano()})
	}

	// Oldest file first → first-write wins for same-length alias conflicts.
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].mtime < ordered[j].mtime
	})

	for _, item := range ordered {
		page, err := s.ReadPage(item.slug)
		if err != nil {
			s.logger.Warn("alias index skip page", "slug", item.slug, "error", err)
			continue
		}
		s.aliasIndex.Set(item.slug, page.Aliases)
	}
	s.logger.Info("alias index warmed", "keys", s.aliasIndex.Len())
	return nil
}

// ResolveAlias maps an alias string to the canonical page slug that claims it.
// Returns (slug, true) on a match or ("", false) when no alias matches.
// The lookup is O(1) after normalization (trim + lowercase + whitespace-collapse + NFC).
func (s *Store) ResolveAlias(alias string) (string, bool) {
	return s.aliasIndex.Resolve(alias)
}
