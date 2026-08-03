package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeGraphView is a scripted GraphView for the graph-api unit tests: it returns canned
// schema/result (or scripted errors) and records whether each method was reached (the
// RequireAuth-gate test asserts the seam is NOT reached without a session).
type fakeGraphView struct {
	schema        GraphSchema
	result        GraphResult
	schemaErr     error
	queryErr      error
	schemaCalls   int
	queryCalls    int
	gotIntent     GraphIntent
	schemaReached bool
	queryReached  bool
	// gotSchemaIdentity records who the schema read was scoped to — the graph store is
	// one database per identity, so an unstamped read is a cross-tenant read.
	gotSchemaIdentity string
}

func (f *fakeGraphView) Schema(_ context.Context, identityID string) (GraphSchema, error) {
	f.schemaCalls++
	f.schemaReached = true
	f.gotSchemaIdentity = identityID
	if f.schemaErr != nil {
		return GraphSchema{}, f.schemaErr
	}
	return f.schema, nil
}

func (f *fakeGraphView) Query(_ context.Context, in GraphIntent) (GraphResult, error) {
	f.queryCalls++
	f.queryReached = true
	f.gotIntent = in
	if f.queryErr != nil {
		return GraphResult{}, f.queryErr
	}
	return f.result, nil
}

// fakeGraph returns a GraphView with a non-empty schema + a small contract result.
func fakeGraph() *fakeGraphView {
	return &fakeGraphView{
		schema: GraphSchema{
			Labels:   []string{"Entity", "Document", "Conversation"},
			RelTypes: []string{"MENTIONS", "HAS_MESSAGE"},
		},
		result: GraphResult{
			Nodes:  []GraphNode{{ID: "e1", Caption: "Aura", Labels: []string{"Entity"}}},
			Edges:  []GraphEdge{{ID: "r1", Source: "e1", Target: "e2", RelType: "MENTIONS"}},
			Schema: GraphSchema{Labels: []string{"Entity"}},
			Query:  "MATCH (e:Entity) RETURN e",
		},
	}
}

func graphServer(gv GraphView) *Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if gv != nil {
		s.SetGraphView(gv)
	}
	return s
}

