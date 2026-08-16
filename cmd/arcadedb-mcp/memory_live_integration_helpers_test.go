//go:build arcadedb_integration

// memory_live_integration_helpers_test.go holds the harness
// memory_live_integration_test.go's three live-scenario tests share: session
// construction against the real ArcadeDB + embedder + tenant provisioning, the
// _meta-stamping call helper (D-108), and cleanup. Split out at the ≤600-LOC
// refactor-on-touch threshold (CLAUDE.md NO GOD CLASS) once this plan's _meta
// migration pushed the single file to 606 lines.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	officialmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/arcadedb"
	auramcp "github.com/chetto1983/aura/internal/mcp"
)

func newAgentMemoryLiveMCP(
	t *testing.T,
	identityCount int,
	operatorDisplayName string,
) (*officialmcp.ClientSession, []string, agentMemoryRuntimeEvidence) {
	t.Helper()
	adminPassword := os.Getenv("ARCADEDB_ADMIN_PASSWORD")
	if strings.TrimSpace(adminPassword) == "" {
		adminPassword = os.Getenv("ARCADEDB_PASSWORD")
	}
	if strings.TrimSpace(adminPassword) == "" {
		agentMemoryLiveGap(t, "ARCADEDB_PASSWORD or ARCADEDB_ADMIN_PASSWORD is not set")
	}
	if strings.TrimSpace(os.Getenv("AURA_ARCADEDB_TENANT_SECRET")) == "" {
		agentMemoryLiveGap(t, "AURA_ARCADEDB_TENANT_SECRET is not set")
	}

	base := arcadedb.Config{
		BaseURL:  agentMemoryLiveEnv("ARCADEDB_URL", "http://127.0.0.1:2480"),
		Database: agentMemoryLiveEnv("ARCADEDB_DATABASE", "aura_memory"),
		User:     agentMemoryLiveEnv("ARCADEDB_ADMIN_USER", "root"),
		Password: adminPassword,
		Timeout:  60 * time.Second,
	}
	admin, err := arcadedb.New(base)
	if err != nil {
		t.Fatalf("create ArcadeDB admin client: %v", err)
	}
	probeCtx, probeCancel := context.WithTimeout(t.Context(), time.Minute)
	defer probeCancel()
	arcadeDBVersion, err := admin.ServerVersion(probeCtx)
	if err != nil {
		agentMemoryLiveGap(t, "ArcadeDB is unreachable: %v", err)
	}
	if err := admin.VerifySecureVersion(probeCtx); err != nil {
		t.Fatalf("ArcadeDB isolation version check: %v", err)
	}

	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	embedURL := agentMemoryLiveEnv("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081")
	embedder := arcadedb.NewSidecarEmbedder(
		embedURL, os.Getenv("AURA_EMBED_MODEL"), os.Getenv("AURA_EMBED_API_KEY"), 60*time.Second)
	if embedder == nil {
		agentMemoryLiveGap(t, "EmbeddingGemma endpoint is not configured")
	}
	vectors, err := embedder.Embed(probeCtx, []string{"task: search result | query: integration health probe"})
	if err != nil {
		agentMemoryLiveGap(t, "EmbeddingGemma is unreachable: %v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) != 768 {
		t.Fatalf("EmbeddingGemma returned shape %d x %d, want 1 x 768",
			len(vectors), agentMemoryLiveVectorWidth(vectors))
	}
	embeddingDimension := len(vectors[0])

	identities := make([]string, identityCount)
	for i := range identities {
		identities[i] = uuid.NewString()
	}
	t.Cleanup(func() {
		cleanupAgentMemoryLiveTenants(t, admin, base, credentials, identities)
	})

	server := newServer(newTenants(base, admin, embedder, credentials), time.Now, operatorDisplayName)
	httpServer := httptest.NewServer(officialmcp.NewStreamableHTTPHandler(
		func(*http.Request) *officialmcp.Server { return server }, nil))
	t.Cleanup(httpServer.Close)
	// Closing the CLIENT session does not close the server's half of it. The SDK keeps a
	// per-session jsonrpc2 read loop alive on the server (streamableServerConn.Read), and
	// httptest's Close only waits for outstanding REQUESTS — a streamable session is not
	// one. That loop is what goleak reported as a leak on 2026-08-16, failing the live tier
	// (and with it the MRS gate at 86.00) while every assertion in the test passed.
	//
	// Registered here so cleanup order is: client session, then server sessions, then the
	// HTTP server (t.Cleanup is LIFO).
	t.Cleanup(func() {
		for serverSession := range server.Sessions() {
			if err := serverSession.Close(); err != nil {
				t.Errorf("close live MCP server session: %v", err)
			}
		}
	})

	// D-108: the bespoke auramcp.HTTPClient/OpenHTTP/HTTPConfig.ToolIdentityArgument
	// argument-stamping path is DELETED (plan 45.1-03). This is the same
	// OpenSDKSession construction path production and calendar_integration_test.go
	// use — identity now travels in _meta, stamped per call by
	// callAgentMemoryLiveJSON, never as a launch-time argument-injection config.
	managed := auramcp.ManagedServer{
		Type:  auramcp.ServerTypeStreamableHTTP,
		URL:   httpServer.URL,
		Trust: auramcp.ManagedTrust{Class: auramcp.TrustTrustedRecipe},
	}
	session, err := auramcp.OpenSDKSession(t.Context(), "agent-memory-live", managed, auramcp.EgressPolicy{}, auramcp.SessionOptions{})
	if err != nil {
		t.Fatalf("initialize live MCP: %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("close live MCP client: %v", err)
		}
	})
	return session, identities, agentMemoryRuntimeEvidence{
		ArcadeDBVersion:    arcadeDBVersion,
		MCPServerVersion:   serverVersion,
		EmbeddingModel:     agentMemoryLiveModelLabel(),
		EmbeddingDimension: embeddingDimension,
	}
}

