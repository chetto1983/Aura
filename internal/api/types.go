// Package api exposes a read-only JSON HTTP surface over the wiki, source,
// and scheduler stores. Mounted under /api/ on the existing health server,
// the routes are the data contract the dashboard frontend (slice 10b) talks
// to.
//
// DTOs in this file are deliberately separate from the internal models so
// that internal field renames don't break the frontend. Times are normalized
// to RFC3339 UTC at the boundary; missing/optional fields use omitempty.
package api

import "time"

// HealthRollup is the response body of GET /health. It aggregates per-
// subsystem state in one round-trip so the dashboard can render the home
// page from a single fetch.
type HealthRollup struct {
	Process   ProcessHealth   `json:"process"`
	Wiki      WikiHealth      `json:"wiki"`
	Sources   SourcesHealth   `json:"sources"`
	Tasks     TasksHealth     `json:"tasks"`
	Scheduler SchedulerHealth `json:"scheduler"`
	// Slice 11j: embedding cache hit/miss counters. Zero when no
	// cache is wired (no EMBEDDING_API_KEY or no DB_PATH).
	EmbedCache EmbedCacheHealth `json:"embed_cache"`
	// Slice 12i: organic wiki growth driven by the auto-summarizer.
	CompoundingRate CompoundingRate `json:"compounding_rate"`
}

// EmbedCacheHealth reports SHA-keyed embedding cache stats. Hits are
// reads that skipped the upstream Mistral round trip; misses are
// reads that fell through and called Mistral.
type EmbedCacheHealth struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
}

// CompoundingRate measures organic wiki growth driven by the auto-summarizer.
// AutoAdded7d is the number of auto-sum log entries in the last 7 days.
// RatePct = AutoAdded7d / TotalPages * 100 (0 when TotalPages == 0).
type CompoundingRate struct {
	AutoAdded7d int     `json:"auto_added_7d"`
	TotalPages  int     `json:"total_pages"`
	RatePct     float64 `json:"rate_pct"`
}

// ProcessHealth surfaces process-level metadata that the dashboard footer
// can show (version + git rev + uptime). All fields are best-effort —
// GitRevision is populated from runtime/debug.ReadBuildInfo when the
// binary was built inside a git tree, otherwise empty.
type ProcessHealth struct {
	Version       string    `json:"version"`
	GitRevision   string    `json:"git_revision,omitempty"`
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
type GraphNode struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Category string `json:"category,omitempty"`
}

// GraphEdge is one directed link in GET /wiki/graph. Type is one of
// "wikilink" (a [[slug]] inside the body) or "related" (frontmatter
// related: [...]).
type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type"`
}

// Graph is the response of GET /wiki/graph.
type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
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

// Task is the response shape for /tasks endpoints.
type Task struct {
	Name                 string     `json:"name"`
	Kind                 string     `json:"kind"`
	Payload              string     `json:"payload,omitempty"`
	RecipientID          string     `json:"recipient_id,omitempty"`
	ScheduleKind         string     `json:"schedule_kind"`
	ScheduleAt           *time.Time `json:"schedule_at,omitempty"`
	ScheduleDaily        string     `json:"schedule_daily,omitempty"`
	ScheduleEveryMinutes int        `json:"schedule_every_minutes,omitempty"`
	NextRunAt            time.Time  `json:"next_run_at"`
	LastRunAt            *time.Time `json:"last_run_at,omitempty"`
	LastError            string     `json:"last_error,omitempty"`
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
	ID            int64    `json:"id"`
	ChatID        int64    `json:"chat_id"`
	Fact          string   `json:"fact"`
	Action        string   `json:"action"`
	TargetSlug    string   `json:"target_slug,omitempty"`
	Similarity    float64  `json:"similarity"`
	SourceTurnIDs []int64  `json:"source_turn_ids"`
	Category      string   `json:"category,omitempty"`
	RelatedSlugs  []string `json:"related_slugs"`
	Status        string   `json:"status"`
	CreatedAt     string   `json:"created_at"`
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
