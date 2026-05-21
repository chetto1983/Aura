package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/storage/qdrant"
	"github.com/aura/aura/internal/wiki"
	"github.com/fsnotify/fsnotify"
)

// WikiOrphanReason classifies the trigger for a reconcile pass.
type WikiOrphanReason string

const (
	WikiOrphanReasonBoot     WikiOrphanReason = "boot"
	WikiOrphanReasonFSNotify WikiOrphanReason = "fsnotify"
	WikiOrphanReasonPeriodic WikiOrphanReason = "periodic"
)

// WikiOrphanReport summarises one ReconcileWikiQdrantWithDisk pass.
type WikiOrphanReport struct {
	Reason         WikiOrphanReason
	OrphansFound   int
	OrphansDeleted int
	ElapsedMs      int64
	Err            error
}

// WikiOrphanReconcilerConfig wires the lifecycle wrapper.
type WikiOrphanReconcilerConfig struct {
	QdrantClient qdrant.Client
	Collection   string
	DB           *sql.DB // FTS5 mirror; nil disables FTS5 cleanup
	WikiDir      string
	Logger       *slog.Logger
	Debounce     time.Duration // default 2s
	Periodic     time.Duration // default 1h; <=0 disables periodic ticker
}

// WikiOrphanReconciler is the lifecycle wrapper — boot + fsnotify + periodic.
// Construct via NewWikiOrphanReconciler, call Reconcile once synchronously
// at boot, then start Run as a background goroutine.
type WikiOrphanReconciler struct {
	cfg    WikiOrphanReconcilerConfig
	mu     sync.Mutex
	notify chan WikiOrphanReason
}

// NewWikiOrphanReconciler builds the reconciler with default values applied.
func NewWikiOrphanReconciler(cfg WikiOrphanReconcilerConfig) *WikiOrphanReconciler {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 2 * time.Second
	}
	if cfg.Periodic == 0 {
		cfg.Periodic = time.Hour
	}
	return &WikiOrphanReconciler{
		cfg:    cfg,
		notify: make(chan WikiOrphanReason, 8),
	}
}

// Notify enqueues a reconcile request. Non-blocking: if the channel is full
// a reconcile is already queued and will cover the same state.
func (r *WikiOrphanReconciler) Notify(reason WikiOrphanReason) {
	select {
	case r.notify <- reason:
	default:
		r.cfg.Logger.Debug("wiki orphan reconciler: notify dropped (queue full)", "reason", reason)
	}
}

// Reconcile is the synchronous primitive. A mutex serialises concurrent
// callers so Qdrant scroll + delete operations never overlap.
func (r *WikiOrphanReconciler) Reconcile(ctx context.Context, reason WikiOrphanReason) WikiOrphanReport {
	r.mu.Lock()
	defer r.mu.Unlock()

	start := time.Now()
	rep := ReconcileWikiQdrantWithDisk(ctx, r.cfg.QdrantClient, r.cfg.Collection, r.cfg.DB, r.cfg.WikiDir, r.cfg.Logger)
	rep.Reason = reason
	rep.ElapsedMs = time.Since(start).Milliseconds()
	r.logReport(rep)
	return rep
}

// Run is the long-lived background goroutine. Drains the notify channel with
// debounce, reacts to wiki-directory Remove events via fsnotify, and fires
// periodic safety-net sweeps.
func (r *WikiOrphanReconciler) Run(ctx context.Context) {
	var periodic *time.Ticker
	if r.cfg.Periodic > 0 {
		periodic = time.NewTicker(r.cfg.Periodic)
		defer periodic.Stop()
	}

	watcher, werr := fsnotify.NewWatcher()
	if werr != nil {
		r.cfg.Logger.Warn("wiki orphan reconciler: fsnotify unavailable", "error", werr)
	} else {
		defer watcher.Close()
		if werr = watcher.Add(r.cfg.WikiDir); werr != nil {
			r.cfg.Logger.Warn("wiki orphan reconciler: cannot watch wiki dir", "dir", r.cfg.WikiDir, "error", werr)
		}
	}

	var fsCh <-chan fsnotify.Event
	if watcher != nil {
		fsCh = watcher.Events
	}

	for {
		var primary WikiOrphanReason
		select {
		case <-ctx.Done():
			return
		case reason := <-r.notify:
			primary = reason
		case ev, ok := <-fsCh:
			if !ok {
				fsCh = nil
				continue
			}
			if ev.Op&fsnotify.Remove == 0 {
				continue
			}
			primary = WikiOrphanReasonFSNotify
		case <-wikiOrphanTickerChan(periodic):
			primary = WikiOrphanReasonPeriodic
		}

		// Debounce: drain additional events within the window before acting.
		debounce := time.NewTimer(r.cfg.Debounce)
	drain:
		for {
			select {
			case <-ctx.Done():
				debounce.Stop()
				return
			case <-r.notify:
			case _, ok := <-fsCh:
				if !ok {
					fsCh = nil
				}
			case <-debounce.C:
				break drain
			}
		}

		r.Reconcile(ctx, primary)
	}
}

