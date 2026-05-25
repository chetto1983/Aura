package swarm

import (
	"context"
	"reflect"
	"testing"

	"github.com/aura/aura/internal/agent/agentdef"
	tools "github.com/aura/aura/internal/agent/tools/registry"
)

type recordingRunRunner struct {
	req RunRequest
}

func (r *recordingRunRunner) Run(_ context.Context, req RunRequest) (RunResult, error) {
	r.req = req
	return RunResult{
		Run:   &Run{ID: "swarm-1", Goal: req.Goal, Status: RunCompleted},
		Tasks: []Task{{ID: "task-1", Role: req.Assignments[0].Role, Status: TaskCompleted, Result: "ok"}},
	}, nil
}

func TestArchetypeRunnerBuildsAssignmentMetadata(t *testing.T) {
	reg, err := agentdef.BuiltinRegistry()
	if err != nil {
		t.Fatalf("BuiltinRegistry: %v", err)
	}
	rec := &recordingRunRunner{}
	runner := &ArchetypeRunner{Manager: rec, Registry: reg}
	ctx := tools.WithUserID(context.Background(), "user-1")
	got, err := runner.Run(ctx, "summarizer", "summarize payload", "fast-model", []string{"orchestrator", "summarizer"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got != "ok" {
		t.Fatalf("result = %q", got)
	}
	if rec.req.Goal != "delegate summarizer" || rec.req.CreatedBy != "user-1" || len(rec.req.Assignments) != 1 {
		t.Fatalf("request metadata = %+v", rec.req)
	}
	a := rec.req.Assignments[0]
	if a.Role != "summarizer" || a.Archetype != "summarizer" || a.ModelOverride != "fast-model" || a.Depth != 1 {
		t.Fatalf("assignment identity fields = %+v", a)
	}
	if want := []string{"orchestrator", "summarizer"}; !reflect.DeepEqual(a.ParentChain, want) {
		t.Fatalf("parent chain = %+v, want %+v", a.ParentChain, want)
	}
	if a.MaxInputTokens != 16384 || a.MaxOutputTokens != 4096 || a.MaxIterations != 1 || a.MaxToolResultChars != 8000 {
		t.Fatalf("assignment limits = %+v", a)
	}
}
