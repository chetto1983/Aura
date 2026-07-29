// Unit-safe branch coverage (NO build tag) for the knowledge adapter and pure
// helpers. Framing/process/lifecycle branches live in internal/mcp tests.
package knowledge

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

// errDirFS is an fs.FS whose Open always fails, so fs.ReadDir errors.
type errDirFS struct{}

func (errDirFS) Open(string) (fs.File, error) { return nil, errors.New("open failed") }

type fakeCypherTransport struct {
	result   string
	err      error
	lastName string
	lastArgs map[string]any
	closed   bool
}

func (f *fakeCypherTransport) CallTool(
	ctx context.Context,
	name string,
	args map[string]any,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f.lastName, f.lastArgs = name, args
	return f.result, f.err
}

func (f *fakeCypherTransport) Close() error {
	f.closed = true
	return nil
}

func newFakeClient(result, password string) (*Client, *fakeCypherTransport) {
	transport := &fakeCypherTransport{result: result}
	return &Client{transport: transport, password: password}, transport
}

func TestCypher_Happy(t *testing.T) {
	c, transport := newFakeClient(`[{"one":1}]`, "")
	raw, err := c.Cypher(context.Background(), "RETURN 1", nil, false)
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	rows, err := decodeRows(raw)
	if err != nil || len(rows) != 1 {
		t.Fatalf("decodeRows: rows=%v err=%v", rows, err)
	}
	if transport.lastName != toolRead || transport.lastArgs["query"] != "RETURN 1" {
		t.Fatalf("transport call = %q %#v", transport.lastName, transport.lastArgs)
	}
}

func TestCypher_TransportErrorIsRedactedAndFailClosed(t *testing.T) {
	c, transport := newFakeClient("", "secret")
	transport.err = fmt.Errorf("%w: boom password=secret", mcp.ErrTransport)
	_, err := c.Cypher(context.Background(), "RETURN 1", nil, false)
	if err == nil || !mcp.IsTransportError(err) || !strings.Contains(err.Error(), crashHint) {
		t.Fatalf("want fail-closed transport error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("transport error leaked password: %v", err)
	}
}

func TestCypher_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c, _ := newFakeClient("", "")
	if _, err := c.Cypher(ctx, "RETURN 1", nil, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestCloseDelegatesToCommonTransport(t *testing.T) {
	c, transport := newFakeClient("", "")
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !transport.closed {
		t.Error("Close did not delegate to the common transport")
	}
}

func TestConnectContextUsesConfiguredTimeout(t *testing.T) {
	ctx, cancel := connectContext(context.Background(), &Config{ConnectTimeoutSec: 2})
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("connectContext with timeout: want deadline")
	}
	if remaining := time.Until(deadline); remaining <= 0 || remaining > 2*time.Second {
		t.Fatalf("deadline remaining = %s, want within configured timeout", remaining)
	}

	ctx, cancel = connectContext(context.Background(), &Config{})
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("zero ConnectTimeoutSec should not add a deadline")
	}
}

func TestOpen_SpawnFailure(t *testing.T) {
	cfg := &Config{MCPBinary: "aura-nonexistent-mcp-binary-xyz", BoltURL: "bolt://127.0.0.1:7687", User: "neo4j", Database: "neo4j"}
	_, err := Open(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "PATH check: pip install mcp-neo4j-cypher") {
		t.Fatalf("want spawn-failure install hint, got %v", err)
	}
}

func TestDecodeRows_Branches(t *testing.T) {
	if rows, err := decodeRows(nil); rows != nil || err != nil {
		t.Errorf("nil result: want nil,nil got %v,%v", rows, err)
	}
	if _, err := decodeRows([]byte("{bad")); err == nil {
		t.Error("bad envelope json should error")
	}
	if _, err := decodeRows([]byte(`{"content":[{"type":"text","text":"oops"}],"isError":true}`)); err == nil || !strings.Contains(err.Error(), "isError=true: oops") {
		t.Errorf("isError should surface detail, got %v", err)
	}
	if rows, err := decodeRows([]byte(`{"content":[],"isError":false}`)); rows != nil || err != nil {
		t.Errorf("empty content: want nil,nil got %v,%v", rows, err)
	}
	if _, err := decodeRows([]byte(`{"content":[{"type":"text","text":"not-an-array"}]}`)); err == nil {
		t.Error("bad rows json should error")
	}
	rows, err := decodeRows([]byte(`{"content":[{"type":"text","text":"[{\"id\":\"a\"}]"}]}`))
	if err != nil || len(rows) != 1 || rows[0]["id"] != "a" {
		t.Errorf("valid rows: got %v err %v", rows, err)
	}
	rows, err = decodeRows([]byte(`{"content":[{"type":"text","text":"{\"_contains_updates\": true, \"nodes_created\": 1}"}]}`))
	if err != nil || len(rows) != 1 || rows[0]["nodes_created"] != float64(1) {
		t.Errorf("write summary object: got %v err %v", rows, err)
	}
}

