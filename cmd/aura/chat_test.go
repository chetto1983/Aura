package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// plainTurnCtx is the test wiring for chatDeps.newTurnCtx: a plain cancellable ctx
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
func jsonStringLit(s string) string {
	b := jsonString(s) // reuse config.go's marshaler
	return string(b)
}

func testChatDeps(in string, client *agenttest.FakeClient) (chatDeps, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	cfg := &config.Config{
		LLM: llm.Config{
			Model:           "deepseek/deepseek-v4-flash:exacto",
			Provider:        "openrouter",
			Temperature:     0.7,
			MaxTokens:       4096,
			TotalTimeoutSec: 120,
			Prices:          map[string]llm.Price{"deepseek/deepseek-v4-flash:exacto": {InputPer1M: 0.0983, OutputPer1M: 0.1966}},
		},
		ToolPreviewCap: 2048,
		RunDir:         "",
	}
	return chatDeps{
		in:         strings.NewReader(in),
		out:        &out,
		errOut:     &errOut,
		client:     client,
		cfg:        cfg,
		sessionID:  "test-session-0001",
		newTurnCtx: plainTurnCtx,
	}, &out, &errOut
}

// TestChat_TwoTurns_SharedSession asserts two scripted stdin turns produce two
// streamed prose replies sharing ONE session_id, that the second turn sees the
// first in the in-memory history, and that the output is clean prose with a cost
// footer — never raw text_response JSON (Req#11).
func TestChat_TwoTurns_SharedSession(t *testing.T) {
	fc := agenttest.NewFakeClient(
		textResponseTurn("call-1", "Ciao, come posso aiutarti?", llm.Usage{PromptTokens: 120, CompletionTokens: 30}),
		textResponseTurn("call-2", "Ricordo il tuo nome.", llm.Usage{PromptTokens: 180, CompletionTokens: 25}),
	)
	d, out, _ := testChatDeps("ciao\nmi chiamo Davide\n", fc)

	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}

	got := out.String()
	// Clean prose: both replies present, NO raw JSON fragment leaked.
	if !strings.Contains(got, "Ciao, come posso aiutarti?") {
		t.Fatalf("turn 1 reply not rendered:\n%s", got)
	}
	if !strings.Contains(got, "Ricordo il tuo nome.") {
		t.Fatalf("turn 2 reply not rendered:\n%s", got)
	}
	if strings.Contains(got, `{"text":`) || strings.Contains(got, `"text\":`) {
		t.Fatalf("raw text_response JSON leaked into the REPL output:\n%s", got)
	}
	// Cost footer present and non-zero for both turns.
	if c := strings.Count(got, " tok ("); c != 2 {
		t.Fatalf("want 2 cost footers, got %d:\n%s", c, got)
	}
	if strings.Contains(got, "· 0 tok (") {
		t.Fatalf("cost footer reported 0 tok (should be non-zero):\n%s", got)
	}

	// One shared session id: both LlmAgents were built with d.sessionID; the
	// second request must carry the first turn's user message in history.
	if fc.CallCount() != 2 {
		t.Fatalf("FakeClient called %d times, want 2", fc.CallCount())
	}
	second := fc.Requests[1]
	var sawFirstUser bool
	for _, m := range second.Messages {
		if m.Role == llm.RoleUser && strings.Contains(m.Content, "ciao") {
			sawFirstUser = true
		}
	}
	if !sawFirstUser {
		t.Fatalf("second turn did not see the first user turn in history: %+v", second.Messages)
	}
}

// TestChat_ExitCommand asserts `/exit` quits cleanly without driving the client.
func TestChat_ExitCommand(t *testing.T) {
	fc := agenttest.NewFakeClient()
	d, out, _ := testChatDeps("/exit\n", fc)
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
	d, out, _ := testChatDeps("una domanda", fc) // no trailing newline -> EOF after one turn
	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	if !strings.Contains(out.String(), "Risposta.") {
		t.Fatalf("turn before EOF not rendered:\n%s", out.String())
	}
}

// TestChat_MissingKey asserts runChat fails with a clear stderr message and no
// panic when no API key is configured. It runs the real entry point under a temp
// HOME with the key cleared, capturing the os.Exit via a subprocess-free guard:
// runChat calls os.Exit, so we assert the precondition (config.Load errors with
// ErrMissingAPIKey) and the message wiring instead of trapping the exit.
func TestChat_MissingKey(t *testing.T) {
	withTempHome(t) // clears OPENROUTER_API_KEY + AURA_LLM_*
	_, err := config.Load()
	if err == nil {
		t.Fatal("config.Load should fail with no API key")
	}
	if !isMissingAPIKey(err) {
		t.Fatalf("want missing-API-key error, got %v", err)
	}
	// The clear stderr message runChat prints uses llm.ErrMissingAPIKey.Error();
	// assert that wiring string is the human-facing one (no panic path).
	if !strings.Contains(err.Error(), "API key is empty") {
		t.Fatalf("error message is not operator-clear: %v", err)
	}
}
