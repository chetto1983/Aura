// Command aura_mcp_server bridges Claude Code (or any MCP host) to a running
// Aura instance over stdio JSON-RPC 2.0.
//
// Exposes a curated subset of Aura's HTTP API as MCP tools so the host LLM
// can: search the wiki, read sources, send chats, query user/operational
// memory, list scheduled tasks, and pull live stats — all by speaking to
// Aura at http://localhost:18080 (configurable) with a bearer token (env
// AURA_TOKEN, minted via Telegram /login).
//
// Wire format: MCP protocol 2024-11-05 over stdio, newline-delimited JSON-RPC.
// Reference: docs/aura-mcp-server-design-2026-05-23.md.
//
// Usage:
//
//	AURA_TOKEN=<bearer> go run ./cmd/aura_mcp_server --api http://localhost:18080
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	mcpProtocolVersion = "2024-11-05"
	serverName         = "aura-mcp-server"
	serverVersion      = "0.1.0"
	defaultAPIBase     = "http://localhost:18080"
	defaultTimeout     = 30 * time.Second
)

func main() {
	apiBase := flag.String("api", defaultAPIBase, "Aura API base URL")
	timeout := flag.Duration("timeout", defaultTimeout, "HTTP timeout per request")
	flag.Parse()

	token := strings.TrimSpace(os.Getenv("AURA_TOKEN"))
	if token == "" {
		fatal("AURA_TOKEN env var is required (mint via Telegram /login)")
	}

	srv := &server{
		client: &auraClient{
			base:    strings.TrimRight(*apiBase, "/"),
			token:   token,
			timeout: *timeout,
			http:    &http.Client{Timeout: *timeout},
		},
		tools: builtinTools(),
	}

	if err := srv.serve(os.Stdin, os.Stdout); err != nil && !errors.Is(err, io.EOF) {
		fatal("serve: %v", err)
	}
}

// ── server ───────────────────────────────────────────────────────────────────

type server struct {
	client *auraClient
	tools  []tool
}

func (s *server) serve(r io.Reader, w io.Writer) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024) // up to 16 MB per frame

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = enc.Encode(rpcError(nil, -32700, "parse error: "+err.Error()))
			continue
		}
		resp := s.dispatch(context.Background(), &req)
		if resp == nil {
			continue // notifications return nothing
		}
		_ = enc.Encode(resp)
	}
	return scanner.Err()
}

func (s *server) dispatch(ctx context.Context, req *rpcRequest) any {
	switch req.Method {
	case "initialize":
		return rpcResult(req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{"name": serverName, "version": serverVersion},
		})
	case "notifications/initialized":
		return nil
	case "ping":
		return rpcResult(req.ID, map[string]any{})
	case "tools/list":
		entries := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			entries = append(entries, map[string]any{
				"name":        t.name,
				"description": t.description,
				"inputSchema": t.inputSchema,
			})
		}
		return rpcResult(req.ID, map[string]any{"tools": entries})
	case "tools/call":
		return s.handleToolCall(ctx, req)
	default:
		return rpcError(req.ID, -32601, "method not found: "+req.Method)
	}
}

func (s *server) handleToolCall(ctx context.Context, req *rpcRequest) any {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &call); err != nil {
		return rpcError(req.ID, -32602, "invalid tool call params: "+err.Error())
	}
	for _, t := range s.tools {
		if t.name != call.Name {
			continue
		}
		out, err := t.handler(ctx, s.client, call.Arguments)
		if err != nil {
			return rpcResult(req.ID, map[string]any{
				"isError": true,
				"content": []map[string]any{{"type": "text", "text": "Error: " + err.Error()}},
			})
		}
		return rpcResult(req.ID, map[string]any{
			"isError": false,
			"content": []map[string]any{{"type": "text", "text": out}},
		})
	}
	return rpcResult(req.ID, map[string]any{
		"isError": true,
		"content": []map[string]any{{"type": "text", "text": "Error: unknown tool " + call.Name}},
	})
}

// ── JSON-RPC envelope ────────────────────────────────────────────────────────

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func rpcResult(id json.RawMessage, result any) map[string]any {
	out := map[string]any{"jsonrpc": "2.0", "result": result}
	if len(id) > 0 {
		out["id"] = id
	}
	return out
}

func rpcError(id json.RawMessage, code int, message string) map[string]any {
	out := map[string]any{
		"jsonrpc": "2.0",
		"error":   map[string]any{"code": code, "message": message},
	}
	if len(id) > 0 {
		out["id"] = id
	}
	return out
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "aura_mcp_server: "+format+"\n", args...)
	os.Exit(1)
}

// ── Aura HTTP client ─────────────────────────────────────────────────────────

type auraClient struct {
	base    string
	token   string
	timeout time.Duration
	http    *http.Client
}

