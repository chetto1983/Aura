package mcptools

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The memory surface is decided in TWO files, in two languages, and neither knew about the
// other. The sidecar's `_instructions.py` teaches the model which tools to reach for; this
// package's memoryHiddenFromModel decides which ones the bridge lets through. A tool taught
// there and hidden here is an instruction the model can never act on.
//
// Both halves failed on 2026-07-30, hours apart, and each had a green test:
//
//   - the sidecar's own test suite verified every taught tool was REGISTERED in _tools.py.
//     It read the file as a flat list of definitions and could not see the
//     `register_platinum` backend gate, so it stayed green while the instructions named
//     three NAMS-only tools this bolt deployment does not register.
//   - that same suite then went green on instructions naming memory_add_entity and
//     memory_create_relationship — both registered by the sidecar, both dropped by
//     memoryHiddenFromModel, which lives here and which no Python test can see.
//
// Reading one side and asserting about the other is the whole point. Everything else about
// this contract has been checked from inside one language at a time, and that is exactly how
// it stayed broken.
func TestEveryTaughtMemoryToolReachesTheModel(t *testing.T) {
	t.Parallel()

	taught := taughtMemoryTools(t)
	if len(taught) < 4 {
		t.Fatalf("parsed %d tool names from the sidecar instructions, want at least 4 — "+
			"the parser is probably wrong, and a silently-empty set would make this test "+
			"pass on any state at all", len(taught))
	}

	var unreachable []string
	for _, name := range taught {
		if _, hidden := memoryHiddenFromModel[name]; hidden {
			unreachable = append(unreachable, name)
		}
	}
	sort.Strings(unreachable)
	if len(unreachable) > 0 {
		t.Fatalf("the sidecar instructions teach %v, but memoryHiddenFromModel drops them "+
			"before they reach the model — either un-hide them here or stop teaching them "+
			"in docker/agent-memory/src/neo4j_agent_memory/mcp/_instructions.py", unreachable)
	}
}

// The converse: a tool the bridge exposes but the instructions never mention is a capability
// the model has and does not know it has. That is how memory_add_entity went unused for the
// whole life of the deployment while facts piled up with nothing to attach to.
func TestEveryModelFacingMemoryToolIsTaught(t *testing.T) {
	t.Parallel()

	taught := make(map[string]struct{})
	for _, name := range taughtMemoryTools(t) {
		taught[name] = struct{}{}
	}

	// The bridge's own view of what it exposes, derived from the deny list rather than
	// restated, so this test cannot drift from bridgeToolsWithPolicy.
	policy := defaultBridgePolicy("memory")
	var untaught []string
	for _, name := range sidecarMemoryTools(t) {
		if !policy.modelFacing(name) {
			continue
		}
		if _, ok := taught[name]; !ok {
			untaught = append(untaught, name)
		}
	}
	sort.Strings(untaught)
	if len(untaught) > 0 {
		t.Fatalf("the bridge exposes %v to the model and the sidecar instructions never "+
			"name them — a capability nobody is told about is one nobody uses", untaught)
	}
}

var memoryToolNamePattern = regexp.MustCompile(`\bmemory_[a-z_]+`)

// taughtMemoryTools returns the tool names named in the sidecar's instruction strings.
func taughtMemoryTools(t *testing.T) []string {
	t.Helper()
	return uniqueMemoryToolNames(readSidecarFile(t, "_instructions.py"))
}

// sidecarMemoryTools returns the tools the sidecar registers on the bolt backend. The
// `_register_platinum_tools` block is cut: it runs only when the backend is NAMS, and Aura
// runs bolt against its own Neo4j, so those definitions exist in the file and never in the
// server.
func sidecarMemoryTools(t *testing.T) []string {
	t.Helper()
	source := readSidecarFile(t, "_tools.py")
	if gate := strings.Index(source, "def _register_platinum_tools"); gate >= 0 {
		source = source[:gate]
	}
	defs := regexp.MustCompile(`async def (memory_[a-z_]+)\s*\(`).FindAllStringSubmatch(source, -1)
	out := make([]string, 0, len(defs))
	for _, m := range defs {
		out = append(out, m[1])
	}
	return out
}

func readSidecarFile(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docker", "agent-memory", "src",
		"neo4j_agent_memory", "mcp", name)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vendored sidecar %s: %v — this test asserts a contract that spans "+
			"the Go bridge and the Python sidecar, so a missing tree is a real failure, "+
			"not a reason to skip", name, err)
	}
	return string(body)
}

func uniqueMemoryToolNames(body string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, name := range memoryToolNamePattern.FindAllString(body, -1) {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
