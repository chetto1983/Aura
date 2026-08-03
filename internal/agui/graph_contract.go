// The cockpit graph contract — the JSON shapes GET /api/graph/schema and
// POST /api/graph/query speak, mirrored field-for-field by web/src/graph/types.ts.
//
// The embedded web bundle is rebuilt with the server. This contract deliberately
// exposes only the current ArcadeDB operations; removed request shapes are not
// kept as compatibility aliases.
package agui

// Op enum (GraphIntent.Op). The REST layer rejects every value outside this set.
const (
	OpOverview = "overview"
	OpExpand   = "expand"
)

// GraphIntent is the structured payload the cockpit submits. The cockpit never
// authors a query; it sends this and the server decides what it can answer.
type GraphIntent struct {
	Op       string   `json:"op"`                  // "overview" | "expand"
	NodeID   string   `json:"node_id,omitempty"`   // ArcadeDB RID required for "expand"
	Labels   []string `json:"labels,omitempty"`    // include-label filter
	RelTypes []string `json:"rel_types,omitempty"` // include-rel filter
	NodeCap  int      `json:"node_cap,omitempty"`  // client hint; the server bounds its own reads
	EdgeCap  int      `json:"edge_cap,omitempty"`  // client hint; the server bounds its own reads
	UserID   string   `json:"-"`                   // authenticated Aura identity; never client-supplied
}

// GraphResult is the flat tagged contract a graph read emits.
type GraphResult struct {
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`
	Paths     []GraphPath `json:"paths,omitempty"`
	Schema    GraphSchema `json:"schema"`
	Query     string      `json:"query"`
	Truncated bool        `json:"truncated,omitempty"`
}

// GraphNode is one node in the contract. ID de-dupes and anchors edges;
// EntityType is the second (non-label) colour dimension the legend uses.
type GraphNode struct {
	ID         string         `json:"id"`
	Caption    string         `json:"caption,omitempty"`
	Labels     []string       `json:"labels,omitempty"`
	EntityType string         `json:"entity_type,omitempty"`
	Degree     int            `json:"degree,omitempty"`
	Props      map[string]any `json:"props,omitempty"`
	RefID      string         `json:"ref_id,omitempty"`
	Citations  []string       `json:"citations,omitempty"`
}

// GraphEdge is one relationship: Source/Target are node IDs.
type GraphEdge struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Target  string `json:"target"`
	RelType string `json:"rel_type,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// GraphPath is an ordered run of edges (the path strip).
type GraphPath struct {
	Steps []GraphEdge `json:"steps"`
}

// GraphSchema is the live introspection result: the label set + rel-types +
// property keys feed the left-panel filters, the colour legend and the
// overview empty state. Counts is optional and, per type, the live record
// count.
type GraphSchema struct {
	Labels       []string       `json:"labels"`
	RelTypes     []string       `json:"rel_types"`
	PropertyKeys []string       `json:"property_keys,omitempty"`
	EntityTypes  []string       `json:"entity_types,omitempty"`
	Counts       map[string]int `json:"counts,omitempty"`
}
