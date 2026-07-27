package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// TestRenderSnippetUseHostFrame asserts the D-01 host-primary contract: the snippet
// `use` frame instructs a shell_exec by-path run on the path actionUse resolved. The
// frame names shell_exec and no other tool — the escalation clause it used to carry
// named a tool deleted on 2026-06-10 (amendment #96).
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
	// WR-04: the rendered command single-quotes the path so spaces are shell-safe.
	if !strings.Contains(frame, "'"+host+"'") {
		t.Fatalf("frame must single-quote the host path for shell safety: %q", frame)
	}
}

// TestSnippetFrameNamesNoUnservedTool is the amendment-#96 regression. The frame goes
// straight into the model's context, so every tool name in it is an instruction the
// model will act on: `sandbox_exec` outlived its deletion here by seven weeks and cost
// a failed call each time a snippet was used. Rather than banning one dead literal, the
// test rejects EVERY tool-shaped token the frame does not intend — inputs are chosen
// underscore-free, so any snake_case token in the output came from the frame itself.
func TestSnippetFrameNamesNoUnservedTool(t *testing.T) {
	t.Parallel()
	allowed := map[string]bool{"shell_exec": true}
	frame := renderSnippetUse("Adds two numbers.", "/home/u/skills/calc/calc.py", "python3")

	for _, tok := range regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z0-9]+)+`).FindAllString(frame, -1) {
		if !allowed[tok] {
			t.Fatalf("frame names %q, which no registry serves; the frame may name only %v: %q", tok, allowed, frame)
		}
	}
}

// TestRenderSnippetUseShellSafePath is the WR-04 regression: a Windows host path
// (backslashes + a spaced parent dir) is normalized to forward slashes and single-
// quoted in the rendered shell_exec command, so a bash -c (Git Bash) consumer does
// not mangle \U/\n escapes or word-split on the space.
func TestRenderSnippetUseShellSafePath(t *testing.T) {
	t.Parallel()
	const winPath = `C:\Program Files\aura\export\calc\calc.py`
	frame := renderSnippetUse("Adds two numbers.", winPath, "python3")

	if strings.Contains(frame, `\Program`) || strings.Contains(frame, `C:\`) {
		t.Fatalf("frame must not carry backslashes (bash -c mangles them): %q", frame)
	}
	want := `'C:/Program Files/aura/export/calc/calc.py'`
	if !strings.Contains(frame, want) {
		t.Fatalf("frame must carry the forward-slashed, single-quoted path %q: %q", want, frame)
	}
}

// TestActionUseSnippetEmitsHostFrame asserts action=use on an active snippet hands the
// model the host shell_exec by-path frame, via the widened Snippet seam returning a
// HOST path.
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
}
