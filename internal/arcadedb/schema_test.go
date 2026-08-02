package arcadedb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// liveSchemaRows is the exact shape ArcadeDB 26.7.3 returns for `SELECT FROM
// schema:types`, captured from the live database rather than invented.
const liveSchemaRows = `{"result":[
{"name":"Chunk","type":"vertex","records":3221,
 "properties":[{"name":"text"},{"name":"embedding"},{"name":"id"}],
 "indexes":[{"name":"Chunk[embedding]"},{"name":"Chunk[text]"}]},
{"name":"MENTIONS","type":"edge","records":12,"properties":[{"name":"score"}],"indexes":[]},
{"name":"AuditEntry","type":"document","records":1,"properties":[],"indexes":[]}
]}`

func schemaTestClient(t *testing.T, body string) (*Client, *string) {
	t.Helper()
	var sent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sent = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	client, err := New(Config{BaseURL: srv.URL, Database: "mem_x", User: "u_x", Password: "pw"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client, &sent
}

func TestClientSchemaReadsTheTypeCatalogue(t *testing.T) {
	client, sent := schemaTestClient(t, liveSchemaRows)
	got, err := client.Schema(context.Background())
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if !strings.Contains(*sent, "schema:types") {
		t.Fatalf("statement did not read the schema pseudo-type: %s", *sent)
	}
	if len(got.Vertices) != 1 || got.Vertices[0].Name != "Chunk" || got.Vertices[0].Records != 3221 {
		t.Fatalf("vertices = %+v", got.Vertices)
	}
	if len(got.Edges) != 1 || got.Edges[0].Name != "MENTIONS" {
		t.Fatalf("edges = %+v", got.Edges)
	}
	if len(got.Documents) != 1 || got.Documents[0].Name != "AuditEntry" {
		t.Fatalf("documents = %+v", got.Documents)
	}
}

// Schema is a read: it must go to the query endpoint, which ArcadeDB refuses a mutation
// on. Sending it to /command would silently give up that guard.
func TestClientSchemaUsesTheQueryEndpoint(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = io.WriteString(w, `{"result":[]}`)
	}))
	t.Cleanup(srv.Close)
	client, err := New(Config{BaseURL: srv.URL, Database: "mem_x", User: "u_x", Password: "pw"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := client.Schema(context.Background()); err != nil {
		t.Fatalf("Schema: %v", err)
	}
	if !strings.HasPrefix(path, "/api/v1/query/") {
		t.Fatalf("schema read hit %q, want the query endpoint", path)
	}
}

func TestClientSchemaSurfacesTheServerRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"Unauthorized","detail":"User 'u_x' is not allowed to access database 'mem_y'"}`)
	}))
	t.Cleanup(srv.Close)
	client, err := New(Config{BaseURL: srv.URL, Database: "mem_y", User: "u_x", Password: "pw"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, schemaErr := client.Schema(context.Background())
	if schemaErr == nil {
		t.Fatal("a refused credential must be an error, never an empty schema")
	}
	if !strings.Contains(schemaErr.Error(), "not allowed to access") {
		t.Fatalf("server detail lost: %v", schemaErr)
	}
}

func TestBuildSchemaSortsAndAcceptsBothPropertyShapes(t *testing.T) {
	got := buildSchema([]map[string]any{
		{"name": "Zeta", "type": "vertex", "records": float64(2), "properties": []any{"b", "a"}},
		{"name": "Alpha", "type": "vertex", "records": 1, "properties": []any{map[string]any{"name": "x"}}},
	})
	if len(got.Vertices) != 2 || got.Vertices[0].Name != "Alpha" {
		t.Fatalf("vertices not sorted: %+v", got.Vertices)
	}
	if strings.Join(got.Vertices[1].Properties, ",") != "a,b" {
		t.Fatalf("bare property names not sorted: %v", got.Vertices[1].Properties)
	}
	if strings.Join(got.Vertices[0].Properties, ",") != "x" {
		t.Fatalf("descriptor property names lost: %v", got.Vertices[0].Properties)
	}
}

func TestBuildSchemaSkipsUnnamedRowsAndTreatsUnknownKindAsVertex(t *testing.T) {
	got := buildSchema([]map[string]any{
		{"type": "vertex", "records": 9},
		{"name": "Odd", "type": "", "records": 0},
	})
	if len(got.Vertices) != 1 || got.Vertices[0].Name != "Odd" {
		t.Fatalf("vertices = %+v", got.Vertices)
	}
	if got.Documents != nil {
		t.Fatalf("documents = %+v, want nil so the field is omitted", got.Documents)
	}
}
