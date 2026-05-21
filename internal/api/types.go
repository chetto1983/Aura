// Package api exposes a read-only JSON HTTP surface over the wiki, source,
// and scheduler stores. Mounted under /api/ on the existing health server,
// the routes are the data contract the dashboard frontend (slice 10b) talks
// to.
//
// DTOs in this file are deliberately separate from the internal models so
// that internal field renames don't break the frontend. Times are normalized
// to RFC3339 UTC at the boundary; missing/optional fields use omitempty.
package api

import (
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/reindex"
	"github.com/aura/aura/internal/wiki"
)

// ReindexHealthResponse mirrors reindex.Health for the /api/health JSON
// contract. Surfaces D-16 worker telemetry to the dashboard. WARNING 12
// of 2026-05-10 plan revision wires this in (NOT deferred to Phase 3).
type ReindexHealthResponse struct {
	QueueDepth       int       `json:"queue_depth"`
	Dropped          int64     `json:"dropped"`
	DroppedAfterStop int64     `json:"dropped_after_stop"`
	LastSuccess      time.Time `json:"last_success"`
	LastError        string    `json:"last_error,omitempty"`
}

// reindexHealthFromHealth converts a reindex.Health snapshot to a
// ReindexHealthResponse suitable for JSON serialization.
func reindexHealthFromHealth(h reindex.Health) ReindexHealthResponse {
	return ReindexHealthResponse{
		QueueDepth:       h.QueueDepth,
		Dropped:          h.Dropped,
		DroppedAfterStop: h.DroppedAfterStop,
		LastSuccess:      h.LastSuccess,
		LastError:        h.LastError,
	}
}

// HealthRollup is the response body of GET /health. It aggregates per-
// subsystem state in one round-trip so the dashboard can render the home
// page from a single fetch.
type HealthRollup struct {
	Process   ProcessHealth   `json:"process"`
	Wiki      WikiHealth      `json:"wiki"`
	Sources   SourcesHealth   `json:"sources"`
	Tasks     TasksHealth     `json:"tasks"`
	Scheduler SchedulerHealth `json:"scheduler"`
	Sandbox   SandboxHealth   `json:"sandbox"`
	// Slice 11j: embedding cache hit/miss counters. Zero when no
	// cache is wired (no EMBEDDING_API_KEY or no DB_PATH).
	EmbedCache    EmbedCacheHealth         `json:"embed_cache"`
	CompactMemory memoryindex.VectorHealth `json:"compact_memory"`
	// Phase 2 D-16: reindex worker operational telemetry. Always present
	// (zero value when worker is nil) so TypeScript strict types stay stable.
	Reindex ReindexHealthResponse `json:"reindex"`
	// Phase-TJ: rule-driven output compaction stats. Always present so
	// TypeScript strict types stay stable; fields are zero when no compaction
	// has occurred yet or when the feature flag is off.
	TokenJuice TokenJuiceHealth `json:"tokenjuice"`
}

// TokenJuiceHealth surfaces process-level rule-driven output compaction stats.
type TokenJuiceHealth struct {
	Enabled         bool         `json:"enabled"`
	TotalCalls      int64        `json:"total_calls"`
	TotalBytesSaved int64        `json:"total_bytes_saved"`
	AvgRatio        float64      `json:"avg_ratio"`
	TopRules        []RuleSaving `json:"top_rules_by_savings"`
}

// RuleSaving pairs a rule ID with total bytes it saved since process start.
type RuleSaving struct {
	RuleID     string `json:"rule_id"`
	BytesSaved int64  `json:"bytes_saved"`
}

// SandboxHealth reports whether local code execution is active. The desired
// end-user path is a bundled runtime shipped with Aura; Runtime is kept
// best-effort for operator diagnostics.
type SandboxHealth struct {
	Enabled     bool   `json:"enabled"`
	Available   bool   `json:"available"`
	RuntimeKind string `json:"runtime_kind,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// EmbedCacheHealth reports SHA-keyed embedding cache stats. Hits are
// reads that skipped the upstream Mistral round trip; misses are
// reads that fell through and called Mistral.
type EmbedCacheHealth struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
}

// ProcessHealth surfaces process-level metadata that the dashboard footer
// can show (version + git rev + uptime). All fields are best-effort —
// GitRevision is populated from runtime/debug.ReadBuildInfo when the
// binary was built inside a git tree, otherwise empty.
// Commit and BuildDate are injected at link time via -ldflags (US-T02).
type ProcessHealth struct {
	Version       string    `json:"version"`
	GitRevision   string    `json:"git_revision,omitempty"`
	Commit        string    `json:"commit,omitempty"`
	BuildDate     string    `json:"build_date,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UptimeSeconds int64     `json:"uptime_seconds"`
}

