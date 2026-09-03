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
		case strings.Contains(statement, "inV().name AS other"):
			return testResponse{Body: `{"result":[{"rid":"#5:0",` +
				`"statement":"M. Bellini specialises in fraud.","other":"Fraud"}]}`}
		case strings.Contains(statement, "outV().name AS other"):
			return testResponse{Body: `{"result":[{"rid":"#5:1",` +
				`"statement":"Questura employs M. Bellini.","other":"Questura"}]}`}
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

	// One move script per scanned fact, each naming the original by @rid so the
	// replacement and the deletion cannot disagree about which fact moved.
	moves := payloadsContaining(*requests, "DELETE FROM FACT WHERE @rid")
	if len(moves) != 2 {
		t.Fatalf("move scripts = %d, want one per direction", len(moves))
	}
	// The survivor takes over the endpoint the duplicate held, and the statement is
	// rewritten with it -- the outgoing fact becomes the target's assertion, the
	// incoming one stays the other entity's assertion ABOUT the target.
	outgoing, incoming := moves[0]["params"].(map[string]any), moves[1]["params"].(map[string]any)
	if outgoing["subject_name"] != "Marta Bellini" || outgoing["object_name"] != "Fraud" {
		t.Fatalf("outgoing endpoints = %v -> %v", outgoing["subject_name"], outgoing["object_name"])
	}
	if incoming["subject_name"] != "Questura" || incoming["object_name"] != "Marta Bellini" {
		t.Fatalf("incoming endpoints = %v -> %v", incoming["subject_name"], incoming["object_name"])
	}
	if outgoing["statement"] != "Marta Bellini specialises in fraud." {
		t.Fatalf("statement not rewritten onto the survivor: %v", outgoing["statement"])
	}
	// fact_key is left for reindexFacts to rebuild once the originals are gone, so
	// the unique key cannot collide while both edges exist.
	if outgoing["fact_key"] != nil {
		t.Fatalf("fact_key = %v, want nil until the reindex", outgoing["fact_key"])
	}

	if len(payloadsContaining(*requests, "DETACH DELETE")) != 1 {
		t.Fatalf("source removal missing: %+v", *requests)
	}
}

// payloadsContaining is the assertion this file needed once the merge stopped being
// one statement per direction: naming a request by its index breaks every time a step
// is added, and says nothing about which step it was.
func payloadsContaining(requests []recordedRequest, needle string) []map[string]any {
	var found []map[string]any
	for _, request := range requests {
		if statement, _ := request.Payload["command"].(string); strings.Contains(statement, needle) {
			found = append(found, request.Payload)
		}
	}
	return found
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
