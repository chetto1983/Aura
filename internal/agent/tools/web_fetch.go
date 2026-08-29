package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/web"
)

// fetchEngine is the fetch seam the adapter delegates to (mirrors
// Execute.Runner sandbox.Runner — an injectable interface, not a concrete type,
// so the adapter is unit-testable with a double). *web.Client satisfies it.
type fetchEngine interface {
	Fetch(ctx context.Context, convID, rawURL string) (web.Page, error)
}

// WebFetch is the thin Deferred:true adapter over the shared web.Client fetch
// engine (D-01). It marshals the model-supplied URL, delegates to the SSRF-
// hardened state machine (scheme allowlist → DNS pin → per-hop redirect
// revalidation → MIME/size gate → readability→markdown), and routes the
// successful page through NewResult so a large content_md spills to the sidecar
// automatically (D-21 — ZERO new spillover code). A sanitized *web.WebError
// (blocked_url / unsupported_scheme / unsupported_content_type / response_too_large
// / timeout / http_error / extraction_failed) becomes an inline {error,reason,
// message} object so the model self-corrects (D-41). The resolved IP / internal
// host / response headers / redirect chain NEVER reach the returned string (D-27).
type WebFetch struct {
	Engine fetchEngine
}

// webFetchArgs is the single model-supplied control (D-15). The conversation ID
// the engine needs to scope its DNS pin comes from the tool-call context the
// agent injects, never from the model.
type webFetchArgs struct {
	URL string `json:"url"`
}

func (e *WebFetch) Spec() Spec {
	params := json.RawMessage(`{
  "type": "object",
  "properties": {
    "url": {"type": "string", "description": "The absolute http(s) URL of a public web page to fetch. Only http and https are supported."}
  },
  "required": ["url"]
}`)
	return Spec{
		Name: "web_fetch",
		// The summary is the ONLY line the model sees before deciding whether to load this
		// tool's schema, so it must name the JSON/API case explicitly. Measured 2026-08-29:
		// with the previous "Fetch a public web page … as readable markdown", a request to
		// read a JSON endpoint went to `shell_exec curl` instead — the tool was widened to
		// handle it and the model still had no way to know.
		Summary: "Fetch a public URL — a web page as markdown, or a JSON/CSV/text API response verbatim. Prefer over curl.",
		Description: "Fetch a single public URL over http/https. An HTML page comes back as clean, readable markdown (navigation, ads, and footer chrome are stripped); a JSON, CSV, XML, YAML or plain-text response comes back VERBATIM in a labelled code fence with warning=raw_content, so use this for a REST/JSON API too rather than shelling out to curl — only this path applies the SSRF guard, the size cap and the fetch audit trail. " +
			"Use it to read a page you found via web_search or a URL the user gave you. " +
			"Only http and https URLs to public hosts are allowed — private, loopback, and cloud-metadata targets are blocked, and a redirect to a blocked target is rejected. " +
			"A large page is truncated in the preview with a tool_call_id you can page through with read_tool_output. " +
			"A block, an unsupported scheme, an oversized body, a timeout, or an extraction failure come back as a small {error,reason,message} object you should read and adapt to — they are not tool failures. " +
			"Example: {\"url\":\"https://en.wikipedia.org/wiki/Knowledge_graph\"}.",
		Parameters: params,
		Deferred:   true,
	}
}

// Execute unmarshals the URL, reads the conversation scope from the tool-call
// context (so the engine's DNS pin is per-conversation), delegates to the engine,
// and routes the outcome through NewResult. A *web.WebError is sanitized to an
// inline object (D-41); a genuine non-web infra fault propagates as a Go error.
func (e *WebFetch) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a webFetchArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("web_fetch args: %w", err)
	}
	a.URL = strings.TrimSpace(a.URL)
	if a.URL == "" {
		return ToolResult{}, fmt.Errorf("web_fetch: url is required")
	}

	page, err := e.Engine.Fetch(ctx, fetchConvID(ctx), a.URL)
	if err != nil {
		return webErrorResult(ctx, err)
	}

	out, mErr := json.Marshal(page)
	if mErr != nil {
		return ToolResult{}, fmt.Errorf("web_fetch: marshal page: %w", mErr)
	}
	// Success path: a large content_md spills to the sidecar via NewResult (D-21).
	return NewResult(ctx, string(out))
}

// fetchConvID reads the conversation/session id the agent injected via
// WithToolCallContext; the engine keys its per-conversation DNS pin by it. An
// empty id (e.g. a bare ctx in a unit test) is fine — the pin simply scopes to
// the empty conversation.
func fetchConvID(ctx context.Context) string {
	tc, ok := toolCallCtx(ctx)
	if !ok {
		return ""
	}
	return tc.sessionID
}

// webErrorResult maps an engine error to the model-visible result channel. A
// *web.WebError (or a rich internal error unwrapped to one) is sanitized to its
// {error,reason,message,status_code?} wire shape and returned INLINE via
// NewResult so the model self-corrects (D-41); the sanitized struct carries no
// IP/host/header/body/redirect-chain (D-26/27/28). Any non-web error is a genuine
// infra fault and propagates to the loop as a Go error.
func webErrorResult(ctx context.Context, err error) (ToolResult, error) {
	we, ok := web.AsWebError(err)
	if !ok {
		return ToolResult{}, err
	}
	b, mErr := we.JSON()
	if mErr != nil {
		return ToolResult{}, fmt.Errorf("web tool: marshal sanitized error: %w", mErr)
	}
	return NewResult(ctx, string(b))
}
