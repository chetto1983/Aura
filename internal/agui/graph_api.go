package agui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// graph_api.go is the thin REST adapter for the cockpit's graph panel: two read-only
// routes — GET /api/graph/schema + POST /api/graph/query — turning a structured
// GraphIntent into the flat {nodes,edges,paths,schema,query} contract (graph_contract.go).
// There is no query logic here; the handlers parse, validate, dispatch to the wired
// GraphView and project to JSON.
//
// The GraphView behind them is schema-only (graph_arcadedb.go): no traversal compiler
// stands behind /api/graph/query, so it answers every op with the live type catalogue and
// an empty canvas. The validation below still earns its keep regardless — it is the
// untrusted-input chokepoint for a route that is authenticated but public-facing.
//
// The routes are registered on the agui Server.Mux under the /api/ carve-out; the
// PARENT-mux mount behind RequireAuth is cmd/aura/serve_webui.go's job (the whole-origin
// gate is inherited, no second auth check here — never a bare /api/, T-27-03). This file
// imports NO runner/SSE adapter and touches NO messages[0]/SSE path (Pitfall 6): it is
// plain net/http JSON, distinct from the chat KV-cache stream.

// graphSeedIDMaxLen / graphSessionMaxLen bound the free-form intent identifier fields
// before they reach the view (V5 length-cap). A node id is short and a session ThreadID
// is a UUID; a payload far past these is a crafted body, rejected 400 rather than
// carried any further.
const (
	graphSeedIDMaxLen  = 256
	graphSessionMaxLen = 256
)

// graphFilterMaxLen bounds each label/rel-type filter token AND the number of filter
// entries — a hostile body cannot smuggle thousands of filter strings through. Labels and
// rel-types are additionally validated against the live schema set (V5) so an unknown
// label is a clean 400, not a silent empty result.
const (
	graphFilterMaxLen     = 128
	graphFilterMaxEntries = 64
)

// errUnknownGraphFilter is returned when an intent's Labels/RelTypes carry a token absent
// from the live schema set. Sanitized — it never echoes back the (untrusted) token verbatim
// into a reflected error beyond the typed marker.
var errUnknownGraphFilter = errors.New("graphview: unknown label or rel-type filter")

// GraphView is the narrow read-only graph surface the handlers consume. Schema returns
// the live label/rel-type/property-key overview for ONE identity; Query dispatches a
// structured intent. A Server with no GraphView wired answers both routes 503 (the
// wiring is optional; the daemon composition root sets it via SetGraphView).
//
// Both methods take the identity explicitly — Schema as a parameter, Query inside the
// intent — because the graph store is one database per identity. The handler is the only
// place that knows who is calling, so it is the only place that may say.
type GraphView interface {
	Schema(ctx context.Context, identityID string) (GraphSchema, error)
	Query(ctx context.Context, in GraphIntent) (GraphResult, error)
}

// registerGraphRoutes mounts the two read-only graph routes on the supplied mux using
// Go 1.22 method-pattern routing. Both are SPECIFIC method+path siblings under the /api/
// carve-out — never a bare /api/ (which would shadow /api/integrations/, T-27-03). The
// parent-mux mount behind RequireAuth lives in cmd/aura/serve_webui.go.
func (s *Server) registerGraphRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/graph/schema", s.handleGraphSchema)
	mux.HandleFunc("POST /api/graph/query", s.handleGraphQuery)
}

// handleGraphSchema serves GET /api/graph/schema: the live label/rel-type/property-key
// overview the cockpit's left-panel filters + color legend + empty state read, for the
// AUTHENTICATED identity only. A missing GraphView (unwired) is 503; a read failure is a
// sanitized 502 (no raw query/DSN/host leak, V13/HARDEN-08).
func (s *Server) handleGraphSchema(w http.ResponseWriter, r *http.Request) {
	if s.graph == nil {
		http.Error(w, "graph view not configured", http.StatusServiceUnavailable)
		return
	}
	schema, err := s.graph.Schema(r.Context(), principalFrom(r.Context()))
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, schema)
}

// handleGraphQuery serves POST /api/graph/query: a structured GraphIntent → the flat
// {nodes,edges,paths,schema,query} contract. The handler is the untrusted-input
// chokepoint (T-27-01/T-27-05): the body is size-capped (MaxBytesReader), the op is
// enum-validated, the id fields are length-capped, and the label/rel-type filters are
// validated against the live schema set BEFORE dispatch. A read failure is a sanitized 502.
//
// The principal is stamped onto the intent BEFORE validation, not after: validation reads
// the live schema to check the filter tokens, and that read is itself per identity. The
// UserID field is `json:"-"`, so a client cannot supply one either way.
func (s *Server) handleGraphQuery(w http.ResponseWriter, r *http.Request) {
	if s.graph == nil {
		http.Error(w, "graph view not configured", http.StatusServiceUnavailable)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)
	var intent GraphIntent
	if err := json.NewDecoder(r.Body).Decode(&intent); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	intent.UserID = principalFrom(r.Context())
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
// intent reaches the view: a known op, length-capped id fields, bounded + live-
// schema-validated label/rel-type filters, and non-negative caps. It returns a sanitized
// error (never reflecting an untrusted token verbatim) so a 400 body leaks nothing.
func (s *Server) validateGraphIntent(ctx context.Context, in GraphIntent) error {
	switch in.Op {
	case OpSeed, OpExpand, OpSchemaOverview:
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
	schema, err := s.graph.Schema(ctx, in.UserID)
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