func agentMemoryLiveModelLabel() string {
	if model := strings.TrimSpace(os.Getenv("AURA_EMBED_MODEL")); model != "" {
		return model
	}
	if modelPath := strings.TrimSpace(os.Getenv("AURA_EMBED_MODEL_PATH")); modelPath != "" {
		return filepath.Base(modelPath)
	}
	return agentMemoryDefaultModelLabel
}

func cleanupAgentMemoryLiveTenants(
	t *testing.T,
	admin *arcadedb.Client,
	base arcadedb.Config,
	credentials *arcadedb.TenantCredentials,
	identities []string,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for _, identity := range identities {
		database, err := arcadedb.DatabaseFor(identity)
		if err != nil {
			t.Errorf("cleanup DatabaseFor(%q): %v", identity, err)
			continue
		}
		_, dropDatabaseErr := admin.DropDatabase(ctx, database)
		exists, err := admin.DatabaseExists(ctx, database)
		if err != nil {
			t.Errorf("verify disposable database %s removed: %v", database, err)
		} else if exists {
			t.Errorf("disposable database %s survived cleanup (drop: %v)", database, dropDatabaseErr)
		}

		user := arcadedb.TenantUserFor(database)
		dropUserErr := admin.DropUser(ctx, user)
		probeConfig := base
		probeConfig.Database = database
		probeConfig.User = user
		probeConfig.Password = credentials.PasswordFor(database)
		probe, err := arcadedb.New(probeConfig)
		if err != nil {
			t.Errorf("build cleanup credential probe for %s: %v", user, err)
			continue
		}
		accepted, err := probe.CredentialAccepted(ctx)
		if err != nil {
			t.Errorf("verify disposable credential %s removed: %v", user, err)
		} else if accepted {
			t.Errorf("disposable credential %s survived cleanup (drop: %v)", user, dropUserErr)
		}
	}
	if transport, ok := http.DefaultTransport.(interface{ CloseIdleConnections() }); ok {
		transport.CloseIdleConnections()
	}
}

