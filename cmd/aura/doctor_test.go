package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/mcp"
)

func installDoctorFakeProbes(t *testing.T) {
	t.Helper()
	oldPostgres := doctorProbePostgres
	oldNeo4j := doctorProbeNeo4j
	oldEmbed := doctorProbeEmbed
	oldMCPBinary := doctorProbeMCPBinary
	oldMCPServers := doctorProbeMCPServers
	oldLLMKey := doctorLookupLLMKey
	t.Cleanup(func() {
		doctorProbePostgres = oldPostgres
		doctorProbeNeo4j = oldNeo4j
		doctorProbeEmbed = oldEmbed
		doctorProbeMCPBinary = oldMCPBinary
		doctorProbeMCPServers = oldMCPServers
		doctorLookupLLMKey = oldLLMKey
	})

	doctorProbePostgres = func(context.Context, *config.Config) (string, error) { return "reachable", nil }
	doctorProbeNeo4j = func(context.Context, *config.Config) (string, error) { return "RETURN 1 ok", nil }
	doctorProbeEmbed = func(context.Context, *config.Config) (string, error) { return "dimension 768", nil }
	doctorProbeMCPBinary = func(context.Context, *config.Config) (string, error) { return "found", nil }
	// Faked like every other doctorProbe: the real defaultDoctorProbeMCPServers reads
	// AURA_MCP_CONFIG (host state), which these generic pass/fail plumbing tests must
	// not depend on (same host-state hazard as the literal-8093 fixture fixed in
	// mcp_test.go — a real managed config on the dev box would make this probe dial a
	// real server during an unrelated unit test).
	doctorProbeMCPServers = func(context.Context, *config.Config) (string, error) { return "0/0 HTTP MCP servers reachable", nil }
	doctorLookupLLMKey = func() string { return "sk-test-doctor" }
}

type fakeDoctorPostgresPool struct {
	pingErr error
	closed  bool
}

func (f *fakeDoctorPostgresPool) Ping(context.Context) error {
	return f.pingErr
}

func (f *fakeDoctorPostgresPool) Close() {
	f.closed = true
}

type fakeDoctorNeo4jClient struct {
	rows    []map[string]any
	readErr error
	closed  bool
	query   string
}

func (f *fakeDoctorNeo4jClient) Read(_ context.Context, query string, _ map[string]any) ([]map[string]any, error) {
	f.query = query
	return f.rows, f.readErr
}

func (f *fakeDoctorNeo4jClient) Close() error {
	f.closed = true
	return nil
}

func TestDoctorAllPass(t *testing.T) {
	installDoctorFakeProbes(t)

	var out bytes.Buffer
	code := runDoctorWithConfig(context.Background(), &out, &config.Config{})

	if code != 0 {
		t.Fatalf("runDoctorWithConfig exit = %d, want 0; output:\n%s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{
		"PASS postgres:",
		"PASS neo4j:",
		"PASS embed:",
		"PASS mcp-neo4j-cypher:",
		"PASS llm_key: configured",
		"PASS mcp:",
		"status: OK",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "sk-test-doctor") {
		t.Fatalf("doctor output leaked the API key:\n%s", got)
	}
}

func TestDoctorHardFailures(t *testing.T) {
	cases := []struct {
		name     string
		wantCode int
		fail     func(error)
		wantLine string
	}{
		{
			name:     "postgres",
			wantCode: exitUnreachable,
			fail: func(err error) {
				doctorProbePostgres = func(context.Context, *config.Config) (string, error) { return "", err }
			},
			wantLine: "FAIL postgres:",
		},
		{
			name:     "neo4j",
			wantCode: exitUnreachable,
			fail: func(err error) {
				doctorProbeNeo4j = func(context.Context, *config.Config) (string, error) { return "", err }
			},
			wantLine: "FAIL neo4j:",
		},
		{
			name:     "embed",
			wantCode: exitUnreachable,
			fail: func(err error) {
				doctorProbeEmbed = func(context.Context, *config.Config) (string, error) { return "", err }
			},
			wantLine: "FAIL embed:",
		},
		{
			name:     "mcp binary",
			wantCode: exitInfra,
			fail: func(err error) {
				doctorProbeMCPBinary = func(context.Context, *config.Config) (string, error) { return "", err }
			},
			wantLine: "FAIL mcp-neo4j-cypher:",
		},
		{
			name:     "mcp servers",
			wantCode: exitUnreachable,
			fail: func(err error) {
				doctorProbeMCPServers = func(context.Context, *config.Config) (string, error) { return "", err }
			},
			wantLine: "FAIL mcp:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installDoctorFakeProbes(t)
			tc.fail(errors.New("boom"))

			var out bytes.Buffer
			code := runDoctorWithConfig(context.Background(), &out, &config.Config{})

			if code != tc.wantCode {
				t.Fatalf("exit = %d, want %d; output:\n%s", code, tc.wantCode, out.String())
			}
			if !strings.Contains(out.String(), tc.wantLine) || !strings.Contains(out.String(), "boom") {
				t.Fatalf("doctor output did not name failed check:\n%s", out.String())
			}
			if !strings.Contains(out.String(), "status: FAIL") {
				t.Fatalf("doctor output missing fail status:\n%s", out.String())
			}
		})
	}
}

