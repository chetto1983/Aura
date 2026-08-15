//go:build arcadedb_integration

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

const (
	agentMemoryLiveTimeout       = 2 * time.Minute
	agentMemoryRuntimeMarker     = "AURA_AGENT_MEMORY_RUNTIME_JSON="
	agentMemoryDefaultModelLabel = "embeddinggemma-300M-Q8_0.gguf"
)

type agentMemoryRuntimeEvidence struct {
	ArcadeDBVersion    string `json:"arcadedb_version"`
	MCPServerVersion   string `json:"mcp_server_version"`
	EmbeddingModel     string `json:"embedding_model"`
	EmbeddingDimension int    `json:"embedding_dimension"`
}

type agentMemoryLiveSource struct {
	RunID     string   `json:"run_id"`
	MemoryIDs []string `json:"memory_ids"`
}

type agentMemoryLiveFact struct {
	Statement string                  `json:"statement"`
	Subject   string                  `json:"subject"`
	Object    string                  `json:"object"`
	FactKey   string                  `json:"fact_key"`
	Sources   []agentMemoryLiveSource `json:"sources"`
}

type agentMemoryLiveSearchOutput struct {
	Facts     []agentMemoryLiveFact `json:"facts"`
	Retrieval struct {
		Path      string `json:"path"`
		Abstained bool   `json:"abstained"`
		Reason    string `json:"reason"`
	} `json:"retrieval"`
}

// agentMemoryLiveUpsertOutput is memory_upsert_fact's full output shape
// (D-15/D-17), reusing agentMemoryLiveFact for candidate previews rather
// than a second fact type.
type agentMemoryLiveUpsertOutput struct {
	Statement  string                `json:"statement"`
	Superseded int                   `json:"superseded"`
	Refused    bool                  `json:"refused"`
	Reason     string                `json:"reason"`
	Candidates []agentMemoryLiveFact `json:"candidates"`
}

func TestAgentMemoryMCPLiveInitializeListCallAndIsolation(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	client, identities, runtime := newAgentMemoryLiveMCP(t, 2, "")
	runtimeJSON, err := json.Marshal(runtime)
	if err != nil {
		t.Fatalf("encode runtime evidence: %v", err)
	}
	t.Logf("%s%s", agentMemoryRuntimeMarker, runtimeJSON)
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	tools, err := client.ListTools(ctx)
	if err != nil {
		t.Fatalf("tools/list after initialize: %v", err)
	}
	assertAgentMemoryLiveTools(t, tools,
		"memory_upsert_fact", "memory_facts_about", "memory_search")
	assertAgentMemoryLiveSourceSchema(t, tools)

	alphaSource := agentMemoryLiveSource{RunID: "live-alpha", MemoryIDs: []string{"alpha-message-1"}}
	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, client, "memory_upsert_fact", map[string]any{
		"user_identifier": identities[0],
		"subject":         "Ada Lovelace",
		"subject_kind":    "person",
		"predicate":       "keeps",
		"object":          "Analytical Engine notes",
		"object_kind":     "document",
		"statement":       "Ada Lovelace keeps the Analytical Engine notes.",
		"source":          alphaSource,
	})

	alpha := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, client, "memory_facts_about", map[string]any{
			"user_identifier": identities[0],
			"entity":          "Ada Lovelace",
		})
	if len(alpha.Facts) != 1 || alpha.Facts[0].Statement != "Ada Lovelace keeps the Analytical Engine notes." {
		t.Fatalf("tenant alpha facts = %+v", alpha.Facts)
	}
	if len(alpha.Facts[0].Sources) != 1 || alpha.Facts[0].Sources[0].RunID != alphaSource.RunID {
		t.Fatalf("tenant alpha provenance = %+v, want %+v", alpha.Facts[0].Sources, alphaSource)
	}

	alphaFromBeta := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, client, "memory_facts_about", map[string]any{
			"user_identifier": identities[1],
			"entity":          "Ada Lovelace",
		})
	if alphaFromBeta.Facts == nil || len(alphaFromBeta.Facts) != 0 {
		t.Fatalf("tenant beta read tenant alpha facts: %+v", alphaFromBeta.Facts)
	}

	betaSource := agentMemoryLiveSource{RunID: "live-beta", MemoryIDs: []string{"beta-message-1"}}
	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, client, "memory_upsert_fact", map[string]any{
		"user_identifier": identities[1],
		"subject":         "Grace Hopper",
		"subject_kind":    "person",
		"predicate":       "documents",
		"object":          "compiler behavior",
		"object_kind":     "topic",
		"statement":       "Grace Hopper documents compiler behavior.",
		"source":          betaSource,
	})

	betaFromAlpha := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, client, "memory_facts_about", map[string]any{
			"user_identifier": identities[0],
			"entity":          "Grace Hopper",
		})
	if betaFromAlpha.Facts == nil || len(betaFromAlpha.Facts) != 0 {
		t.Fatalf("tenant alpha read tenant beta facts: %+v", betaFromAlpha.Facts)
	}

	beta := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, client, "memory_facts_about", map[string]any{
			"user_identifier": identities[1],
			"entity":          "Grace Hopper",
		})
	if len(beta.Facts) != 1 || beta.Facts[0].Statement != "Grace Hopper documents compiler behavior." {
		t.Fatalf("tenant beta facts = %+v", beta.Facts)
	}

	if _, err := client.CallTool(ctx, "memory_facts_about", map[string]any{
		"user_identifier": "not-an-aura-identity",
		"entity":          "Ada Lovelace",
	}); err == nil || !strings.Contains(err.Error(), "not an identity") {
		t.Fatalf("invalid identity error = %v, want UUID refusal", err)
	}
}

