//go:build arcadedb_integration

package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	officialmcp "github.com/modelcontextprotocol/go-sdk/mcp"

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
	session, identities, runtime := newAgentMemoryLiveMCP(t, 2, "")
	runtimeJSON, err := json.Marshal(runtime)
	if err != nil {
		t.Fatalf("encode runtime evidence: %v", err)
	}
	t.Logf("%s%s", agentMemoryRuntimeMarker, runtimeJSON)
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	tools := drainAgentMemoryLiveTools(t, ctx, session)
	assertAgentMemoryLiveTools(t, tools,
		"memory_upsert_fact", "memory_facts_about", "memory_search")
	assertAgentMemoryLiveSourceSchema(t, tools)

	alphaSource := agentMemoryLiveSource{RunID: "live-alpha", MemoryIDs: []string{"alpha-message-1"}}
	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, session, identities[0], "memory_upsert_fact", map[string]any{
		"subject":      "Ada Lovelace",
		"subject_kind": "person",
		"predicate":    "keeps",
		"object":       "Analytical Engine notes",
		"object_kind":  "document",
		"statement":    "Ada Lovelace keeps the Analytical Engine notes.",
		"source":       alphaSource,
	})

	alpha := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, session, identities[0], "memory_facts_about", map[string]any{
			"entity": "Ada Lovelace",
		})
	if len(alpha.Facts) != 1 || alpha.Facts[0].Statement != "Ada Lovelace keeps the Analytical Engine notes." {
		t.Fatalf("tenant alpha facts = %+v", alpha.Facts)
	}
	if len(alpha.Facts[0].Sources) != 1 || alpha.Facts[0].Sources[0].RunID != alphaSource.RunID {
		t.Fatalf("tenant alpha provenance = %+v, want %+v", alpha.Facts[0].Sources, alphaSource)
	}

	alphaFromBeta := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, session, identities[1], "memory_facts_about", map[string]any{
			"entity": "Ada Lovelace",
		})
	if alphaFromBeta.Facts == nil || len(alphaFromBeta.Facts) != 0 {
		t.Fatalf("tenant beta read tenant alpha facts: %+v", alphaFromBeta.Facts)
	}

	betaSource := agentMemoryLiveSource{RunID: "live-beta", MemoryIDs: []string{"beta-message-1"}}
	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, session, identities[1], "memory_upsert_fact", map[string]any{
		"subject":      "Grace Hopper",
		"subject_kind": "person",
		"predicate":    "documents",
		"object":       "compiler behavior",
		"object_kind":  "topic",
		"statement":    "Grace Hopper documents compiler behavior.",
		"source":       betaSource,
	})

	betaFromAlpha := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, session, identities[0], "memory_facts_about", map[string]any{
			"entity": "Grace Hopper",
		})
	if betaFromAlpha.Facts == nil || len(betaFromAlpha.Facts) != 0 {
		t.Fatalf("tenant alpha read tenant beta facts: %+v", betaFromAlpha.Facts)
	}

	beta := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, session, identities[1], "memory_facts_about", map[string]any{
			"entity": "Grace Hopper",
		})
	if len(beta.Facts) != 1 || beta.Facts[0].Statement != "Grace Hopper documents compiler behavior." {
		t.Fatalf("tenant beta facts = %+v", beta.Facts)
	}

	// A forged _meta identity that is not a real Aura identity refuses at the
	// database-resolution layer (arcadedb.DatabaseFor), one boundary past
	// identityFromMeta's own presence/type check — proving the fail-closed chain
	// holds end to end against a live server, not just against identityFromMeta
	// in isolation.
	badParams := &officialmcp.CallToolParams{Name: "memory_facts_about", Arguments: map[string]any{"entity": "Ada Lovelace"}}
	auramcp.SetAuraMetaField(badParams, auramcp.MetaFieldUserIdentifier, "not-an-aura-identity")
	res, err := session.CallTool(ctx, badParams)
	if err != nil {
		t.Fatalf("call memory_facts_about with a forged identity: transport error %v", err)
	}
	text, isErr := auramcp.DecodeToolResult(res)
	if !isErr || !strings.Contains(text, "not an identity") {
		t.Fatalf("forged identity result = (isError=%v) %q, want a UUID refusal", isErr, text)
	}

	// The negative identity cases: no _meta at all, and an empty _meta identity.
	// Both must refuse before any tenant database is resolved.
	noMeta := callAgentMemoryLiveExpectingRefusal(t, ctx, session, "memory_facts_about", map[string]any{"entity": "Ada Lovelace"})
	if !strings.Contains(noMeta, "missing required identity") {
		t.Fatalf("no-_meta refusal text = %q, want it to mention missing required identity", noMeta)
	}
	emptyParams := &officialmcp.CallToolParams{Name: "memory_facts_about", Arguments: map[string]any{"entity": "Ada Lovelace"}}
	auramcp.SetAuraMetaField(emptyParams, auramcp.MetaFieldUserIdentifier, "")
	emptyRes, err := session.CallTool(ctx, emptyParams)
	if err != nil {
		t.Fatalf("call memory_facts_about with an empty identity: transport error %v", err)
	}
	emptyText, emptyIsErr := auramcp.DecodeToolResult(emptyRes)
	if !emptyIsErr || !strings.Contains(emptyText, "missing required identity") {
		t.Fatalf("empty-identity result = (isError=%v) %q, want a missing-identity refusal", emptyIsErr, emptyText)
	}
}

