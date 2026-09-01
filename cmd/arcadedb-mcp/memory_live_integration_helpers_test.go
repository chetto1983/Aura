//go:build arcadedb_integration

// memory_live_integration_helpers_test.go holds the harness
// memory_live_integration_test.go's three live-scenario tests share: session
// construction against the real ArcadeDB + embedder + tenant provisioning,
// the OAuth-subject call helper, and cleanup. Split out at the ≤600-LOC
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
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/goleak"

	"github.com/chetto1983/aura/internal/arcadedb"
	auramcp "github.com/chetto1983/aura/internal/mcp"
)

type agentMemoryLiveMCPOptions struct {
	strictDependencies  bool
	headerFunc          func(context.Context) map[string]string
	receivingMiddleware officialmcp.Middleware
}

const agentMemoryLiveRouteMarker = "AURA_AGENT_MEMORY_MIXED_TIER_JSON="

func newAgentMemoryLiveMCP(
	t *testing.T,
	identityCount int,
	operatorDisplayName string,
) (map[string]*officialmcp.ClientSession, []string, agentMemoryRuntimeEvidence) {
	t.Helper()
	sessions, identities, runtime, _ := newAgentMemoryLiveMCPWithOptions(
		t, identityCount, operatorDisplayName, agentMemoryLiveMCPOptions{},
	)
	return sessions, identities, runtime
}

