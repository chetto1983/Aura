package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/web"
)

// TestWebSearch_DeferredSpec: both web tools are Deferred:true (so the manifest
// shows them by Name + Summary only until tool_search loads the full spec), and
// the web_search schema exposes EXACTLY the D-09 public controls — never raw
// engines/safesearch pass-through (D-10/T-07-31).
func TestWebSearch_DeferredSpec(t *testing.T) {
	ws := (&WebSearch{}).Spec()
	wf := (&WebFetch{}).Spec()

	if !ws.Deferred {
		t.Error("web_search MUST be Deferred:true")
	}
	if !wf.Deferred {
		t.Error("web_fetch MUST be Deferred:true")
	}
	if ws.Name != "web_search" || wf.Name != "web_fetch" {
		t.Fatalf("names: got %q / %q", ws.Name, wf.Name)
	}
	if ws.Summary == "" || wf.Summary == "" {
		t.Fatal("both tools must carry a one-line Summary for the manifest")
	}

	schema := string(ws.Parameters)
	for _, want := range []string{`"query"`, `"max_results"`, `"category"`, `"language"`, `"time_range"`, `"domains"`, `"include_metadata"`} {
		if !strings.Contains(schema, want) {
			t.Errorf("web_search schema missing the D-09 control %s", want)
		}
	}
	// The category enum is restricted to general|news (D-11); images is deferred.
	if !strings.Contains(schema, `"general"`) || !strings.Contains(schema, `"news"`) {
		t.Errorf("web_search category enum must be general|news, schema=%s", schema)
	}
	// D-10/T-07-31: NO raw SearXNG pass-through controls in the model-visible schema.
	for _, forbidden := range []string{`"engines"`, `"safesearch"`, `"pageno"`, `"format"`} {
		if strings.Contains(schema, forbidden) {
			t.Errorf("web_search schema leaks a forbidden raw control %s (D-10): %s", forbidden, schema)
		}
	}

	// web_fetch exposes only the url control.
	fschema := string(wf.Parameters)
	if !strings.Contains(fschema, `"url"`) {
		t.Errorf("web_fetch schema must expose url, got %s", fschema)
	}
}

// TestWebSearch_Success verifies the happy path: the adapter passes the D-09 args
// through to the engine and returns the ranked results inline (not a Go error).
func TestWebSearch_Success(t *testing.T) {
	eng := &fakeSearchEngine{results: []web.Result{{Title: "A", URL: "https://a.example/", Snippet: "s"}}}
	ws := &WebSearch{Engine: eng}
	ctx := ctxWith(t, "sess-s", "call-s")

	res, err := ws.Execute(ctx, json.RawMessage(`{"query":"neo4j","max_results":5,"category":"news","domains":["wikipedia.org"]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if eng.gotPar.Query != "neo4j" || eng.gotPar.MaxResults != 5 || eng.gotPar.Category != "news" {
		t.Fatalf("args not threaded to the engine: %+v", eng.gotPar)
	}
	if len(eng.gotPar.Domains) != 1 || eng.gotPar.Domains[0] != "wikipedia.org" {
		t.Fatalf("domains not threaded: %+v", eng.gotPar.Domains)
	}
	var out struct {
		Results []web.Result `json:"results"`
	}
	if uErr := json.Unmarshal([]byte(res.Preview), &out); uErr != nil {
		t.Fatalf("result preview not JSON: %v (%q)", uErr, res.Preview)
	}
	if len(out.Results) != 1 || out.Results[0].Title != "A" {
		t.Fatalf("results not returned inline: %+v", out.Results)
	}
}

// staticEngine confirms *web.Client satisfies both adapter seams at compile time
// (the production wiring in cmd/aura/main.go depends on this).
var _ searchEngine = (*web.Client)(nil)
var _ fetchEngine = (*web.Client)(nil)
