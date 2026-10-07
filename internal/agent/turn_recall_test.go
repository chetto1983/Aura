package agent

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestTurnContextKeySeparatesDifferentHistories(t *testing.T) {
	codeAsk := []llm.Message{{Role: llm.RoleUser, Content: "rewrite this function in Go with tests"}, {Role: llm.RoleAssistant, Content: "Here is the plan."}}
	thanks := []llm.Message{{Role: llm.RoleUser, Content: "thanks for the help"}, {Role: llm.RoleAssistant, Content: "You're welcome."}}
	if TurnContextKey(codeAsk, "") == TurnContextKey(thanks, "") {
		t.Fatal("two different histories share a context key")
	}
	if TurnContextKey(nil, "") != TurnContextKey([]llm.Message{}, "") {
		t.Fatal("a known empty history has more than one key")
	}
	if TurnContextKey(nil, "catalog: a.pdf\n") == TurnContextKey(nil, "") {
		t.Fatal("the current message's context blocks are not in the key")
	}
	if !strings.HasPrefix(TurnContextKey(nil, ""), "ctx1:") {
		t.Fatal("the key does not name its format")
	}
}

// Tool-call ids correlate a call with its result and differ between conversations that
// did the same thing; the arguments and the result are the task.
func TestTurnContextKeyIgnoresToolCallIDsButNotArguments(t *testing.T) {
	history := func(id, args string) []llm.Message {
		return []llm.Message{
			{Role: llm.RoleUser, Content: "che tempo fa a Cuneo?"},
			{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call(id, "web_search", args)}},
			{Role: llm.RoleTool, ToolCallID: id, Content: "Sereno, 21 gradi."},
			{Role: llm.RoleAssistant, Content: "Sereno."},
		}
	}
	if TurnContextKey(history("call-a", `{"q":"meteo Cuneo"}`), "") != TurnContextKey(history("call-b", `{"q":"meteo Cuneo"}`), "") {
		t.Fatal("tool-call ids changed the key")
	}
	if TurnContextKey(history("call-a", `{"q":"meteo Cuneo"}`), "") == TurnContextKey(history("call-a", `{"q":"meteo Bra"}`), "") {
		t.Fatal("different tool arguments share a key")
	}
}

func TestRouteKeyNamesTheRouteWithoutItsCredentials(t *testing.T) {
	base := llm.Config{
		Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1", Model: "z-ai/glm-5.3-flash",
		SupportedReasoningEfforts: []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortHigh},
		APIKey:                    "sk-secret-value",
	}
	key := routeKey(base)
	if strings.Contains(key, "sk-secret-value") || !strings.HasPrefix(key, "route1:") {
		t.Fatalf("route key %q leaks the credential or misses its format", key)
	}
	same := []func(*llm.Config){
		func(c *llm.Config) { c.APIKey = "sk-another" },
		func(c *llm.Config) { c.Headers = map[string]string{"X-Title": "Aura"} },
		func(c *llm.Config) { c.BaseURL = "https://user:pass@openrouter.ai/api/v1/?key=abc" },
		func(c *llm.Config) { c.Temperature = 0.9 },
	}
	for index, edit := range same {
		cfg := base
		edit(&cfg)
		if routeKey(cfg) != key {
			t.Errorf("credential or sampling change %d moved the route key", index)
		}
	}
	different := []func(*llm.Config){
		func(c *llm.Config) { c.Model = "google/gemini-3.8-flash" },
		func(c *llm.Config) { c.BaseURL = "http://127.0.0.1:11434/v1" },
		func(c *llm.Config) { c.Provider = "ollama" },
		func(c *llm.Config) { c.SupportedReasoningEfforts = []llm.ReasoningEffort{llm.ReasoningEffortHigh} },
		func(c *llm.Config) { c.ReasoningMandatory = true },
	}
	for index, edit := range different {
		cfg := base
		edit(&cfg)
		if routeKey(cfg) == key {
			t.Errorf("route change %d kept the route key", index)
		}
	}
}

func TestTurnPolicyVersionFollowsTheSeedPolicy(t *testing.T) {
	if turnPolicyVersion != policyVersion() || !strings.HasPrefix(turnPolicyVersion, "policy1:") {
		t.Fatalf("turnPolicyVersion = %q", turnPolicyVersion)
	}
}
