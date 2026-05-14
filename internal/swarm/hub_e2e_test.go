package swarm

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/chat"
	silentadapter "github.com/aura/aura/internal/channels/silent"
)

// captureLoop is a fake chat.AgentLoop that records every InboundMessage
// it receives, then returns a synthetic result. Used to verify the bridge
// propagates parent_run_id to children without a real LLM.
type captureLoop struct {
	mu   sync.Mutex
	msgs []chat.InboundMessage
	runs []*chat.Run
}

func (l *captureLoop) Run(_ context.Context, run *chat.Run, msg chat.InboundMessage, emit chat.EmitFn) error {
	l.mu.Lock()
	l.msgs = append(l.msgs, msg)
	l.runs = append(l.runs, run)
	l.mu.Unlock()
	run.Metadata["final_text"] = "done"
	return nil
}

func TestSwarmHubBridgePropagatesParentRunID(t *testing.T) {
	loop := &captureLoop{}
	hub, err := chat.New(chat.Config{Loop: loop})
	if err != nil {
		t.Fatalf("chat.New: %v", err)
	}
	hub.RegisterOutbound(silentadapter.NewSwarm(silentadapter.Config{}))

	store := newInMemoryStore(t)
	mgr, err := NewManager(ManagerConfig{
		Runner:    NewHubBridge(hub, ""),
		Store:     store,
		MaxActive: 3,
		MaxDepth:  1,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	assignments := []Assignment{
		{Role: "worker", Subject: "task1", Prompt: "do task 1", UserID: "u1"},
		{Role: "worker", Subject: "task2", Prompt: "do task 2", UserID: "u1"},
		{Role: "worker", Subject: "task3", Prompt: "do task 3", UserID: "u1"},
	}
	result, err := mgr.Run(context.Background(), RunRequest{
		Goal:        "test parent propagation",
		CreatedBy:   "u1",
		Assignments: assignments,
	})
	if err != nil {
		t.Fatalf("Manager.Run: %v", err)
	}
	swarmRunID := result.Run.ID
	if swarmRunID == "" {
		t.Fatal("swarm run ID is empty")
	}

	loop.mu.Lock()
	defer loop.mu.Unlock()

	if len(loop.runs) != 3 {
		t.Fatalf("expected 3 child runs, got %d", len(loop.runs))
	}
	for i, run := range loop.runs {
		got, _ := run.Metadata["parent_run_id"].(string)
		if got != swarmRunID {
			t.Errorf("child[%d] parent_run_id = %q, want %q", i, got, swarmRunID)
		}
	}
}

// newInMemoryStore creates a fresh in-memory swarm repository for tests.
// Reuses the existing fake from manager_test.go (same package).
func newInMemoryStore(_ *testing.T) Repository {
	return &inMemStore{}
}

// inMemStore is a minimal in-memory Repository so hub_e2e_test.go works
// without a SQLite database.
type inMemStore struct {
	mu    sync.Mutex
	runs  map[string]*Run
	tasks map[string]*Task
	seq   int
}

func (s *inMemStore) nextID() string {
	s.seq++
	return fmt.Sprintf("id-%d", s.seq)
}

func (s *inMemStore) init() {
	if s.runs == nil {
		s.runs = map[string]*Run{}
		s.tasks = map[string]*Task{}
	}
}

func (s *inMemStore) CreateRun(ctx context.Context, goal, createdBy string) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	r := &Run{ID: s.nextID(), Goal: goal, CreatedBy: createdBy, Status: RunPending}
	s.runs[r.ID] = r
	return r, nil
}
func (s *inMemStore) MarkRunRunning(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		r.Status = RunRunning
	}
	return nil
}
func (s *inMemStore) CompleteRun(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		r.Status = RunCompleted
	}
	return nil
}
func (s *inMemStore) FailRun(_ context.Context, id, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.runs[id]; ok {
		r.Status = RunFailed
		r.LastError = msg
	}
	return nil
}
func (s *inMemStore) GetRun(_ context.Context, id string) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, errors.New("run not found")
	}
	return r, nil
}
func (s *inMemStore) ListRuns(_ context.Context, _ int) ([]Run, error) { return nil, nil }
func (s *inMemStore) CreateTask(_ context.Context, runID string, a Assignment) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	t := &Task{ID: s.nextID(), RunID: runID, Role: a.Role, Subject: a.Subject, Prompt: a.Prompt, Status: TaskPending, Depth: a.Depth}
	s.tasks[t.ID] = t
	return t, nil
}
func (s *inMemStore) MarkTaskRunning(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Status = TaskRunning
	}
	return nil
}
func (s *inMemStore) CompleteTask(_ context.Context, id string, r agent.Result) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Status = TaskCompleted
		t.Result = r.Content
	}
	return nil
}
func (s *inMemStore) FailTask(_ context.Context, id, msg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.tasks[id]; ok {
		t.Status = TaskFailed
		t.LastError = msg
	}
	return nil
}
func (s *inMemStore) ListTasks(_ context.Context, runID string) ([]Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.tasks {
		if t.RunID == runID {
			out = append(out, *t)
		}
	}
	return out, nil
}
func (s *inMemStore) ReadTask(_ context.Context, id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errors.New("task not found")
	}
	return t, nil
}

func (s *inMemStore) GetTask(_ context.Context, id string) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, errors.New("task not found")
	}
	return t, nil
}