func doGraph(t *testing.T, s *Server, method, target string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

// TestGraphSchemaOK: GET /api/graph/schema with a wired GraphView returns 200 + a
// non-empty labels array (the schema-overview default open, GRAPH-01 / D-06).
func TestGraphSchemaOK(t *testing.T) {
	rec := doGraph(t, graphServer(fakeGraph()), http.MethodGet, "/api/graph/schema", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got GraphSchema
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got.Labels) == 0 {
		t.Fatalf("expected a non-empty labels array, got %+v", got)
	}
}

// TestGraphSchemaUnwired503: GET /api/graph/schema with no GraphView wired is 503.
func TestGraphSchemaUnwired503(t *testing.T) {
	rec := doGraph(t, graphServer(nil), http.MethodGet, "/api/graph/schema", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestGraphSchemaErrorSanitized: a Schema error surfaces as a non-200 with a sanitized
// body — the tenant credential in a DSN never reaches the client.
func TestGraphSchemaErrorSanitized(t *testing.T) {
	gv := fakeGraph()
	gv.schemaErr = errors.New("dial http://mem_tenant:hunter2@10.0.0.1:2480 refused")
	rec := doGraph(t, graphServer(gv), http.MethodGet, "/api/graph/schema", nil)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("schema error leaked a credential: %s", rec.Body.String())
	}
}

// TestGraphQueryOK: a valid intent returns 200 + the {nodes,edges,paths,schema,query}
// contract JSON.
func TestGraphQueryOK(t *testing.T) {
	gv := fakeGraph()
	body, _ := json.Marshal(GraphIntent{Op: OpOverview})
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var got GraphResult
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "e1" {
		t.Fatalf("contract nodes mismatch: %+v", got.Nodes)
	}
	if len(got.Edges) != 1 || got.Edges[0].RelType != "MENTIONS" {
		t.Fatalf("contract edges mismatch: %+v", got.Edges)
	}
	if got.Query == "" {
		t.Fatalf("expected the display query to round-trip")
	}
	if gv.gotIntent.Op != OpOverview {
		t.Fatalf("normalizer got intent %+v", gv.gotIntent)
	}
}

// TestGraphQueryUnwired503: POST /api/graph/query with no GraphView wired is 503.
func TestGraphQueryUnwired503(t *testing.T) {
	body, _ := json.Marshal(GraphIntent{Op: OpOverview})
	rec := doGraph(t, graphServer(nil), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

// TestGraphQueryMalformedBody: a non-JSON body is a clean 400 (never a 500).
func TestGraphQueryMalformedBody(t *testing.T) {
	rec := doGraph(t, graphServer(fakeGraph()), http.MethodPost, "/api/graph/query", []byte("{not json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

// TestGraphQueryUnknownOp: an unknown op is a 400, never dispatched to the normalizer.
func TestGraphQueryUnknownOp(t *testing.T) {
	gv := fakeGraph()
	body := []byte(`{"op":"drop_everything"}`)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if gv.queryReached {
		t.Fatalf("an unknown op reached the normalizer")
	}
}

func TestGraphQueryRejectsRemovedContractShapes(t *testing.T) {
	for _, body := range []string{
		`{"op":"seed"}`,
		`{"op":"schema_overview"}`,
		`{"op":"overview","seed_id":"#1:0"}`,
		`{"op":"overview","session":"thread-1"}`,
		`{"op":"overview","node_id":"#1:0"}`,
	} {
		gv := fakeGraph()
		rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", []byte(body))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body %s: status = %d, want 400", body, rec.Code)
		}
		if gv.schemaCalls != 0 || gv.queryReached {
			t.Fatalf("removed request caused graph I/O: schema=%d query=%v", gv.schemaCalls, gv.queryReached)
		}
	}
}

// TestGraphQueryNegativeCap: a negative cap is rejected 400 before dispatch (V5).
func TestGraphQueryNegativeCap(t *testing.T) {
	gv := fakeGraph()
	body := []byte(`{"op":"overview","node_cap":-5}`)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if gv.queryReached {
		t.Fatalf("a negative cap reached the normalizer")
	}
}

func TestGraphQueryInvalidArcadeRIDIsRejectedBeforeSchemaIO(t *testing.T) {
	gv := fakeGraph()
	body := []byte(`{"op":"expand","node_id":"#1:0; DELETE FROM Entity","labels":["Entity"]}`)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if gv.schemaCalls != 0 || gv.queryReached {
		t.Fatalf("unsafe RID caused graph I/O: schema=%d query=%v", gv.schemaCalls, gv.queryReached)
	}
}

// TestGraphQueryUnknownLabelFilter: a label absent from the live schema set is a 400
// (validated against Schema before dispatch, V5), and the normalizer's Query is not reached.
func TestGraphQueryUnknownLabelFilter(t *testing.T) {
	gv := fakeGraph()
	body := []byte(`{"op":"expand","node_id":"#1:0","labels":["NotARealLabel"]}`)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if gv.queryReached {
		t.Fatalf("an unknown-label intent reached the normalizer")
	}
}

// TestGraphQueryKnownLabelFilter: a label present in the live schema set is admitted (200)
// and the intent reaches the normalizer with the filter intact.
func TestGraphQueryKnownLabelFilter(t *testing.T) {
	gv := fakeGraph()
	body := []byte(`{"op":"expand","node_id":"#1:0","labels":["Entity"],"rel_types":["MENTIONS"]}`)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if !gv.queryReached || len(gv.gotIntent.Labels) != 1 || gv.gotIntent.Labels[0] != "Entity" {
		t.Fatalf("known-label intent did not reach the normalizer intact: %+v", gv.gotIntent)
	}
}

// TestGraphQueryOverCap413: a body over MaxBytesReader is rejected (never read unbounded).
func TestGraphQueryOverCap413(t *testing.T) {
	gv := fakeGraph()
	// Build a body larger than maxRunBodyBytes (1 MiB) with valid-prefix JSON padding.
	big := append([]byte(`{"op":"overview","padding":"`), bytes.Repeat([]byte("a"), maxRunBodyBytes+16)...)
	big = append(big, []byte(`"}`)...)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", big)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want a non-200 (body cap)", rec.Code)
	}
	if gv.queryReached {
		t.Fatalf("an over-cap body reached the normalizer")
	}
}

// TestGraphQueryErrorSanitized: a normalizer Query error surfaces as a non-200 with a
// sanitized body (no raw Cypher/DSN substring, V13/HARDEN-08).
func TestGraphQueryErrorSanitized(t *testing.T) {
	gv := fakeGraph()
	gv.queryErr = errors.New("read failed at http://mem_tenant:hunter2@arcadedb:2480")
	body, _ := json.Marshal(GraphIntent{Op: OpOverview})
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatalf("query error leaked a credential: %s", rec.Body.String())
	}
}

// TestGraphQueryValidationBranches exercises every server-side V5 reject (T-27-01/T-27-05):
// over-long ids, over-long filter tokens, too many filter entries, and a too-large rel-type
// token. Each is a clean 400 BEFORE the normalizer is reached.
func TestGraphQueryValidationBranches(t *testing.T) {
	longID := strings.Repeat("x", graphNodeIDMaxLen+1)
	longTok := strings.Repeat("L", graphFilterMaxLen+1)
	manyLabels := make([]string, graphFilterMaxEntries+1)
	for i := range manyLabels {
		manyLabels[i] = "Entity"
	}
	cases := []struct {
		name   string
		intent GraphIntent
	}{
		{"node_id too long", GraphIntent{Op: OpExpand, NodeID: longID}},
		{"too many label filters", GraphIntent{Op: OpExpand, NodeID: "#1:0", Labels: manyLabels}},
		{"label token too long", GraphIntent{Op: OpExpand, NodeID: "#1:0", Labels: []string{longTok}}},
		{"rel-type token too long", GraphIntent{Op: OpExpand, NodeID: "#1:0", RelTypes: []string{longTok}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gv := fakeGraph()
			body, _ := json.Marshal(tc.intent)
			rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if gv.queryReached {
				t.Fatalf("a rejected intent (%s) reached the normalizer", tc.name)
			}
		})
	}
}

// TestGraphQueryLabelsOnlyFilter: a labels-only filter (no rel_types) is admitted (200);
// it validates the labels against the live schema set while the empty rel-type list is the
// trivial subset (no filter to check).
func TestGraphQueryLabelsOnlyFilter(t *testing.T) {
	gv := fakeGraph()
	body := []byte(`{"op":"expand","node_id":"#1:0","labels":["Document"]}`)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if !gv.queryReached {
		t.Fatalf("a labels-only intent did not reach the normalizer")
	}
}

// TestGraphQueryFilterSchemaError: when the filter-validation Schema read fails, the
// handler surfaces a sanitized non-200 and never dispatches the Query (the unknown-filter
// gate cannot be evaluated, so the request is refused, not silently passed).
func TestGraphQueryFilterSchemaError(t *testing.T) {
	gv := fakeGraph()
	gv.schemaErr = errors.New("schema read failed at http://mem_tenant:topsecret@arcadedb:2480")
	body := []byte(`{"op":"expand","node_id":"#1:0","labels":["Entity"]}`)
	rec := doGraph(t, graphServer(gv), http.MethodPost, "/api/graph/query", body)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = %d, want non-200 when the filter-schema read fails", rec.Code)
	}
	if gv.queryReached {
		t.Fatalf("Query was dispatched despite a failed filter-schema read")
	}
	if strings.Contains(rec.Body.String(), "topsecret") {
		t.Fatalf("filter-schema error leaked a credential: %s", rec.Body.String())
	}
}

// validCookiePost builds a POST request to path carrying a valid session cookie for the
// local identity (validCookieReq only builds GETs; the graph query route is a POST).
func validCookiePost(deps AuthDeps, path string, body []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	value := signSession(deps.SigningKey, deps.LocalIdentityID, time.Now())
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: value})
	return req
}

