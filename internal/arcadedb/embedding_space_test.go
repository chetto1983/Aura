package arcadedb

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// gateClient answers each memory type's mismatch count from counts (by type name), or with
// status when it is set, and every other statement with an empty result.
func gateClient(t *testing.T, counts map[string]int, status int) (*Client, *[]recordedRequest) {
	t.Helper()
	return routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if !isGateCount(statement) {
			return testResponse{Body: `{"result":[]}`}
		}
		if status != 0 {
			return testResponse{Status: status, Body: `{"detail":"down"}`}
		}
		typeName := strings.Fields(strings.TrimPrefix(statement, "SELECT count(*) AS n FROM "))[0]
		return testResponse{Body: `{"result":[{"n":` + strconv.Itoa(counts[typeName]) + `}]}`}
	})
}

func gateQueries(requests *[]recordedRequest) []recordedRequest {
	var out []recordedRequest
	for _, request := range *requests {
		if statement, _ := request.Payload["command"].(string); isGateCount(statement) {
			out = append(out, request)
		}
	}
	return out
}

func TestMemoryGateOpensOnlyWhenNoVectorIsInAnotherSpace(t *testing.T) {
	open, _ := gateClient(t, map[string]int{}, 0)
	if ok, err := open.memoryDenseOpen(context.Background(), "es1-a"); err != nil || !ok {
		t.Fatalf("empty mismatch counts: open=%v err=%v, want open", ok, err)
	}
	closed, requests := gateClient(t, map[string]int{conversationTurnType: 2}, 0)
	if ok, err := closed.memoryDenseOpen(context.Background(), "es1-a"); err != nil || ok {
		t.Fatalf("two turns in another space: open=%v err=%v, want closed", ok, err)
	}
	for _, request := range gateQueries(requests) {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		if !strings.Contains(statement, "embedding IS NOT NULL") || !strings.Contains(statement, otherSpace) ||
			params["space"] != "es1-a" {
			t.Fatalf("gate count does not ask for vectors outside the space:\n%s params=%v", statement, params)
		}
		if strings.Contains(statement, conversationTurnType) && !strings.Contains(statement, "deleted_at IS NULL") {
			t.Fatalf("soft-deleted turns close the gate (Review Focus 5):\n%s", statement)
		}
	}
}

func TestMemoryGateReusesItsAnswerForThirtySeconds(t *testing.T) {
	client, requests := gateClient(t, map[string]int{}, 0)
	ctx := context.Background()
	for range 2 {
		if _, err := client.memoryDenseOpen(ctx, "es1-a"); err != nil {
			t.Fatalf("memoryDenseOpen: %v", err)
		}
	}
	if n := len(gateQueries(requests)); n != len(memorySpaceTypes) {
		t.Fatalf("two reads within the TTL counted %d times, want one round of %d", n, len(memorySpaceTypes))
	}
	client.memoryGate.checked = time.Now().Add(-spaceGateTTL - time.Second)
	if _, err := client.memoryDenseOpen(ctx, "es1-a"); err != nil {
		t.Fatalf("memoryDenseOpen: %v", err)
	}
	if _, err := client.memoryDenseOpen(ctx, "es1-b"); err != nil {
		t.Fatalf("memoryDenseOpen: %v", err)
	}
	if n := len(gateQueries(requests)); n != 3*len(memorySpaceTypes) {
		t.Fatalf("an expired answer and a new space counted %d times, want three rounds", n)
	}
}

func TestSearchFactsHybridServesLexicallyOutsideTheSpace(t *testing.T) {
	client, _ := gateClient(t, map[string]int{factEdgeType: 1}, 0)
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	client.WithEmbedder(embedder)
	result, err := client.SearchFactsHybrid(context.Background(), "blue notebook", 5, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if result.RetrievalPath != retrievalPathLexical || result.Reason != reasonEmbeddingSpaceMismatch {
		t.Fatalf("result = %+v, want lexical with %q", result, reasonEmbeddingSpaceMismatch)
	}
	if len(embedder.calls) != 0 {
		t.Fatal("the query was embedded although the gate was closed")
	}
}

func TestMemoryGateFailureIsNamed(t *testing.T) {
	client, _ := gateClient(t, nil, http.StatusInternalServerError)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	_, reason := client.denseQueryVector(context.Background(), "blue notebook")
	if reason != reasonSpaceCheckFailed {
		t.Fatalf("reason = %q, want %q", reason, reasonSpaceCheckFailed)
	}
}

func TestMemoryRecallServesLexicallyOutsideTheSpace(t *testing.T) {
	client, _ := gateClient(t, map[string]int{reasoningTraceType: 1}, 0)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic, Query: "blue notebook",
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if result.Retrieval.Path != retrievalPathLexical {
		t.Fatalf("path = %q, want lexical", result.Retrieval.Path)
	}
}

func TestSearchReasoningTracesNamesItsPath(t *testing.T) {
	client, _ := gateClient(t, map[string]int{factEdgeType: 1}, 0)
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.SearchReasoningTraces(context.Background(), "identity-a", "deployment", 5)
	if err != nil {
		t.Fatalf("SearchReasoningTraces: %v", err)
	}
	if result.RetrievalPath != retrievalPathLexical || result.Reason != reasonEmbeddingSpaceMismatch {
		t.Fatalf("result = %+v, want lexical with %q", result, reasonEmbeddingSpaceMismatch)
	}
}