func (c *auraClient) do(ctx context.Context, method, path string, query url.Values, body any) ([]byte, error) {
	u := c.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var bodyReader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("aura %s %s: %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

// ── Tool registry ────────────────────────────────────────────────────────────

type tool struct {
	name        string
	description string
	inputSchema map[string]any
	handler     func(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error)
}

func builtinTools() []tool {
	tools := []tool{
		{
			name:        "aura_wiki_search",
			description: "Search Aura's wiki via hybrid FTS + vector. Returns top-K pages with slug, title, score, snippet.",
			inputSchema: schemaObject(
				prop("query", "string", "search query (free text)"),
				prop("limit", "integer", "max results (default 10, max 50)"),
			),
			handler: handleWikiSearch,
		},
		{
			name:        "aura_wiki_read",
			description: "Read a wiki page by slug. Returns the full markdown with frontmatter.",
			inputSchema: schemaObject(prop("slug", "string", "wiki page slug")),
			handler:     handleWikiRead,
		},
		{
			name:        "aura_wiki_pages",
			description: "List wiki pages with optional category filter. Returns slug + title + category.",
			inputSchema: schemaObject(
				prop("category", "string", "filter by category (optional)"),
				prop("limit", "integer", "max results (default 50, max 200)"),
			),
			handler: handleWikiPages,
		},
		{
			name:        "aura_source_list",
			description: "List sources (PDFs, audio memos, ingested docs). Returns id + kind + title + status.",
			inputSchema: schemaObject(prop("limit", "integer", "max results (default 50)")),
			handler:     handleSourceList,
		},
		{
			name:        "aura_source_read",
			description: "Read a source's OCR/markdown content by source id (src_<hex>).",
			inputSchema: schemaObject(prop("id", "string", "source id, e.g. src_a1b2c3d4")),
			handler:     handleSourceRead,
		},
		{
			name:        "aura_chat",
			description: "Send a chat message to Aura (buffered, non-streaming). Returns the reply text + tool calls made.",
			inputSchema: schemaObject(
				prop("message", "string", "the message to send"),
				prop("thread_id", "string", "optional thread/conversation id for continuity"),
			),
			handler: handleChat,
		},
		{
			name:        "aura_tasks_list",
			description: "List scheduled tasks. Returns name + cron + last/next run + status.",
			inputSchema: schemaObject(),
			handler:     handleTasksList,
		},
		{
			name:        "aura_skills_list",
			description: "List installed skills. Returns name + description + version.",
			inputSchema: schemaObject(),
			handler:     handleSkillsList,
		},
		{
			name:        "aura_mcp_servers",
			description: "List configured MCP servers and their health/tool counts.",
			inputSchema: schemaObject(),
			handler:     handleMCPServers,
		},
		{
			name:        "aura_health",
			description: "Aura health check + version + uptime + module status.",
			inputSchema: schemaObject(),
			handler:     handleHealth,
		},
	}
	tools = append(tools, apiBackedTools()...)
	return tools
}

// ── Tool handlers ────────────────────────────────────────────────────────────

func handleWikiSearch(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Query) == "" {
		return "", errors.New("query is required")
	}
	if args.Limit <= 0 {
		args.Limit = 10
	}
	if args.Limit > 50 {
		args.Limit = 50
	}
	q := url.Values{"q": {args.Query}, "limit": {fmt.Sprint(args.Limit)}}
	data, err := c.do(ctx, http.MethodGet, "/api/wiki/search", q, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleWikiRead(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Slug) == "" {
		return "", errors.New("slug is required")
	}
	q := url.Values{"slug": {args.Slug}}
	data, err := c.do(ctx, http.MethodGet, "/api/wiki/page", q, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleWikiPages(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		Category string `json:"category"`
		Limit    int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if args.Limit <= 0 {
		args.Limit = 50
	}
	if args.Limit > 200 {
		args.Limit = 200
	}
	q := url.Values{"limit": {fmt.Sprint(args.Limit)}}
	if args.Category != "" {
		q.Set("category", args.Category)
	}
	data, err := c.do(ctx, http.MethodGet, "/api/wiki/pages", q, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleSourceList(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(raw, &args)
	if args.Limit <= 0 {
		args.Limit = 50
	}
	q := url.Values{"limit": {fmt.Sprint(args.Limit)}}
	data, err := c.do(ctx, http.MethodGet, "/api/sources", q, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleSourceRead(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.ID) == "" {
		return "", errors.New("id is required")
	}
	data, err := c.do(ctx, http.MethodGet, "/api/sources/"+url.PathEscape(args.ID)+"/markdown", nil, nil)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func handleChat(ctx context.Context, c *auraClient, raw json.RawMessage) (string, error) {
	var args struct {
		Message  string `json:"message"`
		ThreadID string `json:"thread_id"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Message) == "" {
		return "", errors.New("message is required")
	}
	body := map[string]any{"message": args.Message}
	if args.ThreadID != "" {
		body["thread_id"] = args.ThreadID
	}
	data, err := c.do(ctx, http.MethodPost, "/api/chat", nil, body)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleTasksList(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/tasks", nil, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleSkillsList(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/skills", nil, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleMCPServers(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/mcp/servers", nil, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

func handleHealth(ctx context.Context, c *auraClient, _ json.RawMessage) (string, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/health", nil, nil)
	if err != nil {
		return "", err
	}
	return prettyJSON(data), nil
}

// ── Schema + JSON helpers ────────────────────────────────────────────────────

func schemaObject(props ...map[string]any) map[string]any {
	properties := map[string]any{}
	for _, p := range props {
		maps.Copy(properties, p)
	}
	return map[string]any{
		"type":       "object",
		"properties": properties,
	}
}

func prop(name, typ, desc string) map[string]any {
	return map[string]any{name: map[string]any{"type": typ, "description": desc}}
}

func prettyJSON(b []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return string(b)
	}
	return buf.String()
}
