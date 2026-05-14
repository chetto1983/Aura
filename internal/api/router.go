package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/aura/aura/internal/api/auth"
	"github.com/aura/aura/internal/backup"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/storage/sources/ingest"
	"github.com/aura/aura/internal/storage/sources/markitdown"
	"github.com/aura/aura/internal/mcp"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/sources/ocr"
	"github.com/aura/aura/internal/storage/reindex"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/agent/tools/index"
	"github.com/aura/aura/internal/wiki"
)

// BackupService is the dashboard boundary for Garage exports. Production
// builds this from RuntimeConfig; tests can pass a fake so no S3 server is
// needed.
type BackupService interface {
	ListObjects(ctx context.Context) ([]backup.Object, error)
	ExportArtifactSet(ctx context.Context, now time.Time) (backup.ArtifactSetResult, error)
}

type CompactMemoryHealthReader interface {
	Snapshot() memoryindex.VectorHealth
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
	Wiki        wiki.Repository
	Sources     source.Repository
	Scheduler   scheduler.Repository
	OCR         *ocr.Client
	Ingest      *ingest.Pipeline
	Markitdown  markitdown.Converter
	Auth        auth.DashboardRepository
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
	MCP    []mcp.ConnectedClient

	// Slice 11c: skills.sh catalog + admin-gated install/delete.
	// SkillsCatalog is the same client the LLM-facing search tool uses;
	// SkillsInstaller and SkillsDeleter wrap the filesystem mutation
	// boundary so tests can swap fakes; SkillsAdmin gates both write
	// endpoints (read endpoints, including the catalog passthrough,
	// remain available regardless).
	SkillsCatalog   *skills.CatalogClient
	SkillsInstaller SkillInstaller
	SkillsDeleter   SkillDeleter
	SkillProposals  SkillProposalApplier

	// Slice 11j: embedding cache for /health stats. Optional — nil
	// when EMBEDDING_API_KEY or DB_PATH is unset, in which case the
	// EmbeddingCache health block stays zero.
	EmbedCache    search.EmbedCacheStatsReader
	CompactMemory CompactMemoryHealthReader
	Sandbox       SandboxHealth

	// ToolReconciler — Wave 2.10.b. Backs POST /api/tools/reindex and is
	// notified on skill install/delete success. Optional: when nil, the
	// reindex endpoint returns 503 and skill admin handlers run without
	// firing reconcile (next periodic safety-net catches the change).
	ToolReconciler *toolindex.Reconciler

	// SourcePurger purges memoryindex rows (and their Qdrant mirror) for a
	// given source_id. Wired alongside Sources so DELETE /sources/{id} can
	// perform a full file+index cleanup. Optional: when nil, the handler
	// deletes only the on-disk files and warns the operator that the index
	// is now stale until the next rebuild.
	SourcePurger SourcePurger

	// File-manager roots for /files/{root}/... endpoints. Empty values
	// disable the matching root (the endpoint returns a "root not
	// configured" error). WikiDir backs both wiki and sources (sources is
	// resolved as WikiDir/raw, where Aura keeps ingested PDFs).
	WikiDir      string
	WorkspaceDir string
	SkillsDir    string

	// WikiSearch reindexes a wiki page after the dashboard writes,
	// renames, or deletes its .md file. Optional: when nil, files writes
	// still succeed but the LLM's vector index stays stale until the next
	// rebuild.
	WikiSearch WikiReindexer

	SkillsAdmin bool

	// Pending-approval pipeline. Bot wires the real implementation;
	// when nil, the approve/deny endpoints respond 503 — the GET list
	// stays operable since it only needs deps.Auth.
	PendingApprover PendingApprover

	// Slice 12c: conversation archive. Optional — when nil, list returns
	// an empty array and detail returns 404.
	Archive conversation.ArchiveRepository

	// Slice 12k.1: summaries review queue. Optional — when nil, list returns
	// empty array. SummariesWiki is the WikiWriter used to apply approved
	// decisions; when nil, approve still flips status but skips wiki mutation.
	Summaries     summarizer.ProposalReviewRepository
	SummariesWiki summarizer.WikiWriter

	// Slice 12l.1: wiki maintenance issue queue. Optional — when nil, list
	// returns empty array and resolve returns 404.
	Issues scheduler.IssueRepository

	// Slice 14d: runtime settings store. Backs GET /settings (list
	// current values) and POST /settings (bulk upsert) so the dashboard
	// can edit operator-tunable config without a restart. Optional —
	// when nil, the endpoints return 503.
	Settings             config.Repository
	RuntimeConfig        *config.Config
	ApplyRuntimeSettings func(context.Context) error
	Restart              func(context.Context) error

	// Slice 17d: AuraBot swarm observability. Optional â€” when nil, the
	// dashboard returns empty run lists and 404s for details.
	Swarm swarm.Reader

	// Garage artifact vault. Optional; when nil, handlers build a manager
	// from RuntimeConfig so settings changes are picked up without restart.
	Backups BackupService

	// Phase 2 D-16: reindex worker health callback. Optional — nil yields
	// the zero-value reindex response so health JSON is always present.
	// WARNING 12 of 2026-05-10 plan revision (closed without Phase 3 deferral).
	ReindexHealth func() reindex.Health

	// Chat is the in-process chat pipe used by cmd/chat. Optional — when
	// nil, POST /chat responds 503. cmd/aura wires this against an
	// agent.Runner that shares the bot's live LLM client and tool registry.
	Chat ChatService
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

	// Chat pipe for cmd/chat. Bearer-gated like everything else; the CLI
	// reads its token from AURA_CHAT_TOKEN. Returns 503 when deps.Chat is
	// nil (test fixtures, or operator opted out).
	mux.HandleFunc("POST /chat", handleChat(deps))

	mux.HandleFunc("GET /wiki/pages", handleWikiPages(deps))
	mux.HandleFunc("GET /wiki/page", handleWikiPage(deps))
	mux.HandleFunc("GET /wiki/graph", handleWikiGraph(deps))

	mux.HandleFunc("GET /sources", handleSourceList(deps))
	mux.HandleFunc("GET /sources/{id}", handleSourceGet(deps))
	mux.HandleFunc("GET /sources/{id}/ocr", handleSourceOCR(deps))
	mux.HandleFunc("GET /sources/{id}/markdown", handleSourceMarkdown(deps))
	mux.HandleFunc("GET /sources/{id}/raw", handleSourceRaw(deps))

	// Browser PDF upload — same write surface as Telegram. Auth-gated by
	// the outer middleware below; the original requireLoopback gate from
	// 10c.1 was retired when bearer auth landed.
	mux.HandleFunc("POST /sources/upload", handleSourceUpload(deps))

	// Slice 10c: write endpoints, also auth-gated.
	mux.HandleFunc("POST /sources/{id}/ingest", handleSourceIngest(deps))
	mux.HandleFunc("POST /sources/{id}/reocr", handleSourceReocr(deps))
	mux.HandleFunc("DELETE /sources/{id}", handleSourceDelete(deps))

	// Multi-root file manager — wiki, sources, workspace, skills.
	mux.HandleFunc("GET /files/{root}/tree", handleFilesList(deps))
	mux.HandleFunc("GET /files/{root}/file", handleFilesRead(deps))
	mux.HandleFunc("PUT /files/{root}/file", handleFilesWrite(deps))
	mux.HandleFunc("DELETE /files/{root}/file", handleFilesDelete(deps))
	mux.HandleFunc("POST /files/{root}/mkdir", handleFilesMkdir(deps))
	mux.HandleFunc("POST /files/{root}/rename", handleFilesRename(deps))
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
	mux.HandleFunc("GET /mcp/providers", handleMCPProviders(deps))
	mux.HandleFunc("GET /mcp/setup/mail", handleMCPMailSetupStatus(deps))
	mux.HandleFunc("POST /mcp/setup/mail", handleMCPMailSetupSave(deps))
	mux.HandleFunc("GET /mcp/setup/database", handleMCPDatabaseSetupStatus(deps))
	mux.HandleFunc("POST /mcp/setup/database", handleMCPDatabaseSetupSave(deps))
	mux.HandleFunc("POST /mcp/providers/{id}/actions/probe", handleMCPProviderProbe(deps))
	mux.HandleFunc("POST /mcp/providers/{id}/mail/search", handleMCPProviderMailSearch(deps))
	mux.HandleFunc("POST /mcp/providers/{id}/mail/read", handleMCPProviderMailRead(deps))

	// Slice 11c: skills.sh catalog passthrough + admin-gated install/delete.
	mux.HandleFunc("GET /skills/catalog", handleSkillsCatalog(deps))
	mux.HandleFunc("POST /skills/install", handleSkillInstall(deps))
	mux.HandleFunc("POST /skills/{name}/delete", handleSkillDelete(deps))

	// Slice 11d: invoke an MCP tool from the dashboard. Bearer auth is
	// the gate — operators trust the servers they wired into mcp.json
	// since the LLM can already call them, so no extra admin flag.
	mux.HandleFunc("POST /mcp/{server}/tools/{tool}", handleMCPInvoke(deps))

	// Wave 2.10.b — manual tool-index reindex. Synchronous; returns the
	// reconcile Report as JSON. Bearer-gated like everything else.
	mux.HandleFunc("POST /tools/reindex", handleToolsReindex(deps))

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
	mux.HandleFunc("POST /restart", handleRestart(deps))

	// Garage backup/artifact vault.
	mux.HandleFunc("GET /backups", handleBackupList(deps))
	mux.HandleFunc("POST /backups/export", handleBackupExport(deps))

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
