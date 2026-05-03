package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/swarm"
	"github.com/aura/aura/internal/swarmtools"
	"github.com/aura/aura/internal/tools"
)

type config struct {
	jsonOut    bool
	keepDB     bool
	maxActive  int
	maxDepth   int
	timeoutSec int
}

type metrics struct {
	RunID               string        `json:"run_id"`
	Status              string        `json:"status"`
	TasksTotal          int           `json:"tasks_total"`
	TasksCompleted      int           `json:"tasks_completed"`
	TasksFailed         int           `json:"tasks_failed"`
	WallMS              int64         `json:"wall_ms"`
	TaskElapsedMS       int64         `json:"task_elapsed_ms"`
	Speedup             float64       `json:"speedup"`
	MaxActive           int           `json:"max_active"`
	MaxDepth            int           `json:"max_depth"`
	LLMCalls            int           `json:"llm_calls"`
	ToolCalls           int           `json:"tool_calls"`
	TokensPrompt        int           `json:"tokens_prompt"`
	TokensCompletion    int           `json:"tokens_completion"`
	TokensTotal         int           `json:"tokens_total"`
	DBPath              string        `json:"db_path"`
	SpawnAuraBotJSON    bool          `json:"spawn_aurabot_json"`
	ListSwarmTasksJSON  bool          `json:"list_swarm_tasks_json"`
	ReadSwarmResultJSON bool          `json:"read_swarm_result_json"`
	Tasks               []taskMetrics `json:"tasks"`
	Error               string        `json:"error,omitempty"`
	Elapsed             time.Duration `json:"-"`
}

type taskMetrics struct {
	ID               string   `json:"id"`
	Role             string   `json:"role"`
	Subject          string   `json:"subject"`
	Status           string   `json:"status"`
	Depth            int      `json:"depth"`
	LLMCalls         int      `json:"llm_calls"`
	ToolCalls        int      `json:"tool_calls"`
	TokensPrompt     int      `json:"tokens_prompt"`
	TokensCompletion int      `json:"tokens_completion"`
	TokensTotal      int      `json:"tokens_total"`
	ElapsedMS        int64    `json:"elapsed_ms"`
	Tools            []string `json:"tools"`
	Result           string   `json:"result,omitempty"`
	Error            string   `json:"error,omitempty"`
}

func main() {
	cfg := parseFlags()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "debug_swarm: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() config {
	var cfg config
	flag.BoolVar(&cfg.jsonOut, "json", false, "print metrics as JSON")
	flag.BoolVar(&cfg.keepDB, "keep-db", false, "keep the temporary SQLite swarm database")
	flag.IntVar(&cfg.maxActive, "max-active", 3, "maximum concurrently active swarm tasks")
	flag.IntVar(&cfg.maxDepth, "max-depth", 1, "maximum allowed task depth")
	flag.IntVar(&cfg.timeoutSec, "timeout-sec", 20, "overall debug run timeout in seconds")
	flag.Parse()
	return cfg
}

func run(cfg config) error {
	if cfg.maxActive <= 0 {
		return fmt.Errorf("-max-active must be greater than zero")
	}
	if cfg.maxDepth < 0 {
		return fmt.Errorf("-max-depth must be zero or greater")
	}
	if cfg.timeoutSec <= 0 {
		return fmt.Errorf("-timeout-sec must be greater than zero")
	}

	tmpDir, err := os.MkdirTemp("", "aura-debug-swarm-*")
	if err != nil {
		return err
	}
	dbPath := filepath.Join(tmpDir, "swarm.db")
	if !cfg.keepDB {
		defer os.RemoveAll(tmpDir)
	}

	store, err := swarm.OpenStore(dbPath)
	if err != nil {
		return err
	}
	defer store.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	registry := fakeRegistry(logger)
	fakeModel := &fakeLLM{}
	runner, err := agent.NewRunner(agent.Config{
		LLM:           fakeModel,
		Tools:         registry,
		Model:         "debug-fake",
		MaxIterations: 3,
		Timeout:       time.Duration(cfg.timeoutSec) * time.Second,
		ToolTimeout:   2 * time.Second,
		Logger:        logger,
	})
	if err != nil {
		return err
	}
	metered := &meteredRunner{next: runner}
	manager, err := swarm.NewManager(swarm.ManagerConfig{
		Runner:    metered,
		Store:     store,
		MaxActive: cfg.maxActive,
		MaxDepth:  cfg.maxDepth,
		Logger:    logger,
	})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.timeoutSec)*time.Second)
	defer cancel()

	assignments := debugAssignments()
	start := time.Now()
	result, runErr := manager.Run(ctx, swarm.RunRequest{
		Goal:        "debug hermetic AuraBot swarm",
		CreatedBy:   "debug_swarm",
		Assignments: assignments,
	})
	wall := time.Since(start).Round(time.Millisecond)

	out := collectMetrics(result, dbPath, wall, metered)
	out.SpawnAuraBotJSON = smokeSpawnTool(ctx, manager)
	out.ListSwarmTasksJSON = smokeListTool(ctx, store, out.RunID)
	out.ReadSwarmResultJSON = smokeReadTool(ctx, store, out.Tasks)
	if runErr != nil {
		out.Error = runErr.Error()
	}

	if cfg.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			return err
		}
	} else {
		printText(out)
	}
	if runErr != nil {
		return runErr
	}
	return nil
}

