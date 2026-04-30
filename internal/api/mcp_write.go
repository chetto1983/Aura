package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aura/aura/internal/mcp"
)

// mcpToolNameRe matches the tool name segment in
// POST /mcp/{server}/tools/{tool}. Constraining the URL keeps a
// malicious name from breaking out of the path or a log line.
var mcpToolNameRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,128}$`)

// mcpInvokeTimeout caps a single tool invocation so a hung MCP server
// can't pin a request goroutine indefinitely.
const mcpInvokeTimeout = 60 * time.Second

// mcpInvokeMaxOutput is the upper bound on the text we relay back to
// the dashboard. Tool results from web-search / scrape MCP servers
// can be very large; this protects the JSON encoder + browser memory.
const mcpInvokeMaxOutput = 64 * 1024

// mcpInvokeBodyLimit caps inbound argument JSON. 64 KiB is generous
// for any real tool call and protects against memory-bomb posts.
const mcpInvokeBodyLimit = 64 * 1024

func handleMCPInvoke(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serverName := r.PathValue("server")
		toolName := r.PathValue("tool")
		if !serverNameMatchesAny(serverName, deps.MCP) {
			writeError(w, deps.Logger, http.StatusNotFound, "mcp server not found")
			return
		}
		if !mcpToolNameRe.MatchString(toolName) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid tool name")
			return
		}

		client := findMCPClient(serverName, deps.MCP)
		if client == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "mcp server not found")
			return
		}
		if !toolAdvertised(client, toolName) {
			writeError(w, deps.Logger, http.StatusNotFound, "tool not advertised by this server")
			return
		}

		args, err := readMCPArgs(r)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), mcpInvokeTimeout)
		defer cancel()

		output, err := client.CallTool(ctx, toolName, args)
		clipped := clipMCPOutput(output)
		if err != nil {
			deps.Logger.Warn("api: mcp invoke failed", "server", serverName, "tool", toolName, "error", err)
			// Distinguish a server-reported `isError:true` (which the
			// client returns as `tool error: ...`) from a transport /
			// timeout failure. Both go back as 200 with ok:false so the
			// dashboard can show the message inline rather than treating
			// it as an HTTP failure.
			isToolErr := strings.HasPrefix(err.Error(), "tool error:")
			writeJSON(w, deps.Logger, http.StatusOK, MCPInvokeResponse{
				OK:      false,
				IsError: isToolErr,
				Output:  clipped,
				Error:   err.Error(),
			})
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, MCPInvokeResponse{
			OK:     true,
			Output: clipped,
		})
	}
}

// readMCPArgs parses the body. Empty body and `null` both mean "no
// arguments"; an explicit JSON object is passed through as-is.
func readMCPArgs(r *http.Request) (map[string]any, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, mcpInvokeBodyLimit)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, errors.New("body too large or unreadable")
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return map[string]any{}, nil
	}
	var args map[string]any
	dec := json.NewDecoder(strings.NewReader(trimmed))
	dec.UseNumber() // numbers stay precise for upstream tools that care about ints vs floats
	if err := dec.Decode(&args); err != nil {
		return nil, errors.New("body must be a JSON object")
	}
	return args, nil
}

func clipMCPOutput(s string) string {
	if len(s) <= mcpInvokeMaxOutput {
		return s
	}
	return s[:mcpInvokeMaxOutput] + "\n…[truncated]"
}

func serverNameMatchesAny(name string, clients []*mcp.Client) bool {
	for _, c := range clients {
		if c != nil && c.Name() == name {
			return true
		}
	}
	return false
}

func findMCPClient(name string, clients []*mcp.Client) *mcp.Client {
	for _, c := range clients {
		if c != nil && c.Name() == name {
			return c
		}
	}
	return nil
}

func toolAdvertised(c *mcp.Client, toolName string) bool {
	for _, t := range c.Tools() {
		if t.Name == toolName {
			return true
		}
	}
	return false
}
