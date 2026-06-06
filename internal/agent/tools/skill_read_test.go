package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestRenderSnippetUseHostFrame asserts the D-01 host-primary contract: the snippet
// `use` frame instructs a HOST shell_exec by-path run as the PRIMARY action, and only
// names sandbox_exec as the secondary escalation for an untrusted/one-off run. The old
// frame made sandbox_exec the primary verb — that literal is superseded by amendment
// #55 (host shell_exec is the surface the production loop + D-35 eval gate run on).
func TestRenderSnippetUseHostFrame(t *testing.T) {
	t.Parallel()
	const (
		instr  = "Adds two numbers."
		host   = "/home/u/.aura/skills/export/calc/calc.py"
		interp = "python3"
	)
	frame := renderSnippetUse(instr, host, interp)

	if !strings.Contains(frame, "shell_exec") {
		t.Fatalf("frame must name shell_exec (host-primary): %q", frame)
	}
	if !strings.Contains(frame, interp) || !strings.Contains(frame, host) {
		t.Fatalf("frame must carry the interpreter %q and host path %q: %q", interp, host, frame)
	}
	if !strings.Contains(frame, instr) {
		t.Fatalf("frame must carry the docs instructions: %q", frame)
	}
	// shell_exec is the PRIMARY verb: it must appear BEFORE sandbox_exec in the frame.
	shellAt := strings.Index(frame, "shell_exec")
	sandboxAt := strings.Index(frame, "sandbox_exec")
	if sandboxAt >= 0 && shellAt > sandboxAt {
		t.Fatalf("shell_exec must precede sandbox_exec (host-primary, sandbox is escalation): %q", frame)
	}
	// The escalation is named but not the primary instruction.
	if !strings.Contains(frame, "sandbox_exec") {
		t.Fatalf("frame must still NAME sandbox_exec as the escalation: %q", frame)
	}
}

// TestActionUseSnippetEmitsHostFrame asserts action=use on an active snippet hands the
// model the host shell_exec by-path frame (not a sandbox_exec primary frame), via the
// widened Snippet seam returning a HOST path.
func TestActionUseSnippetEmitsHostFrame(t *testing.T) {
	t.Parallel()
	loader := newFakeLoader()
	loader.snippets = map[string]fakeSnippet{
		"calc": {
			instructions: "Adds two numbers.",
			hostPath:     "/home/u/.aura/skills/export/calc/calc.py",
			interpreter:  "python3",
		},
	}
	tool := &SkillTool{Loader: loader}
	ctx := withTestToolCallCtx(context.Background())

	res, err := tool.Execute(ctx, json.RawMessage(`{"action":"use","name":"calc"}`))
	if err != nil {
		t.Fatalf("use snippet: %v", err)
	}
	if !strings.Contains(res.Preview, "shell_exec") {
		t.Fatalf("snippet use must emit a host shell_exec frame: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "/home/u/.aura/skills/export/calc/calc.py") {
		t.Fatalf("snippet use must carry the host path: %q", res.Preview)
	}
	shellAt := strings.Index(res.Preview, "shell_exec")
	sandboxAt := strings.Index(res.Preview, "sandbox_exec")
	if sandboxAt >= 0 && shellAt > sandboxAt {
		t.Fatalf("shell_exec must be the primary verb (precede sandbox_exec): %q", res.Preview)
	}
}
