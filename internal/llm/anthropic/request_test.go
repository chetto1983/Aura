package anthropic

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// send streams req through a fixture and returns the request body the server decoded.
func send(t *testing.T, cfg llm.Config, req llm.Request) map[string]any {
	t.Helper()
	return sendWithHeader(t, cfg, req).body
}

func sendWithHeader(t *testing.T, cfg llm.Config, req llm.Request) captured {
	t.Helper()
	client, sent := recordingClient(t, cfg, textReply("ok"))
	drain(t, client, req)
	return <-sent
}

func object(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("not an object: %#v", v)
	}
	return m
}

func tool(name, description, schema string) llm.ToolDef {
	def := llm.ToolDef{Type: "function"}
	def.Function.Name, def.Function.Description = name, description
	if schema != "" {
		def.Function.Parameters = json.RawMessage(schema)
	}
	return def
}

func toolCall(id, name, args string) llm.ToolCall {
	call := llm.ToolCall{ID: id, Type: "function"}
	call.Function.Name, call.Function.Arguments = name, args
	return call
}

func TestRequestNeverCarriesSampling(t *testing.T) {
	topP, topK, minP, penalty := 0.9, 40, 0.05, 1.1
	req := userRequest("hi")
	req.Temperature = 0.7
	req.Sampling = llm.Sampling{TopP: &topP, TopK: &topK, MinP: &minP, PresencePenalty: &penalty, RepetitionPenalty: &penalty}
	req.Reasoning = llm.ReasoningConfig{Effort: llm.ReasoningEffortNone}
	body := send(t, testConfig(), req)
	for _, key := range []string{"temperature", "top_p", "top_k", "min_p", "presence_penalty", "frequency_penalty", "repetition_penalty", "cache_control", "tool_choice"} {
		if _, ok := body[key]; ok {
			t.Errorf("request carries %s: %v", key, body[key])
		}
	}
	if thinking := object(t, body["thinking"]); thinking["type"] != "adaptive" {
		t.Fatalf("thinking %#v", thinking)
	}
	if body["stream"] != true || body["model"] != "claude-sonnet-5-5" {
		t.Fatalf("body %#v", body)
	}
}

func TestBetaBindingIsAlwaysSent(t *testing.T) {
	for _, show := range []bool{true, false} {
		cfg := testConfig()
		cfg.ShowReasoning = show
		sent := sendWithHeader(t, cfg, userRequest("hi"))
		if beta := sent.header.Get("anthropic-beta"); !strings.Contains(beta, "thinking-binding-controls-2026-08-01") {
			t.Fatalf("anthropic-beta %q", beta)
		}
		binding := object(t, object(t, sent.body["thinking"])["block_binding"])
		if binding["prefix_mismatch_behavior"] != "drop_block" {
			t.Fatalf("block_binding %#v", binding)
		}
	}
}

func TestDisplayFollowsShowReasoning(t *testing.T) {
	for show, want := range map[bool]string{true: "summarized", false: "omitted"} {
		cfg := testConfig()
		cfg.ShowReasoning = show
		if display := object(t, send(t, cfg, userRequest("hi"))["thinking"])["display"]; display != want {
			t.Fatalf("ShowReasoning=%v sent display %v", show, display)
		}
	}
}

func TestEffortMapping(t *testing.T) {
	all := []llm.ReasoningEffort{"low", "medium", "high", "xhigh", "max"}
	for _, tc := range []struct {
		name      string
		effort    llm.ReasoningEffort
		supported []llm.ReasoningEffort
		want      string
	}{
		{"empty sends none", "", all, ""},
		{"high passes", "high", all, "high"},
		{"none takes the lowest by rank", "none", []llm.ReasoningEffort{"max", "high", "medium"}, "medium"},
		{"minimal takes the lowest", "minimal", all, "low"},
		{"none on an empty list", "none", nil, "low"},
		{"minimal on an empty list", "minimal", nil, "low"},
		{"unlisted clamps to the nearest", "xhigh", []llm.ReasoningEffort{"max", "high", "low"}, "high"},
		{"max passes on an empty list", "max", nil, "max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testConfig()
			cfg.SupportedReasoningEfforts = tc.supported
			req := userRequest("hi")
			req.Reasoning.Effort = tc.effort
			body := send(t, cfg, req)
			config, present := body["output_config"]
			if tc.want == "" {
				if present {
					t.Fatalf("output_config sent: %v", config)
				}
				return
			}
			if got := object(t, config)["effort"]; got != tc.want {
				t.Fatalf("effort %v, want %s", got, tc.want)
			}
		})
	}
	req := userRequest("hi")
	req.Reasoning.Effort = "turbo"
	client := fixtureClient(t, testConfig(), nil)
	if _, err := client.Stream(context.Background(), req); err == nil || !strings.Contains(err.Error(), "turbo") {
		t.Fatalf("unknown effort accepted: %v", err)
	}
}

