package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestMergeEntitiesMovesBothDirectionsAndDropsSelfLinks(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		switch {
		case strings.Contains(statement, "count(*) AS n"):
			return testResponse{Body: `{"result":[{"n":3}]}`}
		case strings.Contains(statement, "CREATE (t)-[n:FACT]"):
			return testResponse{Body: `{"result":[{"moved":1}]}`}
		case strings.Contains(statement, "CREATE (s)-[n:FACT]"):
			return testResponse{Body: `{"result":[{"moved":1}]}`}
		default:
			return testResponse{Body: `{"result":[]}`}
		}
	})
	got, err := client.MergeEntities(context.Background(), " M. Bellini ", " Marta Bellini ")
	if err != nil {
		t.Fatalf("MergeEntities: %v", err)
	}
	if got != (MergeResult{Moved: 2, Dropped: 1, Target: "Marta Bellini"}) {
		t.Fatalf("result = %+v", got)
	}
	if len(*requests) != 6 {
		t.Fatalf("requests = %d, want count/upsert/two moves/delete/reindex", len(*requests))
	}
	if !strings.Contains((*requests)[4].Payload["command"].(string), "DETACH DELETE") {
		t.Fatalf("source removal missing: %v", (*requests)[4].Payload)
	}
}

func TestMergeEntitiesValidatesNamesBeforeIO(t *testing.T) {
	client, requests := routedClient(t, func(recordedRequest) testResponse {
		return testResponse{Status: http.StatusInternalServerError, Body: `{}`}
	})
	for _, pair := range [][2]string{{"", "B"}, {"A", ""}, {"A", "A"}} {
		if _, err := client.MergeEntities(context.Background(), pair[0], pair[1]); err == nil {
			t.Fatalf("merge %q -> %q accepted", pair[0], pair[1])
		}
	}
	if len(*requests) != 0 {
		t.Fatalf("validation made %d requests", len(*requests))
	}
}

func TestMergeEntitiesChecksAZeroDegreeSourceBeforeRename(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		return testResponse{Body: `{"result":[]}`}
	})
	got, err := client.MergeEntities(context.Background(), "Old", "New")
	if err != nil {
		t.Fatalf("MergeEntities: %v", err)
	}
	if got.Target != "New" || got.Moved != 0 || got.Dropped != 0 {
		t.Fatalf("result = %+v", got)
	}
	if len(*requests) != 7 || !strings.Contains((*requests)[1].Payload["command"].(string), "SELECT name") {
		t.Fatalf("zero-degree existence check missing: %+v", *requests)
	}
}

func TestMergeEntitiesPreservesServerFailureContext(t *testing.T) {
	client, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "count(*) AS n") {
			return testResponse{Body: `{"result":[{"n":1}]}`}
		}
		return testResponse{Status: http.StatusInternalServerError, Body: `{"detail":"write refused"}`}
	})
	if _, err := client.MergeEntities(context.Background(), "Old", "New"); err == nil || !strings.Contains(err.Error(), "upsert merge target") {
		t.Fatalf("error = %v", err)
	}
}