// WikiHealth summarizes wiki state.
type WikiHealth struct {
	Pages      int       `json:"pages"`
	LastUpdate time.Time `json:"last_update"`
}

// SourcesHealth bundles status counts for the source inbox.
type SourcesHealth struct {
	ByStatus map[string]int `json:"by_status"`
}

// TasksHealth bundles status counts for the scheduler.
type TasksHealth struct {
	ByStatus map[string]int `json:"by_status"`
}

// SchedulerHealth surfaces the soonest pending task. NextRun is nil when no
// active task exists.
type SchedulerHealth struct {
	NextRun *time.Time `json:"next_run"`
}

// WikiPageSummary is the row shape for GET /wiki/pages.
type WikiPageSummary struct {
	Slug      string    `json:"slug"`
	Title     string    `json:"title"`
	Category  string    `json:"category,omitempty"`
	Tags      []string  `json:"tags,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// WikiPage is the response of GET /wiki/page?slug=X. The Frontmatter map is
// the rendered YAML header (excluding Body) so the frontend can show
// arbitrary metadata without coupling to the Go struct.
type WikiPage struct {
	Slug        string         `json:"slug"`
	Title       string         `json:"title"`
	BodyMD      string         `json:"body_md"`
	Frontmatter map[string]any `json:"frontmatter"`
}

// GraphNode is one vertex in GET /wiki/graph.
type GraphNode = wiki.GraphNode

// GraphEdge is one directed link in GET /wiki/graph. Type is one of
// "wikilink" (a [[slug]] inside the body) or "related" (frontmatter
// related: [...]).
type GraphEdge = wiki.GraphEdge

// Graph is the response of GET /wiki/graph.
type Graph = wiki.Graph

// WikiSearchHit is one direct wiki retrieval hit from GET /wiki/search. It is
// intentionally compact: callers get enough ranking evidence to benchmark
// Recall@K without receiving full wiki page bodies.
type WikiSearchHit struct {
	Rank        int      `json:"rank"`
	Kind        string   `json:"kind"`
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Snippet     string   `json:"snippet,omitempty"`
	Score       float32  `json:"score"`
	ScoreExact  float32  `json:"score_exact,omitempty"`
	ScoreFTS    float32  `json:"score_fts,omitempty"`
	ScoreVector float32  `json:"score_vector,omitempty"`
	FilePath    string   `json:"file_path,omitempty"`
	Category    string   `json:"category,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Sources     []string `json:"sources,omitempty"`
}

// WikiSearchResponse is the body returned by GET /wiki/search?q=...
type WikiSearchResponse struct {
	Query     string          `json:"query"`
	TopK      int             `json:"top_k"`
	Indexed   bool            `json:"indexed"`
	ElapsedMS int64           `json:"elapsed_ms"`
	Results   []WikiSearchHit `json:"results"`
}

// SourceSummary is one row of GET /sources. It omits high-volume fields
// (mime_type, sha256, size_bytes, ocr_model, error) that the table view
// doesn't need.
type SourceSummary struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Filename  string    `json:"filename"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	PageCount int       `json:"page_count,omitempty"`
	WikiPages []string  `json:"wiki_pages,omitempty"`
}

// SourceDetail is the response of GET /sources/{id}.
type SourceDetail struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type,omitempty"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	Status    string    `json:"status"`
	OCRModel  string    `json:"ocr_model,omitempty"`
	PageCount int       `json:"page_count,omitempty"`
	WikiPages []string  `json:"wiki_pages,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// SourceOCR is the response of GET /sources/{id}/ocr.
type SourceOCR struct {
	Markdown string `json:"markdown"`
}