func TestDoctorMissingLLMKeyIsInformational(t *testing.T) {
	installDoctorFakeProbes(t)
	doctorLookupLLMKey = func() string { return "" }

	var out bytes.Buffer
	code := runDoctorWithConfig(context.Background(), &out, &config.Config{})

	if code != 0 {
		t.Fatalf("missing LLM key should not hard-fail doctor, exit = %d; output:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "PASS llm_key: not configured") {
		t.Fatalf("doctor output missing keyless informational line:\n%s", out.String())
	}
	if strings.Contains(out.String(), "FAIL llm_key") {
		t.Fatalf("missing LLM key must not be a failed check:\n%s", out.String())
	}
}

func TestDoctorDefaultPostgresProbePingsAndCloses(t *testing.T) {
	oldOpen := doctorOpenPostgres
	t.Cleanup(func() { doctorOpenPostgres = oldOpen })
	pool := &fakeDoctorPostgresPool{}
	doctorOpenPostgres = func(context.Context, *config.Config) (doctorPostgresPool, error) {
		return pool, nil
	}

	detail, err := defaultDoctorProbePostgres(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("defaultDoctorProbePostgres: %v", err)
	}
	if !strings.Contains(detail, "reachable") {
		t.Fatalf("detail = %q, want reachable", detail)
	}
	if !pool.closed {
		t.Fatal("defaultDoctorProbePostgres did not close the pool")
	}
}

func TestDoctorDefaultPostgresProbeNamesEmptyURL(t *testing.T) {
	_, err := defaultDoctorProbePostgres(context.Background(), &config.Config{})
	if err == nil || !strings.Contains(err.Error(), "URL is empty") {
		t.Fatalf("defaultDoctorProbePostgres err = %v, want empty URL error", err)
	}
}

func TestDoctorDefaultNeo4jProbeRunsRoundTripAndCloses(t *testing.T) {
	oldOpen := doctorOpenNeo4j
	t.Cleanup(func() { doctorOpenNeo4j = oldOpen })
	client := &fakeDoctorNeo4jClient{rows: []map[string]any{{"ok": 1}}}
	doctorOpenNeo4j = func(context.Context, *config.Config) (doctorNeo4jClient, error) {
		return client, nil
	}

	detail, err := defaultDoctorProbeNeo4j(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("defaultDoctorProbeNeo4j: %v", err)
	}
	if detail != "RETURN 1 round-trip OK" {
		t.Fatalf("detail = %q, want round-trip OK", detail)
	}
	if client.query != "RETURN 1 AS ok" {
		t.Fatalf("query = %q, want RETURN 1 AS ok", client.query)
	}
	if !client.closed {
		t.Fatal("defaultDoctorProbeNeo4j did not close the client")
	}
}

func TestDoctorDefaultNeo4jProbeNamesSpawnFailure(t *testing.T) {
	cfg := &config.Config{Neo4j: knowledge.Config{
		MCPBinary: "aura-nonexistent-mcp-binary-xyz",
		BoltURL:   "bolt://127.0.0.1:7687",
		User:      "neo4j",
		Database:  "neo4j",
	}}
	_, err := defaultDoctorProbeNeo4j(context.Background(), cfg)
	if err == nil || !strings.Contains(err.Error(), "spawn aura-nonexistent-mcp-binary-xyz") {
		t.Fatalf("defaultDoctorProbeNeo4j err = %v, want spawn failure", err)
	}
}

func TestDoctorDefaultEmbedProbeChecksDimension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path = %s, want /v1/embeddings", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{{"embedding": []float64{0.1, 0.2, 0.3}}},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	oldClient := doctorHTTPClient
	doctorHTTPClient = srv.Client()
	t.Cleanup(func() { doctorHTTPClient = oldClient })

	cfg := &config.Config{Neo4j: knowledge.Config{EmbedURL: srv.URL, EmbedDimensions: 3}}
	detail, err := defaultDoctorProbeEmbed(context.Background(), cfg)
	if err != nil {
		t.Fatalf("defaultDoctorProbeEmbed: %v", err)
	}
	if detail != "dimension 3" {
		t.Fatalf("detail = %q, want dimension 3", detail)
	}
}

