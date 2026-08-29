package display

// Kind is the typed-display discriminator. Each constant string is the wire
// literal the cockpit DisplayRouter switches on (web/src/chat/displays/types.ts,
// 26-02) — renaming one is a wire-contract break, never do it without amending
// both sides.
type Kind string

const (
	// KindWebResult identifies a normalized web search result list payload.
	KindWebResult Kind = "web_result"
	// KindDocument identifies a fetched or extracted markdown document payload.
	KindDocument Kind = "document"
	// KindCode identifies a shell, sandbox, or code-like text payload.
	KindCode Kind = "code"
	// KindLocalArtifact identifies a local file artifact payload.
	KindLocalArtifact Kind = "local_artifact"
	// KindTable identifies a structured table payload.
	KindTable Kind = "table"
	// KindChart identifies a chart-ready numeric payload.
	KindChart Kind = "chart"
	// KindSystemEvent identifies a sanitized system status or error payload.
	KindSystemEvent Kind = "system_event"
	// KindSwarmReport identifies a swarm child-report payload.
	KindSwarmReport Kind = "swarm_report"
)

// Payload is the flat tagged union a normalizer emits (R1). It is a struct, not an
// interface, so decode(encode) is identity and JSON omitempty keeps the wire lean:
// exactly the per-type field for Type is set, the rest omit. ToolCallID is the
// sseAdapter correlation key (the reducer attaches the payload to the matching tool
// part). Sources carries the per-turn registry (D-05) for any payload that consulted
// web sources; it powers the citation hovercard + Source Explorer downstream.
type Payload struct {
	Type       Kind          `json:"type"`
	ToolCallID string        `json:"tool_call_id"`
	Title      string        `json:"title,omitempty"`
	WebResults []WebItem     `json:"web_results,omitempty"`
	Document   *Document     `json:"document,omitempty"`
	Code       *Code         `json:"code,omitempty"`
	Artifact   *Artifact     `json:"artifact,omitempty"`
	Table      *Table        `json:"table,omitempty"`
	Chart      *Chart        `json:"chart,omitempty"`
	System     *System       `json:"system,omitempty"`
	Swarm      []ChildReport `json:"swarm,omitempty"`
	Sources    []Source      `json:"sources,omitempty"`
}

// WebItem is one web_result hit (from web.Result + ResultMetadata). Domain is the
// derived URL host; RefID ties the item to its registry Source entry (D-05). The
// metadata fields stay omitempty so a tier-1 (no-metadata) search renders clean.
type WebItem struct {
	Title       string  `json:"title"`
	URL         string  `json:"url"`
	Snippet     string  `json:"snippet,omitempty"`
	Domain      string  `json:"domain,omitempty"`
	Score       float64 `json:"score,omitempty"`
	PublishedAt *string `json:"published_at,omitempty"`
	Thumbnail   string  `json:"thumbnail,omitempty"`
	RefID       string  `json:"ref_id,omitempty"`
}

// Document is a web_fetch markdown page (from web.Page), rendered through the
// sanitized MarkdownText host on the cockpit (D-10).
type Document struct {
	Title     string `json:"title,omitempty"`
	URL       string `json:"url,omitempty"`
	ContentMD string `json:"content_md"`
}

// Code is a sandbox/shell text body with its language tag for lazy highlighting.
type Code struct {
	Body string `json:"body"`
	Lang string `json:"lang,omitempty"`
}

// Artifact is a local file output chip (filename + size + path); it reuses the
// existing aura.artifact / ArtifactDelta delivery plumbing on the channel side.
type Artifact struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	Path      string `json:"path,omitempty"`
}

// Table is a structured grid (D-14: client sorts/filters/exports it).
type Table struct {
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

// Chart is the zero-dep SVG / table-as-bars MVP shape (D-02), swap-ready for a
// real charting lib later.
type Chart struct {
	XLabels    []string  `json:"x_labels"`
	YValues    []float64 `json:"y_values"`
	XAxisLabel string    `json:"x_axis_label,omitempty"`
}

// System is the system_event payload (D-07). It carries ONLY classified, safe
// fields: Class is the stable error/status enum, Reason the sanitized reason
// string (never a raw internal URL/IP), Message a short safe headline, Severity a
// render hint. It mirrors web.WebError's safe surface — no sensitive field exists.
type System struct {
	Class    string `json:"class"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
	Severity string `json:"severity,omitempty"`
}

// ChildReport is the wire-identical mirror of swarm.ChildReport (swarm/report.go
// :32-41). It is redeclared here rather than imported because swarm imports
// internal/agent, and event.go (package agent) imports this package — importing
// swarm here would close an agent→display→swarm→agent cycle. The same redeclare-
// to-break-a-cycle idiom is used by agent.PauseOption (event.go:135). The JSON
// tags match swarm.ChildReport byte-for-byte so the swarm_spawn result decodes
// straight into this shape (D-08 / SWARM-01).
type ChildReport struct {
	GoalIndex  int      `json:"goal_index"`
	ChildID    string   `json:"child_id"`
	Status     string   `json:"status"`
	Summary    string   `json:"summary,omitempty"`
	Error      string   `json:"error,omitempty"`
	Question   string   `json:"question,omitempty"`
	Options    []string `json:"options,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
	Goal       string   `json:"goal,omitempty"`
	Attempts   int      `json:"attempts,omitempty"`
}

// Source is one registry entry (D-05 / R5). RefID is the stable URL-keyed id the
// model cites; Index is the 1-based display number; Cited distinguishes a source
// the model referenced inline from one merely consulted (Source Explorer shows
// all consulted, chips show cited).
type Source struct {
	RefID      string  `json:"ref_id"`
	Index      int     `json:"index"`
	Type       Kind    `json:"type,omitempty"`
	Title      string  `json:"title,omitempty"`
	URL        string  `json:"url,omitempty"`
	Snippet    string  `json:"snippet,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Cited      bool    `json:"cited"`
}