func TestPingEmbed_ErrorBranches(t *testing.T) {
	// Unreachable host -> client.Do error.
	if err := pingEmbed(context.Background(), "http://127.0.0.1:1", 768); err == nil ||
		!strings.Contains(err.Error(), "embed sidecar unreachable") {
		t.Fatalf("unreachable: want unreachable error, got %v", err)
	}
	// Non-200 status.
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv500.Close()
	if err := pingEmbed(context.Background(), srv500.URL, 768); err == nil ||
		!strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("non-200: want HTTP 500 error, got %v", err)
	}
	// Bad JSON body.
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srvBad.Close()
	if err := pingEmbed(context.Background(), srvBad.URL, 768); err == nil ||
		!strings.Contains(err.Error(), "decode") {
		t.Fatalf("bad json: want decode error, got %v", err)
	}
	// Empty data array -> dim 0 mismatch.
	srvEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srvEmpty.Close()
	if err := pingEmbed(context.Background(), srvEmpty.URL, 768); err == nil ||
		!strings.Contains(err.Error(), "dim=0") {
		t.Fatalf("empty data: want dim=0 mismatch, got %v", err)
	}
	// Malformed base URL -> http.NewRequestWithContext fails.
	if err := pingEmbed(context.Background(), "://bad", 768); err == nil {
		t.Fatal("malformed URL: want request-build error")
	}
}

// componentsResp builds the text returned by the dbms.components MCP tool.
func componentsResp(version string) string {
	return `[{"name":"Neo4j Kernel","versions":["` + version + `"],"edition":"community"}]`
}

func TestPingMCP_Branches(t *testing.T) {
	// recv EOF -> error.
	c, _ := newFakeClient("", "")
	if _, err := pingMCP(context.Background(), c); err == nil {
		t.Error("recv EOF: want error")
	}
	// good 5.26 version.
	c, _ = newFakeClient(componentsResp("5.26.26"), "")
	if v, err := pingMCP(context.Background(), c); err != nil || v != "5.26.26" {
		t.Errorf("good version: v=%q err=%v", v, err)
	}
	// wrong major.minor -> "unexpected".
	c, _ = newFakeClient(componentsResp("4.4.0"), "")
	if _, err := pingMCP(context.Background(), c); err == nil || !strings.Contains(err.Error(), "unexpected Neo4j version") {
		t.Errorf("wrong version: want unexpected, got %v", err)
	}
	// empty rows -> "no ... version row".
	c, _ = newFakeClient(`[]`, "")
	if _, err := pingMCP(context.Background(), c); err == nil || !strings.Contains(err.Error(), "no Neo4j Kernel version") {
		t.Errorf("empty rows: want no-version error, got %v", err)
	}
	// malformed content payload -> decodeRows error surfaces from pingMCP.
	c, _ = newFakeClient(`not-an-array`, "")
	if _, err := pingMCP(context.Background(), c); err == nil {
		t.Error("malformed rows: want decode error from pingMCP")
	}
}

func TestPing_Unit(t *testing.T) {
	// Happy: good MCP version + sidecar returning the default dim.
	embedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var sb strings.Builder
		sb.WriteString(`{"data":[{"embedding":[`)
		for i := range DefaultEmbedDimensions {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString("0.1")
		}
		sb.WriteString(`]}]}`)
		_, _ = w.Write([]byte(sb.String()))
	}))
	defer embedSrv.Close()

	c, _ := newFakeClient(componentsResp("5.26.26"), "")
	res, err := Ping(context.Background(), c, &Config{EmbedURL: embedSrv.URL, EmbedDimensions: DefaultEmbedDimensions})
	if err != nil || res.ServerVersion != "5.26.26" || res.EmbedDim != DefaultEmbedDimensions {
		t.Fatalf("Ping happy: res=%+v err=%v", res, err)
	}
	// MCP failure short-circuits before the embed probe.
	c, _ = newFakeClient(componentsResp("4.0.0"), "")
	if _, err := Ping(context.Background(), c, &Config{EmbedURL: embedSrv.URL, EmbedDimensions: DefaultEmbedDimensions}); err == nil {
		t.Error("Ping: want error on version mismatch")
	}
	// Good MCP version but embed sidecar returns the wrong dim -> Ping fails on probe.
	// The contract width is DefaultEmbedDimensions+1, not a literal: the mock emits
	// exactly DefaultEmbedDimensions, so any hardcoded number here stops being "wider"
	// the moment the default catches up with it — which is how this test broke.
	c, _ = newFakeClient(componentsResp("5.26.26"), "")
	if _, err := Ping(context.Background(), c, &Config{EmbedURL: embedSrv.URL, EmbedDimensions: DefaultEmbedDimensions + 1}); err == nil ||
		!strings.Contains(err.Error(), "refuse to start") {
		t.Errorf("Ping: want embed dim-mismatch error, got %v", err)
	}
}

