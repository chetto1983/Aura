package api

import (
	"time"

	"github.com/aura/aura/internal/wiki"
)

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

// TaskCounts holds per-status counts for a swarm run.
type TaskCounts struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Running   int `json:"running"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// RunMetrics aggregates LLM and token usage for a swarm run.
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