func TestAgentMemoryMCPLiveAbstainsOnNonexistentFact(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	client, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, client, "memory_upsert_fact", map[string]any{
		"user_identifier": identities[0],
		"subject":         "Davide",
		"subject_kind":    "person",
		"predicate":       "lives_in",
		"object":          "Caraglio",
		"object_kind":     "place",
		"statement":       "Davide lives in Caraglio.",
		"source": agentMemoryLiveSource{
			RunID: "live-abstention", MemoryIDs: []string{"known-message-1"},
		},
	})

	out := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, client, "memory_search", map[string]any{
			"user_identifier": identities[0],
			"query":           "Il pinguino notarile di Zog possiede sette lune viola registrate nel 1842",
			"limit":           5,
		})
	if out.Facts == nil || len(out.Facts) != 0 {
		t.Fatalf("nonexistent fact returned %+v, want facts:[]", out.Facts)
	}
	if !out.Retrieval.Abstained || out.Retrieval.Reason != "no_qualified_candidates" {
		t.Fatalf("retrieval = %+v, want explicit no_qualified_candidates abstention", out.Retrieval)
	}
	if out.Retrieval.Path != "hybrid" {
		t.Fatalf("retrieval path = %q, want the live dense+lexical path", out.Retrieval.Path)
	}
}