func TestToolChoice(t *testing.T) {
	tools := []llm.ToolDef{tool("lookup", "Look up.", `{"type":"object","properties":{"q":{"type":"string"}}}`)}
	for _, tc := range []struct {
		name, choice string
		tools        []llm.ToolDef
		wantTools    bool
		wantChoice   any
	}{
		{"none keeps the tools", "none", tools, true, map[string]any{"type": "none"}},
		{"auto sends no choice", "", tools, true, nil},
		{"explicit auto sends no choice", "auto", tools, true, nil},
		{"none without tools", "none", nil, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := userRequest("hi")
			req.Tools, req.ToolChoice = tc.tools, tc.choice
			body := send(t, testConfig(), req)
			if _, ok := body["tools"]; ok != tc.wantTools {
				t.Fatalf("tools present %v", ok)
			}
			if !reflect.DeepEqual(body["tool_choice"], tc.wantChoice) {
				t.Fatalf("tool_choice %#v", body["tool_choice"])
			}
		})
	}
	req := userRequest("hi")
	req.Tools, req.ToolChoice = tools, "required"
	if _, err := fixtureClient(t, testConfig(), nil).Stream(context.Background(), req); err == nil {
		t.Fatal("a forced tool choice was accepted")
	}
}

func TestMaxTokensFloor(t *testing.T) {
	for _, tc := range []struct {
		request, configured, ceiling int
		want                         float64
	}{
		{0, 32_768, 64_000, 32_768},
		{32, 32_768, 64_000, 32_768},
		{4096, 32_768, 64_000, 32_768},
		{50_000, 32_768, 64_000, 50_000},
		{100_000, 32_768, 64_000, 64_000},
		{0, 4096, 64_000, 16_384},
		{0, 32_768, 20_000, 20_000},
		{0, 0, 0, 16_384},
	} {
		cfg := testConfig()
		cfg.MaxTokens, cfg.MaxOutputTokens = tc.configured, tc.ceiling
		req := userRequest("hi")
		req.MaxTokens = tc.request
		if got := send(t, cfg, req)["max_tokens"]; got != tc.want {
			t.Fatalf("%+v: max_tokens %v", tc, got)
		}
	}
}

func TestTopLevelCombinatorSchemaIsFlattened(t *testing.T) {
	combinator := `{"anyOf":[{"type":"object","properties":{"a":{"type":"string"}}},{"type":"object","properties":{"b":{"type":"integer"}}}]}`
	req := userRequest("hi")
	req.Tools = []llm.ToolDef{
		tool("either", "Pick one.", combinator),
		tool("plain", "Plain.", `{"type":"object","properties":{"q":{"type":"string"}},"required":["q"],"additionalProperties":false}`),
		tool("bare", "No schema.", ""),
		tool("nulled", "", "null"),
		tool("scalar", "", `{"type":"string"}`),
	}
	tools := send(t, testConfig(), req)["tools"].([]any)
	for _, i := range []int{3, 4} {
		if schema := object(t, tools[i])["input_schema"]; !reflect.DeepEqual(schema, map[string]any{"type": "object"}) {
			t.Fatalf("tool %d schema %#v", i, schema)
		}
	}
	if description := object(t, tools[4])["description"]; description != `Input JSON schema: {"type":"string"}` {
		t.Fatalf("scalar description %#v", description)
	}
	either := object(t, tools[0])
	if !reflect.DeepEqual(either["input_schema"], map[string]any{"type": "object"}) {
		t.Fatalf("flattened schema %#v", either["input_schema"])
	}
	description := either["description"].(string)
	if !strings.HasPrefix(description, "Pick one.") || !strings.Contains(description, `"anyOf"`) || !strings.Contains(description, `"b"`) {
		t.Fatalf("description lost the schema: %q", description)
	}
	plain := object(t, object(t, tools[1])["input_schema"])
	if plain["type"] != "object" || plain["additionalProperties"] != false || !reflect.DeepEqual(plain["required"], []any{"q"}) || object(t, plain["properties"])["q"] == nil {
		t.Fatalf("plain schema %#v", plain)
	}
	if object(t, tools[1])["description"] != "Plain." {
		t.Fatalf("plain description %#v", tools[1])
	}
	if bare := object(t, object(t, tools[2])["input_schema"]); bare["type"] != "object" {
		t.Fatalf("bare schema %#v", bare)
	}
	for _, broken := range []string{`{"type":`, `[1]`, `{} {}`} {
		req.Tools = []llm.ToolDef{tool("broken", "", broken)}
		if _, err := fixtureClient(t, testConfig(), nil).Stream(context.Background(), req); err == nil {
			t.Fatalf("invalid schema %s was accepted", broken)
		}
	}
}

func TestDuplicateToolNamesKeepTheFirst(t *testing.T) {
	req := userRequest("hi")
	req.Tools = []llm.ToolDef{tool("lookup", "First.", ""), tool("other", "Other.", ""), tool("lookup", "Second.", "")}
	tools := send(t, testConfig(), req)["tools"].([]any)
	if len(tools) != 2 || object(t, tools[0])["description"] != "First." || object(t, tools[1])["name"] != "other" {
		t.Fatalf("tools %#v", tools)
	}
}
