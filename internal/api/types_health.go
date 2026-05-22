// Package api exposes a read-only JSON HTTP surface over the wiki, source,
// and scheduler stores. Mounted under /api/ on the existing health server,
// the routes are the data contract the dashboard frontend (slice 10b) talks
// to.
//
// DTOs in this package are deliberately separate from the internal models so
// that internal field renames don't break the frontend. Times are normalized
// to RFC3339 UTC at the boundary; missing/optional fields use omitempty.
package api

import (
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/reindex"
)

// ReindexHealthResponse mirrors reindex.Health for the /api/health JSON
// contract. Surfaces D-16 worker telemetry to the dashboard.
type ReindexHealthResponse struct {
	QueueDepth       int       `json:"queue_depth"`
	Dropped          int64     `json:"dropped"`
	DroppedAfterStop int64     `json:"dropped_after_stop"`
	LastSuccess      time.Time `json:"last_success"`
	LastError        string    `json:"last_error,omitempty"`
}

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
	// Slice 11j: embedding cache hit/miss counters.
	EmbedCache    EmbedCacheHealth         `json:"embed_cache"`
	CompactMemory memoryindex.VectorHealth `json:"compact_memory"`
	// Phase 2 D-16: reindex worker operational telemetry.
	Reindex ReindexHealthResponse `json:"reindex"`
	// Phase-TJ: rule-driven output compaction stats.
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

// SandboxHealth reports whether local code execution is active.
type SandboxHealth struct {
	Enabled     bool   `json:"enabled"`
	Available   bool   `json:"available"`
	RuntimeKind string `json:"runtime_kind,omitempty"`
	Runtime     string `json:"runtime,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// EmbedCacheHealth reports SHA-keyed embedding cache stats.
type EmbedCacheHealth struct {
	Hits   uint64 `json:"hits"`
	Misses uint64 `json:"misses"`
}

// ProcessHealth surfaces process-level metadata for the dashboard footer.
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

// SchedulerHealth surfaces the soonest pending task.
type SchedulerHealth struct {
	NextRun *time.Time `json:"next_run"`
}

// ErrorResponse is the JSON body for any non-2xx response.
type ErrorResponse struct {
	Error string `json:"error"`
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