// SourceMarkdown is the response of GET /sources/{id}/markdown. File is the
// on-disk generated markdown artifact ("ocr.md" or "extract.md").
type SourceMarkdown struct {
	Markdown string `json:"markdown"`
	File     string `json:"file"`
}

// Task is the response shape for /tasks endpoints.
type Task struct {
	Name                 string     `json:"name"`
	Kind                 string     `json:"kind"`
	Payload              string     `json:"payload,omitempty"`
	RecipientID          string     `json:"recipient_id,omitempty"`
	ScheduleKind         string     `json:"schedule_kind"`
	ScheduleAt           *time.Time `json:"schedule_at,omitempty"`
	ScheduleDaily        string     `json:"schedule_daily,omitempty"`
	ScheduleWeekdays     string     `json:"schedule_weekdays,omitempty"`
	ScheduleEveryMinutes int        `json:"schedule_every_minutes,omitempty"`
	NextRunAt            time.Time  `json:"next_run_at"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
	LastOutput           string     `json:"last_output,omitempty"`
	LastMetricsJSON      string     `json:"last_metrics_json,omitempty"`
	WakeSignature        string     `json:"wake_signature,omitempty"`
	Status               string     `json:"status"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// SkillSummary is one row of GET /skills.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SkillDetail is the response of GET /skills/{name}. Truncated is true when
// Content was clipped at maxSkillBodyChars; the dashboard shows a banner so
// the user knows the full SKILL.md is on disk.
type SkillDetail struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// MCPToolInfo is one tool advertised by an MCP server. InputSchema is the
// raw JSON Schema map returned from tools/list — not normalized so the
// dashboard can render whatever the upstream server emits.
type MCPToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema,omitempty"`
}

// MCPServerSummary is one row of GET /mcp/servers. Only servers that
// connected successfully at boot show up here; failed connections are
// warned in logs but not yet surfaced to the dashboard (deferred to 11d).
type MCPServerSummary struct {
	Name      string        `json:"name"`
	Transport string        `json:"transport"` // "stdio" or "http"
	ToolCount int           `json:"tool_count"`
	Tools     []MCPToolInfo `json:"tools"`
}

// ConnectorProviderSummary is the dashboard-facing configuration card for
// approved MCP provider profiles. It is separate from raw MCP server summaries:
// the operator configures capabilities and risk posture here, while
// /mcp/servers remains a low-level diagnostic view.
type ConnectorProviderSummary struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Kind            string                `json:"kind"`    // "mail" | "database"
	Profile         string                `json:"profile"` // "personal" | "business"
	Description     string                `json:"description"`
	Status          string                `json:"status"`       // "not_configured" | "configured" | "ready" | "enabled" | "failed"
	RuntimeType     string                `json:"runtime_type"` // "container" | "stdio" | "remote_http"
	RepositoryURL   string                `json:"repository_url,omitempty"`
	HomepageURL     string                `json:"homepage_url,omitempty"`
	MCPServerNames  []string              `json:"mcp_server_names,omitempty"`
	Capabilities    []ConnectorCapability `json:"capabilities"`
	RiskBadges      []ConnectorRiskBadge  `json:"risk_badges"`
	RequiredSecrets []string              `json:"required_secrets,omitempty"`
	ApprovedTools   []string              `json:"approved_tools,omitempty"`
	BlockedTools    []string              `json:"blocked_tools,omitempty"`
	SetupHints      []string              `json:"setup_hints,omitempty"`
}

// ConnectorCapability is one Aura-level capability exposed by a provider
// profile. Provider tool names stay behind adapters and allowlists.
type ConnectorCapability struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Description    string `json:"description,omitempty"`
	Enabled        bool   `json:"enabled"`
	ReviewRequired bool   `json:"review_required"`
}

// ConnectorRiskBadge lets the dashboard show risk without parsing prose.
type ConnectorRiskBadge struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Level string `json:"level"` // "low" | "medium" | "high"
}

