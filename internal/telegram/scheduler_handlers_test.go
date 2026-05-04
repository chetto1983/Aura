package telegram

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"
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

func TestDispatchAgentJobUsesSkillsToolsetsAndContextPrompt(t *testing.T) {
	fake := &schedulerFakeLLM{}
	reg := tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, name := range []string{
		"search_memory",
		"list_wiki",
		"read_wiki",
		"list_skills",
		"read_skill",
		"search_skill_catalog",
		"web_search",
	} {
		reg.Register(schedulerFakeTool{name: name})
	}
	runner, err := agent.NewRunner(agent.Config{LLM: fake, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	notify := false
	payload, err := scheduler.AgentJobPayload{
		Goal:            "Review daily memory drift",
		EnabledToolsets: []string{"memory_read"},
		Skills:          []string{"aura-implementation"},
		ContextFrom:     []string{"[[memory-philosophy]]"},
		WakeIfChanged:   []string{"wiki:memory-philosophy"},
		Notify:          &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	b := &Bot{
		agentRunner: runner,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err = b.dispatchTask(t.Context(), &scheduler.Task{
		Name:        "agent-skilled",
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
	req := fake.reqs[0]
	if len(req.Messages) < 2 {
		t.Fatalf("messages = %+v", req.Messages)
	}
	system := req.Messages[0].Content
	user := req.Messages[1].Content
	for _, want := range []string{"skill-backed", "wake_if_changed"} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, system)
		}
	}
	for _, want := range []string{"Attached skills: aura-implementation", "Context anchors: [[memory-philosophy]]", "Wake-if-changed signals: wiki:memory-philosophy"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, user)
		}
	}
	var names []string
	for _, def := range req.Tools {
		names = append(names, def.Name)
	}
	for _, want := range []string{"search_memory", "read_skill"} {
		if !containsTestString(names, want) {
			t.Fatalf("tool %q missing from allowlist: %+v", want, names)
		}
	}
	if containsTestString(names, "web_search") {
		t.Fatalf("web_search should not be enabled by memory_read toolset: %+v", names)
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

func TestRunAgentJobSkipsWhenWakeSignatureUnchanged(t *testing.T) {
	fake := &schedulerFakeLLM{}
	reg := tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg.Register(schedulerFakeTool{name: "search_memory"})
	runner, err := agent.NewRunner(agent.Config{LLM: fake, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	wikiStore, err := wiki.NewStore(filepath.Join(t.TempDir(), "wiki"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	now := time.Date(2026, 5, 4, 10, 0, 0, 0, time.UTC).Format(time.RFC3339)
	if err := wikiStore.WritePage(t.Context(), &wiki.Page{
		Title:         "Memory Philosophy",
		Category:      "system",
		Tags:          []string{"memory"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
		Body:          "Keep graph-aware memory compact.",
	}); err != nil {
		t.Fatalf("WritePage: %v", err)
	}
	notify := false
	payload, err := scheduler.AgentJobPayload{
		Goal:          "Review memory drift",
		WakeIfChanged: []string{"[[memory-philosophy]]"},
		Notify:        &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	b := &Bot{
		wiki:        wikiStore,
		agentRunner: runner,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	normalized, err := scheduler.NormalizeAgentJobPayload(payload)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	signature, ok := scheduler.AgentJobWakeSignature(t.Context(), normalized, scheduler.AgentJobWakeDeps{Wiki: wikiStore})
	if !ok || signature == "" {
		t.Fatalf("expected wake signature, got %q ok=%v", signature, ok)
	}
	run, err := b.runAgentJob(t.Context(), &scheduler.Task{
		Name:          "memory-drift",
		Kind:          scheduler.KindAgentJob,
		Payload:       payload,
		WakeSignature: signature,
	})
	if err != nil {
		t.Fatalf("runAgentJob: %v", err)
	}
	if !run.Skipped {
		t.Fatalf("run was not skipped: %+v", run)
	}
	if len(fake.reqs) != 0 {
		t.Fatalf("LLM calls = %d, want 0", len(fake.reqs))
	}
	if !strings.Contains(run.Result.Content, "skipped") {
		t.Fatalf("skip output = %q", run.Result.Content)
	}
}

func TestAgentJobPromptIncludesPriorTaskOutputs(t *testing.T) {
	store, err := scheduler.OpenStore(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	defer store.Close()
	next := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	task, err := store.Upsert(t.Context(), &scheduler.Task{
		Name:                 "prior-research",
		Kind:                 scheduler.KindAgentJob,
		Payload:              "research topic",
		ScheduleKind:         scheduler.ScheduleEvery,
		ScheduleEveryMinutes: 60,
		NextRunAt:            next,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if err := store.RecordAgentJobResult(t.Context(), task.ID, "Found three durable memory gaps.", `{"llm_calls":1}`, "sig-a"); err != nil {
		t.Fatalf("RecordAgentJobResult: %v", err)
	}
	notify := false
	payload, err := scheduler.AgentJobPayload{
		Goal:        "Continue the review",
		ContextFrom: []string{"task:prior-research"},
		Notify:      &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	normalized, err := scheduler.NormalizeAgentJobPayload(payload)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	b := &Bot{schedDB: store}
	prompt := b.agentJobPrompt(t.Context(), normalized)
	for _, want := range []string{"Prior job outputs", "prior-research", "Found three durable memory gaps"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func containsTestString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
