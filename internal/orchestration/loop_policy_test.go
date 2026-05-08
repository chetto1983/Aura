package orchestration

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoopPolicyForToolsetsKeepsTerminalToolsOnlyWhereUseful(t *testing.T) {
	def, ok := LoopPolicyForToolset(ToolsetDefault)
	if !ok {
		t.Fatal("LoopPolicyForToolset default returned false")
	}
	if len(def.TerminalTools) != 0 {
		t.Fatalf("default TerminalTools = %+v, want none in the hot path", def.TerminalTools)
	}

	compute, ok := LoopPolicyForToolset(ToolsetCompute)
	if !ok {
		t.Fatal("LoopPolicyForToolset compute returned false")
	}
	if !slices.Contains(compute.TerminalTools, "execute_code") {
		t.Fatalf("compute TerminalTools missing execute_code: %+v", compute.TerminalTools)
	}
	if !compute.AllowNoToolFinalization {
		t.Fatal("compute should allow one final no-tool response after execute_code")
	}
	if !strings.Contains(compute.DuplicateToolPolicy, "execute_code") {
		t.Fatalf("compute DuplicateToolPolicy = %q, want execute_code duplicate guidance", compute.DuplicateToolPolicy)
	}

	doc, ok := LoopPolicyForToolset(ToolsetDocument)
	if !ok {
		t.Fatal("LoopPolicyForToolset document returned false")
	}
	if doc.MaxElapsed > 30*time.Second {
		t.Fatalf("document MaxElapsed = %s, want <=30s", doc.MaxElapsed)
	}
	if !strings.Contains(doc.DuplicateToolPolicy, "compact retrieval") {
		t.Fatalf("document DuplicateToolPolicy = %q, want compact retrieval guidance", doc.DuplicateToolPolicy)
	}
}

func TestLoopPolicyForToolsetReturnsCopySafePolicy(t *testing.T) {
	policy, ok := LoopPolicyForToolset(ToolsetDocument)
	if !ok {
		t.Fatal("LoopPolicyForToolset document returned false")
	}
	policy.TerminalTools[0] = "mutated"

	reread, ok := LoopPolicyForToolset(ToolsetDocument)
	if !ok {
		t.Fatal("LoopPolicyForToolset document reread returned false")
	}
	if slices.Contains(reread.TerminalTools, "mutated") {
		t.Fatalf("mutating policy changed policy catalog: %+v", reread)
	}
}

func TestLoopPolicyForEveryToolsetIsConservativeAndBounded(t *testing.T) {
	for _, toolset := range []Toolset{ToolsetDefault, ToolsetCompute, ToolsetDocument, ToolsetAdmin} {
		policy, ok := LoopPolicyForToolset(toolset)
		if !ok {
			t.Fatalf("LoopPolicyForToolset(%q) returned false", toolset)
		}
		if policy.MaxElapsed <= 0 || policy.MaxElapsed > time.Minute {
			t.Fatalf("%q MaxElapsed = %s, want positive duration <= 1m", toolset, policy.MaxElapsed)
		}
		if strings.TrimSpace(policy.DuplicateToolPolicy) == "" {
			t.Fatalf("%q DuplicateToolPolicy is empty", toolset)
		}
	}
}
