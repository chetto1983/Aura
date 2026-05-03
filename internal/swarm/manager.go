package swarm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/aura/aura/internal/agent"
)

const (
	defaultMaxActive = 2
	defaultMaxDepth  = 1
)

type AgentRunner interface {
	Run(ctx context.Context, task agent.Task) (agent.Result, error)
}

type Manager struct {
	runner    AgentRunner
	store     *Store
	mu        sync.RWMutex
	maxActive int
	maxDepth  int
	logger    *slog.Logger
}

type ManagerConfig struct {
	Runner    AgentRunner
	Store     *Store
	MaxActive int
	MaxDepth  int
	Logger    *slog.Logger
}

type RunRequest struct {
	Goal        string
	CreatedBy   string
	Assignments []Assignment
}

type RunResult struct {
	Run   *Run
	Tasks []Task
}

func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.Runner == nil {
		return nil, errors.New("swarm manager: runner required")
	}
	if cfg.Store == nil {
		return nil, errors.New("swarm manager: store required")
	}
	maxActive, maxDepth := normalizeLimits(cfg.MaxActive, cfg.MaxDepth)
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		runner:    cfg.Runner,
		store:     cfg.Store,
		maxActive: maxActive,
		maxDepth:  maxDepth,
		logger:    logger,
	}, nil
}

func normalizeLimits(maxActive, maxDepth int) (int, int) {
	if maxActive <= 0 {
		maxActive = defaultMaxActive
	}
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	return maxActive, maxDepth
}

// Limits returns the concurrency/delegation limits used for new runs.
func (m *Manager) Limits() (maxActive int, maxDepth int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.maxActive, m.maxDepth
}

// UpdateLimits changes the concurrency/delegation limits used by subsequent runs.
func (m *Manager) UpdateLimits(maxActive, maxDepth int) {
	maxActive, maxDepth = normalizeLimits(maxActive, maxDepth)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maxActive = maxActive
	m.maxDepth = maxDepth
}

func (m *Manager) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	if len(req.Assignments) == 0 {
		return RunResult{}, errors.New("swarm manager: at least one assignment required")
	}
	maxActive, maxDepth := m.Limits()
	run, err := m.store.CreateRun(ctx, req.Goal, req.CreatedBy)
	if err != nil {
		return RunResult{}, err
	}
	if err := m.store.MarkRunRunning(ctx, run.ID); err != nil {
		return RunResult{Run: run}, err
	}

	taskRows := make([]*Task, 0, len(req.Assignments))
	for _, assignment := range req.Assignments {
		task, err := m.store.CreateTask(ctx, run.ID, assignment)
		if err != nil {
			_ = m.store.FailRun(ctx, run.ID, err.Error())
			return RunResult{Run: run}, err
		}
		taskRows = append(taskRows, task)
	}

	sem := make(chan struct{}, maxActive)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failed []error

	for i, assignment := range req.Assignments {
		task := taskRows[i]
		wg.Add(1)
		go func(task *Task, assignment Assignment) {
			defer wg.Done()
			if assignment.Depth > maxDepth {
				err := fmt.Errorf("task depth %d exceeds max depth %d", assignment.Depth, maxDepth)
				if markErr := m.store.FailTask(context.Background(), task.ID, err.Error()); markErr != nil {
					err = errors.Join(err, markErr)
				}
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
				return
			}

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				err := ctx.Err()
				_ = m.store.FailTask(context.Background(), task.ID, err.Error())
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
				return
			}

			if err := m.store.MarkTaskRunning(ctx, task.ID); err != nil {
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
				return
			}
			result, err := m.runner.Run(ctx, assignment.AgentTask())
			if err != nil {
				if m.logger != nil {
					m.logger.Warn("swarm task failed", "run", run.ID, "task", task.ID, "role", assignment.Role, "error", err)
				}
				_ = m.store.FailTask(context.Background(), task.ID, err.Error())
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
				return
			}
			if err := m.store.CompleteTask(ctx, task.ID, result); err != nil {
				mu.Lock()
				failed = append(failed, err)
				mu.Unlock()
			}
		}(task, assignment)
	}
	wg.Wait()

	var runErr error
	if len(failed) > 0 {
		runErr = errors.Join(failed...)
		if err := m.store.FailRun(context.Background(), run.ID, runErr.Error()); err != nil {
			runErr = errors.Join(runErr, err)
		}
	} else if err := m.store.CompleteRun(ctx, run.ID); err != nil {
		runErr = err
	}

	freshRun, getRunErr := m.store.GetRun(context.Background(), run.ID)
	if getRunErr != nil {
		return RunResult{Run: run}, errors.Join(runErr, getRunErr)
	}
	tasks, listErr := m.store.ListTasks(context.Background(), run.ID)
	if listErr != nil {
		return RunResult{Run: freshRun}, errors.Join(runErr, listErr)
	}
	out := RunResult{Run: freshRun, Tasks: tasks}
	return out, runErr
}

func (m *Manager) ListTasks(ctx context.Context, runID string) ([]Task, error) {
	return m.store.ListTasks(ctx, runID)
}

func (m *Manager) ReadTask(ctx context.Context, taskID string) (*Task, error) {
	return m.store.GetTask(ctx, taskID)
}
