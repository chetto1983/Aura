package wiki

import "context"

// SetFTS5Syncer wires an optional FTS5PageSyncer so WritePage/DeletePage keep
// the SQLite wiki_documents FTS5 mirror in sync synchronously on every mutation.
// Nil-safe: passing nil disables the hook. Phase WIKI-FIX-01.
func (s *Store) SetFTS5Syncer(syncer FTS5PageSyncer) {
	s.fts5Syncer = syncer
}

// ReconcileFTS5Mirror compares the wiki_documents row count with the number of
// pages on disk. When the mirror is missing more than 5% of pages it re-syncs
// every on-disk page and logs "wiki: FTS5 mirror rebuilt". Call this once at
// boot after SetFTS5Syncer. Phase WIKI-FIX-01.
func (s *Store) ReconcileFTS5Mirror(ctx context.Context) {
	if s.fts5Syncer == nil {
		return
	}
	slugs, err := s.ListPages()
	if err != nil {
		s.logger.Warn("wiki: FTS5 reconcile: list pages failed", "error", err)
		return
	}
	diskCount := len(slugs)
	if diskCount == 0 {
		return
	}
	dbCount, err := s.fts5Syncer.CountDocs(ctx)
	if err != nil {
		s.logger.Warn("wiki: FTS5 reconcile: count docs failed", "error", err)
		return
	}
	// 5% drift tolerance: trigger rebuild when db has fewer than 95% of disk pages.
	threshold := diskCount - diskCount/20
	if dbCount >= threshold {
		return
	}
	s.logger.Info("wiki: FTS5 mirror drift detected, rebuilding",
		"db_rows", dbCount, "disk_pages", diskCount, "threshold", threshold)
	rebuilt := 0
	for _, slug := range slugs {
		page, rerr := s.ReadPage(slug)
		if rerr != nil {
			s.logger.Warn("wiki: FTS5 reconcile: skip unreadable page", "slug", slug, "error", rerr)
			continue
		}
		if syncErr := s.fts5Syncer.SyncPage(ctx, slug, page); syncErr != nil {
			s.logger.Warn("wiki: FTS5 reconcile: sync failed", "slug", slug, "error", syncErr)
			continue
		}
		rebuilt++
	}
	s.logger.Info("wiki: FTS5 mirror rebuilt", "pages_synced", rebuilt)
}
