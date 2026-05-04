package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/aura/aura/internal/auth"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/ingest"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/ocr"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/settings"
	"github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/swarm"
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

// SchedulerStore is the surface for scheduler.Store. Upsert/Cancel/Delete
// are used by the write endpoints (POST /tasks, /tasks/{name}/cancel,
// /tasks/{name}/delete); they live in the same interface so Deps wiring
// stays a single field.
type SchedulerStore interface {
	List(ctx context.Context, statusFilter scheduler.Status) ([]*scheduler.Task, error)
	GetByName(ctx context.Context, name string) (*scheduler.Task, error)
	Upsert(ctx context.Context, t *scheduler.Task) (*scheduler.Task, error)
	Cancel(ctx context.Context, name string) (bool, error)
	Delete(ctx context.Context, name string) error
}

// SwarmStore is the read-side surface for AuraBot run/task observability.
type SwarmStore interface {
	ListRuns(ctx context.Context, limit int) ([]swarm.Run, error)
	GetRun(ctx context.Context, id string) (*swarm.Run, error)
	ListTasks(ctx context.Context, runID string) ([]swarm.Task, error)
	GetTask(ctx context.Context, id string) (*swarm.Task, error)
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

	// Slice 11b: skills + MCP read surfaces. Both optional — when nil,
	// the corresponding endpoints return empty lists (skills) or 404
	// (skill detail). Bot wiring populates them when the loader and the
	// MCP-server snapshot are available.
	Skills *skills.Loader
	MCP    []*mcp.Client

	// Slice 11c: skills.sh catalog + admin-gated install/delete.
	// SkillsCatalog is the same client the LLM-facing search tool uses;
	// SkillsInstaller and SkillsDeleter wrap the filesystem mutation
	// boundary so tests can swap fakes; SkillsAdmin gates both write
	// endpoints (read endpoints, including the catalog passthrough,
	// remain available regardless).
	SkillsCatalog   *skills.CatalogClient
	SkillsInstaller SkillInstaller
	SkillsDeleter   SkillDeleter

	// Slice 11j: embedding cache for /health stats. Optional — nil
	// when EMBEDDING_API_KEY or DB_PATH is unset, in which case the
	// EmbeddingCache health block stays zero.
	EmbedCache *search.EmbedCache
	Sandbox    SandboxHealth

	SkillsAdmin bool

	// Pending-approval pipeline. Bot wires the real implementation;
	// when nil, the approve/deny endpoints respond 503 — the GET list
	// stays operable since it only needs deps.Auth.
	PendingApprover PendingApprover

	// Slice 12c: conversation archive. Optional — when nil, list returns
	// an empty array and detail returns 404.
	Archive *conversation.ArchiveStore

	// Slice 12k.1: summaries review queue. Optional — when nil, list returns
	// empty array. SummariesWiki is the WikiWriter used to apply approved
	// decisions; when nil, approve still flips status but skips wiki mutation.
	Summaries     *summarizer.SummariesStore
	SummariesWiki summarizer.WikiWriter

	// Slice 12l.1: wiki maintenance issue queue. Optional — when nil, list
	// returns empty array and resolve returns 404.
	Issues *scheduler.IssuesStore

	// Slice 14d: runtime settings store. Backs GET /settings (list
	// current values) and POST /settings (bulk upsert) so the dashboard
	// can edit operator-tunable config without a restart. Optional —
	// when nil, the endpoints return 503.
	Settings             *settings.Store
	RuntimeConfig        *config.Config
	ApplyRuntimeSettings func(context.Context) error

	// Slice 17d: AuraBot swarm observability. Optional â€” when nil, the
	// dashboard returns empty run lists and 404s for details.
	Swarm SwarmStore
}

// installTimeout caps how long a single skills install (npx skills add)
// can run. npm cold cache + github clone fits comfortably under this.
const installTimeout = 90 * time.Second

