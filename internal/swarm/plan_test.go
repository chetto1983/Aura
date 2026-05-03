package swarm

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildPlanDefaultsToReadOnlyRoleAssignments(t *testing.T) {
	plan, err := BuildPlan("map the inbox into wiki next steps", PlanOptions{UserID: "user-1"})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	wantRoles := []string{"librarian", "critic", "researcher", "skillsmith", "synthesizer"}
	if !reflect.DeepEqual(plan.Roles, wantRoles) {
		t.Fatalf("roles = %+v, want %+v", plan.Roles, wantRoles)
	}
	if len(plan.Assignments) != len(wantRoles) {
		t.Fatalf("assignments = %d, want %d", len(plan.Assignments), len(wantRoles))
	}

	for i, assignment := range plan.Assignments {
		if assignment.Role != wantRoles[i] {
			t.Fatalf("assignment %d role = %q, want %q", i, assignment.Role, wantRoles[i])
		}
		if assignment.UserID != "user-1" || assignment.Depth != 0 {
			t.Fatalf("assignment %d metadata = %+v", i, assignment)
		}
		if assignment.Subject == "" || !strings.Contains(assignment.Subject, assignment.Role+":") {
			t.Fatalf("assignment %d subject = %q", i, assignment.Subject)
		}
		if !strings.Contains(assignment.Prompt, "Goal: map the inbox into wiki next steps") {
			t.Fatalf("assignment %d prompt missing goal: %q", i, assignment.Prompt)
		}
		if !strings.Contains(assignment.SystemPrompt, "read-only tools") || !strings.Contains(assignment.SystemPrompt, "Do not mutate") {
			t.Fatalf("assignment %d system prompt is not read-only focused: %q", i, assignment.SystemPrompt)
		}
	}
}

func TestBuildPlanMatchesSwarmToolsReadOnlyPresets(t *testing.T) {
	plan, err := BuildPlan("check the system", PlanOptions{})
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}

	want := map[string][]string{
		"librarian":   {"list_wiki", "read_wiki", "search_wiki", "lint_wiki", "list_sources", "read_source", "lint_sources"},
		"critic":      {"lint_wiki", "list_wiki", "read_wiki", "lint_sources", "list_sources"},
		"researcher":  {"web_search", "web_fetch"},
		"skillsmith":  {"list_skills", "read_skill", "search_skill_catalog"},
		"synthesizer": {"list_wiki", "read_wiki", "search_wiki", "list_sources", "read_source"},
	}
	for _, assignment := range plan.Assignments {
		if !reflect.DeepEqual(assignment.ToolAllowlist, want[assignment.Role]) {
			t.Fatalf("%s allowlist = %+v, want %+v", assignment.Role, assignment.ToolAllowlist, want[assignment.Role])
		}
	}
}

func TestBuildPlanValidatesDedupeAndCap(t *testing.T) {
	plan, err := BuildPlan("lint wiki", PlanOptions{
		Roles: []string{" critic ", "librarian", "critic", "LIBRARIAN"},
	})
	if err != nil {
		t.Fatalf("BuildPlan dedupe: %v", err)
	}
	if got, want := plan.Roles, []string{"critic", "librarian"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("roles = %+v, want %+v", got, want)
	}

	if _, err := BuildPlan("lint wiki", PlanOptions{Roles: []string{"critic", "writer"}}); err == nil {
		t.Fatal("expected unknown role error")
	}
	if _, err := BuildPlan("lint wiki", PlanOptions{
		Roles:          []string{"librarian", "critic", "researcher"},
		MaxAssignments: 2,
	}); err == nil {
		t.Fatal("expected cap error")
	}
	if _, err := BuildPlan("   ", PlanOptions{}); err == nil {
		t.Fatal("expected missing goal error")
	}
	if _, err := BuildPlan("lint wiki", PlanOptions{Roles: []string{" ", ""}}); err == nil {
		t.Fatal("expected empty roles error")
	}
}

func TestPlanAssignmentsConvenience(t *testing.T) {
	assignments, err := PlanAssignments("check skills", []string{"skillsmith"}, "user-2")
	if err != nil {
		t.Fatalf("PlanAssignments: %v", err)
	}
	if len(assignments) != 1 || assignments[0].Role != "skillsmith" || assignments[0].UserID != "user-2" {
		t.Fatalf("assignments = %+v", assignments)
	}
}

func TestSynthesizeRunResultRollsUpMetricsDeterministically(t *testing.T) {
	created := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	completed := created.Add(200 * time.Millisecond)
	result := RunResult{
		Run: &Run{
			ID:          "run-1",
			Goal:        "audit wiki",
			Status:      RunCompleted,
			CreatedAt:   created,
			CompletedAt: &completed,
		},
		Tasks: []Task{
			{
				ID:               "task-2",
				Role:             "researcher",
				Subject:          "researcher subject",
				Status:           TaskFailed,
				LastError:        "network unavailable\nretry later",
				LLMCalls:         1,
				ToolCalls:        2,
				TokensPrompt:     3,
				TokensCompletion: 5,
				TokensTotal:      8,
				ElapsedMS:        150,
			},
			{
				ID:               "task-1",
				Role:             "critic",
				Subject:          "critic subject",
				Status:           TaskCompleted,
				Result:           "found stale links\nand missing source evidence",
				LLMCalls:         2,
				ToolCalls:        4,
				TokensPrompt:     7,
				TokensCompletion: 11,
				TokensTotal:      18,
				ElapsedMS:        250,
			},
		},
	}

	got := SynthesizeRunResult(result)
	if got.RunID != "run-1" || got.Goal != "audit wiki" || got.Status != RunCompleted {
		t.Fatalf("run fields = %+v", got)
	}
	if got.Metrics.TotalTasks != 2 || got.Metrics.CompletedTasks != 1 || got.Metrics.FailedTasks != 1 {
		t.Fatalf("counts = %+v", got.Metrics)
	}
	if got.Metrics.LLMCalls != 3 || got.Metrics.ToolCalls != 6 || got.Metrics.TokensTotal != 26 {
		t.Fatalf("call/token metrics = %+v", got.Metrics)
	}
	if got.Metrics.TaskElapsedMS != 400 || got.Metrics.WallMS != 200 || got.Metrics.Speedup != 2 {
		t.Fatalf("elapsed metrics = %+v", got.Metrics)
	}
	if got.Tasks[0].Role != "critic" || got.Tasks[1].Role != "researcher" {
		t.Fatalf("task order = %+v", got.Tasks)
	}
	if got.Tasks[0].ResultPreview != "found stale links and missing source evidence" {
		t.Fatalf("preview = %q", got.Tasks[0].ResultPreview)
	}
	if got.Tasks[1].LastError != "network unavailable retry later" {
		t.Fatalf("last error preview = %q", got.Tasks[1].LastError)
	}
	wantSummary := "Run run-1 (completed): 1/2 completed, 1 failed, 0 running, 0 pending. Roles: critic=completed, researcher=failed. Metrics: llm=3 tools=6 tokens=26 task_elapsed_ms=400 wall_ms=200 speedup=2.00."
	if got.Summary != wantSummary {
		t.Fatalf("summary = %q, want %q", got.Summary, wantSummary)
	}
}