func TestOpenSchema_ConnErr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// Port 1 refuses connections -> VerifyConnectivity fails.
	_, err := OpenSchema(ctx, &Config{BoltURL: "bolt://127.0.0.1:1", User: "neo4j", Password: "x", Database: "neo4j"})
	if err == nil || !strings.Contains(err.Error(), "connectivity") {
		t.Fatalf("want connectivity error, got %v", err)
	}
	// Unsupported URL scheme -> NewDriverWithContext itself errors (driver init).
	if _, err := OpenSchema(context.Background(), &Config{BoltURL: "ftp://x", User: "neo4j", Password: "x", Database: "neo4j"}); err == nil ||
		!strings.Contains(err.Error(), "driver init") {
		t.Fatalf("want driver init error, got %v", err)
	}
}

func TestKernelVersion(t *testing.T) {
	rows := []map[string]any{
		{"name": "other", "versions": []any{"9.9"}},
		{"name": "Neo4j Kernel", "versions": []any{"5.26.26"}},
	}
	if v := kernelVersion(rows); v != "5.26.26" {
		t.Errorf("want 5.26.26, got %q", v)
	}
	if v := kernelVersion([]map[string]any{{"name": "Neo4j Kernel"}}); v != "" {
		t.Errorf("missing versions: want empty, got %q", v)
	}
	if v := kernelVersion([]map[string]any{{"name": "Neo4j Kernel", "versions": []any{}}}); v != "" {
		t.Errorf("empty versions: want empty, got %q", v)
	}
	if v := kernelVersion(nil); v != "" {
		t.Errorf("no rows: want empty, got %q", v)
	}
}

func TestSplitCypherStatements(t *testing.T) {
	body := "// header comment\nCREATE X;\n\n// mid\nCREATE Y;\n   \n"
	got := splitCypherStatements(body)
	if len(got) != 2 || got[0] != "CREATE X" || got[1] != "CREATE Y" {
		t.Fatalf("split: %#v", got)
	}
	if len(splitCypherStatements("// only a comment\n   ")) != 0 {
		t.Error("comment/blank-only body should yield no statements")
	}
}

func TestLoadMigrations_Variants(t *testing.T) {
	// Valid synthetic migration.
	ok := fstest.MapFS{"migrations/0001_init.cypher": &fstest.MapFile{Data: []byte("CREATE X;")}}
	migs, err := loadMigrations(ok)
	if err != nil || len(migs) != 1 || migs[0].version != 1 || migs[0].name != "0001_init" {
		t.Fatalf("valid: migs=%v err=%v", migs, err)
	}
	if migs[0].checksum == "" {
		t.Error("checksum not computed")
	}
	// Non-.cypher and directory entries are skipped.
	mixed := fstest.MapFS{
		"migrations/0001_init.cypher": &fstest.MapFile{Data: []byte("CREATE X;")},
		"migrations/README.md":        &fstest.MapFile{Data: []byte("ignore me")},
	}
	if m, err := loadMigrations(mixed); err != nil || len(m) != 1 {
		t.Fatalf("mixed: want 1 migration, got %d err=%v", len(m), err)
	}
	// Bad filename -> parse error bubbles up.
	bad := fstest.MapFS{"migrations/nonumeric.cypher": &fstest.MapFile{Data: []byte("X;")}}
	if _, err := loadMigrations(bad); err == nil {
		t.Error("bad filename: want parse error")
	}
	// ReadDir failure.
	if _, err := loadMigrations(errDirFS{}); err == nil || !strings.Contains(err.Error(), "read embedded migrations") {
		t.Errorf("readdir error: got %v", err)
	}
}

func TestEmbeddedMigrationsIncludeDocumentSchemaAfterInitialChunkSchema(t *testing.T) {
	migs, err := loadMigrations(cypherFS)
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}
	var initIndex, docIndex = -1, -1
	for i, mig := range migs {
		switch mig.name {
		case "0001_init":
			initIndex = i
		case "0002_documents":
			docIndex = i
		}
	}
	if initIndex == -1 {
		t.Fatal("embedded migrations missing 0001_init")
	}
	if docIndex == -1 {
		t.Fatal("embedded migrations missing 0002_documents")
	}
	if initIndex >= docIndex {
		t.Fatalf("document schema must load after initial chunk schema: init=%d documents=%d", initIndex, docIndex)
	}
	if !strings.Contains(migs[docIndex].body, "CREATE CONSTRAINT document_id IF NOT EXISTS") {
		t.Fatal("document migration should create document_id constraint")
	}
	if !strings.Contains(migs[docIndex].body, "CREATE INDEX chunk_document_id IF NOT EXISTS") {
		t.Fatal("document migration should create chunk_document_id index")
	}
}

func TestParseMigrationName(t *testing.T) {
	v, name, err := parseMigrationName("0001_init.cypher")
	if err != nil || v != 1 || name != "0001_init" {
		t.Fatalf("valid: v=%d name=%q err=%v", v, name, err)
	}
	if _, _, err := parseMigrationName("noprefix.cypher"); err == nil {
		t.Error("missing underscore should error")
	}
	if _, _, err := parseMigrationName("abc_x.cypher"); err == nil {
		t.Error("non-numeric prefix should error")
	}
	if _, _, err := parseMigrationName("0000_zero.cypher"); err == nil {
		t.Error("version 0 should be rejected (out of range)")
	}
}
