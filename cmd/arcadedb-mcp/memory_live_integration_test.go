//go:build arcadedb_integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	officialmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

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
	sessions, identities, runtime := newAgentMemoryLiveMCP(t, 2, "")
	alphaSession := sessions[identities[0]]
	betaSession := sessions[identities[1]]
	runtimeJSON, err := json.Marshal(runtime)
	if err != nil {
		t.Fatalf("encode runtime evidence: %v", err)
	}
	t.Logf("%s%s", agentMemoryRuntimeMarker, runtimeJSON)
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	tools := drainAgentMemoryLiveTools(t, ctx, alphaSession)
	assertAgentMemoryLiveTools(t, tools,
		"memory_upsert_fact", "memory_facts_about", "memory_search")
	assertAgentMemoryLiveSourceSchema(t, tools)

	alphaSource := agentMemoryLiveSource{RunID: "live-alpha", MemoryIDs: []string{"alpha-message-1"}}
	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, alphaSession, "memory_upsert_fact", map[string]any{
		"subject":      "Ada Lovelace",
		"subject_kind": "person",
		"predicate":    "keeps",
		"object":       "Analytical Engine notes",
		"object_kind":  "document",
		"statement":    "Ada Lovelace keeps the Analytical Engine notes.",
		// D-10: run_id is no longer a write-path field -- alphaSource still
		// names it for the READ-side assertion below, but it must not be sent
		// here, or this call would be lying about what the model can control.
		"source": map[string]any{"memory_ids": alphaSource.MemoryIDs},
	})

	alpha := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, alphaSession, "memory_facts_about", map[string]any{
			"entity": "Ada Lovelace",
		})
	if len(alpha.Facts) != 1 || alpha.Facts[0].Statement != "Ada Lovelace keeps the Analytical Engine notes." {
		t.Fatalf("tenant alpha facts = %+v", alpha.Facts)
	}
	// run_id is host-derived (D-10), never alphaSource's -- every session
	// this harness opens carries the SAME fixed parent actor
	// (agentMemoryLiveActorHeaders), so that is what a real write persists.
	if len(alpha.Facts[0].Sources) != 1 || alpha.Facts[0].Sources[0].RunID != agentMemoryLiveParentRunID {
		t.Fatalf("tenant alpha provenance = %+v, want host-derived run_id %q",
			alpha.Facts[0].Sources, agentMemoryLiveParentRunID)
	}

	alphaFromBeta := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, betaSession, "memory_facts_about", map[string]any{
			"entity": "Ada Lovelace",
		})
	if alphaFromBeta.Facts == nil || len(alphaFromBeta.Facts) != 0 {
		t.Fatalf("tenant beta read tenant alpha facts: %+v", alphaFromBeta.Facts)
	}

	betaSource := agentMemoryLiveSource{RunID: "live-beta", MemoryIDs: []string{"beta-message-1"}}
	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, betaSession, "memory_upsert_fact", map[string]any{
		"subject":      "Grace Hopper",
		"subject_kind": "person",
		"predicate":    "documents",
		"object":       "compiler behavior",
		"object_kind":  "topic",
		"statement":    "Grace Hopper documents compiler behavior.",
		"source":       map[string]any{"memory_ids": betaSource.MemoryIDs},
	})

	betaFromAlpha := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, alphaSession, "memory_facts_about", map[string]any{
			"entity": "Grace Hopper",
		})
	if betaFromAlpha.Facts == nil || len(betaFromAlpha.Facts) != 0 {
		t.Fatalf("tenant alpha read tenant beta facts: %+v", betaFromAlpha.Facts)
	}

	beta := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, betaSession, "memory_facts_about", map[string]any{
			"entity": "Grace Hopper",
		})
	if len(beta.Facts) != 1 || beta.Facts[0].Statement != "Grace Hopper documents compiler behavior." {
		t.Fatalf("tenant beta facts = %+v", beta.Facts)
	}

	// Arbitrary client metadata cannot override the authenticated OAuth subject.
	badParams := &officialmcp.CallToolParams{Name: "memory_facts_about", Arguments: map[string]any{"entity": "Ada Lovelace"}}
	badParams.SetMeta(map[string]any{"tenant": identities[1]})
	res, err := alphaSession.CallTool(ctx, badParams)
	if err != nil {
		t.Fatalf("call memory_facts_about with client metadata: transport error %v", err)
	}
	text, isErr := auramcp.DecodeToolResult(res)
	if isErr || !strings.Contains(text, "Ada Lovelace keeps the Analytical Engine notes.") {
		t.Fatalf("client metadata changed tenant resolution: (isError=%v) %q", isErr, text)
	}
}

