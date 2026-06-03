package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

// plainTurnCtx is the test wiring for replDeps.newTurnCtx: a plain cancellable ctx
// with no OS signal handler, so the scripted-stdin tests never register a SIGINT
// notifier (keeping the run goleak-clean and deterministic).
func plainTurnCtx(parent context.Context) (context.Context, context.CancelFunc, func() bool) {
	ctx, cancel := context.WithCancel(parent)
	return ctx, cancel, func() bool { return ctx.Err() != nil }
}

// textResponseTurn scripts one FakeClient turn that calls text_response with the
// given prose and a trailing usage chunk (so the cost footer renders non-zero).
func textResponseTurn(id, prose string, u llm.Usage) agenttest.FakeTurn {
	args := `{"text":` + jsonStringLit(prose) + `}`
	return agenttest.WithUsage(
		agenttest.ToolCallTurn(agenttest.MakeToolCall(id, "text_response", args)),
		u,
	)
}

// jsonStringLit renders s as a JSON string literal for embedding in tool args.
func jsonStringLit(s string) string { return string(jsonString(s)) }

// testChatDeps builds a Runner over the in-memory fakes (cmdfakes_test.go) + a
// scripted FakeClient and a fresh conversation, returning the REPL deps + output
// buffers. No DB, no network.
func testChatDeps(t *testing.T, in string, client *agenttest.FakeClient) (replDeps, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	cfg := &config.Config{
		LLM: llm.Config{
			Model:           "deepseek/deepseek-v4-flash:exacto",
			Provider:        "openrouter",
			Temperature:     0.7,
			MaxTokens:       4096,
			TotalTimeoutSec: 120,
			ContextWindow:   1000000,
			MaxOutputTokens: 32768,
			Prices:          map[string]llm.Price{"deepseek/deepseek-v4-flash:exacto": {InputPer1M: 0.0983, OutputPer1M: 0.1966}},
		},
		ToolPreviewCap: 2048,
	}
	reg := tools.NewRegistry()
	reg.Register(tools.TextResponse{})
	reg.Register(tools.AskUser{})
	reg.Register(&tools.ReadToolOutput{})

	conv := newCmdConvFake()
	run := runner.New(runner.Deps{
		Conv:         conv,
		Pause:        newCmdPauseFake(),
		Identity:     newCmdIdentityFake(),
		CacheMetrics: newCmdCacheMetricFake(),
		Client:       client,
		Registry:     reg,
		LLM:          cfg.LLM,
		TitleTimeout: time.Second,
		StopTimeout:  time.Second,
	})
	convID, err := run.NewConversation(context.Background())
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	return replDeps{
		in:         strings.NewReader(in),
		out:        &out,
		errOut:     &errOut,
		run:        run,
		cfg:        cfg,
		convID:     convID,
		newTurnCtx: plainTurnCtx,
	}, &out, &errOut
}

// TestChat_TwoTurns asserts two scripted stdin turns produce two streamed prose
// replies persisted across the same conversation, with clean prose (no raw
// text_response JSON) + a cost footer per turn (Req#11).
func TestChat_TwoTurns(t *testing.T) {
	fc := agenttest.NewFakeClient(
		textResponseTurn("call-1", "Ciao, come posso aiutarti?", llm.Usage{PromptTokens: 120, CompletionTokens: 30}),
		textResponseTurn("call-2", "Ricordo il tuo nome.", llm.Usage{PromptTokens: 180, CompletionTokens: 25}),
	)
	d, out, _ := testChatDeps(t, "ciao\nmi chiamo Davide\n", fc)

	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "Ciao, come posso aiutarti?") || !strings.Contains(got, "Ricordo il tuo nome.") {
		t.Fatalf("both replies not rendered:\n%s", got)
	}
	if strings.Contains(got, `{"text":`) {
		t.Fatalf("raw text_response JSON leaked into the REPL output:\n%s", got)
	}
	if c := strings.Count(got, " tok ("); c != 2 {
		t.Fatalf("want 2 cost footers, got %d:\n%s", c, got)
	}
	// 2 user turns + 1 best-effort auto-title call: after turn 2 CountTurns crosses
	// autoTitleMinSeq (3), so the auto-title worker fires exactly once and is joined
	// by chatLoop's Stop before this assertion. (Previously this read 2 only because
	// the title worker never fired — the iterator was abandoned on the final Event.)
	if fc.CallCount() != 3 {
		t.Fatalf("FakeClient called %d times, want 3 (2 turns + 1 auto-title)", fc.CallCount())
	}
	// The second turn must see the first user message in the rehydrated history.
	second := fc.Requests[1]
	sawFirstUser := false
	for _, m := range second.Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "ciao") {
			sawFirstUser = true
		}
	}
	if !sawFirstUser {
		t.Fatalf("second turn did not see the first user turn in history: %+v", second.Messages)
	}
}