func graphIntentUserID(t *testing.T, in GraphIntent) string {
	t.Helper()
	v := reflect.ValueOf(in)
	f := v.FieldByName("UserID")
	if !f.IsValid() {
		t.Fatalf("GraphIntent is missing UserID for authenticated graph scoping")
	}
	if f.Kind() != reflect.String {
		t.Fatalf("GraphIntent.UserID must be a string, got %s", f.Kind())
	}
	return f.String()
}

func TestGraphQueryInjectsAuthenticatedPrincipal(t *testing.T) {
	deps := testDeps("operator-secret")
	gv := fakeGraph()
	s := graphServer(gv)
	gated := RequireAuth(s.Mux(), deps)
	body, _ := json.Marshal(GraphIntent{Op: OpOverview})

	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, validCookiePost(deps, "/api/graph/query", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with a valid session (body=%s)", rec.Code, rec.Body.String())
	}
	if got := graphIntentUserID(t, gv.gotIntent); got != deps.LocalIdentityID {
		t.Fatalf("GraphIntent.UserID = %q, want authenticated identity %q", got, deps.LocalIdentityID)
	}
}

// TestGraphSchemaScopesToAuthenticatedIdentity: the schema read is per identity because
// the graph store is one database per identity. An unstamped read would resolve to
// whatever database the view defaults to, which is how one operator ends up looking at
// another's memory shape.
func TestGraphSchemaScopesToAuthenticatedIdentity(t *testing.T) {
	deps := testDeps("operator-secret")
	gv := fakeGraph()
	gated := RequireAuth(graphServer(gv).Mux(), deps)

	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, validCookieReq(deps, "/api/graph/schema", false))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gv.gotSchemaIdentity != deps.LocalIdentityID {
		t.Fatalf("schema read scoped to %q, want the authenticated identity %q",
			gv.gotSchemaIdentity, deps.LocalIdentityID)
	}
}

