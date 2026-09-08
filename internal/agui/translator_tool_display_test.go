package agui

import (
	"encoding/json"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/display"
	"testing"
)

func TestRetainedCancelledToolEmitsCorrectedDisplayWithoutMutatingAudit(t *testing.T) {
	oldDisplay := &display.Payload{Type: display.KindCode, ToolCallID: "call", Code: &display.Code{Body: "[command cancelled]"}}
	ev := &agent.Event{Actions: agent.Actions{
		ToolInvocation: &agent.ToolInvocation{Event: agent.ToolInvocationEnd, ToolCallID: "call", ToolName: "shell_exec", Status: "ok",
			ResultPreview: "[command cancelled]\n[aura_shell {\"cwd\":\"\",\"duration_ms\":36720,\"timed_out\":false}]"},
		Display: oldDisplay,
	}}
	frames := collect(t, "thread", "run", &fixedIDGen{}, []*agent.Event{ev})
	seen := false
	for _, frame := range frames {
		raw, err := json.Marshal(frame)
		if err != nil {
			t.Fatal(err)
		}
		var data struct {
			Name  string          `json:"name"`
			Value display.Payload `json:"value"`
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		if data.Name == DisplayEventName {
			seen = true
			if data.Value.Code == nil || !data.Value.Code.Cancelled {
				t.Fatalf("display lost cancellation: %s", raw)
			}
		}
	}
	if !seen || oldDisplay.Code.Cancelled || ev.Actions.ToolInvocation.Status != "ok" {
		t.Fatal("missing corrected display or mutated retained audit event")
	}
}
