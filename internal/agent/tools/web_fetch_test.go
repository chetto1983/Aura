package tools

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/web"
)

// fakeFetchEngine is a fetchEngine double: it replays a canned Page or error and
// records the convID/url the adapter passed (so the per-conversation pin scoping
// can be asserted).
type fakeFetchEngine struct {
	page      web.Page
	err       error
	gotConvID string
	gotURL    string
}

func (f *fakeFetchEngine) Fetch(_ context.Context, convID, rawURL string) (web.Page, error) {
	f.gotConvID, f.gotURL = convID, rawURL
	return f.page, f.err
}

// fakeSearchEngine is a searchEngine double.
type fakeSearchEngine struct {
	results []web.Result
	err     error
	gotPar  web.SearchParams
}

func (f *fakeSearchEngine) Search(_ context.Context, p web.SearchParams) ([]web.Result, error) {
	f.gotPar = p
	return f.results, f.err
}

// TestWebFetch_Spillover: a >24000-byte content_md returns a ToolResult with
// Truncated==true plus a sidecar file, and read_tool_output(tool_call_id) returns
// the tail bytes (D-21 spillover routed through NewResult — zero new code).
func TestWebFetch_Spillover(t *testing.T) {
	runDir := t.TempDir()
	big := strings.Repeat("x", 100_000) // well over the agent tool-result preview cap (tc.cap)
	eng := &fakeFetchEngine{page: web.Page{Title: "T", URL: "https://example.org/a", ContentMD: big}}
	wf := &WebFetch{Engine: eng}

	ctx := ctxWithRunDir("sess-fetch", "call-fetch", runDir)
	res, err := wf.Execute(ctx, json.RawMessage(`{"url":"https://example.org/a"}`))
	if err != nil {
		t.Fatalf("Execute returned a Go error (a successful fetch must be a ToolResult): %v", err)
	}
	if !res.Truncated {
		t.Fatalf("want Truncated==true for a >cap content_md, got Truncated=false (Bytes=%d)", res.Bytes)
	}
	if res.FullPath == "" {
		t.Fatal("want a sidecar FullPath for the spilled body, got empty")
	}
	if _, statErr := os.Stat(res.FullPath); statErr != nil {
		t.Fatalf("sidecar file not written: %v", statErr)
	}
	if !strings.Contains(res.Preview, "read_tool_output") {
		t.Fatalf("preview footer must point at read_tool_output, got: %q", res.Preview)
	}

	// The full bytes must be pageable via read_tool_output by the same tool_call_id.
	rto := ReadToolOutput{}
	page, rErr := rto.Execute(ctx, json.RawMessage(`{"tool_call_id":"call-fetch","offset":0,"limit":2048}`))
	if rErr != nil {
		t.Fatalf("read_tool_output failed: %v", rErr)
	}
	if !strings.Contains(page.Preview, "xxxx") {
		t.Fatalf("read_tool_output did not return the spilled body bytes, got: %.40q", page.Preview)
	}

	// The marshalled page that spilled must round-trip the convID from the ctx.
	if eng.gotConvID != "sess-fetch" {
		t.Fatalf("adapter must scope the fetch to the tool-call conversation; gotConvID=%q", eng.gotConvID)
	}
}

// TestWeb_SanitizedInlineError: an SSRF block / unavailable returned by either
// tool is an INLINE NewResult whose JSON has ONLY {error,reason,message} keys and
// greps clean for any resolved IP / internal host / header substring (D-26/27/28/41).
func TestWeb_SanitizedInlineError(t *testing.T) {
	// A blocked_url carrying a (sanitized) reason but never the leaked IP/host: the
	// engine returns a *web.WebError; the adapter must inline it, not error out.
	blockErr := &web.WebError{Code: web.CodeBlockedURL, Reason: web.ReasonPrivateOrMetadata, Message: "blocked"}

	t.Run("web_fetch", func(t *testing.T) {
		wf := &WebFetch{Engine: &fakeFetchEngine{err: blockErr}}
		ctx := ctxWith(t, "sess-e", "call-e")
		res, err := wf.Execute(ctx, json.RawMessage(`{"url":"http://169.254.169.254/latest/meta-data/"}`))
		if err != nil {
			t.Fatalf("a sanitized web block must be an inline ToolResult, not a Go error: %v", err)
		}
		assertSanitized(t, res.Preview)
	})

	t.Run("web_search", func(t *testing.T) {
		unavailErr := &web.WebError{Code: web.CodeSearchUnavailable, Reason: web.ReasonSearxngUnreachable, Message: "search backend unavailable"}
		ws := &WebSearch{Engine: &fakeSearchEngine{err: unavailErr}}
		ctx := ctxWith(t, "sess-e", "call-e2")
		res, err := ws.Execute(ctx, json.RawMessage(`{"query":"x"}`))
		if err != nil {
			t.Fatalf("a sanitized unavailable must be an inline ToolResult, not a Go error: %v", err)
		}
		assertSanitized(t, res.Preview)
	})
}

// assertSanitized verifies the inline error JSON exposes only {error,reason,
// message} and never leaks a resolved IP / internal host / header.
func assertSanitized(t *testing.T, preview string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(preview), &m); err != nil {
		t.Fatalf("inline error must be valid JSON, got %q: %v", preview, err)
	}
	allowed := map[string]struct{}{"error": {}, "reason": {}, "message": {}, "status_code": {}}
	for k := range m {
		if _, ok := allowed[k]; !ok {
			t.Fatalf("inline error has a disallowed key %q (only error/reason/message/status_code permitted): %v", k, m)
		}
	}
	// Concrete leaks only: a resolved IP literal, a header echo, or the internal
	// error's ip=/host= debug fields. The sanitized reason names a CLASS
	// ("private_or_metadata_target"), which is allowed — so "metadata" alone is not
	// a leak token here.
	for _, leak := range []string{"169.254.169.254", "127.0.0.1", "::1", "Set-Cookie", "ip=", "host=", "redirect="} {
		if strings.Contains(preview, leak) {
			t.Fatalf("inline error leaked %q: %q", leak, preview)
		}
	}
}
