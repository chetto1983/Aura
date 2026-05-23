package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// migrateLegacyWikiRaw moves the legacy <wikiPath>/raw directory to the
// new sourcesPath if (a) the legacy directory still exists and has at
// least one entry, AND (b) the new sourcesPath does not already contain
// any source folders. The function is a one-shot startup helper: once
// the data has moved, subsequent boots find the legacy dir absent and
// the function returns nil immediately.
//
// 2026-05-23 layout split: pre-split, sources lived under wiki/raw so a
// single 'aura-wiki' volume held everything. The /files/wiki dashboard
// view ended up showing the raw artifacts (PDFs, OCR md, source.json)
// mixed with LLM-curated wiki pages, which confused operators and made
// it easy to nuke ingestion state by mistake. Splitting puts sources
// on its own bind mount + dashboard root.
//
// Edge cases:
//   - Empty WikiPath OR SourcesPath: skip — caller's config is
//     incomplete and another check upstream will error.
//   - sourcesPath == wikiPath/raw: no-op (operator opted to keep the
//     legacy layout via SOURCES_PATH=…/wiki/raw).
//   - Legacy raw dir missing or empty: nothing to do.
//   - New sourcesPath already populated (count > 0): skip with a
//     warning; do NOT overwrite existing data.
func migrateLegacyWikiRaw(wikiPath, sourcesPath string, logger *slog.Logger) error {
	wikiPath = strings.TrimSpace(wikiPath)
	sourcesPath = strings.TrimSpace(sourcesPath)
	if wikiPath == "" || sourcesPath == "" {
		return nil
	}

	legacy := filepath.Join(wikiPath, "raw")
	// Same-path no-op (operator opted-in to legacy layout).
	if absEq(legacy, sourcesPath) {
		return nil
	}

	legacyEntries, err := os.ReadDir(legacy)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("scan legacy %s: %w", legacy, err)
	}
	if len(legacyEntries) == 0 {
		// Empty legacy dir: remove it so the wiki tree no longer shows
		// the orphan placeholder. Ignore RemoveAll errors — best-effort.
		_ = os.RemoveAll(legacy)
		return nil
	}

	if err := os.MkdirAll(sourcesPath, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", sourcesPath, err)
	}
	destEntries, _ := os.ReadDir(sourcesPath)
	if len(destEntries) > 0 {
		logger.Warn("legacy wiki/raw migration skipped — new sources path already has entries",
			"legacy", legacy, "sources", sourcesPath,
			"legacy_count", len(legacyEntries), "new_count", len(destEntries))
		return nil
	}

	moved := 0
	for _, ent := range legacyEntries {
		from := filepath.Join(legacy, ent.Name())
		to := filepath.Join(sourcesPath, ent.Name())
		if err := moveAcrossDevices(from, to); err != nil {
			return fmt.Errorf("move %s → %s: %w", from, to, err)
		}
		moved++
	}
	logger.Info("legacy wiki/raw → sources migration completed",
		"from", legacy, "to", sourcesPath, "entries_moved", moved)

	// Drop the now-empty legacy raw dir so the /files/wiki view stops
	// listing it. Best-effort: a non-empty dir (concurrent write) keeps
	// the operator-facing display but the source store reads from the
	// new path so semantics are unaffected.
	if err := os.Remove(legacy); err != nil {
		logger.Debug("could not remove empty legacy raw dir", "path", legacy, "error", err)
	}
	return nil
}

// moveAcrossDevices moves a directory tree (or single file) from src to
// dst tolerating cross-filesystem boundaries. os.Rename suffices on
// same-fs moves (linux ext4, macOS APFS, host-bind-mount); compose-style
// named volumes mount each path on a separate overlayfs device so the
// kernel returns EXDEV ("invalid cross-device link") and the bare
// Rename fails. The fallback walks the tree, recreates the directory
// hierarchy at dst, copies file contents, then removes src.
func moveAcrossDevices(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !strings.Contains(err.Error(), "cross-device") && !strings.Contains(err.Error(), "EXDEV") {
		return err
	}

	if err := copyTree(src, dst); err != nil {
		return fmt.Errorf("cross-device copy: %w", err)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("cross-device cleanup: %w", err)
	}
	return nil
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		s := filepath.Join(src, ent.Name())
		d := filepath.Join(dst, ent.Name())
		if err := copyTree(s, d); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func absEq(a, b string) bool {
	aa, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	bb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
