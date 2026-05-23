// Package sweep provides LRU+age eviction for on-disk session directories.
// It is designed to run piggybacked on persist operations — no separate daemon.
package sweep

import (
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// MaxSessions is the maximum number of session directories kept under
	// the spill base directory. When exceeded, the oldest directories are
	// evicted (LRU policy measured by directory mtime).
	MaxSessions = 32

	// MaxAge is the wall-clock TTL for session directories. Directories older
	// than this are evicted regardless of total count.
	MaxAge = 7 * 24 * time.Hour
)

type dirEntry struct {
	name  string
	mtime time.Time
}

// SweepSessions evicts directories from baseDir that exceed the LRU session
// cap (MaxSessions) or the age TTL (MaxAge). It is intended to be called
// piggybacked on each spill persist. Errors are not propagated for individual
// entry failures; the function returns the first ReadDir or Info error only.
func SweepSessions(baseDir string) error {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	now := time.Now()
	var keep []dirEntry

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil {
			continue
		}
		if now.Sub(info.ModTime()) > MaxAge {
			_ = os.RemoveAll(filepath.Join(baseDir, e.Name()))
			continue
		}
		keep = append(keep, dirEntry{name: e.Name(), mtime: info.ModTime()})
	}

	// Sort newest-first; evict all entries beyond the LRU cap.
	sort.Slice(keep, func(i, j int) bool {
		return keep[i].mtime.After(keep[j].mtime)
	})
	for i := MaxSessions; i < len(keep); i++ {
		_ = os.RemoveAll(filepath.Join(baseDir, keep[i].name))
	}
	return nil
}
