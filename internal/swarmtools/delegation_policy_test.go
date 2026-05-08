package swarmtools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/swarm"
)

func TestDefaultDelegationPolicy(t *testing.T) {
	got := DefaultDelegationPolicy()
	if got.MaxWorkers != 1 {
		t.Fatalf("MaxWorkers = %d, want 1", got.MaxWorkers)
	}
	if got.Timeout != 25*time.Second {
		t.Fatalf("Timeout = %s, want 25s", got.Timeout)
	}
	if got.FinalizationTimeout != 4*time.Second {
		t.Fatalf("FinalizationTimeout = %s, want 4s", got.FinalizationTimeout)
	}
	if got.ChildMaxIterations != 3 {
		t.Fatalf("ChildMaxIterations = %d, want 3", got.ChildMaxIterations)
	}
	if got.MaxResultChars != 12000 {
		t.Fatalf("MaxResultChars = %d, want 12000", got.MaxResultChars)
	}
	if got.Finalization != "aggregate" {
		t.Fatalf("Finalization = %q, want aggregate", got.Finalization)
	}
}

func TestDelegationPolicyClampEnforcesBounds(t *testing.T) {
	got := (DelegationPolicy{
		MaxWorkers:          20,
		Timeout:             90 * time.Second,
		FinalizationTimeout: 60 * time.Second,
		ChildMaxIterations:  99,
		MaxResultChars:      99999,
		Finalization:        "tool_loop",
	}).Clamp()
	want := DefaultDelegationPolicy()
	want.MaxWorkers = 3
	if got != want {
		t.Fatalf("Clamp() = %+v, want %+v", got, want)
	}

	got = (DelegationPolicy{
		MaxWorkers:          0,
		Timeout:             0,
		FinalizationTimeout: 0,
		ChildMaxIterations:  0,
		MaxResultChars:      1999,
		Finalization:        "",
	}).Clamp()
	if got != (DelegationPolicy{
		MaxWorkers:          1,
		Timeout:             25 * time.Second,
		FinalizationTimeout: 4 * time.Second,
		ChildMaxIterations:  3,
		MaxResultChars:      12000,
		Finalization:        "aggregate",
	}) {
		t.Fatalf("Clamp() lower bounds = %+v", got)
	}
}

func TestDelegationPolicyClampKeepsAllowedValues(t *testing.T) {
	got := (DelegationPolicy{
		MaxWorkers:          2,
		Timeout:             30 * time.Second,
		FinalizationTimeout: 7 * time.Second,
		ChildMaxIterations:  1,
		MaxResultChars:      2000,
		Finalization:        "no_tool_llm",
	}).Clamp()
	if got.MaxWorkers != 2 || got.Timeout != 30*time.Second || got.FinalizationTimeout != 7*time.Second || got.ChildMaxIterations != 1 || got.MaxResultChars != 2000 || got.Finalization != "no_tool_llm" {
		t.Fatalf("Clamp() changed allowed values: %+v", got)
	}
}

func TestLoadDelegationPolicyFromEnvClampsOverrides(t *testing.T) {
	t.Setenv("SWARM_RESEARCH_MAX_WORKERS", "9")
	t.Setenv("SWARM_RESEARCH_TIMEOUT_MS", "45000")
	t.Setenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "30000")
	t.Setenv("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "7")
	t.Setenv("SWARM_RESEARCH_MAX_RESULT_CHARS", "25000")
	t.Setenv("SWARM_RESEARCH_FINALIZATION", "tool_loop")

	got := LoadDelegationPolicyFromEnv()
	want := DefaultDelegationPolicy()
	want.MaxWorkers = 3
	if got != want {
		t.Fatalf("LoadDelegationPolicyFromEnv() = %+v, want %+v", got, want)
	}
}