// ConnectorProbeResponse reports whether a provider profile can satisfy Aura's
// approved capability contract against a currently connected MCP server.
type ConnectorProbeResponse struct {
	OK                      bool     `json:"ok"`
	ProviderID              string   `json:"provider_id"`
	ServerName              string   `json:"server_name,omitempty"`
	CapabilitiesReady       []string `json:"capabilities_ready,omitempty"`
	MissingCapabilities     []string `json:"missing_capabilities,omitempty"`
	ApprovedToolsAdvertised []string `json:"approved_tools_advertised,omitempty"`
	BlockedToolsAdvertised  []string `json:"blocked_tools_advertised,omitempty"`
	Error                   string   `json:"error,omitempty"`
}

// MailSetupStatus is the dashboard-facing state for the guided mail connector
// wizard. Secrets are deliberately omitted; ConfiguredEmail is informational.
type MailSetupStatus struct {
	Configured          bool   `json:"configured"`
	Connected           bool   `json:"connected"`
	NeedsRestart        bool   `json:"needs_restart"`
	RestartRequired     bool   `json:"restart_required"`
	CanRestart          bool   `json:"can_restart"`
	BinaryPresent       bool   `json:"binary_present"`
	Command             string `json:"command,omitempty"`
	Provider            string `json:"provider,omitempty"`
	AccountID           string `json:"account_id,omitempty"`
	Email               string `json:"email,omitempty"`
	ConfiguredEmail     string `json:"configured_email,omitempty"`
	IMAPHost            string `json:"imap_host,omitempty"`
	IMAPPort            int    `json:"imap_port,omitempty"`
	IMAPSecure          bool   `json:"imap_secure,omitempty"`
	SMTPHost            string `json:"smtp_host,omitempty"`
	SMTPPort            int    `json:"smtp_port,omitempty"`
	SMTPSecure          bool   `json:"smtp_secure,omitempty"`
	EnableSMTP          bool   `json:"enable_smtp,omitempty"`
	EnableIMAPMutations bool   `json:"enable_imap_mutations,omitempty"`
	SecretConfigured    bool   `json:"secret_configured,omitempty"`
	Error               string `json:"error,omitempty"`
}

// MailSetupRequest is the guided configuration payload for mail-mcp. The
// provider presets fill IMAP/SMTP hosts for common providers; custom IMAP can
// still override them without exposing raw mcp.json to end users.
type MailSetupRequest struct {
	Provider            string `json:"provider"` // gmail | outlook | imap
	AccountID           string `json:"account_id,omitempty"`
	Email               string `json:"email"`
	AppPassword         string `json:"app_password"`
	IMAPHost            string `json:"imap_host,omitempty"`
	IMAPPort            int    `json:"imap_port,omitempty"`
	IMAPSecure          *bool  `json:"imap_secure,omitempty"`
	EnableSMTP          bool   `json:"enable_smtp,omitempty"`
	SMTPHost            string `json:"smtp_host,omitempty"`
	SMTPPort            int    `json:"smtp_port,omitempty"`
	SMTPSecure          *bool  `json:"smtp_secure,omitempty"`
	EnableIMAPMutations bool   `json:"enable_imap_mutations,omitempty"`
}

type MailSetupResponse struct {
	OK     bool            `json:"ok"`
	Status MailSetupStatus `json:"status"`
}

// DatabaseSetupStatus is the dashboard-facing state for the guided database
// MCP wizard. Passwords are never returned; PasswordConfigured only reports
// whether the saved server args include one.
type DatabaseSetupStatus struct {
	Configured         bool   `json:"configured"`
	Connected          bool   `json:"connected"`
	NeedsRestart       bool   `json:"needs_restart"`
	RestartRequired    bool   `json:"restart_required"`
	CanRestart         bool   `json:"can_restart"`
	BinaryPresent      bool   `json:"binary_present"`
	Command            string `json:"command,omitempty"`
	Provider           string `json:"provider,omitempty"`
	SQLitePath         string `json:"sqlite_path,omitempty"`
	Host               string `json:"host,omitempty"`
	Port               int    `json:"port,omitempty"`
	Database           string `json:"database,omitempty"`
	User               string `json:"user,omitempty"`
	SSL                bool   `json:"ssl,omitempty"`
	PasswordConfigured bool   `json:"password_configured,omitempty"`
	Error              string `json:"error,omitempty"`
}