// TestGraphQueryFilterValidationScopesToAuthenticatedIdentity: the filter-validation
// schema read is the SAME per-identity read, so it must carry the principal too — a
// filter validated against someone else's label set is both a wrong answer and a leak of
// which labels exist over there.
func TestGraphQueryFilterValidationScopesToAuthenticatedIdentity(t *testing.T) {
	deps := testDeps("operator-secret")
	gv := fakeGraph()
	gated := RequireAuth(graphServer(gv).Mux(), deps)
	body := []byte(`{"op":"expand","node_id":"#1:0","labels":["Entity"]}`)

	rec := httptest.NewRecorder()
	gated.ServeHTTP(rec, validCookiePost(deps, "/api/graph/query", body))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	if gv.gotSchemaIdentity != deps.LocalIdentityID {
		t.Fatalf("filter-validation schema read scoped to %q, want %q",
			gv.gotSchemaIdentity, deps.LocalIdentityID)
	}
}

// TestGraphRequireAuthGate (T-27-03): the two graph routes mount behind the same
// RequireAuth whole-origin gate every /api/ route inherits (the wrap lives at the parent
// mux, mirrored here with RequireAuth(s.Mux(), deps)). An unauthenticated request (no
// session cookie) is rejected 401 on BOTH routes and never reaches the GraphView seam; a
// valid session reaches the handler. The bare Server.Mux() (no wrap) proves the routes are
// actually registered (not a 404).
func TestGraphRequireAuthGate(t *testing.T) {
	deps := testDeps("operator-secret")
	gv := fakeGraph()
	s := graphServer(gv)
	gated := RequireAuth(s.Mux(), deps)

	t.Run("unauthenticated schema GET rejected 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/graph/schema", nil)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (no session)", rec.Code)
		}
		if gv.schemaReached {
			t.Fatalf("the GraphView Schema was reached without a session")
		}
	})

	t.Run("unauthenticated query POST rejected 401", func(t *testing.T) {
		body, _ := json.Marshal(GraphIntent{Op: OpOverview})
		req := httptest.NewRequest(http.MethodPost, "/api/graph/query", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (no session)", rec.Code)
		}
		if gv.queryReached {
			t.Fatalf("the GraphView Query was reached without a session")
		}
	})

	t.Run("valid session reaches the schema handler", func(t *testing.T) {
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, validCookieReq(deps, "/api/graph/schema", false))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 with a valid session", rec.Code)
		}
		if !gv.schemaReached {
			t.Fatalf("a valid session did not reach the GraphView Schema")
		}
	})

	t.Run("valid session reaches the query handler", func(t *testing.T) {
		body, _ := json.Marshal(GraphIntent{Op: OpOverview})
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, validCookiePost(deps, "/api/graph/query", body))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 with a valid session (body=%s)", rec.Code, rec.Body.String())
		}
		if !gv.queryReached {
			t.Fatalf("a valid session did not reach the GraphView Query")
		}
	})

	t.Run("route registered on bare Mux (not 404)", func(t *testing.T) {
		rec := doGraph(t, graphServer(fakeGraph()), http.MethodGet, "/api/graph/schema", nil)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("GET /api/graph/schema 404'd on the bare Mux — route not registered")
		}
	})
}
