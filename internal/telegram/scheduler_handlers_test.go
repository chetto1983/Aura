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
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/cron"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/llm"
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
	reg.Register(schedulerFakeTool{name: "web"})
	reg.Register(schedulerFakeTool{name: "read_file"})
	runner, err := agent.NewRunner(agent.Config{LLM: fake, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	notify := false
	payload, err := cron.AgentJobPayload{
		Goal:          "Check Aura gaps",
		ToolAllowlist: []string{"web", "write_file", "read_file"},
		Notify:        &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	b := &Bot{
		rt:     &botRuntime{agentRunner: runner},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err = b.dispatchTask(t.Context(), &cron.Task{
		Name:        "agent-smoke",
		Kind:        cron.KindAgentJob,
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
	for _, forbidden := range []string{"write_file"} {
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
		"file",
		"web",
	} {
		reg.Register(schedulerFakeTool{name: name})
	}
	runner, err := agent.NewRunner(agent.Config{LLM: fake, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	notify := false
	payload, err := cron.AgentJobPayload{
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
		rt:     &botRuntime{agentRunner: runner},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	err = b.dispatchTask(t.Context(), &cron.Task{
		Name:        "agent-skilled",
		Kind:        cron.KindAgentJob,
		Payload:     payload,
		RecipientID: "123",
		NextRunAt:   time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC),
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
	for _, want := range []string{"skill-backed", "wake_if_changed", "## Runtime Context", "Current local time"} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, system)
		}
	}
	for _, want := range []string{"Scheduled task: agent-skilled", "Scheduled for:", "Running at:", "Attached skills: aura-implementation", "Context anchors: [[memory-philosophy]]", "Wake-if-changed signals: wiki:memory-philosophy"} {
		if !strings.Contains(user, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, user)
		}
	}
	var names []string
	for _, def := range req.Tools {
		names = append(names, def.Name)
	}
	for _, want := range []string{"search_memory", "file"} {
		if !containsTestString(names, want) {
			t.Fatalf("tool %q missing from allowlist: %+v", want, names)
		}
	}
	if containsTestString(names, "web") {
		t.Fatalf("web should not be enabled by memory_read toolset: %+v", names)
	}
}

func TestRunTaskNowRunsSavedAgentJob(t *testing.T) {
	fake := &schedulerFakeLLM{}
	reg := tools.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	reg.Register(schedulerFakeTool{name: "web"})
	reg.Register(schedulerFakeTool{name: "read_file"})
	runner, err := agent.NewRunner(agent.Config{LLM: fake, Tools: reg})
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	store := newTelegramTestSchedulerStore(t)
	notify := false
	payload, err := cron.AgentJobPayload{
		Goal:          "Check Aura gaps",
		ToolAllowlist: []string{"web", "write_file", "read_file"},
		Notify:        &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	next := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	task, err := store.Upsert(t.Context(), &cron.Task{
		Name:                 "agent-now",
		Kind:                 cron.KindAgentJob,
		Payload:              payload,
		RecipientID:          "123",
		ScheduleKind:         cron.ScheduleEvery,
		ScheduleEveryMinutes: 60,
		NextRunAt:            next,
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	b := &Bot{
		rt:     &botRuntime{schedDB: store, agentRunner: runner},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
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
		if name == "write_file" {
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
	payload, err := cron.AgentJobPayload{
		Goal:          "Review memory drift",
		WakeIfChanged: []string{"[[memory-philosophy]]"},
		Notify:        &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	b := &Bot{
		rt:     &botRuntime{wiki: wikiStore, agentRunner: runner},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	normalized, err := cron.NormalizeAgentJobPayload(payload)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	signature, ok := cron.AgentJobWakeSignature(t.Context(), normalized, cron.AgentJobWakeDeps{Wiki: wikiStore})
	if !ok || signature == "" {
		t.Fatalf("expected wake signature, got %q ok=%v", signature, ok)
	}
	run, err := b.runAgentJob(t.Context(), &cron.Task{
		Name:          "memory-drift",
		Kind:          cron.KindAgentJob,
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
	store := newTelegramTestSchedulerStore(t)
	next := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	task, err := store.Upsert(t.Context(), &cron.Task{
		Name:                 "prior-research",
		Kind:                 cron.KindAgentJob,
		Payload:              "research topic",
		ScheduleKind:         cron.ScheduleEvery,
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
	payload, err := cron.AgentJobPayload{
		Goal:        "Continue the review",
		ContextFrom: []string{"task:prior-research"},
		Notify:      &notify,
	}.JSON()
	if err != nil {
		t.Fatalf("payload JSON: %v", err)
	}
	normalized, err := cron.NormalizeAgentJobPayload(payload)
	if err != nil {
		t.Fatalf("NormalizeAgentJobPayload: %v", err)
	}
	b := &Bot{rt: &botRuntime{schedDB: store}}
	prompt := b.agentJobPrompt(t.Context(), nil, normalized, time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC), time.UTC)
	for _, want := range []string{"Prior job outputs", "prior-research", "Found three durable memory gaps"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestAgentJobScheduleContextShowsLateRun(t *testing.T) {
	task := &cron.Task{
		Name:             "trading-signals-morning",
		ScheduleKind:     cron.ScheduleDaily,
		ScheduleDaily:    "10:00",
		ScheduleWeekdays: "mon,tue,wed,thu,fri",
		NextRunAt:        time.Date(2026, 5, 4, 8, 0, 0, 0, time.UTC),
	}
	now := time.Date(2026, 5, 4, 13, 19, 0, 0, time.UTC)
	got := agentJobScheduleContext(task, now, time.FixedZone("CEST", 2*3600))
	for _, want := range []string{
		"Scheduled task: trading-signals-morning",
		"Scheduled for: 2026-05-04 10:00:00 local (2026-05-04T08:00:00Z UTC)",
		"Running at: 2026-05-04 15:19:00 local (2026-05-04T13:19:00Z UTC)",
		"Schedule kind: daily daily=10:00 weekdays=mon,tue,wed,thu,fri",
		"Run delay: 5h19m0s",
		"Treat current-date research as of Running at",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("schedule context missing %q:\n%s", want, got)
		}
	}
}

func TestAgentJobNotificationCarriesTaskNameAndMarkdownBody(t *testing.T) {
	// The notification is plain markdown; telegramify-markdown-go renders
	// it into Telegram entities at send time (entity_markdown.go). This
	// test only asserts the message content reaches the renderer intact —
	// the entity transform is covered separately.
	msg := agentJobNotificationMessage(&cron.Task{Name: "daily-brief"}, cron.AgentJobPayload{}, "## Report\n- **Done**")
	for _, want := range []string{`Agent job "daily-brief" completed.`, "## Report", "- **Done**"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("notification missing %q:\n%s", want, msg)
		}
	}
}

func TestAgentJobPromptUsesPayloadLanguage(t *testing.T) {
	got := agentJobSystemPrompt(cron.AgentJobPayload{
		WritePolicy: cron.AgentJobWritePolicyProposeOnly,
		Language:    "it",
	}, time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC), time.UTC)
	if !strings.Contains(got, "Output language: Italian") {
		t.Fatalf("prompt missing Italian language instruction:\n%s", got)
	}
}

func TestAgentJobNotificationLocalizesPrefix(t *testing.T) {
	msg := agentJobNotificationMessage(&cron.Task{Name: "daily-brief"}, cron.AgentJobPayload{Language: "it"}, "Fatto")
	if !strings.HasPrefix(msg, "Job agente \"daily-brief\" completato.") {
		t.Fatalf("unexpected notification prefix: %q", msg)
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

func newTelegramTestSchedulerStore(t *testing.T) *cron.Store {
	t.Helper()
	pool, err := auradb.Open(filepath.Join(t.TempDir(), "sched.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	if err := migrations.Run(context.Background(), pool); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	store, err := cron.NewStoreWithDB(pool)
	if err != nil {
		t.Fatalf("new scheduler store: %v", err)
	}
	return store
}
