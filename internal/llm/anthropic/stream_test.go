package anthropic

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
)

func streamOf(t *testing.T, cfg llm.Config, events []sseEvent) []llm.Chunk {
	t.Helper()
	client := fixtureClient(t, cfg, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		writeEvents(w, events...)
	})
	return drain(t, client, userRequest("go"))
}

func TestStreamMapsTextThinkingToolsAndUsage(t *testing.T) {
	events := []sseEvent{messageStart(1000, 16002, 40, "[]")}
	events = append(events, thinkingBlock(0, "Weighing it.", "sig-1")...)
	events = append(events, textBlock(1, "Looking up.")...)
	events = append(events, toolBlock(2, "toolu_1", "lookup", `{"query":`, `"weather"}`)...)
	events = append(events, messageDelta("tool_use", 50), messageStop())
	chunks := streamOf(t, testConfig(), events)
	if len(chunks) != 6 {
		t.Fatalf("chunks %#v", chunks)
	}
	if chunks[0].Reasoning != "Weighing it." || chunks[1].Text != "Looking up." {
		t.Fatalf("deltas %#v %#v", chunks[0], chunks[1])
	}
	call := chunks[2].ToolCall
	if call == nil || call.ID != "toolu_1" || call.Type != "function" || call.Function.Name != "lookup" || call.Function.Arguments != `{"query":"weather"}` {
		t.Fatalf("tool call %#v", chunks[2])
	}
	var state []map[string]any
	if err := json.Unmarshal(chunks[3].ProviderState, &state); err != nil || len(state) != 3 {
		t.Fatalf("provider state %s: %v", chunks[3].ProviderState, err)
	}
	if state[0]["type"] != "thinking" || state[0]["signature"] != "sig-1" || state[1]["text"] != "Looking up." || state[2]["id"] != "toolu_1" {
		t.Fatalf("provider state %#v", state)
	}
	if chunks[4].FinishReason != "tool_calls" || chunks[4].Text != "" || chunks[4].ToolCall != nil {
		t.Fatalf("finish %#v", chunks[4])
	}
	usage := chunks[5].Usage
	if usage == nil || usage.PromptTokens != 1000+16002+40 || usage.CachedTokens != 16002 || usage.CompletionTokens != 50 || usage.Cost != nil {
		t.Fatalf("usage %#v", usage)
	}
	for i, chunk := range chunks[2:] {
		if populated := chunk.ToolCall != nil || chunk.ProviderState != nil || chunk.FinishReason != "" || chunk.Usage != nil; !populated {
			t.Fatalf("chunk %d after message_stop is empty", i+2)
		}
	}
}

func TestTextOnlyReplyCarriesNoState(t *testing.T) {
	chunks := streamOf(t, testConfig(), textReply("Hello"))
	if len(chunks) != 3 || chunks[0].Text != "Hello" || chunks[1].FinishReason != "stop" || chunks[2].Usage == nil {
		t.Fatalf("chunks %#v", chunks)
	}
}

func TestToolInputWithoutDeltasIsAnEmptyObject(t *testing.T) {
	events := append(reply(toolBlock(0, "toolu_1", "ping")), messageDelta("tool_use", 2), messageStop())
	chunks := streamOf(t, testConfig(), events)
	if call := chunks[0].ToolCall; call == nil || call.Function.Arguments != "{}" {
		t.Fatalf("tool call %#v", chunks[0])
	}
}

func TestStopReasonMapping(t *testing.T) {
	for reason, want := range map[string]string{
		"end_turn": "stop", "stop_sequence": "stop", "tool_use": "tool_calls",
		"max_tokens": "length", "model_context_window_exceeded": "length", "refusal": llm.FinishContentFilter,
	} {
		t.Run(reason, func(t *testing.T) {
			chunks := streamOf(t, testConfig(), append(reply(textBlock(0, "x")), messageDelta(reason, 2), messageStop()))
			if len(chunks) != 3 || chunks[1].FinishReason != want {
				t.Fatalf("%s mapped to %#v", reason, chunks)
			}
		})
	}
	for _, reason := range []string{"pause_turn", "compaction", "something_new"} {
		t.Run(reason, func(t *testing.T) {
			events := append(reply(toolBlock(0, "toolu_1", "lookup", `{}`)), messageDelta(reason, 2), messageStop())
			err := lastErr(t, streamOf(t, testConfig(), events))
			if !strings.Contains(err.Error(), reason) || agent.IsTransientLLMError(err) {
				t.Fatalf("%s: %v", reason, err)
			}
		})
	}
}

func TestRefusalMapsToContentFilter(t *testing.T) {
	events := append(reply(textBlock(0, "I can't help with that.")), messageDelta("refusal", 4), messageStop())
	chunks := streamOf(t, testConfig(), events)
	if chunks[0].Text == "" || chunks[1].FinishReason != llm.FinishContentFilter || chunks[1].Err != nil {
		t.Fatalf("refusal %#v", chunks)
	}
	for _, chunk := range chunks {
		if chunk.ToolCall != nil || chunk.ProviderState != nil {
			t.Fatalf("refusal carried a call or state: %#v", chunk)
		}
	}
}

func TestStreamClosedBeforeMessageStopIsRetryable(t *testing.T) {
	for name, events := range map[string][]sseEvent{
		"mid input_json_delta": append(reply(textBlock(0, "x")), blockStart(1, `{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}`), delta(1, "input_json_delta", "partial_json", `{"query":"we`)),
		"mid thinking":         append(reply(), blockStart(0, `{"type":"thinking","thinking":"","signature":""}`), delta(0, "thinking_delta", "thinking", "half a thou")),
		"after message_delta":  append(reply(textBlock(0, "done")), messageDelta("end_turn", 2)),
		"empty body":           nil,
	} {
		t.Run(name, func(t *testing.T) {
			err := lastErr(t, streamOf(t, testConfig(), events))
			if !errors.Is(err, ErrStreamIncomplete) || !agent.IsTransientLLMError(err) {
				t.Fatalf("truncated stream: %v", err)
			}
		})
	}
}

func TestMalformedEventEndsTheStream(t *testing.T) {
	chunks := streamOf(t, testConfig(), []sseEvent{messageStart(1, 0, 0, "[]"), {"content_block_delta", `{"type":"content_block_delta","index":7,"delta":{"type":"text_delta","text":"x"}}`}})
	if err := lastErr(t, chunks); err == nil {
		t.Fatal("an out-of-range block was accepted")
	}
	chunks = streamOf(t, testConfig(), []sseEvent{{"message_start", `{"type":`}})
	if err := lastErr(t, chunks); err == nil {
		t.Fatal("malformed JSON was accepted")
	}
}
