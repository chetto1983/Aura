package arcadedb

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestForgetBySourceDetachesUntilLastSourceThenDeletes(t *testing.T) {
	stored := []FactSource{{RunID: "run-1", MemoryIDs: []string{"m1"}}, {RunID: "run-2", MemoryIDs: []string{"m2"}}}
	deleted := false
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		switch {
		case strings.HasPrefix(statement, "SELECT @rid AS rid"):
			run, _ := params["source_run_id"].(string)
			present := false
			for _, source := range stored {
				present = present || source.RunID == run
			}
			if deleted || !present {
				return testResponse{Body: `{"result":[]}`}
			}
			body, _ := json.Marshal(map[string]any{"result": []any{map[string]any{
				"rid": "#1:0", "subject": "A", "object": "B", "sources": sourcesParam(stored),
			}}})
			return testResponse{Body: string(body)}
		case strings.HasPrefix(statement, "UPDATE FACT SET sources"):
			stored = factSources(params["sources"])
			return testResponse{Body: `{"result":[{"count":1}]}`}
		case strings.HasPrefix(statement, "DELETE FROM FACT"):
			deleted = true
			return testResponse{Body: `{"result":[{"count":1}]}`}
		case strings.HasPrefix(statement, "DELETE FROM Entity"):
			return testResponse{Body: `{"result":[]}`}
		default:
			return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected statement"}`}
		}
	})
	first, err := client.Forget(context.Background(), ForgetFilter{SourceRunID: "run-1"})
	if err != nil {
		t.Fatalf("detach first source: %v", err)
	}
	if first.Facts != 1 || deleted || len(stored) != 1 || stored[0].RunID != "run-2" {
		t.Fatalf("first=%+v deleted=%v sources=%+v", first, deleted, stored)
	}
	second, err := client.Forget(context.Background(), ForgetFilter{SourceRunID: "run-2"})
	if err != nil {
		t.Fatalf("detach final source: %v", err)
	}
	if second.Facts != 1 || !deleted {
		t.Fatalf("second=%+v deleted=%v", second, deleted)
	}
	var sent strings.Builder
	for _, request := range *requests {
		sent.WriteString(request.Payload["command"].(string))
		sent.WriteByte('\n')
	}
	if joined := sent.String(); strings.Contains(joined, "WHERE source_run_id") {
		t.Fatalf("retired flat provenance was queried:\n%s", joined)
	}
}

func TestForgetFilterBuildsAnchoredConjunction(t *testing.T) {
	clause, params, err := (ForgetFilter{
		SourceRunID: " run-1 ", Subject: " A ", Predicate: " likes ", Object: " B ", Entity: " C ",
	}).where()
	if err != nil {
		t.Fatalf("where: %v", err)
	}
	for _, fragment := range []string{"sources CONTAINS", "outV().name", "predicate", "inV().name", "OR"} {
		if !strings.Contains(clause, fragment) {
			t.Fatalf("clause %q missing %q", clause, fragment)
		}
	}
	if params["source_run_id"] != "run-1" || params["entity"] != "C" {
		t.Fatalf("params = %v", params)
	}
	for _, filter := range []ForgetFilter{{}, {Predicate: "likes"}, {Object: "B"}} {
		if _, _, err := filter.where(); err == nil {
			t.Fatalf("unanchored filter accepted: %+v", filter)
		}
	}
}

