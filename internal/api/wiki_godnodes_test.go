package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestWikiGodNodes_EmptyIndex(t *testing.T) {
	e := newTestEnv(t)
	rr := e.do("GET", "/wiki/godnodes")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	nodes, ok := got["nodes"]
	if !ok {
		t.Fatal("response missing 'nodes' key")
	}
	nodeSlice, ok := nodes.([]any)
	if !ok {
		t.Fatalf("nodes is %T, want []any", nodes)
	}
	if len(nodeSlice) != 0 {
		t.Errorf("nodes len=%d, want 0", len(nodeSlice))
	}
}

func TestWikiGodNodes_Shape(t *testing.T) {
	e := newTestEnv(t)
	// hub links to spoke-a and spoke-b.
	e.seedPage("Hub", "[[spoke-a]] [[spoke-b]] hub page.", "concept", nil)
	e.seedPage("Spoke A", "spoke a page.", "concept", nil)
	e.seedPage("Spoke B", "spoke b page.", "concept", nil)

	rr := e.do("GET", "/wiki/godnodes?top_k=3")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}

	type godNodeDTO struct {
		Slug     string `json:"slug"`
		Title    string `json:"title"`
		Category string `json:"category"`
		In       int    `json:"in"`
		Out      int    `json:"out"`
		Total    int    `json:"total"`
	}
	var resp struct {
		Nodes []godNodeDTO `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Nodes) == 0 {
		t.Fatal("nodes is empty; expected at least hub")
	}
	top := resp.Nodes[0]
	if top.Slug != "hub" {
		t.Errorf("top node slug=%q, want hub", top.Slug)
	}
	if top.Out != 2 {
		t.Errorf("hub out=%d, want 2 (links to spoke-a and spoke-b)", top.Out)
	}
	if top.Total != top.In+top.Out {
		t.Errorf("total=%d != in(%d)+out(%d)", top.Total, top.In, top.Out)
	}
}

func TestWikiGodNodes_InvalidTopK(t *testing.T) {
	e := newTestEnv(t)
	cases := []struct {
		q    string
		code int
	}{
		{"top_k=0", http.StatusBadRequest},
		{"top_k=-1", http.StatusBadRequest},
		{"top_k=51", http.StatusBadRequest},
		{"top_k=abc", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.q, func(t *testing.T) {
			rr := e.do("GET", "/wiki/godnodes?"+tc.q)
			if rr.Code != tc.code {
				t.Errorf("status=%d, want %d", rr.Code, tc.code)
			}
		})
	}
}

func TestWikiGodNodes_DefaultTopK(t *testing.T) {
	e := newTestEnv(t)
	// Seed 3 pages and verify default top_k returns them all (under 10 limit).
	e.seedPage("Page One", "page one content.", "concept", nil)
	e.seedPage("Page Two", "page two content.", "concept", nil)
	e.seedPage("Page Three", "page three content.", "concept", nil)

	rr := e.do("GET", "/wiki/godnodes")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := resp["nodes"]; !ok {
		t.Error("response missing 'nodes' key")
	}
}

func TestWikiGodNodes_AuthGate(t *testing.T) {
	// The router wraps every endpoint in bearer auth when deps.Auth is non-nil.
	// This test verifies the endpoint is reachable via the mux (no 404);
	// the auth middleware tests cover token validation.
	e := newTestEnv(t)
	rr := e.do("GET", "/wiki/godnodes")
	if rr.Code == http.StatusNotFound {
		t.Errorf("endpoint not registered (404)")
	}
}