func debugAssignments() []swarm.Assignment {
	return []swarm.Assignment{
		{
			Role:          "librarian",
			Subject:       "wiki index scan",
			Prompt:        "List wiki pages and read the index page.",
			SystemPrompt:  "You are an AuraBot librarian. Use read-only wiki tools and report concise evidence.",
			ToolAllowlist: []string{"list_wiki", "read_wiki", "search_wiki", "lint_wiki", "list_sources", "read_source", "lint_sources"},
			Depth:         0,
			UserID:        "debug-user",
		},
		{
			Role:          "critic",
			Subject:       "wiki lint check",
			Prompt:        "Check the wiki and source inbox for obvious structural issues.",
			SystemPrompt:  "You are an AuraBot critic. Prefer lint tools and keep the result short.",
			ToolAllowlist: []string{"lint_wiki", "list_wiki", "read_wiki", "lint_sources", "list_sources"},
			Depth:         0,
			UserID:        "debug-user",
		},
		{
			Role:          "synthesizer",
			Subject:       "second brain synthesis",
			Prompt:        "Read available wiki/source context and synthesize one operational takeaway.",
			SystemPrompt:  "You are an AuraBot synthesizer. Combine read-only context into a concise answer.",
			ToolAllowlist: []string{"list_wiki", "read_wiki", "search_wiki", "list_sources", "read_source"},
			Depth:         1,
			UserID:        "debug-user",
		},
		{
			Role:          "librarian",
			Subject:       "source inbox scan",
			Prompt:        "List source inbox entries and report one useful item.",
			SystemPrompt:  "You are an AuraBot librarian. Use read-only source tools and report concise evidence.",
			ToolAllowlist: []string{"list_wiki", "read_wiki", "search_wiki", "lint_wiki", "list_sources", "read_source", "lint_sources"},
			Depth:         0,
			UserID:        "debug-user",
		},
		{
			Role:          "critic",
			Subject:       "link hygiene",
			Prompt:        "Check whether wiki links and source state look structurally healthy.",
			SystemPrompt:  "You are an AuraBot critic. Prefer lint tools and keep the result short.",
			ToolAllowlist: []string{"lint_wiki", "list_wiki", "read_wiki", "lint_sources", "list_sources"},
			Depth:         0,
			UserID:        "debug-user",
		},
		{
			Role:          "skillsmith",
			Subject:       "skill catalog smoke",
			Prompt:        "Inspect the available skill catalog path and summarize whether skill discovery works.",
			SystemPrompt:  "You are an AuraBot skillsmith. Use read-only skill tools and report concise evidence.",
			ToolAllowlist: []string{"list_skills", "read_skill", "search_skill_catalog"},
			Depth:         1,
			UserID:        "debug-user",
		},
	}
}

func collectMetrics(result swarm.RunResult, dbPath string, wall time.Duration, runner *meteredRunner) metrics {
	out := metrics{
		DBPath:    dbPath,
		WallMS:    wall.Milliseconds(),
		Elapsed:   wall,
		MaxActive: runner.maxActiveSeen(),
	}
	if result.Run != nil {
		out.RunID = result.Run.ID
		out.Status = string(result.Run.Status)
	}
	for _, task := range result.Tasks {
		out.TasksTotal++
		if task.Status == swarm.TaskCompleted {
			out.TasksCompleted++
		}
		if task.Status == swarm.TaskFailed {
			out.TasksFailed++
		}
		if task.Depth > out.MaxDepth {
			out.MaxDepth = task.Depth
		}
		out.TaskElapsedMS += task.ElapsedMS
		out.LLMCalls += task.LLMCalls
		out.ToolCalls += task.ToolCalls
		out.TokensPrompt += task.TokensPrompt
		out.TokensCompletion += task.TokensCompletion
		out.TokensTotal += task.TokensTotal
		out.Tasks = append(out.Tasks, taskMetrics{
			ID:               task.ID,
			Role:             task.Role,
			Subject:          task.Subject,
			Status:           string(task.Status),
			Depth:            task.Depth,
			LLMCalls:         task.LLMCalls,
			ToolCalls:        task.ToolCalls,
			TokensPrompt:     task.TokensPrompt,
			TokensCompletion: task.TokensCompletion,
			TokensTotal:      task.TokensTotal,
			ElapsedMS:        task.ElapsedMS,
			Tools:            task.ToolAllowlist,
			Result:           trim(task.Result, 180),
			Error:            task.LastError,
		})
	}
	if out.WallMS > 0 {
		out.Speedup = float64(out.TaskElapsedMS) / float64(out.WallMS)
	}
	return out
}