func TestLoadDelegationPolicyFromEnvAcceptsValidOverrides(t *testing.T) {
	t.Setenv("SWARM_RESEARCH_MAX_WORKERS", "2")
	t.Setenv("SWARM_RESEARCH_TIMEOUT_MS", "30000")
	t.Setenv("SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS", "7000")
	t.Setenv("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "1")
	t.Setenv("SWARM_RESEARCH_MAX_RESULT_CHARS", "2000")
	t.Setenv("SWARM_RESEARCH_FINALIZATION", " NO_TOOL_LLM ")

	got := LoadDelegationPolicyFromEnv()
	want := DelegationPolicy{
		MaxWorkers:          2,
		Timeout:             30 * time.Second,
		FinalizationTimeout: 7 * time.Second,
		ChildMaxIterations:  1,
		MaxResultChars:      2000,
		Finalization:        "no_tool_llm",
	}
	if got != want {
		t.Fatalf("LoadDelegationPolicyFromEnv() = %+v, want %+v", got, want)
	}
}

func TestValidateWorkerRoles(t *testing.T) {
	catalog := fakeWorkerCatalog{roles: map[string]bool{
		"librarian":   true,
		"critic":      true,
		"researcher":  true,
		"skillsmith":  true,
		"synthesizer": true,
	}}
	if err := ValidateWorkerRoles([]string{"librarian", "critic", "synthesizer"}, catalog); err != nil {
		t.Fatalf("ValidateWorkerRoles(valid): %v", err)
	}

	err := ValidateWorkerRoles([]string{"golem.yaml"}, catalog)
	if err == nil || !strings.Contains(err.Error(), `stale worker alias "golem.yaml": yaml slugs are not valid worker roles`) {
		t.Fatalf("stale alias error = %v", err)
	}

	err = ValidateWorkerRoles([]string{"writer"}, catalog)
	if err == nil || !strings.Contains(err.Error(), `unknown worker role "writer"`) {
		t.Fatalf("unknown role error = %v", err)
	}
}

func TestWorkerRolesForPolicyDefaultsAfterCleaning(t *testing.T) {
	got := workerRolesForPolicy([]string{" ", "\t"}, DelegationPolicy{MaxWorkers: 2}.Clamp())
	want := []string{"librarian", "synthesizer"}
	if len(got) != len(want) {
		t.Fatalf("roles = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("roles = %+v, want %+v", got, want)
		}
	}
}

func TestRunAuraBotSwarmRejectsStaleAliasBeforeRunnerStarts(t *testing.T) {
	runner := &recordingSwarmRunner{}
	tool := NewRunAuraBotSwarmTool(runner)
	_, err := tool.Execute(context.Background(), map[string]any{
		"goal":  "audit wiki health",
		"roles": []any{"golem.yaml"},
	})
	if err == nil || !strings.Contains(err.Error(), "stale worker alias") {
		t.Fatalf("Execute error = %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

func TestRunAuraBotSwarmValidatesRolesBeforeWorkerCap(t *testing.T) {
	runner := &recordingSwarmRunner{}
	tool := NewRunAuraBotSwarmTool(runner)
	_, err := tool.Execute(context.Background(), map[string]any{
		"goal":  "audit wiki health",
		"roles": []any{"librarian", "critic", "researcher", "golem.yaml"},
	})
	if err == nil || !strings.Contains(err.Error(), "stale worker alias") {
		t.Fatalf("Execute error = %v", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

func TestRunAuraBotSwarmAppliesDelegationPolicyToAssignments(t *testing.T) {
	t.Setenv("SWARM_RESEARCH_MAX_WORKERS", "2")
	t.Setenv("SWARM_RESEARCH_CHILD_MAX_ITERATIONS", "1")
	t.Setenv("SWARM_RESEARCH_MAX_RESULT_CHARS", "2000")

	runner := &recordingSwarmRunner{}
	tool := NewRunAuraBotSwarmTool(runner)
	_, err := tool.Execute(context.Background(), map[string]any{
		"goal":  "audit wiki health",
		"roles": []any{"librarian", "critic", "researcher"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if len(runner.last.Assignments) != 2 {
		t.Fatalf("assignments = %d, want 2", len(runner.last.Assignments))
	}
	for _, assignment := range runner.last.Assignments {
		if assignment.MaxToolCalls != 1 {
			t.Fatalf("%s MaxToolCalls = %d, want 1", assignment.Role, assignment.MaxToolCalls)
		}
		if assignment.MaxToolResultChars > 2000 {
			t.Fatalf("%s MaxToolResultChars = %d, want <= 2000", assignment.Role, assignment.MaxToolResultChars)
		}
		if assignment.FinalizationTimeout != 4*time.Second {
			t.Fatalf("%s FinalizationTimeout = %s, want 4s", assignment.Role, assignment.FinalizationTimeout)
		}
	}
}

type fakeWorkerCatalog struct {
	roles map[string]bool
}

func (f fakeWorkerCatalog) HasRole(role string) bool {
	return f.roles[role]
}

type recordingSwarmRunner struct {
	calls int
	last  swarm.RunRequest
}

func (r *recordingSwarmRunner) Run(_ context.Context, req swarm.RunRequest) (swarm.RunResult, error) {
	r.calls++
	r.last = req
	return swarm.RunResult{}, nil
}
