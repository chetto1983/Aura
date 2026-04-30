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
	Wiki      WikiHealth      `json:"wiki"`
	Sources   SourcesHealth   `json:"sources"`
	Tasks     TasksHealth     `json:"tasks"`
	Scheduler SchedulerHealth `json:"scheduler"`
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
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	Payload       string     `json:"payload,omitempty"`
	RecipientID   string     `json:"recipient_id,omitempty"`
	ScheduleKind  string     `json:"schedule_kind"`
	ScheduleAt    *time.Time `json:"schedule_at,omitempty"`
	ScheduleDaily string     `json:"schedule_daily,omitempty"`
	NextRunAt     time.Time  `json:"next_run_at"`
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// ErrorResponse is the JSON body for any non-2xx response.
type ErrorResponse struct {
	Error string `json:"error"`
}