func smokeListTool(ctx context.Context, store *swarm.Store, runID string) bool {
	if runID == "" {
		return false
	}
	out, err := swarmtools.NewListSwarmTasksTool(store).Execute(ctx, map[string]any{"run_id": runID})
	return err == nil && json.Valid([]byte(out))
}

func smokeReadTool(ctx context.Context, store *swarm.Store, tasks []taskMetrics) bool {
	for _, task := range tasks {
		if task.ID == "" {
			continue
		}
		out, err := swarmtools.NewReadSwarmResultTool(store).Execute(ctx, map[string]any{"task_id": task.ID})
		return err == nil && json.Valid([]byte(out))
	}
	return false
}

func smokeSpawnTool(ctx context.Context, manager *swarm.Manager) bool {
	out, err := swarmtools.NewSpawnAuraBotTool(manager).Execute(ctx, map[string]any{
		"name":  "debug spawn tool smoke",
		"role":  "librarian",
		"task":  "Use the wiki listing path and return a concise debug result.",
		"tools": []any{"list_wiki"},
	})
	return err == nil && json.Valid([]byte(out))
}

func printText(m metrics) {
	fmt.Printf("run_id=%s status=%s db_path=%s\n", m.RunID, m.Status, m.DBPath)
	fmt.Printf("tasks_total=%d completed=%d failed=%d\n", m.TasksTotal, m.TasksCompleted, m.TasksFailed)
	fmt.Printf("wall_ms=%d task_elapsed_ms=%d speedup=%.2fx max_active=%d max_depth=%d\n", m.WallMS, m.TaskElapsedMS, m.Speedup, m.MaxActive, m.MaxDepth)
	fmt.Printf("llm_calls=%d tool_calls=%d tokens_prompt=%d tokens_completion=%d tokens_total=%d\n", m.LLMCalls, m.ToolCalls, m.TokensPrompt, m.TokensCompletion, m.TokensTotal)
	fmt.Printf("spawn_aurabot_json=%t swarmtools_list_json=%t swarmtools_read_json=%t\n", m.SpawnAuraBotJSON, m.ListSwarmTasksJSON, m.ReadSwarmResultJSON)
	if m.Error != "" {
		fmt.Printf("error=%s\n", m.Error)
	}
	fmt.Println()
	fmt.Println("tasks:")
	fmt.Printf("%-18s %-11s %-12s %-5s %-4s %-5s %-6s %-7s %s\n", "id", "role", "status", "depth", "llm", "tools", "tokens", "ms", "subject")
	for _, task := range m.Tasks {
		fmt.Printf("%-18s %-11s %-12s %-5d %-4d %-5d %-6d %-7d %s\n",
			task.ID, task.Role, task.Status, task.Depth, task.LLMCalls, task.ToolCalls, task.TokensTotal, task.ElapsedMS, task.Subject)
		if task.Error != "" {
			fmt.Printf("  error: %s\n", task.Error)
		}
		if task.Result != "" {
			fmt.Printf("  result: %s\n", task.Result)
		}
	}
}

type meteredRunner struct {
	next agentRunner

	mu        sync.Mutex
	active    int
	maxActive int
}

type agentRunner interface {
	Run(ctx context.Context, task agent.Task) (agent.Result, error)
}

func (r *meteredRunner) Run(ctx context.Context, task agent.Task) (agent.Result, error) {
	r.mu.Lock()
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.active--
		r.mu.Unlock()
	}()

	return r.next.Run(ctx, task)
}

func (r *meteredRunner) maxActiveSeen() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.maxActive
}

type fakeLLM struct {
	mu  sync.Mutex
	seq int
}

