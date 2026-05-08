package orchestration

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoopPolicyForToolsetsKeepsTerminalToolsOnlyWhereUseful(t *testing.T) {
	def, ok := LoopPolicyForToolset(ProfileDefault)
	if !ok {
		t.Fatal("LoopPolicyForToolset default returned false")
	}
	if def.MaxSteps != 4 {
		t.Fatalf("default MaxSteps = %d, want 4-step budget", def.MaxSteps)
	}
	if !slices.Contains(def.TerminalTools, "run_aurabot_swarm") {
		t.Fatalf("default TerminalTools missing run_aurabot_swarm: %+v", def.TerminalTools)
	}

	compute, ok := LoopPolicyForToolset(ProfileCompute)
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
}

func TestLoopPolicyForProfileReturnsCopySafePolicy(t *testing.T) {
	policy, ok := LoopPolicyForProfile(ProfileDefault)
	if !ok {
		t.Fatal("LoopPolicyForProfile default returned false")
	}
	policy.MaxSteps = 99

	reread, ok := LoopPolicyForProfile(ProfileDefault)
	if !ok {
		t.Fatal("LoopPolicyForProfile default reread returned false")
	}
	if reread.MaxSteps == 99 {
		t.Fatalf("mutating policy changed policy catalog: %+v", reread)
	}
}

func TestLoopPolicyForEveryToolsetIsConservativeAndBounded(t *testing.T) {
	for _, profile := range []Profile{ProfileDefault, ProfileCompute, ProfileDocument, ProfileAdmin, ProfileMemory, ProfileSwarmResearch, ProfileSandboxCompute, ProfileAdminReview} {
		policy, ok := LoopPolicyForProfile(profile)
		if !ok {
			t.Fatalf("LoopPolicyForProfile(%q) returned false", profile)
		}
		if policy.MaxSteps <= 0 || policy.MaxSteps > 8 {
			t.Fatalf("%q MaxSteps = %d, want 1..8", profile, policy.MaxSteps)
		}
		if policy.MaxElapsed <= 0 || policy.MaxElapsed > time.Minute {
			t.Fatalf("%q MaxElapsed = %s, want positive duration <= 1m", profile, policy.MaxElapsed)
		}
		if strings.TrimSpace(policy.DuplicateToolPolicy) == "" {
			t.Fatalf("%q DuplicateToolPolicy is empty", profile)
		}
	}
}
