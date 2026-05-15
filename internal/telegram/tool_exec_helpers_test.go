package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/conversation"
	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/llm"
)

func TestExecuteToolCallsRunsRegistryToolRegardlessOfAdvertisedAllowlist(t *testing.T) {
	// Registry is the single source of truth. The toolsExposed slice is kept
	// for snapshot/logging only — execution never gates on it. A registered
	// tool always runs, even when the model picked something outside the
	// curated prompt-side allowlist (e.g. recovered from prior-turn memory).
	reg := tools.NewRegistry(nil)
	target := &countingTelegramTool{name: "execute_code", result: "ran"}
	reg.Register(target)
	b := &Bot{
		cfg: &config.Config{},
		rt:  &botRuntime{tools: reg},
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"search_memory"}, // narrower than registry
		nil,
	)

	if target.calls != 1 {
		t.Fatalf("execute_code called %d times, want 1", target.calls)
	}
	if summary.LastResult != "ran" {
		t.Fatalf("lastResult = %q, want %q", summary.LastResult, "ran")
	}
}

func TestExecuteToolCallsRunsDocumentToolWithoutSkillGate(t *testing.T) {
	reg := tools.NewRegistry(nil)
	doc := &countingTelegramTool{name: "create_pdf", result: "pdf created"}
	reg.Register(doc)
	b := &Bot{
		cfg: &config.Config{},
		rt:  &botRuntime{tools: reg},
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "pdf-1", Name: "create_pdf"}},
		[]string{"create_pdf"},
		nil,
	)
	if doc.calls != 1 {
		t.Fatalf("protected tool calls = %d, want 1", doc.calls)
	}
	if summary.LastResult != "pdf created" {
		t.Fatalf("lastResult = %q", summary.LastResult)
	}
}

func TestExecuteToolCallsSameBatchSkillReadAndProtectedToolBothRun(t *testing.T) {
	reg := tools.NewRegistry(nil)
	read := &countingTelegramTool{name: "read_file", result: "skill body"}
	doc := &countingTelegramTool{name: "create_pdf", result: "pdf created"}
	reg.Register(read)
	reg.Register(doc)
	b := &Bot{
		cfg: &config.Config{},
		rt:  &botRuntime{tools: reg},
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{
			{ID: "skill-1", Name: "read_file", Arguments: map[string]any{"path": ".agents/skills/document-pdf/SKILL.md"}},
			{ID: "pdf-1", Name: "create_pdf"},
		},
		[]string{"read_file", "create_pdf"},
		nil,
	)

	if len(summary.ReadSkillNames) != 1 || summary.ReadSkillNames[0] != "document-pdf" {
		t.Fatalf("readSkillNames = %+v, want document-pdf", summary.ReadSkillNames)
	}
	if read.calls != 1 {
		t.Fatalf("read_file calls = %d, want 1", read.calls)
	}
	if doc.calls != 1 {
		t.Fatalf("create_pdf calls = %d, want 1", doc.calls)
	}
}

func TestExecuteToolCallsTracksTerminalTools(t *testing.T) {
	reg := tools.NewRegistry(nil)
	exec := &countingTelegramTool{name: "execute_code", result: "5050"}
	reg.Register(exec)
	b := &Bot{
		cfg: &config.Config{},
		rt:  &botRuntime{tools: reg},
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"execute_code"},
		[]string{"systematic-debugging"},
	)

	if summary.TerminalTool != "execute_code" {
		t.Fatalf("terminalTool = %q, want execute_code", summary.TerminalTool)
	}
	if exec.calls != 1 {
		t.Fatalf("execute_code calls = %d, want 1", exec.calls)
	}
}

func TestExecuteToolCallsHonorsTerminalToolPolicyOff(t *testing.T) {
	reg := tools.NewRegistry(nil)
	exec := &countingTelegramTool{name: "execute_code", result: "5050"}
	reg.Register(exec)
	b := &Bot{
		cfg: &config.Config{
			TerminalToolPolicy: "off",
		},
		rt: &botRuntime{tools: reg},
	}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "exec-1", Name: "execute_code"}},
		[]string{"execute_code"},
		[]string{"systematic-debugging"},
	)

	if summary.TerminalTool != "" {
		t.Fatalf("terminalTool = %q, want disabled terminal policy", summary.TerminalTool)
	}
	if exec.calls != 1 {
		t.Fatalf("execute_code calls = %d, want 1", exec.calls)
	}
}

