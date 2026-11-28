package approvaltext

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
	"unicode/utf8"
)

func gatewayCard() Presentation {
	return Presentation{Key: GatewayKey, Params: map[string]string{
		"tool": "skill_manage", "risk": "destructive", "args": "action, name", "target": "Skill obsoleta",
	}}
}

func TestToolApprovalContextRoundTrips(t *testing.T) {
	t.Parallel()
	raw, err := ToolApprovalContext("skill_manage", gatewayCard())
	if err != nil {
		t.Fatalf("ToolApprovalContext: %v", err)
	}
	if !IsToolApproval(raw) {
		t.Fatalf("IsToolApproval(%s) = false, want true", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatalf("context is not an object: %v", err)
	}
	if RootString(fields, "tool") != "skill_manage" {
		t.Fatalf("tool = %q, want skill_manage", RootString(fields, "tool"))
	}
	// The card is the host's: no question to derive it from, and the stored one is trusted.
	got, ok := FromContext("", raw)
	if !ok || got.Key != GatewayKey || !maps.Equal(got.Params, gatewayCard().Params) {
		t.Fatalf("FromContext = %+v, %v, want the stored gateway card", got, ok)
	}
	// Enrich is the model-context path: it never keeps a stored card for a type it cannot derive.
	enriched, err := Enrich("", raw)
	if err != nil {
		t.Fatalf("Enrich: %v", err)
	}
	if _, ok := FromContext("", enriched); ok {
		t.Fatal("Enrich kept a tool_approval card it did not derive")
	}
}

func TestToolApprovalContextRejectsInvalidPresentation(t *testing.T) {
	t.Parallel()
	unknownParam := gatewayCard()
	unknownParam.Params["body"] = "secret"
	oversized := gatewayCard()
	oversized.Params["args"] = strings.Repeat("a", maxArgsBytes+1)
	for name, p := range map[string]Presentation{
		"unknown key":      {Key: "approval.forged", Params: map[string]string{"tool": "x"}},
		"no params":        {Key: GatewayKey},
		"missing required": {Key: GatewayKey, Params: map[string]string{"tool": "x", "risk": "destructive"}},
		"unknown param":    unknownParam,
		"oversized param":  oversized,
	} {
		t.Run(name, func(t *testing.T) {
			if Valid(p) {
				t.Fatalf("Valid(%+v) = true", p)
			}
			if raw, err := ToolApprovalContext("skill_manage", p); err == nil {
				t.Fatalf("ToolApprovalContext accepted an invalid card: %s", raw)
			}
		})
	}
	if raw, err := ToolApprovalContext("", gatewayCard()); err == nil {
		t.Fatalf("ToolApprovalContext accepted an empty tool: %s", raw)
	}
	// A stored card that is not valid is not trusted either.
	forged := json.RawMessage(`{"type":"tool_approval","tool":"x","approval_presentation":{"key":"approval.forged","params":{"a":"b"}}}`)
	if _, ok := FromContext("", forged); ok {
		t.Fatal("FromContext trusted an invalid stored card")
	}
}

func TestIsToolApprovalReadsTheExactKey(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]bool{
		`{"type":"tool_approval"}`:                          true,
		`{"TYPE":"tool_approval"}`:                          false,
		`{"type":"Tool_Approval"}`:                          false,
		`{"type":"gateway_approval"}`:                       false,
		`{"type":["tool_approval"]}`:                        false,
		`not json`:                                          false,
		`{"tool":"x","type":"other"}`:                       false,
		`{"tool":"x","type":"tool_approval","tier":"high"}`: true,
	} {
		if got := IsToolApproval(json.RawMessage(raw)); got != want {
			t.Errorf("IsToolApproval(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestBriefPerKey(t *testing.T) {
	t.Parallel()
	noTarget := gatewayCard()
	delete(noTarget.Params, "target")
	for name, tc := range map[string]struct {
		p    Presentation
		want string
	}{
		"gateway with target": {gatewayCard(), "(args: action, name) on Skill obsoleta"},
		"gateway":             {noTarget, "(args: action, name)"},
		"shell": {Presentation{Key: ShellKey, Params: map[string]string{
			"cwd": "/workspace/proj", "command": "rm -rf build\necho done", "digest": "abc",
		}}, "`rm -rf build` in /workspace/proj"},
		"plugin install": {Presentation{Key: PluginInstallKey, Params: cardParams(PluginInstallKey, "plugin", "acme-tools")}, "plugin acme-tools"},
		"plugin remove":  {Presentation{Key: PluginRemoveKey, Params: cardParams(PluginRemoveKey, "plugin", "acme-tools")}, "plugin acme-tools"},
		"task cancel":    {Presentation{Key: TaskCancelKey, Params: cardParams(TaskCancelKey, "task", "019f9349")}, "cancel of task 019f9349"},
		"scheduled":      {Presentation{Key: ScheduledKey, Params: map[string]string{"task": "019f9349", "kind": "agent_job"}}, ""},
		"invalid":        {Presentation{Key: GatewayKey, Params: map[string]string{"tool": "x"}}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := Brief(tc.p); got != tc.want {
				t.Fatalf("Brief = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBriefIsBoundedOnARuneBoundary(t *testing.T) {
	t.Parallel()
	long := Presentation{Key: ShellKey, Params: map[string]string{
		"cwd": "/workspace", "command": strings.Repeat("è", 300), "digest": "abc",
	}}
	got := Brief(long)
	if n := utf8.RuneCountInString(got); n != maxBriefRunes {
		t.Fatalf("Brief has %d runes, want %d", n, maxBriefRunes)
	}
	if !utf8.ValidString(got) || !strings.HasSuffix(got, "…") || !strings.HasPrefix(got, "`èèè") {
		t.Fatalf("Brief = %q, want a valid prefix cut with an ellipsis", got)
	}
}

// cardParams fills every line of a line card, the subject line with value.
func cardParams(key, subject, value string) map[string]string {
	card, _ := cardFor(key)
	params := map[string]string{"digest": "abc"}
	for _, line := range card.lines {
		params[line.param] = "line"
	}
	params[subject] = value
	return params
}
