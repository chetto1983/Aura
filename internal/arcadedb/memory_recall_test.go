package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

const recallFactRow = `{"result":[{"@rid":"#10:1","statement":"Davide keeps the blue notebook.","predicate":"keeps","subject":"Davide","object":"blue notebook","valid_from":"2026-01-01T00:00:00Z","fact_key":"fact-blue","sources":[{"run_id":"run-1","writer_role":"parent"}]}]}`

const recallAnchorRow = `{"result":[{"@rid":"#20:1","identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":7,"role":"user","content":"We discussed the blue notebook in Turin.","content_hash":"hash-7","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/7"}]}`

const recallWindowRows = `{"result":[{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":6,"role":"assistant","content":"I found the earlier itinerary.","content_hash":"hash-6","occurred_at":"2026-08-31T11:59:00Z","source_ref":"postgres://conversation/conversation-1/turn/6"},{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":7,"role":"user","content":"We discussed the blue notebook in Turin.","content_hash":"hash-7","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/7"}]}`

func TestMemoryRecallVectorFuse(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "vector.fuse"):
			return testResponse{Body: `{"result":[{"rid":"#20:1","score":0.04},{"rid":"#10:1","score":0.03}]}`}
		case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
			return testResponse{Body: recallFactRow}
		case strings.Contains(statement, "FROM ConversationTurn") && strings.Contains(statement, "@rid IN"):
			return testResponse{Body: recallAnchorRow}
		case strings.Contains(statement, "turn_seq"):
			return testResponse{Body: recallWindowRows}
		default:
			return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected query"}`}
		}
	})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})

	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Mode: RecallModeSemantic,
		Query: "where was the blue notebook discussed", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if len(result.Evidence) != 2 || result.Evidence[0].Kind != RecallEvidenceConversation ||
		result.Evidence[1].Kind != RecallEvidenceFact {
		t.Fatalf("fused evidence = %+v", result.Evidence)
	}
	if result.Retrieval.EffectivePath != "mixed" || result.Retrieval.Path != retrievalPathHybrid {
		t.Fatalf("retrieval = %+v", result.Retrieval)
	}
	var fusion string
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "vector.fuse") {
			fusion = statement
			break
		}
	}
	if strings.Count(fusion, "`vector.fuse`") < 3 || strings.Count(fusion, "fusion: 'RRF'") < 3 {
		t.Fatalf("recall did not use nested native rank-only fusion: %s", fusion)
	}
	if !strings.Contains(fusion, "ORDER BY score DESC, rid ASC") {
		t.Fatalf("fusion ties are not deterministic: %s", fusion)
	}
}

func TestMemoryRecallBackendPath(t *testing.T) {
	tests := []struct {
		name     string
		request  RecallRequest
		embedder Embedder
		wantPath string
	}{
		{name: "query", request: RecallRequest{IdentityID: "identity-a", Query: "blue notebook"}, embedder: &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}, wantPath: retrievalPathHybrid},
		{name: "entity", request: RecallRequest{IdentityID: "identity-a", Entity: "Davide"}, wantPath: "graph"},
		{name: "embedding unavailable", request: RecallRequest{IdentityID: "identity-a", Query: "blue notebook"}, embedder: &stubEmbedder{err: context.DeadlineExceeded}, wantPath: retrievalPathLexical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := routedClient(t, func(request recordedRequest) testResponse {
				statement, _ := request.Payload["command"].(string)
				switch {
				case strings.Contains(statement, "vector.fuse"):
					return testResponse{Body: `{"result":[{"rid":"#10:1","score":0.03}]}`}
				case strings.Contains(statement, "FROM FACT") && strings.Contains(statement, "@rid IN"):
					return testResponse{Body: recallFactRow}
				case strings.Contains(statement, "outV().name"):
					return testResponse{Body: recallFactRow}
				default:
					return testResponse{Body: `{"result":[]}`}
				}
			})
			client.WithEmbedder(tt.embedder)
			result, err := client.RecallMemory(context.Background(), tt.request)
			if err != nil {
				t.Fatalf("RecallMemory: %v", err)
			}
			if result.Retrieval.Path != tt.wantPath {
				t.Fatalf("path = %q, want %q", result.Retrieval.Path, tt.wantPath)
			}
		})
	}
}

func TestMemoryRecallAbstains(t *testing.T) {
	client, requests := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	result, err := client.RecallMemory(context.Background(), RecallRequest{
		IdentityID: "identity-a", Query: "unrelated", Limit: 5,
	})
	if err != nil {
		t.Fatalf("RecallMemory: %v", err)
	}
	if !result.Abstained || result.Reason != reasonNoQualifiedCandidates || len(result.Evidence) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(*requests) != 1 {
		t.Fatalf("abstention made %d queries, want one ranked retrieval", len(*requests))
	}
	if result.Retrieval.BackendLatency < 0 || result.Retrieval.BackendLatency > time.Minute {
		t.Fatalf("backend latency = %s", result.Retrieval.BackendLatency)
	}
}
