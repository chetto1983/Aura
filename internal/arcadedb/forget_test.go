package arcadedb

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestForgetFilterBuildsAnchoredConjunction(t *testing.T) {
	clause, params, err := (ForgetFilter{
		SourceRunID: " run-1 ", Subject: " A ", Predicate: " likes ", Object: " B ", Entity: " C ",
	}).where()
	if err != nil {
		t.Fatalf("where: %v", err)
	}
	for _, fragment := range []string{"source_run_id", "outV().name", "predicate", "inV().name", "OR"} {
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
		return testResponse{Body: `{"result":[{"subject":"A","object":"B"},{"subject":"A","object":"B"}]}`}
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
			return testResponse{Body: `{"result":[{"subject":"A","object":"B"}]}`}
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
