package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/aura/aura/internal/auth"
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
	Auth        *auth.Store
	Allowlist   auth.AllowlistFunc
	MaxUploadMB int // upper bound enforced by /sources/upload; 0 means use default 100
	Location    *time.Location
	Logger      *slog.Logger

	// Slice 10e: process metadata for /health. Version is the human label
	// (e.g. "3.0"); StartedAt is captured at bot startup so /health can
	// report uptime. Both are optional — empty/zero values just elide the
	// fields from the JSON response.
	Version   string
	StartedAt time.Time
}

// NewRouter returns the API as an http.Handler. Routes do not include
// the /api prefix — callers should mount via http.StripPrefix so the
// package stays mount-agnostic and tests can hit `/health` directly.
//
// When deps.Auth is non-nil the entire mux is wrapped in RequireBearer.
// No /api/* route is publicly reachable; tokens are minted out-of-band
// via the request_dashboard_token LLM tool and delivered through the
// existing Telegram channel. When deps.Auth is nil (test fixtures) the
// router is unwrapped so test cases don't have to mint a token to drive
// the read endpoints.
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

	// Browser PDF upload — same write surface as Telegram. Auth-gated by
	// the outer middleware below; the original requireLoopback gate from
	// 10c.1 was retired when bearer auth landed.
	mux.HandleFunc("POST /sources/upload", handleSourceUpload(deps))

	// Slice 10c: write endpoints, also auth-gated.
	mux.HandleFunc("POST /sources/{id}/ingest", handleSourceIngest(deps))
	mux.HandleFunc("POST /sources/{id}/reocr", handleSourceReocr(deps))
	mux.HandleFunc("POST /wiki/index/rebuild", handleWikiRebuild(deps))
	mux.HandleFunc("POST /wiki/log", handleWikiAppendLog(deps))
	mux.HandleFunc("POST /tasks", handleTaskUpsert(deps))
	mux.HandleFunc("POST /tasks/{name}/cancel", handleTaskCancel(deps))

	mux.HandleFunc("GET /tasks", handleTaskList(deps))
	mux.HandleFunc("GET /tasks/{name}", handleTaskGet(deps))

	// Slice 10d: auth endpoints. Both authed — there's intentionally no
	// public /auth/login route. Tokens enter the dashboard through the
	// Telegram bot's request_dashboard_token tool, where the user is
	// already authenticated.
	mux.HandleFunc("GET /auth/whoami", handleAuthWhoami(deps))
	mux.HandleFunc("POST /auth/logout", handleAuthLogout(deps))

	if deps.Auth != nil {
		return auth.RequireBearer(deps.Auth, deps.Allowlist, deps.Logger, mux)
	}
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
