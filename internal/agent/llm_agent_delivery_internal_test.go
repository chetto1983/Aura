package agent

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/llm"
)

func TestUndeliveredArtifacts(t *testing.T) {
	cases := []struct {
		name              string
		workspace         string
		edited, delivered []string
		want              []string
	}{
		{name: "artifact written and never sent is pending",
			edited: []string{"/workspace/artifacts/report.html"}, want: []string{"/workspace/artifacts/report.html"}},
		{name: "delivered path clears it whatever spelling send_file used",
			edited: []string{"/workspace/artifacts/report.html"}, delivered: []string{"/workspace/artifacts/../artifacts/report.html"}},
		{name: "relative paths resolve against the workspace",
			edited: []string{"artifacts/chart.png"}, delivered: []string{"/workspace/artifacts/chart.png"}},
		{name: "a script outside artifacts is not a deliverable",
			edited: []string{"/workspace/scripts/fetch.py", "/workspace/artifacts.md"}},
		{name: "a custom workspace moves the artifacts prefix",
			workspace: "/home/box", edited: []string{"/home/box/artifacts/a.md", "/workspace/artifacts/b.md"},
			want: []string{"/home/box/artifacts/a.md"}},
		{name: "duplicates collapse", edited: []string{"/workspace/artifacts/a.md", "artifacts/a.md"},
			want: []string{"/workspace/artifacts/a.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := undeliveredArtifacts(tc.workspace, tc.edited, tc.delivered)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("undelivered = %v, want %v", got, tc.want)
			}
		})
	}
}

func sendFileCall(args string) llm.ToolCall {
	call := llm.ToolCall{ID: "s1"}
	call.Function.Name = "send_file"
	call.Function.Arguments = args
	return call
}

func deliveredResult() tools.ToolResult {
	return tools.ToolResult{Preview: "sent", Meta: &tools.ToolResultMeta{"artifact": map[string]any{"name": "report.html"}}}
}

func TestRecordDeliveredPath(t *testing.T) {
	a := &LlmAgent{}
	// A send_file that produced no artifact descriptor delivered nothing.
	a.recordDeliveredPath(sendFileCall(`{"path":"/workspace/artifacts/report.html"}`), tools.ToolResult{Preview: `{"error":"not found"}`})
	if len(a.deliveredPaths) != 0 {
		t.Fatalf("a failed send_file recorded %v", a.deliveredPaths)
	}
	// Any tool that is not send_file is ignored even with an artifact in its Meta.
	other := sendFileCall(`{"path":"/workspace/artifacts/report.html"}`)
	other.Function.Name = "write_file"
	a.recordDeliveredPath(other, deliveredResult())
	if len(a.deliveredPaths) != 0 {
		t.Fatalf("write_file recorded a delivery: %v", a.deliveredPaths)
	}
	// The alias spellings send_file accepts count, once each.
	a.recordDeliveredPath(sendFileCall(`{"file_path":"/workspace/artifacts/report.html"}`), deliveredResult())
	a.recordDeliveredPath(sendFileCall(`{"absolute_path":"/workspace/artifacts/report.html"}`), deliveredResult())
	a.recordDeliveredPath(sendFileCall(`not json`), deliveredResult())
	if strings.Join(a.deliveredPaths, ",") != "/workspace/artifacts/report.html" {
		t.Fatalf("deliveredPaths = %v", a.deliveredPaths)
	}
}

func TestGateDeliveryNudgesOncePerRun(t *testing.T) {
	a := &LlmAgent{editedPaths: []string{"/workspace/artifacts/a.html", "/workspace/artifacts/b.png"}}
	nudge, ok := a.gateDelivery()
	if !ok || !strings.HasPrefix(nudge, deliverOnStopNudgePrefix) || !isAgentNudge(nudge) {
		t.Fatalf("first stop: ok=%v nudge=%q", ok, nudge)
	}
	if !strings.Contains(nudge, "/workspace/artifacts/a.html, /workspace/artifacts/b.png") {
		t.Fatalf("nudge does not name the artifacts: %q", nudge)
	}
	if _, again := a.gateDelivery(); again {
		t.Fatal("the gate nudged twice in one run")
	}
	quiet := &LlmAgent{editedPaths: []string{"/workspace/artifacts/a.html"}, deliveredPaths: []string{"/workspace/artifacts/a.html"}}
	if _, ok := quiet.gateDelivery(); ok || quiet.deliveryAttempts != 0 {
		t.Fatal("a delivered artifact still tripped the gate")
	}
}
