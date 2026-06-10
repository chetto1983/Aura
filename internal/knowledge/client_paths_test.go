// Unit-safe branch coverage (NO build tag) for the MCP client and pure helpers.
// The client is constructed with fake stdin/stdout (this file is package
// knowledge, so unexported fields are reachable), which exercises every
// Cypher/initialize/decode error path without spawning a real subprocess.
package knowledge

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/chetto1983/aura/internal/boundedbuffer"
)

// errDirFS is an fs.FS whose Open always fails, so fs.ReadDir errors.
type errDirFS struct{}

func (errDirFS) Open(string) (fs.File, error) { return nil, errors.New("open failed") }

// fakeStdin is an io.WriteCloser whose Write can be forced to fail — either
// always (writeErr) or on the Nth write (failOnWriteN, 1-based).
type fakeStdin struct {
	buf          bytes.Buffer
	writeErr     error
	failOnWriteN int
	writes       int
	closed       bool
}

func (f *fakeStdin) Write(p []byte) (int, error) {
	f.writes++
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.failOnWriteN != 0 && f.writes == f.failOnWriteN {
		return 0, errors.New("write failed")
	}
	return f.buf.Write(p)
}

func (f *fakeStdin) Close() error { f.closed = true; return nil }

// newFakeClient wires a Client to canned stdout bytes and a capturable stdin.
func newFakeClient(stdout, password string) (*Client, *fakeStdin) {
	in := &fakeStdin{}
	c := &Client{
		stdin:    in,
		stdout:   bufio.NewReader(strings.NewReader(stdout)),
		stderr:   boundedbuffer.New(0),
		password: password,
	}
	return c, in
}

func TestCypher_Happy(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[{\"one\":1}]"}],"isError":false}}` + "\n"
	c, in := newFakeClient(resp, "")
	raw, err := c.Cypher(context.Background(), "RETURN 1", nil, false)
	if err != nil {
		t.Fatalf("Cypher: %v", err)
	}
	rows, err := decodeRows(raw)
	if err != nil || len(rows) != 1 {
		t.Fatalf("decodeRows: rows=%v err=%v", rows, err)
	}
	if !strings.Contains(in.buf.String(), `"tools/call"`) {
		t.Errorf("request not written to stdin: %q", in.buf.String())
	}
}

func TestCypher_RPCError(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"boom password=secret"}}` + "\n"
	c, _ := newFakeClient(resp, "secret")
	_, err := c.Cypher(context.Background(), "RETURN 1", nil, false)
	if err == nil || !strings.Contains(err.Error(), "cypher error -32000") {
		t.Fatalf("want rpc error, got %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("rpc error leaked password: %v", err)
	}
}

func TestCypher_RecvEOF(t *testing.T) {
	c, _ := newFakeClient("", "") // empty stdout -> immediate EOF
	_, err := c.Cypher(context.Background(), "RETURN 1", nil, false)
	if err == nil || !strings.Contains(err.Error(), crashHint) {
		t.Fatalf("want crash hint on recv EOF, got %v", err)
	}
}

func TestCypher_SendError(t *testing.T) {
	c, in := newFakeClient("", "")
	in.writeErr = errors.New("pipe broken")
	_, err := c.Cypher(context.Background(), "RETURN 1", nil, true)
	if err == nil || !strings.Contains(err.Error(), crashHint) {
		t.Fatalf("want crash hint on send error, got %v", err)
	}
}