func TestAgentMemoryMCPLiveAbstainsOnNonexistentFact(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	sessions, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	session := sessions[identities[0]]
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	callAgentMemoryLiveJSON[struct {
		Statement string `json:"statement"`
	}](t, ctx, session, "memory_upsert_fact", map[string]any{
		"subject":      "Davide",
		"subject_kind": "person",
		"predicate":    "lives_in",
		"object":       "Caraglio",
		"object_kind":  "place",
		"statement":    "Davide lives in Caraglio.",
		// D-10: memory_ids only -- run_id is host-derived, not a field this
		// call can set.
		"source": map[string]any{"memory_ids": []string{"known-message-1"}},
	})

	out := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](
		t, ctx, session, "memory_search", map[string]any{
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
	sessions, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	session := sessions[identities[0]]
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	// D-10: memory_ids only in every write call below -- run_id is
	// host-derived (agentMemoryLiveActorHeaders), not a field these calls set.
	source := map[string]any{"memory_ids": []string{"m1"}}
	write := func(object, statement string) {
		callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, session, "memory_upsert_fact", map[string]any{
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

	before := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, session, "memory_facts_about", map[string]any{
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
	refusal := callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, session, "memory_upsert_fact", map[string]any{
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

	afterRefusal := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, session, "memory_facts_about", map[string]any{
		"entity": "Isaac Newton",
	})
	if len(afterRefusal.Facts) != 2 {
		t.Fatalf("facts after refusal = %+v, want the write to be effect-free", afterRefusal.Facts)
	}

	// Naming the exact fact_key closes only that one edge.
	closeResult := callAgentMemoryLiveJSON[agentMemoryLiveUpsertOutput](t, ctx, session, "memory_upsert_fact", map[string]any{
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

	final := callAgentMemoryLiveJSON[agentMemoryLiveSearchOutput](t, ctx, session, "memory_facts_about", map[string]any{
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

func TestAgentMemoryMCPLive_MixedTierRecall(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	recorder, receivingMiddleware := newAgentMemoryLiveSpanRecorder(t)
	sessions, identities, _, _ := newAgentMemoryLiveMCPWithOptions(t, 2, "", agentMemoryLiveMCPOptions{
		strictDependencies:  true,
		headerFunc:          agentMemoryLiveRecallHeaders,
		receivingMiddleware: receivingMiddleware,
	})
	identityID, foreignIdentityID := identities[0], identities[1]
	session := sessions[identityID]
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	callAgentMemoryLiveJSON[MemoryUpsertFactOutput](t, ctx, session, "memory_upsert_fact", map[string]any{
		"subject": "Aurora notebook", "predicate": "stored_in", "object": "Turin archive",
		"statement": "The aurora notebook is stored in the Turin archive.",
		"source":    map[string]any{"memory_ids": []string{"mixed-tier-fact-source"}},
	})
	seedAgentMemoryLiveConversation(t, ctx, identityID, "conversation-active", "active",
		"We are currently discussing the aurora notebook route through Turin.")
	seedAgentMemoryLiveConversation(t, ctx, identityID, "conversation-historical", "historical",
		"We previously discussed the aurora notebook route through Turin.")
	seedAgentMemoryLiveConversation(t, ctx, foreignIdentityID, "conversation-foreign", "foreign",
		"A foreign identity discussed the aurora notebook route through Turin.")
	seedClient := agentMemoryLiveTenantClient(t, ctx, identityID)
	seeded, err := seedClient.SearchConversationTurnsHybrid(ctx, identityID, "aurora notebook Turin", 10)
	if err != nil {
		t.Fatalf("verify projected recall fixtures: %v", err)
	}
	seededConversations := make(map[string]bool, len(seeded.Turns))
	for _, turn := range seeded.Turns {
		seededConversations[turn.ConversationID] = true
	}
	if !seededConversations["conversation-active"] || !seededConversations["conversation-historical"] ||
		seededConversations["conversation-foreign"] {
		t.Fatalf("projected recall fixtures = %+v, want active and historical owner candidates only", seeded.Turns)
	}

	activeHeader, err := encodeMemoryRecallActiveSources([]memoryRecallActiveSource{
		{ConversationID: "conversation-active", TurnID: agentMemoryLiveParentRunID},
	})
	if err != nil {
		t.Fatalf("encode active source: %v", err)
	}
	callCtx := context.WithValue(ctx, agentMemoryLiveRecallHeaderKey{}, activeHeader)
	spanStart := len(recorder.Ended())
	output := callAgentMemoryLiveJSON[MemoryRecallOutput](t, callCtx, session, "memory_recall", map[string]any{
		"query": "aurora notebook Turin", "limit": 10,
	})
	if output.Retrieval.EffectivePath != "mixed" || output.Retrieval.Path != "hybrid" {
		t.Fatalf("retrieval = %+v, want mixed contribution over hybrid backend; evidence=%+v",
			output.Retrieval, output.Evidence)
	}
	factCount, conversationCount := 0, 0
	historicalFound := false
	for _, evidence := range output.Evidence {
		switch evidence.Kind {
		case "fact":
			factCount++
			if evidence.Fact == nil || len(evidence.Fact.Sources) == 0 ||
				len(evidence.Fact.Sources[0].MemoryIDs) == 0 ||
				evidence.Fact.Sources[0].MemoryIDs[0] != "mixed-tier-fact-source" {
				t.Fatalf("fact provenance = %+v", evidence.Fact)
			}
		case "conversation":
			conversationCount++
			if evidence.Conversation == nil {
				t.Fatal("conversation evidence has no typed payload")
			}
			conversationID := evidence.Conversation.ConversationID
			if conversationID == "conversation-active" {
				t.Fatalf("active conversation leaked into recall: %+v", evidence.Conversation)
			}
			if conversationID == "conversation-foreign" {
				t.Fatalf("foreign conversation leaked into recall: %+v", evidence.Conversation)
			}
			if conversationID == "conversation-historical" {
				historicalFound = true
			}
			for _, turn := range evidence.Conversation.Turns {
				if !strings.HasPrefix(turn.SourceRef, "postgres://mixed-tier/") {
					t.Fatalf("conversation provenance = %+v", turn)
				}
			}
		}
	}
	if factCount != 1 || !historicalFound {
		t.Fatalf("evidence = %+v, want the fact and eligible historical conversation", output.Evidence)
	}
	if output.Retrieval.FactCount != factCount || output.Retrieval.ConversationCount != conversationCount {
		t.Fatalf("retrieval counts = %+v, evidence facts=%d conversations=%d",
			output.Retrieval, factCount, conversationCount)
	}
	attributes := agentMemoryLiveRecallSpan(t, recorder, spanStart)
	assertAgentMemoryLiveRecallSpan(t, attributes, output.Retrieval)
	logAgentMemoryLiveRouteEvidence(t, []map[string]any{
		agentMemoryLiveRouteCase("mixed", output, attributes, 0, 0),
	})
}

func TestAgentMemoryMCPLive_BackendPath(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	recorder, receivingMiddleware := newAgentMemoryLiveSpanRecorder(t)
	sessions, identities, _, tenantClients := newAgentMemoryLiveMCPWithOptions(
		t, 1, "", agentMemoryLiveMCPOptions{
			strictDependencies:  true,
			receivingMiddleware: receivingMiddleware,
		},
	)
	identityID := identities[0]
	session := sessions[identityID]
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()
	callAgentMemoryLiveJSON[MemoryUpsertFactOutput](t, ctx, session, "memory_upsert_fact", map[string]any{
		"subject": "Lumen compass", "predicate": "stored_in", "object": "apricot observatory",
		"statement": "The Lumen compass is stored in the apricot observatory.",
		"source":    map[string]any{"memory_ids": []string{"backend-path-fact-source"}},
	})

	tests := []struct {
		name      string
		arguments map[string]any
		wantPath  string
		fallback  bool
	}{
		{name: "query", arguments: map[string]any{"query": "Lumen compass apricot observatory", "limit": 5}, wantPath: "hybrid"},
		{name: "entity", arguments: map[string]any{"entity": "Lumen compass", "limit": 5}, wantPath: "graph"},
		{name: "forced fallback", arguments: map[string]any{"query": "Lumen", "limit": 5}, wantPath: "lexical", fallback: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.fallback {
				client, err := tenantClients.For(ctx, identityID)
				if err != nil {
					t.Fatalf("resolve server tenant client: %v", err)
				}
				client.WithEmbedder(agentMemoryLiveUnavailableEmbedder{})
			}
			spanStart := len(recorder.Ended())
			output := callAgentMemoryLiveJSON[MemoryRecallOutput](
				t, ctx, session, "memory_recall", test.arguments,
			)
			if output.Retrieval.Path != test.wantPath {
				t.Fatalf("path = %q, want %q; retrieval=%+v", output.Retrieval.Path, test.wantPath, output.Retrieval)
			}
			if output.Retrieval.EffectivePath != "facts" || output.Retrieval.FactCount != 1 ||
				output.Retrieval.ConversationCount != 0 {
				t.Fatalf("tier contribution = %+v, want one fact and no conversations", output.Retrieval)
			}
			attributes := agentMemoryLiveRecallSpan(t, recorder, spanStart)
			assertAgentMemoryLiveRecallSpan(t, attributes, output.Retrieval)
			logAgentMemoryLiveRouteEvidence(t, []map[string]any{
				agentMemoryLiveRouteCase(strings.ReplaceAll(test.name, " ", "_"), output, attributes, 0, 0),
			})
		})
	}
}

type agentMemoryLiveRecallHeaderKey struct{}

func agentMemoryLiveRecallHeaders(ctx context.Context) map[string]string {
	headers := agentMemoryLiveActorHeaders(ctx)
	if encoded, ok := ctx.Value(agentMemoryLiveRecallHeaderKey{}).(string); ok && encoded != "" {
		headers[memoryRecallActiveSourcesHeader] = encoded
	}
	return headers
}

type agentMemoryLiveUnavailableEmbedder struct{}

func (agentMemoryLiveUnavailableEmbedder) Embed(context.Context, []string) ([][]float64, error) {
	return nil, errors.New("forced live embedding fallback")
}

func newAgentMemoryLiveSpanRecorder(
	t *testing.T,
) (*tracetest.SpanRecorder, officialmcp.Middleware) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown live trace provider: %v", err)
		}
	})
	return recorder, func(next officialmcp.MethodHandler) officialmcp.MethodHandler {
		return func(ctx context.Context, method string, request officialmcp.Request) (officialmcp.Result, error) {
			ctx, span := provider.Tracer("agent-memory-live").Start(ctx, method)
			defer span.End()
			return next(ctx, method, request)
		}
	}
}

func agentMemoryLiveRecallSpan(
	t *testing.T,
	recorder *tracetest.SpanRecorder,
	start int,
) []attribute.KeyValue {
	t.Helper()
	spans := recorder.Ended()
	for index := len(spans) - 1; index >= start; index-- {
		attributes := spans[index].Attributes()
		if agentMemoryLiveAttributeString(attributes, "memory.recall.path") != "" {
			return attributes
		}
	}
	t.Fatalf("no same-call span with memory.recall.path after span index %d (ended=%d)", start, len(spans))
	return nil
}

func assertAgentMemoryLiveRecallSpan(
	t *testing.T,
	attributes []attribute.KeyValue,
	retrieval MemoryRecallRetrievalMetadata,
) {
	t.Helper()
	if got := agentMemoryLiveAttributeString(attributes, "memory.recall.effective_path"); got != retrieval.EffectivePath {
		t.Fatalf("span effective_path = %q, response = %q", got, retrieval.EffectivePath)
	}
	if got := agentMemoryLiveAttributeString(attributes, "memory.recall.path"); got != retrieval.Path {
		t.Fatalf("span path = %q, response = %q", got, retrieval.Path)
	}
	if got := agentMemoryLiveAttributeInt(attributes, "memory.recall.fact_count"); got != retrieval.FactCount {
		t.Fatalf("span fact_count = %d, response = %d", got, retrieval.FactCount)
	}
	if got := agentMemoryLiveAttributeInt(attributes, "memory.recall.conversation_count"); got != retrieval.ConversationCount {
		t.Fatalf("span conversation_count = %d, response = %d", got, retrieval.ConversationCount)
	}
}

func agentMemoryLiveAttributeString(attributes []attribute.KeyValue, key string) string {
	for _, item := range attributes {
		if string(item.Key) == key {
			return item.Value.AsString()
		}
	}
	return ""
}

func agentMemoryLiveAttributeInt(attributes []attribute.KeyValue, key string) int {
	for _, item := range attributes {
		if string(item.Key) == key {
			return int(item.Value.AsInt64())
		}
	}
	return 0
}
