package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// schemaRows is the exact shape ArcadeDB 26.7.3 returns for `SELECT FROM
// schema:types`, captured from the live database rather than invented.
const schemaRows = `{"result":[
{"name":"Chunk","type":"vertex","records":3221,
 "properties":[{"name":"text"},{"name":"embedding"},{"name":"id"}],
 "indexes":[{"name":"Chunk[embedding]"},{"name":"Chunk[text]"}]},
{"name":"Document","type":"vertex","records":6,"properties":[{"name":"id"}],"indexes":[]},
{"name":"MENTIONS","type":"edge","records":12,"properties":[{"name":"score"}],"indexes":[]},
{"name":"AuditEntry","type":"document","records":1,"properties":[],"indexes":[]}
]}`

func schemaClient(t *testing.T, status int, body string) *arcadedb.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	client, err := arcadedb.New(arcadedb.Config{
		BaseURL: srv.URL, Database: "aura", User: "root", Password: "pw",
	})
	if err != nil {
		t.Fatalf("arcadedb.New: %v", err)
	}
	return client
}

func callSchema(t *testing.T, client *arcadedb.Client) (GraphSchemaOutput, error) {
	t.Helper()
	_, out, err := graphSchemaHandler(singleTenant(t, client))(context.Background(), nil, GraphSchemaInput{UserIdentifier: testIdentity})
	return out, err
}

func TestGraphSchemaSplitsTypesByKind(t *testing.T) {
	out, err := callSchema(t, schemaClient(t, http.StatusOK, schemaRows))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(out.Vertices) != 2 {
		t.Fatalf("vertices = %d, want 2", len(out.Vertices))
	}
	if len(out.Edges) != 1 || out.Edges[0].Name != "MENTIONS" {
		t.Fatalf("edges = %+v", out.Edges)
	}
	if len(out.Documents) != 1 || out.Documents[0].Name != "AuditEntry" {
		t.Fatalf("documents = %+v", out.Documents)
	}
}

func TestGraphSchemaCarriesRecordCounts(t *testing.T) {
	out, err := callSchema(t, schemaClient(t, http.StatusOK, schemaRows))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.Vertices[0].Name != "Chunk" || out.Vertices[0].Records != 3221 {
		t.Fatalf("first vertex = %+v, want Chunk/3221", out.Vertices[0])
	}
}

func TestGraphSchemaSortsNamesForStableOutput(t *testing.T) {
	out, err := callSchema(t, schemaClient(t, http.StatusOK, schemaRows))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.Vertices[0].Name != "Chunk" || out.Vertices[1].Name != "Document" {
		t.Fatalf("vertices not sorted: %+v", out.Vertices)
	}
	props := out.Vertices[0].Properties
	if strings.Join(props, ",") != "embedding,id,text" {
		t.Fatalf("properties not sorted: %v", props)
	}
	if strings.Join(out.Vertices[0].Indexes, ",") != "Chunk[embedding],Chunk[text]" {
		t.Fatalf("indexes = %v", out.Vertices[0].Indexes)
	}
}

// ArcadeDB has been observed emitting `properties` both as descriptor objects
// and as bare names; neither shape may drop a property.
func TestGraphSchemaAcceptsBarePropertyNames(t *testing.T) {
	body := `{"result":[{"name":"Chunk","type":"vertex","records":1,"properties":["text","id"]}]}`
	out, err := callSchema(t, schemaClient(t, http.StatusOK, body))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if strings.Join(out.Vertices[0].Properties, ",") != "id,text" {
		t.Fatalf("properties = %v", out.Vertices[0].Properties)
	}
}

func TestGraphSchemaTreatsUnknownKindAsVertex(t *testing.T) {
	body := `{"result":[{"name":"Odd","type":"","records":0}]}`
	out, err := callSchema(t, schemaClient(t, http.StatusOK, body))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(out.Vertices) != 1 || out.Vertices[0].Name != "Odd" {
		t.Fatalf("vertices = %+v", out.Vertices)
	}
}

func TestGraphSchemaSkipsUnnamedRows(t *testing.T) {
	body := `{"result":[{"type":"vertex","records":9},{"name":"Real","type":"vertex","records":1}]}`
	out, err := callSchema(t, schemaClient(t, http.StatusOK, body))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(out.Vertices) != 1 || out.Vertices[0].Name != "Real" {
		t.Fatalf("vertices = %+v", out.Vertices)
	}
}

func TestGraphSchemaOmitsDocumentsWhenThereAreNone(t *testing.T) {
	body := `{"result":[{"name":"Chunk","type":"vertex","records":1}]}`
	out, err := callSchema(t, schemaClient(t, http.StatusOK, body))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if out.Documents != nil {
		t.Fatalf("documents = %+v, want nil so the field is omitted", out.Documents)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(encoded), "documents") {
		t.Fatalf("documents key present in %s", encoded)
	}
}

// An empty graph is a legitimate answer, not an error: the arrays must still
// encode as [] so a caller can iterate without a nil check.
func TestGraphSchemaOnEmptyDatabaseReturnsEmptyArrays(t *testing.T) {
	out, err := callSchema(t, schemaClient(t, http.StatusOK, `{"result":[]}`))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"vertices":[]`) {
		t.Fatalf("vertices did not encode as an empty array: %s", encoded)
	}
}

func TestGraphSchemaWrapsDatabaseFailure(t *testing.T) {
	body := `{"error":"Cannot execute command","detail":"no such type"}`
	_, err := callSchema(t, schemaClient(t, http.StatusInternalServerError, body))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.HasPrefix(err.Error(), "graph_schema:") {
		t.Fatalf("error not attributed to the tool: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "no such type") {
		t.Fatalf("database detail lost: %q", err.Error())
	}
}

func TestNewServerRegistersTheTools(t *testing.T) {
	client := schemaClient(t, http.StatusOK, schemaRows)
	if server := newServer(singleTenant(t, client), time.Now); server == nil {
		t.Fatal("newServer returned nil")
	}
}
