package arcadedb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func mustClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: baseURL, Database: "aura", User: "root", Password: "pw"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

type captured struct {
	path    string
	auth    string
	ctype   string
	payload map[string]any
}

// fakeServer answers every request with body and records what it received.
func fakeServer(t *testing.T, status int, body string, seen *captured) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if seen != nil {
			seen.path = r.URL.Path
			seen.auth = r.Header.Get("Authorization")
			seen.ctype = r.Header.Get("Content-Type")
			seen.payload = map[string]any{}
			_ = json.Unmarshal(raw, &seen.payload)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	cases := map[string]Config{
		"empty base URL": {Database: "aura", User: "root"},
		"blank base URL": {BaseURL: "   ", Database: "aura", User: "root"},
		"bad scheme":     {BaseURL: "ftp://host:2480", Database: "aura", User: "root"},
		"unparsable":     {BaseURL: "http://[::1", Database: "aura", User: "root"},
		"empty database": {BaseURL: "http://host:2480", User: "root"},
		"blank database": {BaseURL: "http://host:2480", Database: " ", User: "root"},
		"empty user":     {BaseURL: "http://host:2480", Database: "aura"},
		"blank user":     {BaseURL: "http://host:2480", Database: "aura", User: "\t"},
		"negative memory limit": {
			BaseURL: "http://host:2480", Database: "aura", User: "root",
			MemoryLimits: MemoryLimits{QueryRunes: -1},
		},
		"negative relevance limit": {
			BaseURL: "http://host:2480", Database: "aura", User: "root",
			MemoryLimits: MemoryLimits{DenseMaxDistance: -0.1},
		},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(cfg); err == nil {
				t.Fatal("expected an error, got nil")
			}
		})
	}
}