// callAgentMemoryLiveJSON calls tool on session, stamping identity into
// _meta.aura.user_identifier through the SAME mcp.SetAuraMetaField helper the
// agent bridge and the CLI use (D-108) — never as a "user_identifier" argument,
// which no longer exists on any memory tool's schema.
func callAgentMemoryLiveJSON[T any](
	t *testing.T,
	ctx context.Context,
	session *officialmcp.ClientSession,
	identity, tool string,
	arguments map[string]any,
) T {
	t.Helper()
	params := &officialmcp.CallToolParams{Name: tool, Arguments: arguments}
	auramcp.SetAuraMetaField(params, auramcp.MetaFieldUserIdentifier, identity)
	res, err := session.CallTool(ctx, params)
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
	}
	text, isErr := auramcp.DecodeToolResult(res)
	if isErr {
		t.Fatalf("call %s: %v", tool, auramcp.DecodeToolCallError("agent-memory-live", tool, text))
	}
	if strings.Contains(text, `"source_run_id"`) || strings.Contains(text, `"source_memory_ids"`) {
		t.Fatalf("call %s emitted retired provenance fields: %s", tool, text)
	}
	var output T
	if err := json.Unmarshal([]byte(text), &output); err != nil {
		t.Fatalf("decode %s output %q: %v", tool, text, err)
	}
	return output
}

// callAgentMemoryLiveExpectingRefusal proves the negative identity case against
// the LIVE server: a call whose _meta carries no usable identity (or none at
// all) refuses with IsError:true, before any tenant database is touched.
func callAgentMemoryLiveExpectingRefusal(
	t *testing.T,
	ctx context.Context,
	session *officialmcp.ClientSession,
	tool string,
	arguments map[string]any,
) string {
	t.Helper()
	res, err := session.CallTool(ctx, &officialmcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		t.Fatalf("call %s: transport error %v", tool, err)
	}
	text, isErr := auramcp.DecodeToolResult(res)
	if !isErr {
		t.Fatalf("call %s with no identity succeeded, want a refusal: %s", tool, text)
	}
	return text
}

// drainAgentMemoryLiveTools drains session's paginated tool list, mirroring
// internal/mcp's drainSDKToolsForTest / cmd/aura's drainSDKTools — pagination
// is a capability the deleted bespoke client never had.
func drainAgentMemoryLiveTools(t *testing.T, ctx context.Context, session *officialmcp.ClientSession) []*officialmcp.Tool {
	t.Helper()
	var out []*officialmcp.Tool
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			t.Fatalf("tools/list: %v", err)
		}
		out = append(out, tool)
	}
	return out
}

func assertAgentMemoryLiveTools(t *testing.T, tools []*officialmcp.Tool, names ...string) {
	t.Helper()
	available := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		available[tool.Name] = struct{}{}
	}
	for _, name := range names {
		if _, ok := available[name]; !ok {
			t.Errorf("tools/list omitted %q", name)
		}
	}
}

func assertAgentMemoryLiveSourceSchema(t *testing.T, tools []*officialmcp.Tool) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name != "memory_upsert_fact" {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal memory_upsert_fact schema: %v", err)
		}
		schema := string(raw)
		if !strings.Contains(schema, `"source"`) {
			t.Fatalf("memory_upsert_fact schema has no structured source: %s", schema)
		}
		if strings.Contains(schema, `"source_run_id"`) || strings.Contains(schema, `"source_memory_ids"`) {
			t.Fatalf("memory_upsert_fact schema advertises retired provenance fields: %s", schema)
		}
		if strings.Contains(schema, `"user_identifier"`) {
			t.Fatalf("memory_upsert_fact schema still advertises user_identifier (D-108): %s", schema)
		}
		return
	}
	t.Fatal("memory_upsert_fact schema unavailable")
}

func agentMemoryLiveEnv(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func agentMemoryLiveGap(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf("arcadedb_integration cannot skip under CI: "+format, args...)
	}
	t.Skipf(format, args...)
}

func agentMemoryLiveVectorWidth(vectors [][]float64) int {
	if len(vectors) == 0 {
		return 0
	}
	return len(vectors[0])
}

func verifyAgentMemoryLiveNoLeaks(t *testing.T) {
	t.Helper()
	ignoreExisting := goleak.IgnoreCurrent()
	t.Cleanup(func() {
		goleak.VerifyNone(t, ignoreExisting)
	})
}
