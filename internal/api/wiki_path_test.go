package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestWikiPath_MissingParams(t *testing.T) {
	e := newTestEnv(t)
	cases := []struct {
		url  string
		code int
	}{
		{"/wiki/path", http.StatusBadRequest},
		{"/wiki/path?from=alpha", http.StatusBadRequest},
		{"/wiki/path?to=beta", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			rr := e.do("GET", tc.url)
			if rr.Code != tc.code {
				t.Errorf("url=%s: status=%d, want %d", tc.url, rr.Code, tc.code)
			}
		})
	}
}

func TestWikiPath_InvalidMaxHops(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Alpha", "body", "concept", nil)
	e.seedPage("Beta", "body", "concept", nil)
	cases := []string{
		"/wiki/path?from=alpha&to=beta&max_hops=0",
		"/wiki/path?from=alpha&to=beta&max_hops=-1",
		"/wiki/path?from=alpha&to=beta&max_hops=21",
		"/wiki/path?from=alpha&to=beta&max_hops=abc",
	}
	for _, q := range cases {
		t.Run(q, func(t *testing.T) {
			rr := e.do("GET", q)
			if rr.Code != http.StatusBadRequest {
				t.Errorf("status=%d, want 400", rr.Code)
			}
		})
	}
}

func TestWikiPath_HappyPath(t *testing.T) {
	e := newTestEnv(t)
	// alpha -> beta -> gamma chain via [[wikilinks]]
	e.seedPage("Alpha", "see [[beta]] for more.", "concept", nil)
	e.seedPage("Beta", "connects to [[gamma]].", "concept", nil)
	e.seedPage("Gamma", "leaf page.", "concept", nil)

	rr := e.do("GET", "/wiki/path?from=alpha&to=gamma&max_hops=5")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var resp struct {
		Path []string `json:"path"`
		Hops int      `json:"hops"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Path) == 0 {
		t.Fatal("path is empty")
	}
	if resp.Path[0] != "alpha" || resp.Path[len(resp.Path)-1] != "gamma" {
		t.Errorf("path = %v; want alpha at start, gamma at end", resp.Path)
	}
	if resp.Hops != len(resp.Path)-1 {
		t.Errorf("hops=%d, want %d", resp.Hops, len(resp.Path)-1)
	}
}

func TestWikiPath_NoPath(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Alpha", "no links.", "concept", nil)
	e.seedPage("Beta", "no links.", "concept", nil)

	rr := e.do("GET", "/wiki/path?from=alpha&to=beta")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d", rr.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, hasReason := resp["reason"]; !hasReason {
		t.Error("expected 'reason' key in no-path response")
	}
	if path, ok := resp["path"]; ok && path != nil {
		t.Errorf("expected null path, got %v", path)
	}
}

func TestWikiPath_AuthGate(t *testing.T) {
	e := newTestEnv(t)
	rr := e.do("GET", "/wiki/path?from=a&to=b")
	if rr.Code == http.StatusNotFound {
		t.Error("endpoint not registered (404)")
	}
}