func TestDoctorDefaultMCPBinaryProbeUsesLookPath(t *testing.T) {
	oldLookPath := doctorLookPath
	t.Cleanup(func() { doctorLookPath = oldLookPath })
	doctorLookPath = func(name string) (string, error) {
		if name != "mcp-neo4j-cypher" {
			t.Fatalf("lookPath name = %q", name)
		}
		return "/usr/local/bin/mcp-neo4j-cypher", nil
	}

	detail, err := defaultDoctorProbeMCPBinary(context.Background(), &config.Config{
		Neo4j: knowledge.Config{MCPBinary: "mcp-neo4j-cypher"},
	})
	if err != nil {
		t.Fatalf("defaultDoctorProbeMCPBinary: %v", err)
	}
	if !strings.Contains(detail, "/usr/local/bin/mcp-neo4j-cypher") {
		t.Fatalf("detail = %q, want resolved path", detail)
	}
}

// TestDoctorChecksIncludesMCPServers proves the 6th check (D-16/D-18) is registered
// in the doctor registry, distinct from the unrelated mcp-neo4j-cypher binary check.
// Position is not itself a contract, so this scans by name rather than asserting index.
func TestDoctorChecksIncludesMCPServers(t *testing.T) {
	checks := doctorChecks()
	if len(checks) != 6 {
		t.Fatalf("doctorChecks() len = %d, want 6 (5 pre-existing + the new mcp check)", len(checks))
	}
	for _, c := range checks {
		if c.name != "mcp" {
			continue
		}
		if c.failureCode != exitUnreachable {
			t.Fatalf("mcp check failureCode = %d, want exitUnreachable (%d)", c.failureCode, exitUnreachable)
		}
		return
	}
	t.Fatalf("doctorChecks() missing the \"mcp\" 6th check: %+v", checks)
}

// TestDoctorProbeMCPServersReachable proves a reachable, enabled HTTP MCP server
// passes with an aggregated "N/N reachable" detail.
func TestDoctorProbeMCPServersReachable(t *testing.T) {
	path := withTempMCPConfig(t)
	srv := newMCPHTTPTestServer(t)
	defer srv.Close()

	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"good": {Type: mcp.ServerTypeStreamableHTTP, URL: srv.URL, Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}

	detail, err := defaultDoctorProbeMCPServers(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("defaultDoctorProbeMCPServers: %v", err)
	}
	if detail != "1/1 HTTP MCP servers reachable" {
		t.Fatalf("detail = %q, want 1/1 reachable", detail)
	}
}

