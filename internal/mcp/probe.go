package mcp

import (
	"context"
	"fmt"
	"strings"
)

// probe.go is the structured, per-server live MCP probe (GOV-01). It is extracted
// from the cmd/aura `mcp doctor` text-writer (writeRuntimeCheck/writeRecipeChecks) so
// the governance board handler can fan a bounded probe per row and render structured
// results instead of parsing CLI text. The CLI doctor now renders from these results.
//
// Concurrency / isolation (28-RESEARCH Hard Problem 3): ProbeServer takes the
// caller-supplied context so the handler can bound EACH server with
// context.WithTimeout(r.Context(), 3*time.Second). A hung or dead server fails ONLY
// its own ProbeResult (OK=false, Err set) — it never panics and never blocks past the
// supplied deadline, because the dial + tools/list run under that context and the
// client is always closed. The probe targets ONLY the ManagedServer value the caller
// looked up in the loaded config (prohibition #5: configured-servers-only, never a
// URL/command supplied by a request body).

// ProbeResult is the structured outcome of probing one configured MCP server. Detail
// is a human-readable status line (already secret-redacted); Err is the failure when
// OK is false (also redacted). ToolCount is the live tools/list count on success.
type ProbeResult struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	ToolCount int    `json:"tool_count"`
	Detail    string `json:"detail"`
	Err       string `json:"err,omitempty"`
}

// ProbeServer dials one configured MCP server under ctx, completes the initialize
// handshake, and counts its advertised tools (tools/list) — the real "mounted tool
// count", not a mere LookPath reachability check (the heavier live probe GOV-01's
// acceptance requires). An HTTP/streamable server (no stdio command) is reported as
// reachable-by-config without a dial (the stdio client cannot speak to it). A dial,
// handshake, or list failure yields OK=false with a redacted Err for THAT server only.
//
// It is exported so cmd/aura's `mcp doctor` (package main) can render its text output
// from the same structured probe the governance handler consumes.
func ProbeServer(ctx context.Context, name string, server ManagedServer) ProbeResult {
	res := ProbeResult{Name: name}

	// HTTP/streamable endpoints have no stdio command to spawn; the stdio Client cannot
	// dial them, so report the configured endpoint rather than a tool count.
	if server.Type == ServerTypeStreamableHTTP || strings.TrimSpace(server.URL) != "" {
		res.OK = true
		res.Detail = RedactSecrets("http endpoint configured")
		return res
	}
	if strings.TrimSpace(server.Command) == "" {
		res.Detail = "runtime missing command"
		res.Err = res.Detail
		return res
	}

	client, err := Open(ctx, name, ServerConfig{Command: server.Command, Args: server.Args, Env: server.Env})
	if err != nil {
		res.Detail = "dial failed"
		res.Err = RedactSecrets(err.Error())
		return res
	}
	defer func() { _ = client.Close() }()

	tools, err := client.ListTools(ctx)
	if err != nil {
		res.Detail = "tools/list failed"
		res.Err = RedactSecrets(err.Error())
		return res
	}
	res.OK = true
	res.ToolCount = len(tools)
	res.Detail = RedactSecrets(fmt.Sprintf("ok (%d tools)", len(tools)))
	return res
}
