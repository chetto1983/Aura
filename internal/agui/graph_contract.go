// The cockpit graph contract — the JSON shapes GET /api/graph/schema and
// POST /api/graph/query speak, mirrored field-for-field by web/src/graph/types.ts.
//
// The shipped web bundle decodes these and is NOT rebuilt in lockstep with the
// server, so every json tag, every omitempty and the three op string values are
// frozen: renaming one breaks the panel silently instead of failing to compile.
package agui

// Op enum (GraphIntent.Op). The REST layer enum-validates an inbound intent
// against these; the STRING values are the wire (web/src/graph/types.ts GraphOp),
// so they are not renameable.
const (
	OpSeed           = "seed"
	OpExpand         = "expand"
	OpSchemaOverview = "schema_overview"
)

// GraphIntent is the structured payload the cockpit submits. The cockpit never
// authors a query; it sends this and the server decides what it can answer.
type GraphIntent struct {
	Op       string   `json:"op"`                  // "seed" | "expand" | "schema_overview"
	SeedID   string   `json:"seed_id,omitempty"`   // node anchor for "expand"
	Session  string   `json:"session,omitempty"`   // active conversation ThreadID for "seed"
	Labels   []string `json:"labels,omitempty"`    // include-label filter
	RelTypes []string `json:"rel_types,omitempty"` // include-rel filter
	NodeCap  int      `json:"node_cap,omitempty"`  // client hint; the server bounds its own reads
	EdgeCap  int      `json:"edge_cap,omitempty"`  // client hint; the server bounds its own reads
	UserID   string   `json:"-"`                   // authenticated Aura identity; never client-supplied
}

// GraphResult is the flat tagged contract a graph read emits.
type GraphResult struct {
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
	Paths  []GraphPath `json:"paths,omitempty"`
	Schema GraphSchema `json:"schema"`
	Query  string      `json:"query"`
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
}

// GraphPath is an ordered run of edges (the path strip).
type GraphPath struct {
	Steps []GraphEdge `json:"steps"`
}

// GraphSchema is the live introspection result: the label set + rel-types +
// property keys feed the left-panel filters, the colour legend and the
// schema-overview empty state. Counts is optional and, per type, the live record
// count.
type GraphSchema struct {
	Labels       []string       `json:"labels"`
	RelTypes     []string       `json:"rel_types"`
	PropertyKeys []string       `json:"property_keys,omitempty"`
	EntityTypes  []string       `json:"entity_types,omitempty"`
	Counts       map[string]int `json:"counts,omitempty"`
}