func (f *fakeLLM) Send(ctx context.Context, req llm.Request) (llm.Response, error) {
	if err := sleepContext(ctx, 120*time.Millisecond); err != nil {
		return llm.Response{}, err
	}
	f.mu.Lock()
	f.seq++
	seq := f.seq
	f.mu.Unlock()

	usage := llm.TokenUsage{
		PromptTokens:     estimateTokens(req.Messages) + len(req.Tools)*3,
		CompletionTokens: 16,
	}
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens

	if len(req.Tools) > 0 && !hasToolResult(req.Messages) {
		tool := preferredTool(req.Tools)
		return llm.Response{
			Content:      "Calling " + tool + " for deterministic debug evidence.",
			Usage:        usage,
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{{
				ID:        fmt.Sprintf("call_%03d", seq),
				Name:      tool,
				Arguments: fakeArgs(tool),
			}},
		}, nil
	}

	role := roleFromMessages(req.Messages)
	content := fmt.Sprintf("debug %s complete: observed %d messages, %d tools offered, hermetic fake LLM path ok.", role, len(req.Messages), len(req.Tools))
	usage.CompletionTokens = estimateStringTokens(content)
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	return llm.Response{Content: content, Usage: usage}, nil
}

func (f *fakeLLM) Stream(ctx context.Context, req llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token, 1)
	resp, err := f.Send(ctx, req)
	if err != nil {
		ch <- llm.Token{Done: true, Err: err}
		close(ch)
		return ch, nil
	}
	ch <- llm.Token{Content: resp.Content, ToolCalls: resp.ToolCalls, Usage: resp.Usage, Done: true}
	close(ch)
	return ch, nil
}

func fakeRegistry(logger *slog.Logger) *tools.Registry {
	reg := tools.NewRegistry(logger)
	for _, name := range []string{
		"list_wiki", "read_wiki", "search_wiki", "lint_wiki",
		"list_sources", "read_source", "lint_sources",
		"web_search", "web_fetch",
		"list_skills", "read_skill", "search_skill_catalog",
	} {
		reg.Register(fakeTool{name: name})
	}
	return reg
}

type fakeTool struct {
	name string
}

func (t fakeTool) Name() string { return t.name }

func (t fakeTool) Description() string {
	return "Hermetic debug fake for the read-only Aura tool " + t.name + "."
}

func (t fakeTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query":  map[string]any{"type": "string"},
			"slug":   map[string]any{"type": "string"},
			"source": map[string]any{"type": "string"},
			"name":   map[string]any{"type": "string"},
		},
	}
}

func (t fakeTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if err := sleepContext(ctx, 80*time.Millisecond); err != nil {
		return "", err
	}
	keys := make([]string, 0, len(args))
	for key := range args {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return fmt.Sprintf(`{"tool":%q,"ok":true,"keys":%q,"items":["index","source-inbox","aura-skills"]}`, t.name, strings.Join(keys, ",")), nil
}

func preferredTool(defs []llm.ToolDefinition) string {
	preferred := []string{"list_wiki", "read_wiki", "lint_wiki", "search_wiki", "list_sources", "read_source", "list_skills", "web_search", "web_fetch"}
	for _, want := range preferred {
		for _, def := range defs {
			if def.Name == want {
				return def.Name
			}
		}
	}
	return defs[0].Name
}

func fakeArgs(tool string) map[string]any {
	switch tool {
	case "read_wiki":
		return map[string]any{"slug": "index"}
	case "read_source":
		return map[string]any{"source": "source-inbox"}
	case "read_skill":
		return map[string]any{"name": "aura-implementation"}
	case "search_wiki", "web_search", "search_skill_catalog":
		return map[string]any{"query": "Aura second brain"}
	default:
		return map[string]any{}
	}
}

func hasToolResult(messages []llm.Message) bool {
	for _, msg := range messages {
		if msg.Role == "tool" {
			return true
		}
	}
	return false
}

func roleFromMessages(messages []llm.Message) string {
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		lower := strings.ToLower(msg.Content)
		for _, role := range []string{"librarian", "critic", "researcher", "synthesizer", "skillsmith"} {
			if strings.Contains(lower, role) {
				return role
			}
		}
	}
	return "worker"
}

func estimateTokens(messages []llm.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateStringTokens(msg.Role) + estimateStringTokens(msg.Content)
		total += len(msg.ToolCalls) * 12
	}
	if total == 0 {
		return 1
	}
	return total
}

func estimateStringTokens(s string) int {
	n := len(strings.Fields(s))
	if n == 0 && s != "" {
		return 1
	}
	return n
}

func trim(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	if max < 4 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
