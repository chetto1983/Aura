//go:build reasoning_live

// E2E of the FULL self-improvement loop against the real stack: a real
// classifier (granite embedder) + a learner whose oracle is faked (free,
// deterministic — it stands in for the DeepSeek router) + the real Neo4j store.
// An uncertain query is observed, labeled, persisted to Neo4j, and after the
// async worker + refresh the SAME query routes correctly. Skips if the granite
// sidecar, the cypher binary, or Neo4j are unavailable.
//
//	go test -tags reasoning_live -run TestLearnerLoopE2E ./internal/reasoninglearn/
package reasoninglearn

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/reasoningstore"
)

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

// staticOracle stands in for the DeepSeek router (free + deterministic).
type staticOracle struct{ tier prompt.ReasoningTier }

func (o staticOracle) Label(_ context.Context, _ string) (prompt.ReasoningTier, bool) {
	return o.tier, true
}

func TestLearnerLoopE2E(t *testing.T) {
	embedBase := envOr("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081")
	if resp, err := http.Get(embedBase + "/health"); err != nil {
		t.Skipf("granite sidecar unreachable: %v", err)
	} else {
		_ = resp.Body.Close()
	}
	ctx := context.Background()
	client, err := knowledge.Open(ctx, &knowledge.Config{
		BoltURL:           envOr("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
		User:              envOr("NEO4J_USER", "neo4j"),
		Password:          os.Getenv("NEO4J_PASSWORD"),
		Database:          "neo4j",
		MCPBinary:         envOr("AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
		ConnectTimeoutSec: 15,
	})
	if err != nil {
		t.Skipf("cannot open mcp-neo4j-cypher: %v", err)
	}
	defer func() { _ = client.Close() }()

	embedder := &documents.EmbeddingClient{
		BaseURL:    embedBase,
		Client:     &http.Client{Timeout: 30 * time.Second},
		Dimensions: knowledge.DefaultEmbedDimensions,
	}
	defer embedder.Client.CloseIdleConnections()
	store := &reasoningstore.Store{Client: client}
	clean := func() { _, _ = client.Write(ctx, "MATCH (e:ReasoningExample) DETACH DELETE e", nil) }
	clean()
	defer clean()

	query := "qual e la capitale dell'Italia?"
	classifier := prompt.NewReasoningClassifier(embedder, store)

	base, ok := classifier.Classify(ctx, query)
	if !ok {
		t.Fatal("baseline classify failed")
	}
	t.Logf("baseline: %q → %s", query, base)

	// Attach a learner: generous margin floor so the (mildly uncertain) stable
	// facts are observed, a static "none" oracle (the teacher), the real Neo4j
	// store. The worker saves + refreshes asynchronously, off the hot path.
	learner := New(Config{
		Oracle:      staticOracle{tier: prompt.ReasoningTierNone},
		Saver:       store,
		Refresh:     classifier.Refresh,
		MarginFloor: 0.5,
	})
	defer learner.Close()
	classifier.SetLearner(learner)

	// Drive several distinct stable-fact queries THROUGH the classifier: each is
	// uncertain → observed → the worker labels (none) + persists to Neo4j. This
	// is the real async write-path, not a direct store call.
	feed := []string{
		"qual e la capitale della Germania?",
		"qual e la capitale del Portogallo?",
		"chi ha scoperto l'America?",
		"quanti pianeti ci sono nel sistema solare?",
		"in che anno e finita la seconda guerra mondiale?",
	}
	for _, q := range feed {
		_, _ = classifier.Classify(ctx, q)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		ex, _ := store.LoadExamples(ctx)
		if len(ex) >= len(feed) {
			t.Logf("worker persisted %d example(s) to Neo4j via the async loop", len(ex))
			break
		}
		if time.Now().After(deadline) {
			ex, _ := store.LoadExamples(ctx)
			t.Fatalf("worker persisted only %d/%d examples in time", len(ex), len(feed))
		}
		time.Sleep(250 * time.Millisecond)
	}

	// After the worker's saves + refreshes, the held-out target should route none.
	classifier.Refresh() // ensure the latest examples are folded for this assertion
	learned, ok := classifier.Classify(ctx, query)
	if !ok {
		t.Fatal("post-learn classify failed")
	}
	t.Logf("learned:  %q → %s", query, learned)
	if learned != prompt.ReasoningTierNone {
		t.Errorf("after the async learner labeled+saved %d stable facts, the target should route none; got %s (baseline %s)", len(feed), learned, base)
	}
}