func TestDoctorProbeMCPServersUsesResolvedRuntimeConfig(t *testing.T) {
	withTempMCPConfig(t)
	srv := newMCPHTTPTestServer(t)
	defer srv.Close()

	cfg := &config.Config{MCPPolicies: map[string]mcp.ManagedServer{
		"default-on": {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    srv.URL,
			Source: "recipe:test",
			Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}
	detail, err := defaultDoctorProbeMCPServers(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if detail != "1/1 HTTP MCP servers reachable" {
		t.Fatalf("detail = %q, want resolved default-on server", detail)
	}
}

// TestDoctorProbeMCPServersUnreachableNamesServer proves a dead HTTP MCP server
// fails the check and names the unreachable server in the aggregated error.
func TestDoctorProbeMCPServersUnreachableNamesServer(t *testing.T) {
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"deadhttp": {Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:0/mcp", Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}

	_, err := defaultDoctorProbeMCPServers(context.Background(), &config.Config{})
	if err == nil {
		t.Fatal("defaultDoctorProbeMCPServers: want an error for an unreachable HTTP server")
	}
	if !strings.Contains(err.Error(), "deadhttp") {
		t.Fatalf("error = %v, want it to name the unreachable server", err)
	}
}

// TestDoctorProbeMCPServersSkipsDisabledBlockedStdio proves D-18's scope: a
// disabled HTTP server, a trust-blocked HTTP server, and a stdio server are never
// dialed by this check — only the one enabled+runnable+HTTP server counts, so the
// aggregated detail stays "1/1" even though three OTHER servers are configured and
// would each fail if actually probed (dead command / unreachable URL).
func TestDoctorProbeMCPServersSkipsDisabledBlockedStdio(t *testing.T) {
	path := withTempMCPConfig(t)
	srv := newMCPHTTPTestServer(t)
	defer srv.Close()
	disabled := false

	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"good":    {Type: mcp.ServerTypeStreamableHTTP, URL: srv.URL, Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		"off":     {Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:0/mcp", Enabled: &disabled, Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
		"blocked": {Type: mcp.ServerTypeStreamableHTTP, URL: "http://127.0.0.1:0/mcp", Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustBlocked}},
		"shell":   {Command: "aura-nonexistent-mcp-binary-xyz", Source: "manual", Trust: mcp.ManagedTrust{Class: mcp.TrustTrustedLocal}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}

	detail, err := defaultDoctorProbeMCPServers(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("defaultDoctorProbeMCPServers: %v (should skip off/blocked/shell entirely)", err)
	}
	if detail != "1/1 HTTP MCP servers reachable" {
		t.Fatalf("detail = %q, want 1/1 reachable (off/blocked/shell must be skipped, not counted)", detail)
	}
}

// TestDoctorProbeMCPServersNoneConfigured proves an empty/absent managed config is
// an informational pass, not a failure.
func TestDoctorProbeMCPServersNoneConfigured(t *testing.T) {
	withTempMCPConfig(t)

	detail, err := defaultDoctorProbeMCPServers(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("defaultDoctorProbeMCPServers: %v", err)
	}
	if detail != "no HTTP MCP servers configured" {
		t.Fatalf("detail = %q, want the no-servers informational message", detail)
	}
}

// TestDoctorProbeMCPServersBoundedByProbeTimeout proves a hung HTTP endpoint fails
// this check within ~AURA_MCP_PROBE_TIMEOUT instead of blocking the whole doctor
// run indefinitely (T-38-10b). The handler self-bounds its own block with a 3s
// fallback in addition to watching r.Context().Done() — see
// TestWriteRuntimeCheckBoundedByProbeTimeout in mcp_status_test.go for why relying
// solely on client-side context cancellation to close the server's connection is
// not deterministic across platforms; this keeps the test itself from hanging past
// a bounded ceiling regardless.
func TestDoctorProbeMCPServersBoundedByProbeTimeout(t *testing.T) {
	t.Setenv("AURA_MCP_PROBE_TIMEOUT", "1")
	path := withTempMCPConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	defer srv.Close()

	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		"hung": {Type: mcp.ServerTypeStreamableHTTP, URL: srv.URL, Source: "manual:http", Trust: mcp.ManagedTrust{Class: mcp.TrustRemoteHTTP}},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save config: %v", err)
	}

	start := time.Now()
	_, err := defaultDoctorProbeMCPServers(context.Background(), &config.Config{})
	elapsed := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "hung") {
		t.Fatalf("defaultDoctorProbeMCPServers(hung): err = %v, want it to name the hung server", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("defaultDoctorProbeMCPServers(hung) took %v, want bounded by ~1s AURA_MCP_PROBE_TIMEOUT", elapsed)
	}
}
