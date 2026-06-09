package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const fakeMCPEnv = "AURA_KNOWLEDGE_FAKE_MCP"

func TestOpenKeepsProcessAliveAfterConnectTimeout(t *testing.T) {
	if os.Getenv(fakeMCPEnv) == "1" {
		runFakeMCPServer()
		return
	}

	cfg := &Config{
		BoltURL:           "bolt://127.0.0.1:7687",
		User:              "neo4j",
		Password:          "test-password",
		Database:          "neo4j",
		MCPBinary:         writeFakeMCPLauncher(t),
		ConnectTimeoutSec: 1,
	}
	c, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer c.Close()

	time.Sleep(1200 * time.Millisecond)
	raw, err := c.Cypher(context.Background(), "RETURN 1 AS one", nil, false)
	if err != nil {
		t.Fatalf("Cypher after connect timeout elapsed: %v", err)
	}
	rows, err := decodeRows(raw)
	if err != nil {
		t.Fatalf("decodeRows: %v", err)
	}
	if len(rows) != 1 || rows[0]["one"] != float64(1) {
		t.Fatalf("rows = %+v, want one=1", rows)
	}
}

func writeFakeMCPLauncher(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-mcp")
	exe := os.Args[0]
	if runtime.GOOS == "windows" {
		path += ".cmd"
		exe = strings.ReplaceAll(exe, `"`, `""`)
		body := fmt.Sprintf("@echo off\r\nset %s=1\r\n\"%s\" -test.run=TestOpenKeepsProcessAliveAfterConnectTimeout -- %%*\r\n", fakeMCPEnv, exe)
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatalf("write fake MCP launcher: %v", err)
		}
		return path
	}
	body := fmt.Sprintf("#!/bin/sh\n%s=1 exec %s -test.run=TestOpenKeepsProcessAliveAfterConnectTimeout -- \"$@\"\n", fakeMCPEnv, shellQuote(exe))
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake MCP launcher: %v", err)
	}
	return path
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func runFakeMCPServer() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req rpcReq
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}
		switch req.Method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result":  map[string]any{"protocolVersion": "2024-11-05"},
			})
		case "notifications/initialized":
			continue
		case "tools/call":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"result": map[string]any{
					"content": []map[string]string{{
						"type": "text",
						"text": `[{"one":1}]`,
					}},
					"isError": false,
				},
			})
		}
	}
}