func TestCypher_BadJSON(t *testing.T) {
	c, _ := newFakeClient("not-json\n", "")
	_, err := c.Cypher(context.Background(), "RETURN 1", nil, false)
	if err == nil || !strings.Contains(err.Error(), "decode cypher response") {
		t.Fatalf("want decode error, got %v", err)
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

func TestInitialize_Happy(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05"}}` + "\n"
	c, in := newFakeClient(resp, "")
	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if !strings.Contains(in.buf.String(), `"initialize"`) || !strings.Contains(in.buf.String(), "notifications/initialized") {
		t.Errorf("handshake messages missing: %q", in.buf.String())
	}
}

func TestInitialize_ServerError(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-1,"message":"nope"}}` + "\n"
	c, _ := newFakeClient(resp, "")
	if err := c.initialize(); err == nil || !strings.Contains(err.Error(), "initialize error") {
		t.Fatalf("want initialize error, got %v", err)
	}
}

func TestInitialize_RecvEOF(t *testing.T) {
	c, _ := newFakeClient("", "")
	if err := c.initialize(); err == nil || !strings.Contains(err.Error(), crashHint) {
		t.Fatalf("want crash hint, got %v", err)
	}
}

func TestInitialize_BadJSON(t *testing.T) {
	c, _ := newFakeClient("garbage\n", "")
	if err := c.initialize(); err == nil || !strings.Contains(err.Error(), "decode initialize") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestInitialize_NotificationSendError(t *testing.T) {
	// Write 1 (initialize) succeeds, response reads OK, write 2 (notification) fails.
	in := &fakeStdin{failOnWriteN: 2}
	c := &Client{
		stdin:  in,
		stdout: bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}` + "\n")),
		stderr: boundedbuffer.New(0),
	}
	if err := c.initialize(); err == nil || !strings.Contains(err.Error(), "initialized notification") {
		t.Fatalf("want initialized-notification send error, got %v", err)
	}
}

func TestClose_NilCmd(t *testing.T) {
	c, in := newFakeClient("", "")
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !in.closed {
		t.Error("Close did not close stdin")
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

func TestStderrTail(t *testing.T) {
	c := &Client{stderr: boundedbuffer.New(0), password: "topsecret"}
	if c.stderrTail() != "" {
		t.Error("empty stderr should yield empty tail")
	}
	_, _ = c.stderr.Write([]byte("auth failed password=topsecret extra"))
	tail := c.stderrTail()
	if strings.Contains(tail, "topsecret") {
		t.Errorf("stderrTail leaked secret: %q", tail)
	}
	// Truncation: >200 bytes keeps only the suffix.
	c2 := &Client{stderr: boundedbuffer.New(0)}
	_, _ = c2.stderr.Write(bytes.Repeat([]byte("x"), 500))
	if len(c2.stderrTail()) > 210 {
		t.Errorf("stderrTail not truncated: len=%d", len(c2.stderrTail()))
	}
}

func TestBoundedStderrBufferKeepsTail(t *testing.T) {
	buf := boundedbuffer.New(64)
	_, _ = buf.Write(bytes.Repeat([]byte("a"), 120))
	_, _ = buf.Write([]byte("NEO4J-END"))
	if got := buf.Len(); got > 64 {
		t.Fatalf("buffer len = %d, want <= 64", got)
	}
	c := &Client{stderr: buf}
	if tail := c.stderrTail(); !strings.Contains(tail, "NEO4J-END") {
		t.Fatalf("stderr tail lost newest bytes: %q", tail)
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

// componentsResp builds a canned dbms.components MCP result line.
func componentsResp(version string) string {
	inner := `[{\"name\":\"Neo4j Kernel\",\"versions\":[\"` + version + `\"],\"edition\":\"community\"}]`
	return `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"` + inner + `"}],"isError":false}}` + "\n"
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
	c, _ = newFakeClient(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"[]"}],"isError":false}}`+"\n", "")
	if _, err := pingMCP(context.Background(), c); err == nil || !strings.Contains(err.Error(), "no Neo4j Kernel version") {
		t.Errorf("empty rows: want no-version error, got %v", err)
	}
	// malformed content payload -> decodeRows error surfaces from pingMCP.
	c, _ = newFakeClient(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"not-an-array"}],"isError":false}}`+"\n", "")
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
	c, _ = newFakeClient(componentsResp("5.26.26"), "")
	if _, err := Ping(context.Background(), c, &Config{EmbedURL: embedSrv.URL, EmbedDimensions: 1024}); err == nil ||
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
