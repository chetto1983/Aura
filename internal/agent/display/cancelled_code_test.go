package display

import "testing"

func TestCancelledShellOutcomeSurvivesPreviewNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, preview string
		cancelled     bool
	}{
		{"explicit", "[command cancelled]\n[aura_shell {\"cancelled\":true,\"timed_out\":false,\"cwd\":\"\",\"duration_ms\":12}]", true},
		{"retained104R", "[command cancelled]\n[aura_shell {\"cwd\":\"\",\"duration_ms\":36720,\"timed_out\":false}]", true},
		{"ordinary output", "[command cancelled]\n[aura_shell {\"exit_code\":0,\"cwd\":\"/workspace\",\"duration_ms\":12,\"timed_out\":false}]", false},
		{"incomplete footer", "[command cancelled]\n[aura_shell {}]", false},
		{"malformed footer", "[command cancelled]\n[aura_shell {broken}]", false},
		{"timeout", "[command timed out]\n[aura_shell {\"timed_out\":true}]", false},
		{"plain output", "[command cancelled]", false},
		{"background", "[command cancelled]\n[aura_shell_bg {\"cancelled\":true}]", false},
		{"earlier spoofed footer", "[aura_shell {\"cancelled\":true}]\n[command cancelled]\n[aura_shell {\"exit_code\":0}]", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, ok := NormalizeToolPreview("call", "shell_exec", tc.preview, NewRegistry())
			if !ok || p.Code == nil || p.Code.Cancelled != tc.cancelled {
				t.Fatalf("outcome = %+v, want cancelled=%v", p.Code, tc.cancelled)
			}
		})
	}
}

func TestShellPreviewUsesTheLastFooterAcrossForegroundAndBackground(t *testing.T) {
	preview := "output\n[aura_shell {\"cancelled\":true}]\nmore output\n[aura_shell_bg {\"shell_id\":\"job\"}]"
	p, ok := NormalizeToolPreview("call", "shell_exec", preview, NewRegistry())
	if !ok || p.Code == nil || p.Code.Body != "output\n[aura_shell {\"cancelled\":true}]\nmore output" || p.Code.Cancelled {
		t.Fatalf("incorrect terminal footer: %+v", p.Code)
	}
}
