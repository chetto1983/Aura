package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/knowledge"
)

// graph_api.go is the Phase-27 thin REST adapter over the plan-01 graph normalizer
// (GRAPH-01 "served over REST, not SSE" + the authenticated half of GRAPH-04). Two
// read-only routes — GET /api/graph/schema + POST /api/graph/query — turn a structured
// GraphIntent into the flat {nodes,edges,paths,schema,query} contract. There is NO
// business logic here: the compile → assertReadOnly → Read → normalize path all lives
// in internal/knowledge (the locked seam); these handlers only parse, validate, dispatch,
// and project to JSON.
//
// The routes are registered on the agui Server.Mux under the /api/ carve-out; the
// PARENT-mux mount behind RequireAuth is cmd/aura/serve_webui.go's job (the whole-origin
// gate is inherited, no second auth check here — never a bare /api/, T-27-03). This file
// imports NO runner/SSE adapter and touches NO messages[0]/SSE path (Pitfall 6): it is
// plain net/http JSON, distinct from the chat KV-cache stream.

// graphSeedIDMaxLen / graphSessionMaxLen bound the free-form intent identifier fields
// before they reach the normalizer (V5 length-cap). A Neo4j elementId is short
// (`<db>:<uuid>:<id>`) and a session ThreadID is a UUID; a payload far past these is a
// crafted body, rejected 400 before the param map is built. The normalizer binds them as
// data (never interpolated), so this is defense-in-depth, not the sole control.
const (
	graphSeedIDMaxLen  = 256
	graphSessionMaxLen = 256
)

// graphFilterMaxLen bounds each label/rel-type filter token AND the number of filter
// entries — a hostile body cannot smuggle thousands of filter strings to inflate the
// bound IN-list. Labels/rel-types are additionally validated against the live schema set
// (V5) so an unknown label is a clean 400, not a silent empty result.
const (
	graphFilterMaxLen     = 128
	graphFilterMaxEntries = 64
)

// errUnknownGraphFilter is returned when an intent's Labels/RelTypes carry a token absent
// from the live schema set. Sanitized — it never echoes back the (untrusted) token verbatim
// into a reflected error beyond the typed marker.
var errUnknownGraphFilter = errors.New("graphview: unknown label or rel-type filter")

// GraphView is the narrow read-only graph surface the handlers consume (D-A2-02:
// declared consumer-side so the handler depends only on the two methods it calls, not
// the whole *knowledge.GraphView). *knowledge.GraphView satisfies it. Schema returns the
// live label/rel-type/property-key overview (D-06); Query dispatches a structured intent
// through the plan-01 compile → assertReadOnly → Read → normalize path. A Server with no
// GraphView wired answers both routes 503 (the wiring is optional; the daemon composition
// root sets it via SetGraphView).
type GraphView interface {
	Schema(ctx context.Context) (knowledge.GraphSchema, error)
	Query(ctx context.Context, in knowledge.GraphIntent) (knowledge.GraphResult, error)
}

// registerGraphRoutes mounts the two read-only graph routes on the supplied mux using
// Go 1.22 method-pattern routing. Both are SPECIFIC method+path siblings under the /api/
// carve-out — never a bare /api/ (which would shadow /api/integrations/, T-27-03). The
// parent-mux mount behind RequireAuth lives in cmd/aura/serve_webui.go.
func (s *Server) registerGraphRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/graph/schema", s.handleGraphSchema)
	mux.HandleFunc("POST /api/graph/query", s.handleGraphQuery)
}

// handleGraphSchema serves GET /api/graph/schema (GRAPH-01 / D-06): the live label/
// rel-type/property-key overview the cockpit's left-panel filters + color legend +
// schema-overview empty-state read. A missing GraphView (unwired) is 503; a read failure
// is a sanitized 502 (no raw Cypher/DSN/host leak, V13/HARDEN-08).
func (s *Server) handleGraphSchema(w http.ResponseWriter, r *http.Request) {
	if s.graph == nil {
		http.Error(w, "graph view not configured", http.StatusServiceUnavailable)
		return
	}
	schema, err := s.graph.Schema(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, schema)
}

// handleGraphQuery serves POST /api/graph/query (GRAPH-01 / D-05): a structured
// GraphIntent → the flat {nodes,edges,paths,schema,query} contract. The handler is the
// untrusted-input chokepoint (T-27-01/T-27-05): the body is size-capped (MaxBytesReader),
// the op is enum-validated, the id fields are length-capped, and the label/rel-type
// filters are validated against the live schema set BEFORE dispatch. It NEVER builds
// Cypher — the param-bound compile + assertReadOnly backstop live in the plan-01
// normalizer (the only query path is GraphView.Query). A read failure is a sanitized 502.
func (s *Server) handleGraphQuery(w http.ResponseWriter, r *http.Request) {
	if s.graph == nil {
		http.Error(w, "graph view not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var intent knowledge.GraphIntent
	if err := json.NewDecoder(r.Body).Decode(&intent); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := s.validateGraphIntent(r.Context(), intent); err != nil {
		http.Error(w, sanitizeErr(err), http.StatusBadRequest)
		return
	}
	res, err := s.graph.Query(r.Context(), intent)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, res)
}

// validateGraphIntent enforces the server-side V5 input-validation contract before the
// intent reaches the normalizer: a known op, length-capped id fields, bounded + live-
// schema-validated label/rel-type filters, and non-negative caps. It returns a sanitized
// error (never reflecting an untrusted token verbatim) so a 400 body leaks nothing. The
// normalizer clamps caps and parameter-binds every value regardless; this is the
// fail-fast, defense-in-depth front door (T-27-01).
func (s *Server) validateGraphIntent(ctx context.Context, in knowledge.GraphIntent) error {
	switch in.Op {
	case knowledge.OpSeed, knowledge.OpExpand, knowledge.OpSchemaOverview:
	default:
		return errors.New("graphview: unknown op")
	}
	if len(in.SeedID) > graphSeedIDMaxLen {
		return errors.New("graphview: seed_id too long")
	}
	if len(in.Session) > graphSessionMaxLen {
		return errors.New("graphview: session too long")
	}
	if in.NodeCap < 0 || in.EdgeCap < 0 {
		return errors.New("graphview: caps must not be negative")
	}
	if len(in.Labels) > graphFilterMaxEntries || len(in.RelTypes) > graphFilterMaxEntries {
		return errors.New("graphview: too many filter entries")
	}
	for _, l := range in.Labels {
		if len(l) > graphFilterMaxLen {
			return errors.New("graphview: filter token too long")
		}
	}
	for _, rt := range in.RelTypes {
		if len(rt) > graphFilterMaxLen {
			return errors.New("graphview: filter token too long")
		}
	}
	if len(in.Labels) == 0 && len(in.RelTypes) == 0 {
		return nil
	}
	// Validate the filter tokens against the live schema set so an unknown label/rel-type
	// is a clean 400 (not a silent empty subgraph). One Schema read amortizes both lists.
	schema, err := s.graph.Schema(ctx)
	if err != nil {
		return err
	}
	if !subset(in.Labels, schema.Labels) || !subset(in.RelTypes, schema.RelTypes) {
		return errUnknownGraphFilter
	}
	return nil
}

// subset reports whether every member of want is present in have. An empty want is a
// trivial subset (no filter to validate). Used to gate label/rel-type filters against the
// live schema set.
func subset(want, have []string) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(have))
	for _, h := range have {
		set[h] = struct{}{}
	}
	for _, w := range want {
		if _, ok := set[w]; !ok {
			return false
		}
	}
	return true
}
