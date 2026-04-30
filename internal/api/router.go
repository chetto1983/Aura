package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"

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

// SourceStore is the read-side surface for source.Store.
type SourceStore interface {
	Get(id string) (*source.Source, error)
	List(filter source.ListFilter) ([]*source.Source, error)
	Path(id, name string) string
}

// SchedulerStore is the read-side surface for scheduler.Store.
type SchedulerStore interface {
	List(ctx context.Context, statusFilter scheduler.Status) ([]*scheduler.Task, error)
	GetByName(ctx context.Context, name string) (*scheduler.Task, error)
}

// Deps is the set of stores the router handlers operate on.
type Deps struct {
	Wiki      WikiStore
	Sources   SourceStore
	Scheduler SchedulerStore
	Logger    *slog.Logger
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