// TestChat_AskUserPauseResumesInline is the plan's scripted-stdin pause test: a
// single ask_user(clarification) pause renders inline, the scripted answer resumes
// the loop, and the conversation completes — no live OpenRouter.
func TestChat_AskUserPauseResumesInline(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "ask_user", `{"question":"What city?","kind":"clarification"}`)),
		textResponseTurn("call-2", "Rome it is.", llm.Usage{PromptTokens: 50, CompletionTokens: 10}),
	)
	// stdin: the chat message, then the inline pause answer.
	d, out, _ := testChatDeps(t, "Where should I book?\nRome\n", fc)

	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "What city?") {
		t.Fatalf("pause prompt not rendered inline:\n%s", got)
	}
	if !strings.Contains(got, "Rome it is.") {
		t.Fatalf("resumed reply not rendered:\n%s", got)
	}
	// At least the pause turn + the resume turn (a 3rd best-effort auto-title call may
	// also fire in the background after the conversation reaches seq>=3).
	if fc.CallCount() < 2 {
		t.Fatalf("want at least 2 LLM calls (pause + resume), got %d", fc.CallCount())
	}
}

// TestRenderTurn_ToolResultNotRenderedAsProse is the regression for the live tool
// turn that double-printed the answer and leaked the raw tool output (now via
// renderRunnerTurn).
func TestRenderTurn_ToolResultNotRenderedAsProse(t *testing.T) {
	var toolCall llm.ToolCall
	toolCall.Function.Name = "current_time"

	seq := func(yield func(*agent.Event, error) bool) {
		if !yield(&agent.Event{LLMResponse: &agent.LLMResponse{ToolCalls: []llm.ToolCall{toolCall}}}, nil) {
			return
		}
		if !yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "2026-05-30T14:24:51Z"},
			Actions:     agent.Actions{StateDelta: map[string]any{"tool_call_id": "call-1"}},
		}, nil) {
			return
		}
		yield(&agent.Event{
			LLMResponse: &agent.LLMResponse{Content: "Adesso sono le 14:24 UTC.", FinishReason: "stop"},
			Actions:     agent.Actions{StateDelta: map[string]any{"prompt_tokens": 996, "completion_tokens": 62}},
		}, nil)
	}

	var buf bytes.Buffer
	answer, finish, usage, paused, err := renderRunnerTurn(&buf, seq)
	if err != nil {
		t.Fatalf("renderRunnerTurn: %v", err)
	}
	if paused {
		t.Fatal("no pause Event was emitted — paused must be false")
	}
	if finish != "stop" || answer != "Adesso sono le 14:24 UTC." {
		t.Fatalf("answer/finish wrong: %q / %q", answer, finish)
	}
	got := buf.String()
	if strings.Contains(got, "2026-05-30T14:24:51Z") {
		t.Fatalf("raw tool-result preview leaked into the REPL prose:\n%s", got)
	}
	if n := strings.Count(got, "Adesso sono le 14:24 UTC."); n != 1 {
		t.Fatalf("answer rendered %d times, want exactly 1:\n%s", n, got)
	}
	if !strings.Contains(got, "· current_time") {
		t.Fatalf("dim tool-activity line missing:\n%s", got)
	}
	if usage.PromptTokens != 996 || usage.CompletionTokens != 62 {
		t.Fatalf("usage = %+v, want 996/62", usage)
	}
}

// TestChat_ExitCommand asserts `/exit` quits cleanly without driving the client.
func TestChat_ExitCommand(t *testing.T) {
	fc := agenttest.NewFakeClient()
	d, out, _ := testChatDeps(t, "/exit\n", fc)
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	if fc.CallCount() != 0 {
		t.Fatalf("/exit should not call the client, got %d calls", fc.CallCount())
	}
	if !strings.Contains(out.String(), "bye") {
		t.Fatalf("/exit did not print a clean farewell:\n%s", out.String())
	}
}

// TestChat_EOFQuitsClean asserts EOF (no trailing newline) quits without a panic.
func TestChat_EOFQuitsClean(t *testing.T) {
	fc := agenttest.NewFakeClient(
		textResponseTurn("call-1", "Risposta.", llm.Usage{PromptTokens: 10, CompletionTokens: 5}),
	)
	d, out, _ := testChatDeps(t, "una domanda", fc)
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	if !strings.Contains(out.String(), "Risposta.") {
		t.Fatalf("turn before EOF not rendered:\n%s", out.String())
	}
}

// TestChat_MissingKey asserts the config precondition runChat checks before booting.
func TestChat_MissingKey(t *testing.T) {
	withTempHome(t)
	_, err := config.Load()
	if err == nil {
		t.Fatal("config.Load should fail with no API key")
	}
	if !isMissingAPIKey(err) {
		t.Fatalf("want missing-API-key error, got %v", err)
	}
	if !strings.Contains(err.Error(), "API key is empty") {
		t.Fatalf("error message is not operator-clear: %v", err)
	}
}

func TestBuildChatRegistry_RegistersSandboxExec(t *testing.T) {
	cfg := &config.Config{
		SandboxAgent: config.LoadDB().SandboxAgent,
	}
	reg := buildChatRegistry(cfg)
	if _, ok := reg.Get("sandbox_exec"); !ok {
		t.Fatal("chat registry is missing sandbox_exec backed by the local sandbox-agent container")
	}
}