func (r *WikiOrphanReconciler) logReport(rep WikiOrphanReport) {
	if rep.Err != nil {
		r.cfg.Logger.Warn("wiki orphan reconcile", "reason", rep.Reason,
			"orphans_found", rep.OrphansFound, "error", rep.Err, "elapsed_ms", rep.ElapsedMs)
		return
	}
	if rep.OrphansFound == 0 {
		r.cfg.Logger.Debug("wiki orphan reconcile: clean", "reason", rep.Reason, "elapsed_ms", rep.ElapsedMs)
		return
	}
	r.cfg.Logger.Info("wiki orphan reconcile", "reason", rep.Reason,
		"orphans_found", rep.OrphansFound, "orphans_deleted", rep.OrphansDeleted,
		"elapsed_ms", rep.ElapsedMs)
}

// wikiOrphanTickerChan returns a nil-safe channel from an optional ticker.
func wikiOrphanTickerChan(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// ReconcileWikiQdrantWithDisk scrolls all points in the Qdrant collection,
// identifies slugs that no longer have a corresponding file on disk (orphans),
// and deletes the orphan vectors plus their FTS5 mirror rows.
//
// Hard safety floor: if orphan count > 50% of on-disk page count the function
// returns an error without deleting anything — this guards against filesystem
// mount failures masquerading as mass page deletions.
func ReconcileWikiQdrantWithDisk(ctx context.Context, qdrantClient qdrant.Client, collection string, db *sql.DB, wikiDir string, logger *slog.Logger) WikiOrphanReport {
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Scroll all doc_ids currently in Qdrant.
	docIDs, err := qdrantClient.ScrollSlugs(ctx, collection)
	if err != nil {
		return WikiOrphanReport{Err: fmt.Errorf("scroll qdrant: %w", err)}
	}

	// 2. Enumerate wiki pages present on disk.
	diskSlugs, err := listWikiSlugsOnDisk(wikiDir)
	if err != nil {
		return WikiOrphanReport{Err: fmt.Errorf("list wiki slugs on disk: %w", err)}
	}
	diskSet := make(map[string]struct{}, len(diskSlugs))
	for _, s := range diskSlugs {
		diskSet[s] = struct{}{}
	}

	// 3. Extract distinct base slugs from Qdrant (strip "graph:node:" prefix).
	qdrantSlugs := make(map[string]struct{}, len(docIDs))
	for _, docID := range docIDs {
		qdrantSlugs[strings.TrimPrefix(docID, "graph:node:")] = struct{}{}
	}

	// 4. Compute orphan set: present in Qdrant but absent from disk.
	var orphans []string
	for slug := range qdrantSlugs {
		if _, ok := diskSet[slug]; !ok {
			orphans = append(orphans, slug)
		}
	}
	if len(orphans) == 0 {
		return WikiOrphanReport{}
	}

	// 5. Hard safety floor: abort if orphans > 50% of on-disk page count.
	if len(diskSlugs) > 0 && len(orphans)*2 > len(diskSlugs) {
		return WikiOrphanReport{
			OrphansFound: len(orphans),
			Err: fmt.Errorf("wiki orphan safety floor: %d orphans > 50%% of %d on-disk pages — possible filesystem mount issue; refusing to delete",
				len(orphans), len(diskSlugs)),
		}
	}

	// 6. Delete orphan Qdrant vectors (both wiki_page and graph:node kinds)
	// and the FTS5 mirror row.
	deleted := 0
	for _, slug := range orphans {
		pointIDs := []string{
			qdrantPointID(slug),
			qdrantPointID("graph:node:" + slug),
		}
		if delErr := qdrantClient.Delete(ctx, collection, pointIDs); delErr != nil {
			logger.Warn("wiki orphan reconcile: qdrant delete failed", "slug", slug, "error", delErr)
			continue
		}
		if db != nil {
			if _, dbErr := db.ExecContext(ctx, "DELETE FROM wiki_documents WHERE id = ?", slug); dbErr != nil {
				logger.Warn("wiki orphan reconcile: fts5 delete failed", "slug", slug, "error", dbErr)
			}
		}
		logger.Info("wiki orphan reconcile: deleted orphan", "slug", slug)
		deleted++
	}

	return WikiOrphanReport{OrphansFound: len(orphans), OrphansDeleted: deleted}
}

// listWikiSlugsOnDisk enumerates content page slugs present in wikiDir.
// Only .md and .yaml files are considered; operational slugs (index/log/schema)
// and sub-directories are skipped. When both .md and .yaml exist for the same
// slug the slug is counted once.
func listWikiSlugsOnDisk(wikiDir string) ([]string, error) {
	entries, err := os.ReadDir(wikiDir)
	if err != nil {
		return nil, fmt.Errorf("reading wiki directory %s: %w", wikiDir, err)
	}
	slugSet := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		var slug string
		switch {
		case strings.HasSuffix(name, ".md"):
			slug = strings.TrimSuffix(name, ".md")
		case strings.HasSuffix(name, ".yaml"):
			slug = strings.TrimSuffix(name, ".yaml")
		default:
			continue
		}
		if wiki.IsOperationalSlug(slug) {
			continue
		}
		slugSet[slug] = struct{}{}
	}
	slugs := make([]string, 0, len(slugSet))
	for s := range slugSet {
		slugs = append(slugs, s)
	}
	return slugs, nil
}
