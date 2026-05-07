package orchestration

import (
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLoopPolicyForProfileDeclaresTerminalSwarmAndSandboxFinalization(t *testing.T) {
	swarm, ok := LoopPolicyForProfile(ProfileSwarmResearch)
	if !ok {
		t.Fatal("LoopPolicyForProfile swarm_research returned false")
	}
	if swarm.MaxSteps < 1 || swarm.MaxSteps > 2 {
		t.Fatalf("swarm MaxSteps = %d, want low 1-2 budget", swarm.MaxSteps)
	}
	if !slices.Contains(swarm.TerminalTools, "run_aurabot_swarm") {
		t.Fatalf("swarm TerminalTools missing run_aurabot_swarm: %+v", swarm.TerminalTools)
	}
	if !strings.Contains(swarm.DuplicateToolPolicy, "Reject duplicate run_aurabot_swarm") {
		t.Fatalf("swarm DuplicateToolPolicy = %q, want duplicate swarm rejection", swarm.DuplicateToolPolicy)
	}
	if swarm.MaxElapsed <= 0 {
		t.Fatalf("swarm MaxElapsed = %s, want positive duration", swarm.MaxElapsed)
	}

	sandbox, ok := LoopPolicyForProfile(ProfileSandboxCompute)
	if !ok {
		t.Fatal("LoopPolicyForProfile sandbox_compute returned false")
	}
	if !slices.Contains(sandbox.TerminalTools, "execute_code") {
		t.Fatalf("sandbox TerminalTools missing execute_code: %+v", sandbox.TerminalTools)
	}
	if !sandbox.AllowNoToolFinalization {
		t.Fatal("sandbox should allow one final no-tool response after execute_code")
	}
	if !strings.Contains(sandbox.DuplicateToolPolicy, "execute_code") {
		t.Fatalf("sandbox DuplicateToolPolicy = %q, want execute_code duplicate guidance", sandbox.DuplicateToolPolicy)
	}
}

func TestLoopPolicyForProfileReturnsCopySafePolicy(t *testing.T) {
	policy, ok := LoopPolicyForProfile(ProfileSwarmResearch)
	if !ok {
		t.Fatal("LoopPolicyForProfile swarm_research returned false")
	}
	policy.TerminalTools[0] = "mutated"

	reread, ok := LoopPolicyForProfile(ProfileSwarmResearch)
	if !ok {
		t.Fatal("LoopPolicyForProfile swarm_research reread returned false")
	}
	if reread.TerminalTools[0] == "mutated" {
		t.Fatalf("mutating TerminalTools changed policy catalog: %+v", reread.TerminalTools)
	}
}

func TestLoopPolicyForEveryProfileIsConservativeAndBounded(t *testing.T) {
	for _, profile := range []Profile{ProfileDefault, ProfileMemory, ProfileSwarmResearch, ProfileSandboxCompute, ProfileDocument, ProfileAdminReview} {
		policy, ok := LoopPolicyForProfile(profile)
		if !ok {
			t.Fatalf("LoopPolicyForProfile(%q) returned false", profile)
		}
		if policy.MaxSteps <= 0 || policy.MaxSteps > 6 {
			t.Fatalf("%q MaxSteps = %d, want 1..6", profile, policy.MaxSteps)
		}
		if policy.MaxElapsed <= 0 || policy.MaxElapsed > time.Minute {
			t.Fatalf("%q MaxElapsed = %s, want positive duration <= 1m", profile, policy.MaxElapsed)
		}
		if strings.TrimSpace(policy.DuplicateToolPolicy) == "" {
			t.Fatalf("%q DuplicateToolPolicy is empty", profile)
		}
	}
}
