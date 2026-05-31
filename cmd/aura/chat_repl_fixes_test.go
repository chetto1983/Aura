package main

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

// pendingClarification builds a clarification-kind Pending for the prompt tests.
func pendingClarification(toolCallID, question string) askuser.Pending {
	return askuser.Pending{Token: toolCallID, ToolCallID: toolCallID, Kind: tools.KindClarification, Question: question}
}

// blockReader wraps a strings.Reader so the whole scripted stdin is delivered in ONE
// Read call (the worst case for the WR-01 double-bufio bug: the first bufio.Reader
// buffers EVERYTHING, so a second reader over the same stream sees only EOF). A
// strings.Reader already returns all remaining bytes per Read, but this makes the
// "buffered ahead" property explicit and pipe-like for the regression.
type blockReader struct{ r io.Reader }

func (b *blockReader) Read(p []byte) (int, error) { return b.r.Read(p) }

// TestChat_PauseAnswerConsumedFromBufferedStdin_WR01 is the regression for WR-01: a
// scripted pause answer buffered behind the user line MUST be consumed and injected.
// Pre-fix, promptForPause built a SECOND bufio.Reader over the same stdin, which saw
// EOF (the chatLoop reader had already buffered "Rome") and submitted an EMPTY answer.
// The assertion is on the injected tool answer CONTENT, not just that turn 2 ran.
func TestChat_PauseAnswerConsumedFromBufferedStdin_WR01(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(agenttest.MakeToolCall("call-1", "ask_user", `{"question":"What city?","kind":"clarification"}`)),
		textResponseTurn("call-2", "Rome it is.", llm.Usage{PromptTokens: 50, CompletionTokens: 10}),
	)
	// stdin: the chat message, then the inline pause answer "Roma" — both available in
	// the stream BEFORE the pause prompt is rendered (the buffered-ahead case).
	d, _, _ := testChatDeps(t, "Where should I book?\nRoma\n", fc)
	d.in = &blockReader{r: strings.NewReader("Where should I book?\nRoma\n")}

	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	if fc.CallCount() < 2 {
		t.Fatalf("want at least 2 LLM calls (pause + resume), got %d", fc.CallCount())
	}
	// The resume request must carry the scripted answer "Roma" as the injected
	// RoleTool answer — proof the buffered pause answer was actually consumed.
	resume := fc.Requests[1]
	sawAnswer := false
	for _, m := range resume.Messages {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "Roma") {
			sawAnswer = true
		}
	}
	if !sawAnswer {
		t.Fatalf("WR-01: the buffered scripted answer 'Roma' was not consumed/injected; resume messages=%+v", resume.Messages)
	}
}

// TestChat_TwoPauses_BothAnswersConsumed_WR01 stresses WR-01 across a 2-pause round:
// two answers buffered behind the user line must BOTH be read from the single shared
// reader, in order.
func TestChat_TwoPauses_BothAnswersConsumed_WR01(t *testing.T) {
	fc := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			agenttest.MakeToolCall("call-1", "ask_user", `{"question":"First?","kind":"clarification"}`),
			agenttest.MakeToolCall("call-2", "ask_user", `{"question":"Second?","kind":"clarification"}`),
		),
		textResponseTurn("call-3", "Got both.", llm.Usage{PromptTokens: 60, CompletionTokens: 12}),
	)
	d, _, _ := testChatDeps(t, "", fc)
	d.in = &blockReader{r: strings.NewReader("go\nalpha\nbeta\n")}

	if err := chatLoop(context.Background(), d); err != nil {
		t.Fatalf("chatLoop: %v", err)
	}
	// Search ALL recorded requests for the injected tool answers — the auto-title
	// worker (now reached because chatLoop drains the iterator) appends a trailing
	// title request, so the resume request is no longer guaranteed to be last.
	sawAlpha, sawBeta := false, false
	for _, req := range fc.Requests {
		for _, m := range req.Messages {
			if m.Role == llm.RoleTool && strings.Contains(m.Content, "alpha") {
				sawAlpha = true
			}
			if m.Role == llm.RoleTool && strings.Contains(m.Content, "beta") {
				sawBeta = true
			}
		}
	}
	if !sawAlpha || !sawBeta {
		t.Fatalf("WR-01: both buffered answers must be consumed (alpha=%v beta=%v); requests=%+v", sawAlpha, sawBeta, fc.Requests)
	}
}

// TestPromptForPause_SharedReader_WR01 is a focused unit assertion that promptForPause
// reads from the SHARED reader (not a fresh one over d.in): two sequential prompts
// over the same reader read consecutive lines.
func TestPromptForPause_SharedReader_WR01(t *testing.T) {
	d, _, _ := testChatDeps(t, "", agenttest.NewFakeClient())
	reader := bufio.NewReader(strings.NewReader("first answer\nsecond answer\n"))

	p := pendingClarification("call-1", "Q1?")
	r1 := promptForPause(d, p, reader)
	if r1.Content != "first answer" {
		t.Fatalf("first prompt read %q, want 'first answer'", r1.Content)
	}
	r2 := promptForPause(d, pendingClarification("call-2", "Q2?"), reader)
	if r2.Content != "second answer" {
		t.Fatalf("second prompt over the SAME reader read %q, want 'second answer'", r2.Content)
	}
}
