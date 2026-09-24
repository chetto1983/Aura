package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The table's one row is keyed by the space the lab VM's sidecar attests (logged by the
// daemon and the ingest supervisor, 2026-09-24). A key that drifted from it would leave the
// production model uncalibrated with nothing to say so but a field.
func TestEmbeddingGemmaFloorsAreKeyedByTheSpaceTheVMAttests(t *testing.T) {
	if embeddingGemmaSpace != "es1-e0aa6accf0b79c6b" {
		t.Fatalf("embeddingGemmaSpace = %q, want es1-e0aa6accf0b79c6b", embeddingGemmaSpace)
	}
	floors, measured := floorsFor(embeddingGemmaSpace)
	if !measured || floors.memory != (denseFloors{maxDistance: 0.72, minRelevance: 0.28}) {
		t.Fatalf("floors = %+v measured = %v", floors, measured)
	}
}

func TestFloorsForAnUnmeasuredSpaceAreEmbeddingGemmasAndSaySo(t *testing.T) {
	floors, measured := floorsFor("es1-never-measured")
	if measured || floors != embeddingGemmaFloors {
		t.Fatalf("floors = %+v measured = %v, want EmbeddingGemma's, unmeasured", floors, measured)
	}
}

// AURA_MEMORY_DENSE_MAX_DISTANCE_RATIO and AURA_MEMORY_MIN_RELEVANCE are an operator's
// calibration, so a positive limit wins over the table; the reason still says whether the
// table had a row.
func TestBindDenseFloorsLetsAnOperatorOverrideWin(t *testing.T) {
	params := map[string]any{}
	reason := MemoryLimits{DenseMaxDistance: 0.5}.bindDenseFloors(params, embeddingGemmaSpace)
	if reason != "" || params["max_distance"] != 0.5 || params["min_relevance"] != 0.28 {
		t.Fatalf("measured space with an override: params = %v reason = %q", params, reason)
	}
	params = map[string]any{}
	reason = MemoryLimits{}.bindDenseFloors(params, "es1-never-measured")
	if reason != ReasonUncalibratedFloors || params["max_distance"] != 0.72 || params["min_relevance"] != 0.28 {
		t.Fatalf("unmeasured space: params = %v reason = %q", params, reason)
	}
}

func hybridFactsClient(t *testing.T, embedder DenseEmbedder) *Client {
	t.Helper()
	client, _ := routedClient(t, withOpenGate(func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "vector.fuse"):
			return testResponse{Body: `{"result":[{"rid":"#3:1"}]}`}
		case strings.Contains(statement, "@rid IN"):
			return testResponse{Body: `{"result":[{"@rid":"#3:1","statement":"first","subject":"A","object":"B"}]}`}
		}
		return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected query"}`}
	}))
	return client.WithEmbedder(embedder)
}

func TestSearchFactsHybridNamesFloorsNeverMeasuredForItsSpace(t *testing.T) {
	stub := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	result, err := hybridFactsClient(t, stub).SearchFactsHybrid(context.Background(), "cliente torino", 1, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if result.RetrievalPath != retrievalPathHybrid || result.Reason != "" ||
		result.FloorsReason != ReasonUncalibratedFloors {
		t.Fatalf("result = %+v, want a hybrid answer naming its uncalibrated floors", result)
	}
	measured := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}, space: embeddingGemmaSpace}
	result, err = hybridFactsClient(t, measured).SearchFactsHybrid(context.Background(), "cliente torino", 1, time.Time{})
	if err != nil {
		t.Fatalf("SearchFactsHybrid: %v", err)
	}
	if result.RetrievalPath != retrievalPathHybrid || result.FloorsReason != "" {
		t.Fatalf("result = %+v, want no floors reason in the measured space", result)
	}
}

func TestRecallNamesFloorsNeverMeasuredForItsSpace(t *testing.T) {
	recall := func(embedder DenseEmbedder) RecallResult {
		t.Helper()
		client, _ := routedClient(t, withOpenGate(func(request recordedRequest) testResponse {
			statement, _ := request.Payload["command"].(string)
			switch {
			case strings.Contains(statement, "vector.fuse"):
				return testResponse{Body: `{"result":[{"rid":"#10:1","score":0.03}]}`}
			case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
				return testResponse{Body: recallFactRow}
			}
			return testResponse{Body: `{"result":[]}`}
		}))
		result, err := client.WithEmbedder(embedder).RecallMemory(context.Background(), RecallRequest{
			IdentityID: "identity-a", Mode: RecallModeSemantic, Query: "blue notebook", Limit: 5,
		})
		if err != nil {
			t.Fatalf("RecallMemory: %v", err)
		}
		return result
	}
	if got := recall(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}); got.Retrieval.Path != retrievalPathHybrid ||
		got.FloorsReason != ReasonUncalibratedFloors {
		t.Fatalf("hybrid recall in an unmeasured space: path %q floors %q", got.Retrieval.Path, got.FloorsReason)
	}
	if got := recall(&stubEmbedder{err: context.DeadlineExceeded}); got.Retrieval.Path != retrievalPathLexical ||
		got.FloorsReason != "" {
		t.Fatalf("lexical recall named dense floors: path %q floors %q", got.Retrieval.Path, got.FloorsReason)
	}
}
