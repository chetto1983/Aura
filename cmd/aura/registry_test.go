package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
)

// TestBuildRegistry_RegistersAskUser guards the production wiring gap found in
// Phase-4 verification: ask_user is non-deferred and MUST appear in the live tool
// manifest, or the LLM never calls it and the entire HITL pause/resume flow is
// unreachable from `aura chat`. Unit/runner tests register the tool manually, so
// only an assertion against buildRegistry() itself catches the omission.
func TestBuildRegistry_RegistersAskUser(t *testing.T) {
	reg := buildRegistry()
	askUserName := tools.AskUser{}.Spec().Name
	if _, ok := reg.Get(askUserName); !ok {
		t.Fatalf("production registry is missing %q — the LLM cannot trigger HITL pause", askUserName)
	}
}

// TestBuildRegistry_CoreToolsPresent locks the rest of the always-on manifest so a
// future edit cannot silently drop a tool the agent depends on.
func TestBuildRegistry_CoreToolsPresent(t *testing.T) {
	reg := buildRegistry()
	for _, name := range []string{
		tools.TextResponse{}.Spec().Name,
		tools.CurrentTime{}.Spec().Name,
		(&tools.ReadToolOutput{}).Spec().Name,
		(&tools.ToolSearch{}).Spec().Name,
	} {
		if _, ok := reg.Get(name); !ok {
			t.Errorf("production registry is missing core tool %q", name)
		}
	}
}

func TestBuildRegistry_RegistersSandboxExec(t *testing.T) {
	reg := buildRegistry()
	if _, ok := reg.Get("sandbox_exec"); !ok {
		t.Fatal("production registry is missing sandbox_exec backed by the local sandbox-agent container")
	}
}

func TestBuildRegistryWithMCP_MountsConfiguredServer(t *testing.T) {
	cfg := &config.Config{
		MCPServers: map[string]mcp.ServerConfig{
			"calculator": {
				Command: os.Args[0],
				Args:    []string{"-test.run=TestMCPServerHelperProcess", "--"},
				Env:     []string{"AURA_MCP_HELPER=1"},
			},
		},
		SandboxAgent: config.LoadDB().SandboxAgent,
	}

	reg, closers, err := buildRegistryWithMCP(context.Background(), cfg)
	if err != nil {
		t.Fatalf("buildRegistryWithMCP: %v", err)
	}
	defer func() { _ = closeMCPServers(closers) }()

	calc, ok := reg.Get("calculator__calculate")
	if !ok {
		t.Fatal("configured MCP tool calculator__calculate was not mounted")
	}
	ctx := tools.WithToolCallContext(context.Background(), "sess", "call-1", t.TempDir(), 2048)
	res, err := calc.Execute(ctx, json.RawMessage(`{"expression":"2+2"}`))
	if err != nil {
		t.Fatalf("calculate Execute: %v", err)
	}
	if res.Preview != "4" {
		t.Fatalf("calculate result = %q, want 4", res.Preview)
	}
}

func TestMCPServerHelperProcess(t *testing.T) {
	if os.Getenv("AURA_MCP_HELPER") != "1" {
		return
	}
	runRegistryTestMCPServer(os.Stdin, os.Stdout)
	os.Exit(0)
}

func runRegistryTestMCPServer(in io.Reader, out io.Writer) {
	dec := json.NewDecoder(in)
	enc := json.NewEncoder(out)
	for {
		var req map[string]any
		if err := dec.Decode(&req); err != nil {
			return
		}
		method, _ := req["method"].(string)
		id, hasID := req["id"]
		switch method {
		case "initialize":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"protocolVersion": "2024-11-05",
				"serverInfo":      map[string]any{"name": "calculator-test"},
			}})
		case "notifications/initialized":
		case "tools/list":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"tools": []map[string]any{{
					"name":        "calculate",
					"description": "Evaluate a mathematical expression.",
					"inputSchema": map[string]any{
						"type":       "object",
						"properties": map[string]any{"expression": map[string]any{"type": "string"}},
						"required":   []string{"expression"},
					},
				}},
			}})
		case "tools/call":
			_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "4"}},
			}})
		default:
			if hasID {
				_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "method not found"}})
			}
		}
	}
}