func forgetResponder(request recordedRequest) testResponse {
	statement, _ := request.Payload["command"].(string)
	params, _ := request.Payload["params"].(map[string]any)
	switch {
	case strings.Contains(statement, "outV().name AS subject"):
		return testResponse{Body: `{"result":[
			{"rid":"#1:0","subject":"A","object":"B","sources":[{"run_id":"run-1","memory_ids":["m1"]}]},
			{"rid":"#1:1","subject":"A","object":"B","sources":[{"run_id":"run-1","memory_ids":["m2"]}]}
		]}`}
	case strings.Contains(statement, "bothE().size() AS degree"):
		if params["name"] == "A" {
			return testResponse{Body: `{"result":[{"degree":2}]}`}
		}
		return testResponse{Body: `{"result":[{"degree":3}]}`}
	case strings.Contains(statement, "count(*) AS n"):
		return testResponse{Body: `{"result":[{"n":2}]}`}
	case strings.HasPrefix(strings.TrimSpace(statement), "DELETE FROM FACT"):
		return testResponse{Body: `{"result":[{"count":2}]}`}
	case strings.HasPrefix(strings.TrimSpace(statement), "DELETE FROM Entity"):
		if params["name"] == "A" {
			return testResponse{Body: `{"result":[{"count":1}]}`}
		}
		return testResponse{Body: `{"result":[]}`}
	default:
		return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected statement"}`}
	}
}

func TestForgetDryRunCountsFactsAndStrandableEntities(t *testing.T) {
	client, requests := routedClient(t, forgetResponder)
	got, err := client.Forget(context.Background(), ForgetFilter{SourceRunID: "run-1", DryRun: true})
	if err != nil {
		t.Fatalf("Forget dry run: %v", err)
	}
	if got != (ForgetResult{Facts: 2, Entities: 1, DryRun: true}) {
		t.Fatalf("result = %+v", got)
	}
	for _, request := range *requests {
		statement, _ := request.Payload["command"].(string)
		if strings.HasPrefix(strings.TrimSpace(statement), "DELETE") {
			t.Fatalf("dry run mutated: %s", statement)
		}
	}
}

func TestForgetDeletesFactsAndPrunesOnlyOrphans(t *testing.T) {
	client, requests := routedClient(t, forgetResponder)
	got, err := client.Forget(context.Background(), ForgetFilter{Entity: "A"})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if got != (ForgetResult{Facts: 2, Entities: 1}) {
		t.Fatalf("result = %+v", got)
	}
	if len(*requests) != 4 {
		t.Fatalf("requests = %d, want endpoint read + fact delete + two prune attempts", len(*requests))
	}
}

func TestForgetKeepsOrphansAndSkipsEmptyDelete(t *testing.T) {
	client, requests := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "outV().name AS subject") {
			return testResponse{Body: `{"result":[]}`}
		}
		return testResponse{Status: http.StatusBadRequest, Body: `{"detail":"unexpected mutation"}`}
	})
	got, err := client.Forget(context.Background(), ForgetFilter{Subject: "missing", KeepOrphans: true})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if got.Facts != 0 || got.Entities != 0 || len(*requests) != 1 {
		t.Fatalf("result=%+v requests=%d", got, len(*requests))
	}
}

func TestForgetSurfacesEndpointAndPruneFailures(t *testing.T) {
	srv := fakeServer(t, http.StatusInternalServerError, `{"detail":"boom"}`, nil)
	if _, err := mustClient(t, srv.URL).Forget(context.Background(), ForgetFilter{Subject: "A"}); err == nil || !strings.Contains(err.Error(), "read facts") {
		t.Fatalf("endpoint error = %v", err)
	}
	client, _ := routedClient(t, func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		if strings.Contains(statement, "outV().name AS subject") {
			return testResponse{Body: `{"result":[{"rid":"#1:0","subject":"A","object":"B","sources":[{"run_id":"run-1","memory_ids":[]}]}]}`}
		}
		if strings.HasPrefix(strings.TrimSpace(statement), "DELETE FROM FACT") {
			return testResponse{Body: `{"result":[]}`}
		}
		return testResponse{Status: http.StatusInternalServerError, Body: `{"detail":"prune failed"}`}
	})
	if _, err := client.Forget(context.Background(), ForgetFilter{Subject: "A"}); err == nil || !strings.Contains(err.Error(), "prune entity") {
		t.Fatalf("prune error = %v", err)
	}
}