// DatabaseSetupRequest writes the single managed ExecuteAutomation database
// MCP entry. Provider is one of sqlite, postgresql, mysql, or sqlserver.
type DatabaseSetupRequest struct {
	Provider   string `json:"provider"`
	SQLitePath string `json:"sqlite_path,omitempty"`
	Host       string `json:"host,omitempty"`
	Port       int    `json:"port,omitempty"`
	Database   string `json:"database,omitempty"`
	User       string `json:"user,omitempty"`
	Password   string `json:"password,omitempty"`
	SSL        bool   `json:"ssl,omitempty"`
}

type DatabaseSetupResponse struct {
	OK     bool                `json:"ok"`
	Status DatabaseSetupStatus `json:"status"`
}

// MailMessage is Aura's provider-agnostic mail record. Provider-specific
// payloads stay behind adapters.
type MailMessage struct {
	ID       string   `json:"id"`
	ThreadID string   `json:"thread_id,omitempty"`
	Subject  string   `json:"subject,omitempty"`
	From     string   `json:"from,omitempty"`
	To       []string `json:"to,omitempty"`
	Date     string   `json:"date,omitempty"`
	Snippet  string   `json:"snippet,omitempty"`
	Body     string   `json:"body,omitempty"`
}

type MailSearchResponse struct {
	ProviderID   string        `json:"provider_id"`
	ProviderTool string        `json:"provider_tool"`
	Messages     []MailMessage `json:"messages"`
	Raw          string        `json:"raw,omitempty"`
}

type MailReadResponse struct {
	ProviderID   string      `json:"provider_id"`
	ProviderTool string      `json:"provider_tool"`
	Message      MailMessage `json:"message"`
	Raw          string      `json:"raw,omitempty"`
}

// Slice 11c — skills.sh catalog + admin-gated install/delete.

// SkillCatalogItem is one row from GET /skills/catalog (proxies skills.sh).
type SkillCatalogItem struct {
	Source         string `json:"source"`
	SkillID        string `json:"skill_id,omitempty"`
	Name           string `json:"name"`
	Installs       int    `json:"installs"`
	InstallCommand string `json:"install_command,omitempty"`
}