func TestMaxToolLoopIterationsHonorsRuntimeConfigInsteadOfToolsetHardCap(t *testing.T) {
	b := &Bot{cfg: &config.Config{AgentLoopMaxSteps: 8}}

	if got := b.maxToolLoopIterations(); got != 8 {
		t.Fatalf("maxToolLoopIterations = %d, want runtime cap 8", got)
	}
}

func TestSearchMemoryArgumentsForceCallerChatID(t *testing.T) {
	reg := tools.NewRegistry(nil)
	search := &countingTelegramTool{name: "search_memory", result: "memory"}
	reg.Register(search)
	b := &Bot{cfg: &config.Config{}, rt: &botRuntime{tools: reg}}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "search-1", Name: "search_memory", Arguments: map[string]any{"query": "documents", "limit": float64(9)}}},
		[]string{"search_memory"},
		nil,
	)

	if !strings.Contains(summary.LastResult, "memory") {
		t.Fatalf("lastResult = %q", summary.LastResult)
	}
	args := agent.ToolArgumentsForTool(
		"search_memory",
		map[string]any{"query": "private notes", "chat_id": float64(999), "limit": float64(9)},
		42,
	)
	if got := args["chat_id"]; got != float64(42) {
		t.Fatalf("chat_id = %#v, want 42", got)
	}
	if got := args["limit"]; got != float64(9) {
		t.Fatalf("limit = %#v, want model-provided limit preserved", got)
	}
}

func TestFailedTerminalToolDoesNotStopTurn(t *testing.T) {
	reg := tools.NewRegistry(nil)
	reg.Register(&errorTelegramTool{name: "execute_shell"})
	b := &Bot{cfg: &config.Config{TerminalToolPolicy: "on"}, rt: &botRuntime{tools: reg}, logger: slog.Default()}
	convCtx := conversation.NewContext(conversation.Config{})

	summary := b.executeToolCalls(context.Background(), nil, convCtx, "1148481707",
		[]llm.ToolCall{{ID: "shell-1", Name: "execute_shell", Arguments: map[string]any{"command": "find /home/user"}}},
		[]string{"execute_shell"},
		nil,
	)

	if summary.TerminalTool != "" {
		t.Fatalf("terminalTool = %q, want empty for failed terminal tool", summary.TerminalTool)
	}
	if got := convCtx.Messages()[len(convCtx.Messages())-1].Content; !strings.HasPrefix(got, "Error: ") {
		t.Fatalf("tool result = %q, want plain Error: prefix for model recovery", got)
	}
}

func TestTerminalToolFinalizationMessagesAppendsLLMPrompt(t *testing.T) {
	messages := []llm.Message{
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "file-1", Name: "write_file"}}},
		{Role: "tool", ToolCallID: "file-1", Content: `{"ok":true}`},
	}

	got := terminalToolFinalizationMessages(messages, "write_file")
	if len(got) != len(messages)+1 {
		t.Fatalf("messages len = %d, want %d", len(got), len(messages)+1)
	}
	last := got[len(got)-1].Content
	for _, want := range []string{"Do not call tools", "natural prose"} {
		if !strings.Contains(last, want) {
			t.Fatalf("terminal finalization instruction = %q, missing %q", last, want)
		}
	}
}

func TestLooksLikeToolCallMarkupRecognisesDSML(t *testing.T) {
	raw := `<｜｜DSML｜｜tool_calls><｜｜DSML｜｜invoke name="write_file">`
	if !looksLikeToolCallMarkup(raw) {
		t.Fatal("looksLikeToolCallMarkup = false, want true")
	}
}

type countingTelegramTool struct {
	name     string
	result   string
	calls    int
	lastArgs map[string]any
}

func (t *countingTelegramTool) Name() string { return t.name }

func (t *countingTelegramTool) Description() string { return "counting fake tool" }

func (t *countingTelegramTool) Parameters() map[string]any { return map[string]any{"type": "object"} }

func (t *countingTelegramTool) Execute(_ context.Context, args map[string]any) (string, error) {
	t.calls++
	t.lastArgs = args
	return t.result, nil
}

type errorTelegramTool struct {
	name string
}

func (t *errorTelegramTool) Name() string { return t.name }

func (t *errorTelegramTool) Description() string { return "error fake tool" }

func (t *errorTelegramTool) Parameters() map[string]any { return map[string]any{"type": "object"} }

func (t *errorTelegramTool) Execute(context.Context, map[string]any) (string, error) {
	return "", fmt.Errorf("shell command failed (exit=1): find: '/home/user': No such file or directory")
}
