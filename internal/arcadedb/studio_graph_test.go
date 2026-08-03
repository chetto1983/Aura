package arcadedb

import (
	"context"
	"net/http"
	"testing"
)

func TestQueryStudioGraphUsesReadEndpointAndDecodesCompactGraph(t *testing.T) {
	seen := &captured{}
	srv := fakeServer(t, http.StatusOK, `{
		"result": {
			"vertices": [{"r":"#1:0","t":"Entity","p":{"name":"Aura"},"i":2,"o":3}],
			"edges": [{"r":"#5:0","t":"FACT","p":{"predicate":"uses"},"i":"#1:1","o":"#1:0"}],
			"records": [{"@rid":"#5:0"}]
		}
	}`, seen)

	got, err := mustClient(t, srv.URL).QueryStudioGraph(
		context.Background(),
		"SELECT FROM FACT LIMIT :limit",
		map[string]any{"limit": 12},
		12,
	)
	if err != nil {
		t.Fatalf("QueryStudioGraph: %v", err)
	}
	if seen.path != "/api/v1/query/aura" {
		t.Fatalf("path = %q, want the read-only query endpoint", seen.path)
	}
	if seen.payload["serializer"] != "studio" {
		t.Fatalf("serializer = %v, want studio", seen.payload["serializer"])
	}
	if seen.payload["limit"] != float64(12) {
		t.Fatalf("transport limit = %v, want 12", seen.payload["limit"])
	}
	params, ok := seen.payload["params"].(map[string]any)
	if !ok || params["limit"] != float64(12) {
		t.Fatalf("transport params = %#v, want limit=12", seen.payload["params"])
	}
	if len(got.Vertices) != 1 || got.Vertices[0].RID != "#1:0" || got.Vertices[0].Degree() != 5 {
		t.Fatalf("vertices = %+v", got.Vertices)
	}
	if len(got.Edges) != 1 || got.Edges[0].Out != "#1:0" || got.Edges[0].In != "#1:1" {
		t.Fatalf("edges = %+v", got.Edges)
	}
}

func TestQueryStudioGraphRejectsOutOfRangeLimitBeforeIO(t *testing.T) {
	if MaxStudioGraphLimit != 200 {
		t.Fatalf("MaxStudioGraphLimit = %d, want the transport contract 200", MaxStudioGraphLimit)
	}
	seen := &captured{}
	srv := fakeServer(t, http.StatusOK, `{"result":{"vertices":[],"edges":[],"records":[]}}`, seen)
	client := mustClient(t, srv.URL)

	for _, limit := range []int{0, -1, MaxStudioGraphLimit + 1} {
		if _, err := client.QueryStudioGraph(context.Background(), "SELECT FROM Entity", nil, limit); err == nil {
			t.Fatalf("limit %d was accepted", limit)
		}
	}
	if seen.path != "" {
		t.Fatalf("invalid graph read reached %q", seen.path)
	}
	if _, err := client.QueryStudioGraph(context.Background(), "SELECT FROM Entity", nil, 200); err != nil {
		t.Fatalf("maximum transport limit was rejected: %v", err)
	}
}

func TestQueryStudioGraphRejectsEmptyStatementBeforeIO(t *testing.T) {
	seen := &captured{}
	srv := fakeServer(t, http.StatusOK, `{"result":{}}`, seen)

	if _, err := mustClient(t, srv.URL).QueryStudioGraph(context.Background(), " \n\t", nil, 1); err == nil {
		t.Fatal("blank Studio graph query was accepted")
	}
	if seen.path != "" {
		t.Fatalf("blank graph read reached %q", seen.path)
	}
}

func TestQueryStudioGraphNormalizesMissingCollections(t *testing.T) {
	seen := &captured{}
	srv := fakeServer(t, http.StatusOK, `{"result":{}}`, seen)

	got, err := mustClient(t, srv.URL).QueryStudioGraph(context.Background(), "SELECT FROM Entity", nil, 1)
	if err != nil {
		t.Fatalf("QueryStudioGraph: %v", err)
	}
	if got.Vertices == nil || got.Edges == nil {
		t.Fatalf("missing collections were not normalized: %+v", got)
	}
	if _, present := seen.payload["params"]; present {
		t.Fatalf("empty params leaked into payload: %+v", seen.payload)
	}
}

func TestQueryStudioGraphSurfacesServerFailure(t *testing.T) {
	seen := &captured{}
	srv := fakeServer(t, http.StatusInternalServerError, `{"error":"query failed"}`, seen)

	if _, err := mustClient(t, srv.URL).QueryStudioGraph(context.Background(), "SELECT FROM Entity", nil, 1); err == nil {
		t.Fatal("Studio graph server failure was accepted")
	}
}
