package telegram

import (
	"context"
	"io"
	"log/slog"
	"testing"

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