func TestAgentMemoryMCPLiveAbstainsOnNonexistentFact(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	session, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, session, identities[0], "memory_upsert_fact", map[string]any{
		"subject":      "Davide",
		"subject_kind": "person",
		"predicate":    "lives_in",
		"object":       "Caraglio",
		"object_kind":  "place",
		"statement":    "Davide lives in Caraglio.",
		"source": agentMemoryLiveSource{
			RunID: "live-abstention", MemoryIDs: []string{"known-message-1"},
		},
	})

	out := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, session, identities[0], "memory_search", map[string]any{
			"query": "Il pinguino notarile di Zog possiede sette lune viola registrate nel 1842",
			"limit": 5,
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
	session, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	source := agentMemoryLiveSource{RunID: "live-supersede", MemoryIDs: []string{"m1"}}
	write := func(object, statement string) {
		callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, session, identities[0], "memory_upsert_fact", map[string]any{
			"subject":      "Isaac Newton",
			"subject_kind": "person",
			"predicate":    "worked_at",
			"object":       object,
			"object_kind":  "place",
			"statement":    statement,
			"source":       source,
		})
	}
	write("Cambridge", "Isaac Newton worked at Cambridge.")
	write("the Royal Mint", "Isaac Newton worked at the Royal Mint.")

	before := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, session, identities[0], "memory_facts_about", map[string]any{
		"entity": "Isaac Newton",
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
	refusal := callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, session, identities[0], "memory_upsert_fact", map[string]any{
		"subject":      "Isaac Newton",
		"subject_kind": "person",
		"predicate":    "worked_at",
		"object":       "the Royal Society",
		"object_kind":  "place",
		"statement":    "Isaac Newton worked at the Royal Society.",
		"supersedes":   true,
		"source":       source,
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

	afterRefusal := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, session, identities[0], "memory_facts_about", map[string]any{
		"entity": "Isaac Newton",
	})
	if len(afterRefusal.Facts) != 2 {
		t.Fatalf("facts after refusal = %+v, want the write to be effect-free", afterRefusal.Facts)
	}

	// Naming the exact fact_key closes only that one edge.
	closeResult := callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, session, identities[0], "memory_upsert_fact", map[string]any{
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

	final := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, session, identities[0], "memory_facts_about", map[string]any{
		"entity": "Isaac Newton",
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
