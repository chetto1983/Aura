package telegram

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/tools"
)

type schedulerFakeLLM struct {
	reqs []llm.Request
}

func (f *schedulerFakeLLM) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.reqs = append(f.reqs, req)
	return llm.Response{Content: "checked sources and created one proposal"}, nil
}

func (f *schedulerFakeLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

type schedulerFakeTool struct {
	name string
}

func (t schedulerFakeTool) Name() string               { return t.name }
func (t schedulerFakeTool) Description() string        { return t.name }
func (t schedulerFakeTool) Parameters() map[string]any { return map[string]any{"type": "object"} }
func (t schedulerFakeTool) Execute(context.Context, map[string]any) (string, error) {
	return "ok", nil
}

func TestDispatchAgentJobRunsBoundedRunner(t *testing.T) {
	fake := &schedulerFakeLLM{}
	reg := tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg.Register(schedulerFakeTool{name: "web_search"})
	reg.Register(schedulerFakeTool{name: "write_wiki"})
	reg.Register(schedulerFakeTool{name: "propose_wiki_change"})
	runner, err := agent.NewRunner(agent.Config{LLM: fake, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	notify := false
	payload, err := scheduler.AgentJobPayload{
		Goal:          "Check Aura gaps",
		ToolAllowlist: []string{"web_search", "write_wiki", "propose_wiki_change"},
		Notify:        &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	b := &Bot{
		agentRunner: runner,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err = b.dispatchTask(t.Context(), &scheduler.Task{
		Name:        "agent-smoke",
		Kind:        scheduler.KindAgentJob,
		Payload:     payload,
		RecipientID: "123",
	})
	if err != nil {
		t.Fatalf("dispatchTask: %v", err)
	}
	if len(fake.reqs) != 1 {
		t.Fatalf("LLM calls = %d, want 1", len(fake.reqs))
	}
	var names []string
	for _, def := range fake.reqs[0].Tools {
		names = append(names, def.Name)
	}
	for _, forbidden := range []string{"write_wiki"} {
		for _, name := range names {
			if name == forbidden {
				t.Fatalf("forbidden tool %q leaked into agent job allowlist: %#v", forbidden, names)
			}
		}
	}
}

func TestRunTaskNowRunsSavedAgentJob(t *testing.T) {
	fake := &schedulerFakeLLM{}
	reg := tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg.Register(schedulerFakeTool{name: "web_search"})
	reg.Register(schedulerFakeTool{name: "write_wiki"})
	reg.Register(schedulerFakeTool{name: "propose_wiki_change"})
	runner, err := agent.NewRunner(agent.Config{LLM: fake, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	notify := false
	payload, err := scheduler.AgentJobPayload{
		Goal:          "Check Aura gaps",
		ToolAllowlist: []string{"web_search", "write_wiki", "propose_wiki_change"},
		Notify:        &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	next := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	task, err := store.Upsert(t.Context(), &scheduler.Task{
		Name:                 "agent-now",
		Kind:                 scheduler.KindAgentJob,
		Payload:              payload,
		RecipientID:          "123",
		ScheduleKind:         scheduler.ScheduleEvery,
		ScheduleEveryMinutes: 60,
		NextRunAt:            next,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	b := &Bot{
		schedDB:     store,
		agentRunner: runner,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	result, err := b.RunTaskNow(t.Context(), task.Name)
	if err != nil {
		t.Fatalf("RunTaskNow: %v", err)
	}
	if !result.OK || result.Name != "agent-now" || result.Status != "completed" || result.LLMCalls != 1 {
		t.Fatalf("result = %+v", result)
	}
	got, err := store.GetByName(t.Context(), "agent-now")
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if got.LastRunAt.IsZero() {
		t.Fatal("manual run did not record last_run_at")
	}
	if !got.NextRunAt.Equal(next) {
		t.Fatalf("next_run_at = %v, want preserved %v", got.NextRunAt, next)
	}
	var names []string
	for _, def := range fake.reqs[0].Tools {
		names = append(names, def.Name)
	}
	for _, name := range names {
		if name == "write_wiki" {
			t.Fatalf("forbidden tool leaked into run_task_now allowlist: %#v", names)
		}
	}
}
