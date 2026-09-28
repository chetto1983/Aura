package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/agent/tools"
)

func TestToolResultEventStampsMCPPreviewDigest(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry())
	run := artifactRun(t, nil)
	run.ToolName = "pim__calendar"
	run.Result.Preview = `{"accounts":[]}`
	run.Preview = run.Result.Preview
	run.Result.Meta = &tools.ToolResultMeta{
		"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar", "action": "list_accounts"},
	}
	ev := a.toolResultEvent(internalPauseIC(t), [8]byte{}, nil, run)
	sum := sha256.Sum256([]byte(run.Preview))
	want := hex.EncodeToString(sum[:])
	if got := ev.Actions.ToolInvocation.Meta["aura_display_preview_sha256"]; got != want {
		t.Fatalf("preview digest = %v, want %s", got, want)
	}
}

func TestToolResultEventKeepsMCPReadAndView(t *testing.T) {
	a := newBareAgent(t, tools.NewRegistry())
	a.sources = display.NewRegistry()
	run := artifactRun(t, nil)
	run.ToolName = "pim__calendar"
	run.Arguments = `{"action":"list_accounts"}`
	run.Result.Preview = `{"accounts":[{"accountId":"a1","provider":"google","displayName":"Work"}]}`
	run.Preview = run.Result.Preview
	run.Result.Meta = &tools.ToolResultMeta{
		"aura_display_source": map[string]any{"recipe": "recipe:calendar", "tool": "calendar", "action": "list_accounts"},
		"mcp_view":            map[string]any{"server": "pim", "resource_uri": "ui://calendar/view.html", "text_content": run.Result.Preview},
	}
	ev := a.toolResultEvent(internalPauseIC(t), [8]byte{}, nil, run)
	if ev.Actions.Display == nil || ev.Actions.Display.Table == nil || ev.Actions.ViewDelta == nil {
		t.Fatalf("live call lost read card or view: %+v", ev.Actions)
	}
	if ev.Actions.ViewDelta["tool_call_id"] != run.ToolCallID {
		t.Fatalf("view correlation mismatch: %+v", ev.Actions.ViewDelta)
	}
}
