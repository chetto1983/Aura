package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/aura/aura/internal/ingest"
	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/wiki"
)

// WikiStore is the read-side surface the API needs. The package depends on
// the interface (not the concrete type) so tests can swap in fakes if they
// later prove cheaper than spinning up a real wiki dir on tmpfs.
type WikiStore interface {
	ReadPage(slug string) (*wiki.Page, error)
	ListPages() ([]string, error)
	Dir() string // for last-update mtime walks
}

// SourceStore is the read-side surface for source.Store. The two write
// methods (Put + Update) are used only by the upload endpoint; they're
// included in the same interface to keep Deps wiring simple.
type SourceStore interface {
	Get(id string) (*source.Source, error)
	List(filter source.ListFilter) ([]*source.Source, error)
	Path(id, name string) string
	Put(ctx context.Context, in source.PutInput) (*source.Source, bool, error)
	Update(id string, mutator func(*source.Source) error) (*source.Source, error)
}

// SchedulerStore is the surface for scheduler.Store. Upsert/Cancel are used
// only by the write endpoints (POST /tasks, POST /tasks/{name}/cancel); they
// live in the same interface so Deps wiring stays a single field.
type SchedulerStore interface {
	List(ctx context.Context, statusFilter scheduler.Status) ([]*scheduler.Task, error)
	GetByName(ctx context.Context, name string) (*scheduler.Task, error)
	Upsert(ctx context.Context, t *scheduler.Task) (*scheduler.Task, error)
	Cancel(ctx context.Context, name string) (bool, error)
}

// Deps is the set of stores the router handlers operate on.
//
// OCR and Ingest are optional — when nil, the upload endpoint accepts the
// file but stops at "stored" status. Bot.New populates them when
// MISTRAL_API_KEY is configured.
//
// Location is used by POST /tasks to resolve daily HH:MM into the next UTC
// run. Nil means time.Local — matching the LLM-facing schedule_task tool.
type Deps struct {
	Wiki        WikiStore
	Sources     SourceStore
	Scheduler   SchedulerStore
	OCR         *ocr.Client
	Ingest      *ingest.Pipeline
	MaxUploadMB int // upper bound enforced by /sources/upload; 0 means use default 100
	Location    *time.Location
	Logger      *slog.Logger
}

// NewRouter returns the read-only API as an http.Handler. Routes do not
// include the /api prefix — callers should mount via http.StripPrefix so
// the package stays mount-agnostic and tests can hit `/health` directly.
func NewRouter(deps Deps) http.Handler {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", handleHealth(deps))

	mux.HandleFunc("GET /wiki/pages", handleWikiPages(deps))
	mux.HandleFunc("GET /wiki/page", handleWikiPage(deps))
	mux.HandleFunc("GET /wiki/graph", handleWikiGraph(deps))

	mux.HandleFunc("GET /sources", handleSourceList(deps))
	mux.HandleFunc("GET /sources/{id}", handleSourceGet(deps))
	mux.HandleFunc("GET /sources/{id}/ocr", handleSourceOCR(deps))
	mux.HandleFunc("GET /sources/{id}/raw", handleSourceRaw(deps))

	// Slice 10b mini: browser PDF upload. Loopback-gated until 10d ships
	// bearer auth, so a LAN-exposed listener (HTTP_PORT=:8080 etc) can't
	// accept writes from other devices.
	mux.Handle("POST /sources/upload", requireLoopback(deps.Logger, handleSourceUpload(deps)))

	// Slice 10c: write endpoints. All loopback-gated for the same reason as
	// /sources/upload. The dashboard uses these for ingest/reocr/cancel/
	// rebuild/log/upsert actions.
	mux.Handle("POST /sources/{id}/ingest", requireLoopback(deps.Logger, handleSourceIngest(deps)))
	mux.Handle("POST /sources/{id}/reocr", requireLoopback(deps.Logger, handleSourceReocr(deps)))
	mux.Handle("POST /wiki/index/rebuild", requireLoopback(deps.Logger, handleWikiRebuild(deps)))
	mux.Handle("POST /wiki/log", requireLoopback(deps.Logger, handleWikiAppendLog(deps)))
	mux.Handle("POST /tasks", requireLoopback(deps.Logger, handleTaskUpsert(deps)))
	mux.Handle("POST /tasks/{name}/cancel", requireLoopback(deps.Logger, handleTaskCancel(deps)))

	mux.HandleFunc("GET /tasks", handleTaskList(deps))
	mux.HandleFunc("GET /tasks/{name}", handleTaskGet(deps))

	return mux
}

// sourceIDRe mirrors the validation in internal/source so we never let an
// untrusted path segment through to filesystem joins.
var sourceIDRe = regexp.MustCompile(`^src_[a-f0-9]{16}$`)

// taskNameRe restricts to a conservative shell-safe character set so a
// malicious name in the URL can't break out of the path or a log line.
var taskNameRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,64}$`)

// writeJSON serializes v as JSON with the given status code. Errors during
// encoding are logged but not surfaced — the response is already partially
// flushed by then.
func writeJSON(w http.ResponseWriter, logger *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil && logger != nil {
		logger.Warn("api: encode response", "error", err)
	}
}

// writeError emits a JSON error body at the given status code.
func writeError(w http.ResponseWriter, logger *slog.Logger, status int, msg string) {
	writeJSON(w, logger, status, ErrorResponse{Error: msg})
}
