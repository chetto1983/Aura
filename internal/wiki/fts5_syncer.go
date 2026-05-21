package wiki

import "context"

// FTS5PageSyncer is called synchronously from WritePage and DeletePage to keep
// the SQLite FTS5 wiki_documents mirror in step with page mutations on disk.
// Implementations live in internal/storage/search (WikiFTS5Syncer); the
// interface lives here to avoid an import cycle (search already imports wiki).
type FTS5PageSyncer interface {
	// SyncPage upserts the given page into the FTS5 mirror under key slug.
	SyncPage(ctx context.Context, slug string, page *Page) error
	// RemovePage deletes the row for slug from the FTS5 mirror.
	RemovePage(ctx context.Context, slug string) error
	// CountDocs returns the total row count in wiki_documents.
	CountDocs(ctx context.Context) (int, error)
}
