package swarm

import (
	"time"

	"github.com/aura/aura/internal/agent"
)

type RunStatus string

const (
	RunPending   RunStatus = "pending"
	RunRunning   RunStatus = "running"
	RunCompleted RunStatus = "completed"
	RunFailed    RunStatus = "failed"
)

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
)

type Run struct {
	ID          string
	Goal        string
	Status      RunStatus
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt *time.Time
	LastError   string
}

type Task struct {
	ID               string
	RunID            string
	ParentID         string
	Role             string
	Subject          string
	Prompt           string
	ToolAllowlist    []string
	Status           TaskStatus
	Depth            int
	Attempts         int
	BlockedBy        []string
	Result           string
	ToolCalls        int
	LLMCalls         int
	TokensPrompt     int
	TokensCompletion int
	TokensTotal      int
	ElapsedMS        int64
	CreatedAt        time.Time
	StartedAt        *time.Time
	CompletedAt      *time.Time
	LastError        string
}

type Assignment struct {
	ParentID           string
	Role               string
	Subject            string
	Prompt             string
	SystemPrompt       string
	ToolAllowlist      []string
	Depth              int
	UserID             string
	Temperature        *float64
	MaxToolCalls       int
	MaxToolResultChars int
	CompleteOnDeadline bool
}

func (a Assignment) AgentTask() agent.Task {
	return agent.Task{
		SystemPrompt:       a.SystemPrompt,
		Prompt:             a.Prompt,
		ToolAllowlist:      a.ToolAllowlist,
		UserID:             a.UserID,
		Temperature:        a.Temperature,
		MaxToolCalls:       a.MaxToolCalls,
		MaxToolResultChars: a.MaxToolResultChars,
		CompleteOnDeadline: a.CompleteOnDeadline,
	}
}