// TestAgentMemoryMCPLiveSupersedeRefusalThenFactKeyCloses replays D-15/D-16/D-17
// at the model-facing MCP boundary against a live ArcadeDB: recall surfaces
// fact_key, an ambiguous supersedes:true refuses as a successful, effect-free
// call, and naming the exact fact_key closes only the one edge it names,
// leaving the sibling untouched.
func TestAgentMemoryMCPLiveSupersedeRefusalThenFactKeyCloses(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	client, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	source := agentMemoryLiveSource{RunID: "live-supersede", MemoryIDs: []string{"m1"}}
	write := func(object, statement string) {
		callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, client, "memory_upsert_fact", map[string]any{
			"user_identifier": identities[0],
			"subject":         "Isaac Newton",
			"subject_kind":    "person",
			"predicate":       "worked_at",
			"object":          object,
			"object_kind":     "place",
			"statement":       statement,
			"source":          source,
		})
	}
	write("Cambridge", "Isaac Newton worked at Cambridge.")
	write("the Royal Mint", "Isaac Newton worked at the Royal Mint.")

	before := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, client, "memory_facts_about", map[string]any{
		"user_identifier": identities[0], "entity": "Isaac Newton",
	})
	if len(before.Facts) != 2 {
		t.Fatalf("facts_about = %+v, want the two facts written above", before.Facts)
	}
	keys := map[string]string{}
	for _, fact := range before.Facts {
		if fact.FactKey == "" {
			t.Fatalf("fact %+v has no fact_key -- recall must surface one for a still-valid fact", fact)
		}
		keys[fact.Object] = fact.FactKey
	}

	// An ambiguous correction (two candidates, no fact_key) refuses as a
	// successful call and touches nothing.
	refusal := callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, client, "memory_upsert_fact", map[string]any{
		"user_identifier": identities[0],
		"subject":         "Isaac Newton",
		"subject_kind":    "person",
		"predicate":       "worked_at",
		"object":          "the Royal Society",
		"object_kind":     "place",
		"statement":       "Isaac Newton worked at the Royal Society.",
		"supersedes":      true,
		"source":          source,
	})
	if !refusal.Refused || refusal.Superseded != 0 {
		t.Fatalf("refusal = %+v, want refused=true, superseded=0", refusal)
	}
	if len(refusal.Candidates) != 2 {
		t.Fatalf("refusal candidates = %+v, want both prior facts", refusal.Candidates)
	}
	if !strings.Contains(refusal.Reason, "supersedes_fact_key") {
		t.Fatalf("reason = %q, want it to name supersedes_fact_key", refusal.Reason)
	}

	afterRefusal := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, client, "memory_facts_about", map[string]any{
		"user_identifier": identities[0], "entity": "Isaac Newton",
	})
	if len(afterRefusal.Facts) != 2 {
		t.Fatalf("facts after refusal = %+v, want the write to be effect-free", afterRefusal.Facts)
	}

	// Naming the exact fact_key closes only that one edge.
	closeResult := callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, client, "memory_upsert_fact", map[string]any{
		"user_identifier":     identities[0],
		"subject":             "Isaac Newton",
		"subject_kind":        "person",
		"predicate":           "worked_at",
		"object":              "the Royal Society",
		"object_kind":         "place",
		"statement":           "Isaac Newton worked at the Royal Society.",
		"supersedes_fact_key": keys["the Royal Mint"],
		"source":              source,
	})
	if closeResult.Refused || closeResult.Superseded != 1 {
		t.Fatalf("close result = %+v, want refused=false, superseded=1", closeResult)
	}

	final := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, client, "memory_facts_about", map[string]any{
		"user_identifier": identities[0], "entity": "Isaac Newton",
	})
	if len(final.Facts) != 2 {
		t.Fatalf("final facts = %+v, want the untouched sibling plus the new fact", final.Facts)
	}
	objects := map[string]bool{}
	for _, fact := range final.Facts {
		objects[fact.Object] = true
	}
	if !objects["Cambridge"] || !objects["the Royal Society"] || objects["the Royal Mint"] {
		t.Fatalf("final facts = %+v, want Cambridge untouched, the Royal Mint closed, the Royal Society new", final.Facts)
	}
}

func newAgentMemoryLiveMCP(
	t *testing.T,
	identityCount int,
	operatorDisplayName string,
) (*auramcp.HTTPClient, []string, agentMemoryRuntimeEvidence) {
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

	client, err := auramcp.OpenHTTP(t.Context(), "agent-memory-live", auramcp.HTTPConfig{
		URL:                  httpServer.URL,
		Client:               httpServer.Client(),
		ToolIdentityArgument: "user_identifier",
	})
	if err != nil {
		t.Fatalf("initialize live MCP: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Errorf("close live MCP client: %v", err)
		}
	})
	return client, identities, agentMemoryRuntimeEvidence{
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

func callAgentMemoryLiveJSON[T any](
	t *testing.T,
	ctx context.Context,
	client *auramcp.HTTPClient,
	tool string,
	arguments map[string]any,
) T {
	t.Helper()
	text, err := client.CallTool(ctx, tool, arguments)
	if err != nil {
		t.Fatalf("call %s: %v", tool, err)
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

func assertAgentMemoryLiveTools(t *testing.T, tools []auramcp.ToolDef, names ...string) {
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

func assertAgentMemoryLiveSourceSchema(t *testing.T, tools []auramcp.ToolDef) {
	t.Helper()
	for _, tool := range tools {
		if tool.Name != "memory_upsert_fact" {
			continue
		}
		schema := string(tool.InputSchema)
		if !strings.Contains(schema, `"source"`) {
			t.Fatalf("memory_upsert_fact schema has no structured source: %s", schema)
		}
		if strings.Contains(schema, `"source_run_id"`) || strings.Contains(schema, `"source_memory_ids"`) {
			t.Fatalf("memory_upsert_fact schema advertises retired provenance fields: %s", schema)
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
