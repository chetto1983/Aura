package mcp

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestSDKNoisyHelperProcess is the SDK helper server behind a line of npm output on stdout,
// as a self-installing server prints on its first start. It is not a test.
func TestSDKNoisyHelperProcess(t *testing.T) {
	if os.Getenv("AURA_MCP_SDK_HELPER") != "1" {
		t.Skip("helper process: only runs when re-exec'd as an MCP server")
	}
	fmt.Println("added 39 packages, and audited 40 packages in 3s")
	if err := newSDKFixtureServer(1, 0).Run(context.Background(), &sdkmcp.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, "sdk helper:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// A host stdio server that prints to stdout keeps its session, as a box server does.
func TestLocalSessionSurvivesNonProtocolStdout(t *testing.T) {
	server := sdkHelperServer(1)
	server.Args = []string{"-test.run=TestSDKNoisyHelperProcess"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := OpenSDKSession(ctx, "noisy", server, EgressPolicy{}, SessionOptions{})
	if err != nil {
		t.Fatalf("open behind npm's stdout: %v", err)
	}
	defer func() { _ = session.Close() }()
	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: "echo", Arguments: map[string]any{"text": "hi"}})
	if err != nil || res.IsError {
		t.Fatalf("call after the noise = %+v, %v", res, err)
	}
}
