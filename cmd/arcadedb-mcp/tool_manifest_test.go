package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestToolManifestMatchesServer(t *testing.T) {
	ctx := t.Context()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(nil, time.Now, "").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "tool-manifest-test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	sort.Slice(listed.Tools, func(i, j int) bool {
		return listed.Tools[i].Name < listed.Tools[j].Name
	})
	got, err := json.MarshalIndent(listed.Tools, "", "  ")
	if err != nil {
		t.Fatalf("encode tools: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("..", "..", "docs", "arcadedb-mcp-live-tools.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("update %s: %v", path, err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s does not match the live MCP surface; run UPDATE_GOLDEN=1 go test ./cmd/arcadedb-mcp", path)
	}
}
