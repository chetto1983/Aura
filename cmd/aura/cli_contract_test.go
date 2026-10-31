package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// memoryToolsGolden is the tool list cmd/arcadedb-mcp publishes; its tool_manifest_test.go fails
// when the live server drifts from it, so a CLI call that validates here validates there.
var memoryToolsGolden = filepath.Join("..", "..", "docs", "arcadedb-mcp-live-tools.json")

// memoryToolsWithoutAVerb are the server's tools no `aura memory` verb sends, each with why.
var memoryToolsWithoutAVerb = map[string]string{
	"memory_batch":      "the agent's multi-operation write; the CLI writes one fact at a time",
	"memory_recall":     "the agent's turn-scoped recall; `memory search` is the CLI's read",
	"graph_path":        "an agent graph traversal with no operator verb",
	"graph_diagnostics": "an agent graph summary with no operator verb",
}

// memoryVerbCalls are argv lines covering every verb and each optional flag it maps to a field.
var memoryVerbCalls = [][]string{
	{"search", "mario rossi"},
	{"search", "mario", "--as-of", "2026-01-01T00:00:00Z"},
	{"facts", "Mario Rossi"},
	{"facts", "Mario Rossi", "lives_in", "--as-of", "2026-01-01T00:00:00Z"},
	{"entities"},
	{"digest"},
	{"remember", "Mario", "KNOWS", "Luigi", "Mario knows Luigi."},
	{"remember", "--supersedes", "--subject-kind", "Person", "--object-kind", "Person", "--run", "import-2026",
		"Mario", "KNOWS", "Luigi", "Mario knows Luigi."},
	{"merge", "M. Rossi", "Mario Rossi"},
	{"forget", "--entity", "Mario Rossi"},
	{"forget", "--subject", "Mario", "--predicate", "KNOWS", "--object", "Luigi", "--apply"},
	{"forget", "--run", "cli-2026-10-31"},
	{"reembed"},
	{"reembed", "--all"},
	{"schema"},
}

// TestMemoryVerbsMatchTheServerSchemas validates each verb's arguments the way the go-sdk does
// on the server (mcp/tool.go: JSON round-trip, ApplyDefaults, Validate). D4 of
// docs/superpowers/verification/2026-10-31-briefing-model-in-the-loop.md is what it guards:
// `remember` sent a run_id the write schema had dropped, and every call failed for two months.
func TestMemoryVerbsMatchTheServerSchemas(t *testing.T) {
	t.Parallel()
	schemas := loadMemoryToolSchemas(t)
	reached := map[string]bool{}
	for _, argv := range memoryVerbCalls {
		t.Run(strings.Join(argv, " "), func(t *testing.T) {
			call, err := memoryCallFor(argv, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatalf("memoryCallFor: %v", err)
			}
			resolved, ok := schemas[call.tool]
			if !ok {
				t.Fatalf("verb sends %q, which the server does not publish", call.tool)
			}
			if err := validateLikeTheServer(resolved, call.args); err != nil {
				t.Fatalf("the server would refuse %s(%v): %v", call.tool, call.args, err)
			}
		})
		call, err := memoryCallFor(argv, time.Now())
		if err == nil {
			reached[call.tool] = true
		}
	}
	for tool := range schemas {
		if !reached[tool] && memoryToolsWithoutAVerb[tool] == "" {
			t.Errorf("the server publishes %q and no verb sends it: map a verb or list it in memoryToolsWithoutAVerb", tool)
		}
	}
	for tool := range memoryToolsWithoutAVerb {
		if _, ok := schemas[tool]; !ok || reached[tool] {
			t.Errorf("memoryToolsWithoutAVerb lists %q, which the server no longer publishes or a verb now sends", tool)
		}
	}
}

// A write names its run in the actor headers, never in the arguments: the write schema has no
// field for it (D-10), and the remember rows above would fail if one crept back.
func TestOnlyRememberNamesARun(t *testing.T) {
	t.Parallel()
	for _, argv := range memoryVerbCalls {
		call, err := memoryCallFor(argv, time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case argv[0] != "remember" && call.run != "":
			t.Errorf("%v names run %q; only a write names one", argv, call.run)
		case argv[0] == "remember" && !slices.Contains([]string{"cli-2026-10-31", "import-2026"}, call.run):
			t.Errorf("%v names run %q, want --run or the day's cli run", argv, call.run)
		}
	}
	if _, err := memoryCallFor(nil, time.Now()); err == nil {
		t.Error("an empty argv mapped to a call")
	}
	if _, err := memoryCallFor([]string{"remember", "too", "few"}, time.Now()); err == nil {
		t.Error("remember with three positionals mapped to a call")
	}
}

// A shape the server refuses must fail here too, or the test above proves nothing.
func TestAnArgumentTheServerRefusesFailsTheContract(t *testing.T) {
	t.Parallel()
	upsert := loadMemoryToolSchemas(t)["memory_upsert_fact"]
	d4 := map[string]any{
		"subject": "Mario", "predicate": "KNOWS", "object": "Luigi", "statement": "Mario knows Luigi.",
		"source": map[string]any{"run_id": "cli-2026-08-28"},
	}
	err := validateLikeTheServer(upsert, d4)
	if err == nil || !strings.Contains(err.Error(), "unexpected additional properties") {
		t.Fatalf("D4's arguments validated (err=%v); the contract would not have caught it", err)
	}
	if err := validateLikeTheServer(upsert, map[string]any{"subject": "Mario"}); err == nil {
		t.Error("an upsert missing its required fields validated")
	}
}

func loadMemoryToolSchemas(t *testing.T) map[string]*jsonschema.Resolved {
	t.Helper()
	raw, err := os.ReadFile(memoryToolsGolden)
	if err != nil {
		t.Fatal(err)
	}
	var tools []struct {
		Name        string             `json:"name"`
		InputSchema *jsonschema.Schema `json:"inputSchema"`
	}
	if err := json.Unmarshal(raw, &tools); err != nil {
		t.Fatalf("%s: %v", memoryToolsGolden, err)
	}
	out := make(map[string]*jsonschema.Resolved, len(tools))
	for _, tool := range tools {
		resolved, err := tool.InputSchema.Resolve(&jsonschema.ResolveOptions{ValidateDefaults: true})
		if err != nil {
			t.Fatalf("resolve %s: %v", tool.Name, err)
		}
		out[tool.Name] = resolved
	}
	if len(out) == 0 {
		t.Fatalf("%s lists no tools", memoryToolsGolden)
	}
	return out
}

// validateLikeTheServer is go-sdk v1.8.0's mcp/tool.go applySchema for a tool's input: the
// arguments as JSON, decoded into a map, defaults applied, then validated.
func validateLikeTheServer(resolved *jsonschema.Resolved, args map[string]any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var instance any = decoded
	if err := resolved.ApplyDefaults(&instance); err != nil {
		return err
	}
	return resolved.Validate(&instance)
}
