package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The markers name the six write rules; spec 2026-11-28-memory-write-rules-design.md §Design 1.
var memoryWriteRuleMarkers = []string{"memory_entities", "snake_case", "one or two sentences", "source: {}", "in a month"}

func listServerTools(t *testing.T) []*mcp.Tool {
	t.Helper()
	ctx := t.Context()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := newServer(nil, time.Now, "").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "tool-list-test", Version: "1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	return listed.Tools
}

func schemaDescription(t *testing.T, schema any, path ...string) string {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("encode schema: %v", err)
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	for _, key := range append(path, "description") {
		object, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("schema has no %v", path)
		}
		node = object[key]
	}
	description, _ := node.(string)
	return description
}

// On the lab VM on 2026-11-28 the 30 KB skill holding these rules had left the
// model's context by turn #227. The tool descriptions ride in every request.
func TestWriteToolsCarryTheRules(t *testing.T) {
	fields := map[string][]string{
		"memory_upsert_fact": {"properties"},
		"memory_batch":       {"properties", "operations", "items", "properties", "fact", "properties"},
	}
	for _, tool := range listServerTools(t) {
		prefix, isWrite := fields[tool.Name]
		if !isWrite {
			continue
		}
		delete(fields, tool.Name)
		for _, marker := range memoryWriteRuleMarkers {
			if !strings.Contains(tool.Description, marker) {
				t.Errorf("%s description lacks the rule marker %q", tool.Name, marker)
			}
		}
		for field, marker := range map[string]string{
			"predicate": "snake_case", "statement": "one or two sentences",
			"subject": "memory_entities", "object": "memory_entities",
		} {
			if got := schemaDescription(t, tool.InputSchema, append(prefix, field)...); !strings.Contains(got, marker) {
				t.Errorf("%s %s description %q lacks %q", tool.Name, field, got, marker)
			}
		}
	}
	for name := range fields {
		t.Errorf("write tool %s is not served", name)
	}
}

// Both descriptions ride in every request, so the rules stay a paragraph.
func TestWriteRulesStayShort(t *testing.T) {
	if len(memoryWriteRules) > 700 {
		t.Fatalf("memoryWriteRules is %d bytes, cap 700", len(memoryWriteRules))
	}
}
