package swarmtools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/swarm"
)

type fakeSwarmTaskReader struct {
	tasks []swarm.Task
}

func (f fakeSwarmTaskReader) ListTasks(_ context.Context, runID string) ([]swarm.Task, error) {
	var out []swarm.Task
	for _, task := range f.tasks {
		if task.RunID == runID {
			out = append(out, task)
		}
	}
	return out, nil
}

func (f fakeSwarmTaskReader) GetTask(_ context.Context, id string) (*swarm.Task, error) {
	for _, task := range f.tasks {
		if task.ID == id {
			cp := task
			return &cp, nil
		}
	}
	return nil, context.Canceled
}

func TestListAndReadSwarmToolsAcceptTaskReaderInterfaces(t *testing.T) {
	completed := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	reader := fakeSwarmTaskReader{tasks: []swarm.Task{{
		ID:          "task_1234567890abcdef",
		RunID:       "swarm_1234567890abcdef",
		Role:        "summarizer",
		Subject:     "summarize payload",
		Status:      swarm.TaskCompleted,
		Result:      "delegated result",
		CreatedAt:   completed.Add(-time.Minute),
		CompletedAt: &completed,
	}}}

	listOut, err := NewListSwarmTasksTool(reader).Execute(context.Background(), map[string]any{"run_id": "swarm_1234567890abcdef"})
	if err != nil {
		t.Fatalf("list Execute: %v", err)
	}
	if !json.Valid([]byte(listOut)) {
		t.Fatalf("list output not JSON: %q", listOut)
	}

	readOut, err := NewReadSwarmResultTool(reader).Execute(context.Background(), map[string]any{"task_id": "task_1234567890abcdef"})
	if err != nil {
		t.Fatalf("read Execute: %v", err)
	}
	var task taskSummary
	if err := json.Unmarshal([]byte(readOut), &task); err != nil {
		t.Fatalf("unmarshal read: %v", err)
	}
	if task.Result != "delegated result" || task.Status != string(swarm.TaskCompleted) {
		t.Fatalf("task summary = %+v", task)
	}
}

// TestCatalogueSwarmToolMetadata verifies that every swarm readback tool has
// been explicitly catalogued with MCP hints + VisibilityTier. Swarm tools
// cannot be checked from the registry package scan test due to the import cycle.
func TestCatalogueSwarmToolMetadata(t *testing.T) {
	isDefaultState := func(d tools.ToolDefinition) bool {
		return !d.ReadOnlyHint && !d.DestructiveHint && !d.IdempotentHint && !d.OpenWorldHint &&
			d.VisibilityTier == tools.VisibilityActiveTurn
	}

	swarmTools := []interface{ Definition() tools.ToolDefinition }{
		&ListSwarmTasksTool{},
		&ReadSwarmResultTool{},
	}

	for _, tool := range swarmTools {
		def := tool.Definition()
		if isDefaultState(def) {
			t.Errorf("swarm tool %q has all-defaults metadata (not explicitly catalogued); "+
				"add explicit hints + VisibilityTier to its Definition() method", def.Name)
		}
	}
}