func TestNewDefaultsTimeoutAndTrimsBaseURL(t *testing.T) {
	c, err := New(Config{BaseURL: "  http://host:2480///  ", Database: "aura", User: "root"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.http.Timeout != DefaultTimeout {
		t.Fatalf("timeout = %v, want %v", c.http.Timeout, DefaultTimeout)
	}
	if c.queryURL != "http://host:2480/api/v1/query/aura" {
		t.Fatalf("queryURL = %q", c.queryURL)
	}
}

func TestNewHonoursExplicitTimeout(t *testing.T) {
	c, err := New(Config{BaseURL: "http://host:2480", Database: "aura", User: "root", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.http.Timeout != 5*time.Second {
		t.Fatalf("timeout = %v, want 5s", c.http.Timeout)
	}
}

func TestNewDefaultsMemoryLimits(t *testing.T) {
	c, err := New(Config{BaseURL: "http://host:2480", Database: "aura", User: "root"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	want := MemoryLimits{
		QueryRunes: 2048, EntityRunes: 512, StatementRunes: 4096,
		PredicateRunes: 100, SourceRunIDRunes: 100, SourceMemoryIDRunes: 100,
		SourceMemoryIDs: 64, Results: 100, DigestFactsPerEntity: 20,
		MaintenanceBatch: 100, DigestScan: 2000, HybridCandidates: 400,
		DenseMaxDistance: 0.55, LexicalMinScore: 2,
	}
	if c.limits != want {
		t.Fatalf("limits = %+v, want %+v", c.limits, want)
	}
}

func TestNewHonoursMemoryLimitOverrides(t *testing.T) {
	want := MemoryLimits{
		QueryRunes: 101, EntityRunes: 102, StatementRunes: 103,
		PredicateRunes: 104, SourceRunIDRunes: 105, SourceMemoryIDRunes: 106,
		SourceMemoryIDs: 107, Results: 108, DigestFactsPerEntity: 109,
		MaintenanceBatch: 110, DigestScan: 111, HybridCandidates: 112,
		DenseMaxDistance: 0.25, LexicalMinScore: 3.5,
	}
	c, err := New(Config{
		BaseURL: "http://host:2480", Database: "aura", User: "root", MemoryLimits: want,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.limits != want {
		t.Fatalf("limits = %+v, want %+v", c.limits, want)
	}
}

// A database name is user-controlled in a multi-tenant deployment, so it must
// not be able to escape its path segment.
func TestNewEscapesDatabaseNameInPath(t *testing.T) {
	c, err := New(Config{BaseURL: "http://host:2480", Database: "a/../b", User: "root"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if strings.Contains(c.queryURL, "a/../b") {
		t.Fatalf("database name not escaped: %q", c.queryURL)
	}
}

func TestQueryDecodesResultRows(t *testing.T) {
	var seen captured
	srv := fakeServer(t, http.StatusOK, `{"user":"root","result":[{"n":2},{"n":3}]}`, &seen)
	rows, err := mustClient(t, srv.URL).Query(context.Background(), "SELECT 1", nil)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0]["n"] != float64(2) {
		t.Fatalf("rows[0][n] = %v", rows[0]["n"])
	}
}

func TestQuerySendsStatementAuthAndParams(t *testing.T) {
	var seen captured
	srv := fakeServer(t, http.StatusOK, `{"result":[]}`, &seen)
	params := map[string]any{"id": "chunk_1"}
	if _, err := mustClient(t, srv.URL).Query(context.Background(), "SELECT FROM Chunk", params); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if seen.path != "/api/v1/query/aura" {
		t.Fatalf("path = %q", seen.path)
	}
	if seen.ctype != "application/json" {
		t.Fatalf("content-type = %q", seen.ctype)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("root:pw"))
	if seen.auth != want {
		t.Fatalf("authorization = %q, want %q", seen.auth, want)
	}
	if seen.payload["language"] != "sql" {
		t.Fatalf("language = %v", seen.payload["language"])
	}
	if seen.payload["command"] != "SELECT FROM Chunk" {
		t.Fatalf("command = %v", seen.payload["command"])
	}
	got, ok := seen.payload["params"].(map[string]any)
	if !ok || got["id"] != "chunk_1" {
		t.Fatalf("params = %v", seen.payload["params"])
	}
}

// An empty params map must not appear on the wire: ArcadeDB treats a present
// but empty `params` differently from an absent one for some statements.
func TestQueryOmitsEmptyParams(t *testing.T) {
	var seen captured
	srv := fakeServer(t, http.StatusOK, `{"result":[]}`, &seen)
	if _, err := mustClient(t, srv.URL).Query(context.Background(), "SELECT 1", map[string]any{}); err != nil {
		t.Fatalf("Query: %v", err)
	}
	if _, present := seen.payload["params"]; present {
		t.Fatal("params must be omitted when empty")
	}
}

func TestCommandUsesTheCommandEndpoint(t *testing.T) {
	var seen captured
	srv := fakeServer(t, http.StatusOK, `{"result":[]}`, &seen)
	if _, err := mustClient(t, srv.URL).Command(context.Background(), "CREATE VERTEX TYPE X", nil); err != nil {
		t.Fatalf("Command: %v", err)
	}
	if seen.path != "/api/v1/command/aura" {
		t.Fatalf("path = %q, want the command endpoint", seen.path)
	}
}

func TestEmptyStatementIsRejectedBeforeAnyRequest(t *testing.T) {
	var seen captured
	srv := fakeServer(t, http.StatusOK, `{"result":[]}`, &seen)
	for _, statement := range []string{"", "   ", "\n\t"} {
		if _, err := mustClient(t, srv.URL).Query(context.Background(), statement, nil); !errors.Is(err, ErrEmptyStatement) {
			t.Fatalf("statement %q: err = %v, want ErrEmptyStatement", statement, err)
		}
	}
	if seen.path != "" {
		t.Fatal("no request should have been sent")
	}
}

// The parse error ArcadeDB returns names the offending column; losing it turns
// a five-second fix into an afternoon.
func TestServerErrorPreservesArcadeDBDetail(t *testing.T) {
	body := `{"error":"Cannot execute command","detail":"SQL syntax error at line 1, column 247: mismatched input ','","exception":"com.arcadedb.exception.CommandSQLParsingException"}`
	srv := fakeServer(t, http.StatusInternalServerError, body, nil)
	_, err := mustClient(t, srv.URL).Query(context.Background(), "CREATE EDGE X SET end = 1", nil)
	var serverErr *ServerError
	if !errors.As(err, &serverErr) {
		t.Fatalf("err = %v, want *ServerError", err)
	}
	if serverErr.Status != http.StatusInternalServerError {
		t.Fatalf("status = %d", serverErr.Status)
	}
	if !strings.Contains(serverErr.Detail, "column 247") {
		t.Fatalf("detail lost the column: %q", serverErr.Detail)
	}
	if serverErr.Exception != "com.arcadedb.exception.CommandSQLParsingException" {
		t.Fatalf("exception = %q", serverErr.Exception)
	}
	if !strings.Contains(err.Error(), "column 247") {
		t.Fatalf("Error() lost the detail: %q", err.Error())
	}
}

func TestServerErrorFallsBackToRawBody(t *testing.T) {
	srv := fakeServer(t, http.StatusUnauthorized, "not json at all", nil)
	_, err := mustClient(t, srv.URL).Query(context.Background(), "SELECT 1", nil)
	var serverErr *ServerError
	if !errors.As(err, &serverErr) {
		t.Fatalf("err = %v, want *ServerError", err)
	}
	if serverErr.Detail != "not json at all" {
		t.Fatalf("detail = %q", serverErr.Detail)
	}
}

func TestMalformedSuccessBodyIsAnError(t *testing.T) {
	srv := fakeServer(t, http.StatusOK, `{"result": not-json}`, nil)
	if _, err := mustClient(t, srv.URL).Query(context.Background(), "SELECT 1", nil); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestUnreachableServerIsWrapped(t *testing.T) {
	srv := fakeServer(t, http.StatusOK, `{"result":[]}`, nil)
	url := srv.URL
	srv.Close()
	c := mustClient(t, url)
	if _, err := c.Query(context.Background(), "SELECT 1", nil); err == nil {
		t.Fatal("expected a transport error")
	}
}

func TestCancelledContextStopsTheRequest(t *testing.T) {
	srv := fakeServer(t, http.StatusOK, `{"result":[]}`, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := mustClient(t, srv.URL).Query(ctx, "SELECT 1", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// --- erasure proofs (DatabaseExists / CredentialAccepted) ---

func TestDatabaseExistsReadsTheServersAnswer(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    bool
		wantErr bool
	}{
		{name: "present", status: http.StatusOK, body: `{"result":true}`, want: true},
		{name: "erased", status: http.StatusOK, body: `{"result":false}`},
		{name: "refused", status: http.StatusForbidden, body: `{"error":"no"}`, wantErr: true},
		{name: "malformed", status: http.StatusOK, body: `{"result":`, wantErr: true},
		// A body with no `result` is unreadable, NOT a "false". Reading it as
		// erased would certify a purge on an answer the server never gave.
		{name: "no result field", status: http.StatusOK, body: `{}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen captured
			srv := fakeServer(t, tt.status, tt.body, &seen)
			got, err := mustClient(t, srv.URL).DatabaseExists(context.Background(), "mem_x")
			if tt.wantErr {
				if err == nil {
					t.Fatalf("exists = %t, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DatabaseExists: %v", err)
			}
			if got != tt.want {
				t.Fatalf("exists = %t, want %t", got, tt.want)
			}
			if seen.path != "/api/v1/exists/mem_x" {
				t.Fatalf("path = %q, want /api/v1/exists/mem_x", seen.path)
			}
			if seen.auth == "" {
				t.Fatal("the existence probe went out unauthenticated")
			}
		})
	}
}

func TestDatabaseExistsRejectsAnEmptyName(t *testing.T) {
	srv := fakeServer(t, http.StatusOK, `{"result":true}`, nil)
	if _, err := mustClient(t, srv.URL).DatabaseExists(context.Background(), "  "); err == nil {
		t.Fatal("an empty database name was probed")
	}
}

func TestDatabaseExistsFailsOnAnUnreachableServer(t *testing.T) {
	srv := fakeServer(t, http.StatusOK, `{"result":false}`, nil)
	url := srv.URL
	srv.Close()
	if _, err := mustClient(t, url).DatabaseExists(context.Background(), "mem_x"); err == nil {
		t.Fatal("an unreachable server answered that the database is gone")
	}
}

// The (bool, error) split is the point: false means the server REFUSED the
// credential, an error means the answer is unknown. A server that is merely down
// must never read as a revoked credential.
func TestCredentialAcceptedSeparatesRefusalFromUnreachable(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		want    bool
		wantErr bool
	}{
		{name: "accepted", status: http.StatusNoContent, want: true},
		{name: "unauthorized", status: http.StatusUnauthorized},
		{name: "forbidden", status: http.StatusForbidden},
		{name: "unexpected ok", status: http.StatusOK, wantErr: true},
		{name: "server error", status: http.StatusInternalServerError, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen captured
			srv := fakeServer(t, tt.status, `{}`, &seen)
			got, err := mustClient(t, srv.URL).CredentialAccepted(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("accepted = %t, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("CredentialAccepted: %v", err)
			}
			if got != tt.want {
				t.Fatalf("accepted = %t, want %t", got, tt.want)
			}
			if seen.path != "/api/v1/ready" {
				t.Fatalf("path = %q, want /api/v1/ready", seen.path)
			}
		})
	}
}

func TestCredentialAcceptedFailsOnAnUnreachableServer(t *testing.T) {
	srv := fakeServer(t, http.StatusUnauthorized, `{}`, nil)
	url := srv.URL
	srv.Close()
	if _, err := mustClient(t, url).CredentialAccepted(context.Background()); err == nil {
		t.Fatal("an unreachable server was read as a revoked credential")
	}
}
