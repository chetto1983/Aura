package display

import "testing"

func TestTerminalPreview(t *testing.T) {
	base := PreviewInput{ToolCallID: "s1", ToolName: "shell_exec", Arguments: `{"command":"make test"}`}
	for _, tc := range []struct {
		name, tool, args, preview string
		wantExit                  int
		wantOutput, wantCommand   string
		wantTruncated, wantOK     bool
	}{
		{"foreground failure", "shell_exec", base.Arguments, "failed\n[exit code 7]\n[aura_shell {\"exit_code\":7,\"cwd\":\"/workspace\",\"duration_ms\":34,\"timed_out\":false}]", 7, "failed\n[exit code 7]", "make test", false, true},
		{"combined stderr tail", "shell_exec", base.Arguments, "output\n[stderr tail]\nERRTAIL\n[aura_shell {\"exit_code\":1,\"cwd\":\"/workspace\",\"duration_ms\":34,\"timed_out\":false}]", 1, "output\n[stderr tail]\nERRTAIL", "make test", false, true},
		{"truncated", "shell_exec", base.Arguments, "[output truncated: dropped 100 byte(s); showing last 6 byte(s)]\noutput\n[aura_shell {\"exit_code\":0,\"cwd\":\"/workspace\",\"duration_ms\":34,\"timed_out\":false}]", 0, "[output truncated: dropped 100 byte(s); showing last 6 byte(s)]\noutput", "make test", true, true},
		{"poll finished", "shell_poll", `{"shell_id":"job-1"}`, "new line\n[aura_shell_bg {\"shell_id\":\"job-1\",\"status\":\"exited:3\",\"age_ms\":1200}]", 3, "new line", "shell_poll job-1", false, true},
		{"poll running", "shell_poll", `{"shell_id":"job-1"}`, "new line\n[aura_shell_bg {\"shell_id\":\"job-1\",\"status\":\"running\"}]", 0, "", "", false, false},
		{"poll wrong ID", "shell_poll", `{"shell_id":"job-1"}`, "new line\n[aura_shell_bg {\"shell_id\":\"job-2\",\"status\":\"exited:0\"}]", 0, "", "", false, false},
		{"running background", "shell_exec", base.Arguments, "Started in background\n[aura_shell_bg {\"shell_id\":\"job-1\",\"status\":\"running\"}]", 0, "", "", false, false},
		{"legacy cancelled", "shell_exec", base.Arguments, "[command cancelled]\n[aura_shell {\"cancelled\":true,\"timed_out\":false,\"cwd\":\"\",\"duration_ms\":12}]", 0, "", "", false, false},
		{"malformed footer", "shell_exec", base.Arguments, "output\n[aura_shell {broken}]", 0, "", "", false, false},
		{"missing exit", "shell_exec", base.Arguments, "output\n[aura_shell {\"cwd\":\"/tmp\",\"duration_ms\":2}]", 0, "", "", false, false},
		{"invalid exit", "shell_exec", base.Arguments, "output\n[aura_shell {\"exit_code\":-1,\"cwd\":\"/tmp\",\"duration_ms\":2}]", 0, "", "", false, false},
		{"missing args", "shell_exec", `{}`, "output\n[aura_shell {\"exit_code\":0}]", 0, "", "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := PreviewInput{ToolCallID: base.ToolCallID, ToolName: tc.tool, Arguments: tc.args, ResultPreview: tc.preview}
			p, ok := NormalizeToolPreview(in, NewRegistry())
			if ok != tc.wantOK {
				t.Fatalf("recognized = %v, payload = %+v", ok, p)
			}
			if !tc.wantOK {
				return
			}
			if p.Type != KindTerminal || p.Terminal == nil || p.Terminal.ExitCode != tc.wantExit || p.Terminal.Output != tc.wantOutput || p.Terminal.Command != tc.wantCommand || p.Terminal.Truncated != tc.wantTruncated {
				t.Fatalf("terminal = %+v", p)
			}
		})
	}
}