// SkillInstallResponse is the body of POST /skills/install.
type SkillInstallResponse struct {
	OK     bool   `json:"ok"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// SkillDeleteResponse is the body of POST /skills/{name}/delete.
type SkillDeleteResponse struct {
	OK   bool   `json:"ok"`
	Name string `json:"name"`
}

// MCPInvokeResponse is the body of POST /mcp/{server}/tools/{tool}.
// OK=true means the server returned success. OK=false means either the
// server returned isError:true (IsError=true), the request timed out,
// or the transport failed (IsError=false).
type MCPInvokeResponse struct {
	OK      bool   `json:"ok"`
	IsError bool   `json:"is_error,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ToolWarningItem is one aggregated failure row in GET /tool-warnings.
type ToolWarningItem struct {
	Kind     string `json:"kind"`
	Tool     string `json:"tool"`
	Class    string `json:"class"`
	N        int    `json:"n"`
	LastSeen string `json:"last_seen"`
}

// ToolWarningsResponse is the body of GET /tool-warnings.
type ToolWarningsResponse struct {
	Warnings []ToolWarningItem `json:"warnings"`
}

// AuthzResponse is the body of GET /maintenance/authz.
type AuthzResponse struct {
	DenialRate24h         float64           `json:"denial_rate_24h"`
	TopDeniedCapabilities []CapabilityCount `json:"top_denied_capabilities"`
	RecentDenials         []RecentDenial    `json:"recent_denials"`
}

// CapabilityCount is one entry in the top-denied list.
type CapabilityCount struct {
	Capability string `json:"capability"`
	Count      int    `json:"count"`
}

// RecentDenial is one denied authz event.
type RecentDenial struct {
	ActorID    string `json:"actor_id"`
	Capability string `json:"capability"`
	Reason     string `json:"reason"`
	CreatedAt  string `json:"created_at"`
}

// ToolAttemptsResponse is the body of GET /maintenance/tool-attempts.
type ToolAttemptsResponse struct {
	PerTool                map[string]ToolOutcomeCounts `json:"per_tool"`
	RetryBudgetConsumption []ToolBudgetRow              `json:"retry_budget_consumption"`
}

// ToolOutcomeCounts holds the 5-bucket outcome split for one tool over 24h.
type ToolOutcomeCounts struct {
	OK          int `json:"ok"`
	Recoverable int `json:"recoverable"`
	Blocked     int `json:"blocked"`
	Fatal       int `json:"fatal"`
	Cancelled   int `json:"cancelled"`
}

// ToolBudgetRow is one tool's daily attempt budget summary.
type ToolBudgetRow struct {
	ToolName      string  `json:"tool_name"`
	AttemptsToday int     `json:"attempts_today"`
	Budget        int     `json:"budget"`
	Percent       float64 `json:"percent"`
}

// ErrorResponse is the JSON body for any non-2xx response.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ConversationTurn is one row of GET /conversations.
type ConversationTurn struct {
	ID             int64  `json:"id"`
	ChatID         int64  `json:"chat_id"`
	UserID         int64  `json:"user_id"`
	TurnIndex      int64  `json:"turn_index"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	ToolCalls      string `json:"tool_calls,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	LLMCalls       int    `json:"llm_calls,omitempty"`
	ToolCallsCount int    `json:"tool_calls_count,omitempty"`
	ElapsedMS      int64  `json:"elapsed_ms,omitempty"`
	TokensIn       int    `json:"tokens_in,omitempty"`
	TokensOut      int    `json:"tokens_out,omitempty"`
	CreatedAt      string `json:"created_at"`
}

// WikiIssue is one row of GET /maintenance/issues (mirrors wiki_issues table).
type WikiIssue struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Severity   string `json:"severity"`
	Slug       string `json:"slug,omitempty"`
	BrokenLink string `json:"broken_link,omitempty"`
	Message    string `json:"message,omitempty"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
	ResolvedAt string `json:"resolved_at,omitempty"`
}

// ProposedUpdate is one row of GET /summaries (mirrors proposed_updates table).
type ProposedUpdate struct {
	ID             int64           `json:"id"`
	ChatID         int64           `json:"chat_id"`
	Fact           string          `json:"fact"`
	Action         string          `json:"action"`
	TargetSlug     string          `json:"target_slug,omitempty"`
	Similarity     float64         `json:"similarity"`
	SourceTurnIDs  []int64         `json:"source_turn_ids"`
	Category       string          `json:"category,omitempty"`
	RelatedSlugs   []string        `json:"related_slugs"`
	Provenance     Provenance      `json:"provenance,omitempty"`
	SkillLifecycle *SkillLifecycle `json:"skill_lifecycle,omitempty"`
	Status         string          `json:"status"`
	Kind           string          `json:"kind,omitempty"`
	SignatureHash  string          `json:"signature_hash,omitempty"`
	CreatedAt      string          `json:"created_at"`
}

type SummaryBatchRequest struct {
	IDs []int64 `json:"ids"`
}

type SummaryBatchFailure struct {
	ID    int64  `json:"id"`
	Error string `json:"error"`
}

type SummaryBatchResponse struct {
	Updated []ProposedUpdate      `json:"updated"`
	Failed  []SummaryBatchFailure `json:"failed"`
}

type EvidenceRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	Page    int    `json:"page,omitempty"`
	Snippet string `json:"snippet,omitempty"`
}

type Provenance struct {
	OriginTool   string         `json:"origin_tool,omitempty"`
	OriginReason string         `json:"origin_reason,omitempty"`
	ProposalKind string         `json:"proposal_kind,omitempty"`
	Evidence     []EvidenceRef  `json:"evidence,omitempty"`
	Skill        *SkillProposal `json:"skill,omitempty"`
	AgentJobID   string         `json:"agent_job_id,omitempty"`
	SwarmRunID   string         `json:"swarm_run_id,omitempty"`
	SwarmTaskID  string         `json:"swarm_task_id,omitempty"`
}

type SkillProposal struct {
	Action       string   `json:"action"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	SmokePrompt  string   `json:"smoke_prompt,omitempty"`
	Content      string   `json:"content,omitempty"`
	Reason       string   `json:"reason,omitempty"`
}