func newAgentMemoryLiveMCPWithOptions(
	t *testing.T,
	identityCount int,
	operatorDisplayName string,
	options agentMemoryLiveMCPOptions,
) (map[string]*officialmcp.ClientSession, []string, agentMemoryRuntimeEvidence, *tenants) {
	t.Helper()
	adminPassword := os.Getenv("ARCADEDB_ADMIN_PASSWORD")
	if strings.TrimSpace(adminPassword) == "" {
		adminPassword = os.Getenv("ARCADEDB_PASSWORD")
	}
	if strings.TrimSpace(adminPassword) == "" {
		agentMemoryLiveDependencyGap(t, options.strictDependencies,
			"ARCADEDB_PASSWORD or ARCADEDB_ADMIN_PASSWORD is not set")
	}
	if strings.TrimSpace(os.Getenv("AURA_ARCADEDB_TENANT_SECRET")) == "" {
		agentMemoryLiveDependencyGap(t, options.strictDependencies,
			"AURA_ARCADEDB_TENANT_SECRET is not set")
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
		agentMemoryLiveDependencyGap(t, options.strictDependencies, "ArcadeDB is unreachable: %v", err)
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
		agentMemoryLiveDependencyGap(t, options.strictDependencies, "EmbeddingGemma endpoint is not configured")
	}
	vectors, err := embedder.Embed(probeCtx, []string{"task: search result | query: integration health probe"})
	if err != nil {
		agentMemoryLiveDependencyGap(t, options.strictDependencies, "EmbeddingGemma is unreachable: %v", err)
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

	tenantClients := newTenants(base, admin, embedder, credentials)
	server := newServer(tenantClients, time.Now, operatorDisplayName)
	if options.receivingMiddleware != nil {
		server.AddReceivingMiddleware(options.receivingMiddleware)
	}
	authFixture := newArcadeAuthFixture(t)
	httpServer := httptest.NewUnstartedServer(nil)
	resource := "http://" + httpServer.Listener.Addr().String() + "/mcp/"
	oauthConfig := oauthResourceConfig{
		Issuer: authFixture.issuer, JWKSURL: authFixture.server.URL, Resource: resource, Scope: defaultOAuthScope,
	}
	verifier := newArcadeTokenVerifier(oauthConfig, authFixture.server.Client())
	mux := http.NewServeMux()
	mux.Handle("/mcp/", protectedArcadeMCP(oauthConfig, verifier.Verify,
		officialmcp.NewStreamableHTTPHandler(func(*http.Request) *officialmcp.Server { return server }, nil)))
	mux.Handle("/.well-known/oauth-protected-resource/mcp/", arcadeProtectedResourceMetadata(oauthConfig))
	httpServer.Config.Handler = mux
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, httpServer.URL+"/mcp/", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("build anonymous MCP request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("anonymous MCP request: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous MCP status = %d, want 401", response.StatusCode)
	}
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

	// This is the same generic remote-MCP session path production uses. Each
	// session carries a different OAuth subject and therefore resolves a different
	// tenant database without model-visible identity arguments.
	sessions := make(map[string]*officialmcp.ClientSession, len(identities))
	headerFunc := options.headerFunc
	if headerFunc == nil {
		headerFunc = agentMemoryLiveActorHeaders
	}
	for _, identity := range identities {
		managed := auramcp.ManagedServer{
			Type: auramcp.ServerTypeStreamableHTTP,
			URL:  httpServer.URL + "/mcp/",
			Env: []string{"MCP_BEARER_TOKEN=" + authFixture.token(t, resource,
				identity, defaultOAuthScope, time.Now().Add(time.Hour))},
			Trust: auramcp.ManagedTrust{Class: auramcp.TrustTrustedRecipe},
		}
		// D-10: real production traffic carries a host-derived actor on this
		// same HeaderFunc mechanism (internal/agent/mcptools/mount.go); this
		// generic harness calls auramcp.OpenSDKSession directly, bypassing
		// mount.go entirely, so it must attach the SAME header itself or
		// every memory_upsert_fact call below fails with "missing host-
		// derived actor" -- these sessions all represent an operator's own
		// foreground turn, so a fixed PARENT actor is the correct shape.
		session, openErr := auramcp.OpenSDKSession(t.Context(), "agent-memory-live", managed, auramcp.EgressPolicy{},
			auramcp.SessionOptions{HeaderFunc: headerFunc})
		if openErr != nil {
			t.Fatalf("initialize live MCP for %s: %v", identity, openErr)
		}
		sessions[identity] = session
	}
	t.Cleanup(func() {
		for identity, session := range sessions {
			if err := session.Close(); err != nil {
				t.Errorf("close live MCP client for %s: %v", identity, err)
			}
		}
	})
	return sessions, identities, agentMemoryRuntimeEvidence{
		ArcadeDBVersion:    arcadeDBVersion,
		MCPServerVersion:   serverVersion,
		EmbeddingModel:     agentMemoryLiveModelLabel(),
		EmbeddingDimension: embeddingDimension,
	}, tenantClients
}

// agentMemoryLiveParentRunID is the fixed actor run id every session this
// harness opens carries (D-10) -- these tests exercise tenant isolation and
// retrieval, not actor attribution, so one shared parent run id across all
// sessions is correct: isolation here comes from each session's OWN OAuth
// identity resolving to its OWN tenant database, never from the actor.
const agentMemoryLiveParentRunID = "live-test-parent-run"

func agentMemoryLiveActorHeaders(context.Context) map[string]string {
	return map[string]string{
		memoryActorRunIDHeader: agentMemoryLiveParentRunID,
		memoryActorRoleHeader:  string(arcadedb.WriterParent),
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

// callAgentMemoryLiveJSON calls a tool through an OAuth-authenticated session.
func callAgentMemoryLiveJSON[T any](
	t *testing.T,
	ctx context.Context,
	session *officialmcp.ClientSession,
	tool string,
	arguments map[string]any,
) T {
	t.Helper()
	params := &officialmcp.CallToolParams{Name: tool, Arguments: arguments}
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
		// D-10 (Phase 51): the model must have no field left to assert who
		// wrote a fact -- run_id left the WRITE schema entirely (it still
		// exists on memory_forget's filter and on read-back, a DIFFERENT
		// type, MemoryFactSource; this asserts on memory_upsert_fact's own
		// InputSchema only).
		if strings.Contains(schema, `"run_id"`) {
			t.Fatalf("memory_upsert_fact schema still advertises run_id on the WRITE path (D-10): %s", schema)
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

func agentMemoryLiveDependencyGap(t *testing.T, strict bool, format string, args ...any) {
	t.Helper()
	if strict {
		t.Fatalf("arcadedb_integration dependency unavailable: "+format, args...)
	}
	agentMemoryLiveGap(t, format, args...)
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

func logAgentMemoryLiveRouteEvidence(t *testing.T, cases []map[string]any) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"scenario": "mixed_tier_recall",
		"cases":    cases,
	})
	if err != nil {
		t.Fatalf("encode mixed-tier route evidence: %v", err)
	}
	t.Logf("%s%s", agentMemoryLiveRouteMarker, raw)
}

func agentMemoryLiveRouteCase(
	name string,
	output MemoryRecallOutput,
	attributes []attribute.KeyValue,
	activeSourceCount int,
	foreignSourceCount int,
) map[string]any {
	returnedCounts := map[string]int{"facts": 0, "conversations": 0, "reasoning": 0}
	for _, evidence := range output.Evidence {
		switch evidence.Kind {
		case "fact":
			returnedCounts["facts"]++
		case "conversation":
			returnedCounts["conversations"]++
		case "reasoning":
			returnedCounts["reasoning"]++
		}
	}
	return map[string]any{
		"name": name,
		"response": map[string]any{
			"effective_path": output.Retrieval.EffectivePath,
			"path":           output.Retrieval.Path,
			"tier_counts": map[string]int{
				"facts": output.Retrieval.FactCount, "conversations": output.Retrieval.ConversationCount,
				"reasoning": output.Retrieval.ReasoningCount,
			},
		},
		"otel": map[string]any{
			"memory.recall.effective_path":     agentMemoryLiveAttributeString(attributes, "memory.recall.effective_path"),
			"memory.recall.path":               agentMemoryLiveAttributeString(attributes, "memory.recall.path"),
			"memory.recall.fact_count":         agentMemoryLiveAttributeInt(attributes, "memory.recall.fact_count"),
			"memory.recall.conversation_count": agentMemoryLiveAttributeInt(attributes, "memory.recall.conversation_count"),
			"memory.recall.reasoning_count":    agentMemoryLiveAttributeInt(attributes, "memory.recall.reasoning_count"),
		},
		"returned_tier_counts": returnedCounts,
		"active_source_count":  activeSourceCount,
		"foreign_source_count": foreignSourceCount,
	}
}

func seedAgentMemoryLiveConversation(
	t *testing.T,
	ctx context.Context,
	identityID string,
	conversationID string,
	marker string,
	content string,
) {
	t.Helper()
	client := agentMemoryLiveTenantClient(t, ctx, identityID)
	assistantContent := "Historical setup for " + marker + "."
	if err := client.ApplyConversationProjection(ctx, arcadedb.ConversationProjection{
		IdentityID: identityID, ConversationID: conversationID,
		Turns: []arcadedb.ConversationTurnProjection{
			{
				IdentityID: identityID, ConversationID: conversationID, Seq: 1,
				Role: "assistant", Content: assistantContent,
				ContentHash: recallLiveContentHash(assistantContent),
				OccurredAt:  time.Now().UTC().Add(-2 * time.Minute),
				SourceRef:   "postgres://mixed-tier/" + marker + "/turn/1",
			},
			{
				IdentityID: identityID, ConversationID: conversationID, Seq: 2,
				Role: "user", Content: content, ContentHash: recallLiveContentHash(content),
				OccurredAt: time.Now().UTC().Add(-time.Minute),
				SourceRef:  "postgres://mixed-tier/" + marker + "/turn/2",
			},
		},
	}); err != nil {
		t.Fatalf("ApplyConversationProjection(%s): %v", conversationID, err)
	}
}
