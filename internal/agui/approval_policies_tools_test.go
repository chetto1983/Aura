package agui

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// specTool is a registered tool reduced to its spec, which is all the policy form reads.
type specTool tools.Spec

func (s specTool) Spec() tools.Spec { return tools.Spec(s) }

func (specTool) Execute(context.Context, json.RawMessage) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

type registeredTools []tools.Tool

func (r registeredTools) All() []tools.Tool { return r }

func actionSchema(actions ...string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"type":       "object",
		"properties": map[string]any{"action": map[string]any{"type": "string", "enum": actions}},
	})
	return raw
}

func TestPolicyToolsListEveryToolWithTheActionsTheGatewayReads(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	s.SetPolicyTools(registeredTools{
		specTool{Name: "web_search", Summary: "Search the web."},
		specTool{Name: "calendar__send_email", Summary: "Send an email.", Parameters: json.RawMessage(`{"type":"object","properties":{"accountId":{"type":"string"}}}`)},
		specTool{Name: "skill", Summary: "Use a skill.", Parameters: actionSchema("list", "info", "use")},
		specTool{Name: "board", Summary: "The work board.", Multiplexed: true, Parameters: actionSchema("list", "delete")},
		specTool{Name: "swarm_spawn", Summary: "Spawn workers.", Multiplexed: true, Parameters: json.RawMessage(`{"type":"object"}`)},
	})
	rec := policyRequest(t, s, http.MethodGet, "/api/approvals/policies/tools", "", policyOwner)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	var got []policyToolItem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %s: %v", rec.Body, err)
	}
	want := []policyToolItem{
		{Name: "board", Summary: "The work board.", Actions: []string{"list", "delete"}},
		{Name: "calendar__send_email", Summary: "Send an email."},
		{Name: "skill", Summary: "Use a skill."},
		{Name: "swarm_spawn", Summary: "Spawn workers."},
		{Name: "web_search", Summary: "Search the web."},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tools = %+v\nwant   %+v", got, want)
	}
}

func TestPolicyToolsAnswer503Unwired(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	if rec := policyRequest(t, s, http.MethodGet, "/api/approvals/policies/tools", "", policyOwner); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET unwired = %d, want 503", rec.Code)
	}
}