// SkillLifecycle documents the review contract for skill proposals. Reviewed
// proposals are not necessarily installed; callers should confirm availability
// through the skills list before claiming a skill exists.
type SkillLifecycle struct {
	Mode          string `json:"mode"`
	ReviewStatus  string `json:"review_status"`
	InstallStatus string `json:"install_status"`
	SmokeStatus   string `json:"smoke_status"`
	NextStep      string `json:"next_step"`
}

type BackupObject struct {
	Key          string    `json:"key"`
	Category     string    `json:"category"`
	Timestamp    string    `json:"timestamp,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
	LastModified time.Time `json:"last_modified"`
}

type BackupListResponse struct {
	Bucket  string         `json:"bucket"`
	Objects []BackupObject `json:"objects"`
}

type BackupExportObject struct {
	Category  string `json:"category"`
	Key       string `json:"key"`
	SizeBytes int64  `json:"size_bytes"`
	Files     int    `json:"files"`
}

type BackupExportResponse struct {
	Bucket    string               `json:"bucket"`
	Timestamp string               `json:"timestamp"`
	Objects   []BackupExportObject `json:"objects"`
}

// SwarmRunSummary is one row of GET /swarm/runs.
type SwarmRunSummary struct {
	ID          string     `json:"id"`
	Goal        string     `json:"goal"`
	Status      string     `json:"status"`
	CreatedBy   string     `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	TaskCounts  TaskCounts `json:"task_counts"`
	Metrics     RunMetrics `json:"metrics"`
}

// SwarmRunDetail is GET /swarm/runs/{id}.
type SwarmRunDetail struct {
	SwarmRunSummary
	Tasks []SwarmTask `json:"tasks"`
}

type TaskCounts struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

type RunMetrics struct {
	LLMCalls         int     `json:"llm_calls"`
	ToolCalls        int     `json:"tool_calls"`
	TokensPrompt     int     `json:"tokens_prompt"`
	TokensCompletion int     `json:"tokens_completion"`
	TokensTotal      int     `json:"tokens_total"`
	TaskElapsedMS    int64   `json:"task_elapsed_ms"`
	WallMS           int64   `json:"wall_ms"`
	Speedup          float64 `json:"speedup"`
}

// SwarmTask is one AuraBot worker task.
type SwarmTask struct {
	ID               string     `json:"id"`
	RunID            string     `json:"run_id"`
	ParentID         string     `json:"parent_id,omitempty"`
	Role             string     `json:"role"`
	Subject          string     `json:"subject,omitempty"`
	Status           string     `json:"status"`
	Depth            int        `json:"depth"`
	Attempts         int        `json:"attempts"`
	ToolAllowlist    []string   `json:"tool_allowlist,omitempty"`
	BlockedBy        []string   `json:"blocked_by,omitempty"`
	Result           string     `json:"result,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	LLMCalls         int        `json:"llm_calls"`
	ToolCalls        int        `json:"tool_calls"`
	TokensPrompt     int        `json:"tokens_prompt"`
	TokensCompletion int        `json:"tokens_completion"`
	TokensTotal      int        `json:"tokens_total"`
	ElapsedMS        int64      `json:"elapsed_ms"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// ConversationDetail is the response of GET /conversations/{id}. ToolCalls
// is the raw JSON string from the DB so the frontend can parse/expand it.
type ConversationDetail struct {
	ID             int64  `json:"id"`
	ChatID         int64  `json:"chat_id"`
	UserID         int64  `json:"user_id"`
	TurnIndex      int64  `json:"turn_index"`
	Role           string `json:"role"`
	Content        string `json:"content"`
	ToolCalls      string `json:"tool_calls,omitempty"`
	ToolCallID     string `json:"tool_call_id,omitempty"`
	LLMCalls       int    `json:"llm_calls,omitempty"`
	ToolCallsCount int    `json:"tool_calls_count,omitempty"`
	ElapsedMS      int64  `json:"elapsed_ms,omitempty"`
	TokensIn       int    `json:"tokens_in,omitempty"`
	TokensOut      int    `json:"tokens_out,omitempty"`
	CreatedAt      string `json:"created_at"`
}
