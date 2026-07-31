package tools

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// builtinTools is the registry-order set of built-in tools (mirrors cmd/aura/main.go),
// constructed as zero values because Spec() is static and reads no dependency. The
// discovery hook tool_search is exercised by search_test.go and is intentionally
// excluded here.
func builtinTools() []Tool {
	return []Tool{
		TextResponse{},
		&ReadToolOutput{},
		CurrentTime{},
		AskUser{},
		&TodoTool{},
		&WebSearch{},
		&WebFetch{},
		&DocumentSearch{},
		&ShellExec{},
		&ShellPoll{},
		&ShellKill{},
		&FSRead{},
		&FSWrite{},
		&FSEdit{},
		&FSGrep{},
		&FSGlob{},
		&SendFile{},
		// document_index/document_open register only when a live pool exists
		// (cmd/aura/main.go), but Spec() reads no dependency and the sweep is the
		// only thing asserting a deferred tool carries a legible description —
		// exactly what tool_search retrieves on. Excluding the pool-gated pair
		// left document_index outside it; both belong here.
		&DocumentIndex{},
		&DocumentOpen{},
		&DocumentDescribe{},
		&SwarmSpawn{},
	}
}

var toolNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// TestBuiltinToolSpecsAreWellFormed is the golden sweep over every built-in tool's
// Spec() (T-03): names are unique valid identifiers, Summary/Description are present,
// Parameters is a JSON-Schema object, and a Deferred tool carries the rich
// description the manifest-protection contract depends on.
func TestBuiltinToolSpecsAreWellFormed(t *testing.T) {
	seen := make(map[string]bool)
	for _, tl := range builtinTools() {
		s := tl.Spec()
		t.Run(s.Name, func(t *testing.T) {
			if !toolNameRe.MatchString(s.Name) {
				t.Fatalf("name %q is not a valid lower_snake identifier", s.Name)
			}
			if seen[s.Name] {
				t.Fatalf("duplicate tool name %q", s.Name)
			}
			seen[s.Name] = true

			if strings.TrimSpace(s.Summary) == "" {
				t.Error("empty Summary")
			}
			if strings.ContainsRune(s.Summary, '\n') {
				t.Errorf("Summary must be one line, got %q", s.Summary)
			}
			if strings.TrimSpace(s.Description) == "" {
				t.Error("empty Description")
			}

			var schema map[string]any
			if err := json.Unmarshal(s.Parameters, &schema); err != nil {
				t.Fatalf("Parameters is not valid JSON: %v", err)
			}
			if schema["type"] != "object" {
				t.Errorf("Parameters top-level type = %v, want \"object\"", schema["type"])
			}

			if s.Deferred && len(strings.TrimSpace(s.Description)) < 40 {
				t.Errorf("deferred tool has a thin Description (%d bytes) — defeats tool_search legibility", len(s.Description))
			}
		})
	}
}