// NewRouter returns the API as an http.Handler. Routes do not include
// the /api prefix — callers should mount via http.StripPrefix so the
// package stays mount-agnostic and tests can hit `/health` directly.
//
// When deps.Auth is non-nil the entire mux is wrapped in RequireBearer.
// No /api/* route is publicly reachable; tokens are minted out-of-band
// via the Telegram /start or /login commands, or by the LLM-backed
// request_dashboard_token tool. When deps.Auth is nil (test fixtures) the
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
	mux.HandleFunc("POST /tasks/{name}/delete", handleTaskDelete(deps))

	mux.HandleFunc("GET /tasks", handleTaskList(deps))
	mux.HandleFunc("GET /tasks/{name}", handleTaskGet(deps))

	// Slice 11b: skills + MCP read surfaces.
	mux.HandleFunc("GET /skills", handleSkillsList(deps))
	mux.HandleFunc("GET /skills/{name}", handleSkillGet(deps))
	mux.HandleFunc("GET /mcp/servers", handleMCPServers(deps))

	// Slice 11c: skills.sh catalog passthrough + admin-gated install/delete.
	mux.HandleFunc("GET /skills/catalog", handleSkillsCatalog(deps))
	mux.HandleFunc("POST /skills/install", handleSkillInstall(deps))
	mux.HandleFunc("POST /skills/{name}/delete", handleSkillDelete(deps))

	// Slice 11d: invoke an MCP tool from the dashboard. Bearer auth is
	// the gate — operators trust the servers they wired into mcp.json
	// since the LLM can already call them, so no extra admin flag.
	mux.HandleFunc("POST /mcp/{server}/tools/{tool}", handleMCPInvoke(deps))

	// Slice 10d: auth endpoints. Both authed — there's intentionally no
	// public /auth/login route. Tokens enter the dashboard through the
	// Telegram bot, where the user is already authenticated.
	mux.HandleFunc("GET /auth/whoami", handleAuthWhoami(deps))
	mux.HandleFunc("POST /auth/logout", handleAuthLogout(deps))

	// Pending access requests. Sits behind the same bearer middleware as
	// the rest of the dashboard — only an already-allowlisted user can
	// list/approve/deny strangers, which is the inversion of the old TOFU
	// bootstrap behavior.
	mux.HandleFunc("GET /pending-users", handlePendingList(deps))
	mux.HandleFunc("POST /pending-users/{id}/approve", handlePendingApprove(deps))
	mux.HandleFunc("POST /pending-users/{id}/deny", handlePendingDeny(deps))

	// Slice 12c: conversation archive endpoints.
	mux.HandleFunc("GET /conversations", handleConversationList(deps))
	mux.HandleFunc("GET /conversations/{id}", handleConversationDetail(deps))
	// Slice 14: retention controls — stats + scoped cleanup.
	mux.HandleFunc("GET /conversations/stats", handleConversationStats(deps))
	mux.HandleFunc("POST /conversations/cleanup", handleConversationCleanup(deps))

	// Slice 12k.1: summaries review queue.
	mux.HandleFunc("GET /summaries", handleSummariesList(deps))
	mux.HandleFunc("POST /summaries/batch/approve", handleSummariesBatchApprove(deps))
	mux.HandleFunc("POST /summaries/batch/reject", handleSummariesBatchReject(deps))
	mux.HandleFunc("POST /summaries/{id}/approve", handleSummariesApprove(deps))
	mux.HandleFunc("POST /summaries/{id}/reject", handleSummariesReject(deps))

	// Slice 12l.1: wiki maintenance issue queue.
	mux.HandleFunc("GET /maintenance/issues", handleMaintenanceList(deps))
	mux.HandleFunc("POST /maintenance/issues/{id}/resolve", handleMaintenanceResolve(deps))

	// Slice 14d: runtime settings page.
	mux.HandleFunc("GET /settings", handleSettingsList(deps))
	mux.HandleFunc("POST /settings", handleSettingsUpdate(deps))
	mux.HandleFunc("POST /settings/test", handleSettingsTest(deps))

	// Slice 17d: AuraBot swarm observability.
	mux.HandleFunc("GET /swarm/runs", handleSwarmRunList(deps))
	mux.HandleFunc("GET /swarm/runs/{id}", handleSwarmRunGet(deps))
	mux.HandleFunc("GET /swarm/tasks/{id}", handleSwarmTaskGet(deps))

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
var swarmRunIDRe = regexp.MustCompile(`^swarm_[a-f0-9]{16}$`)
var swarmTaskIDRe = regexp.MustCompile(`^task_[a-f0-9]{16}$`)

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
