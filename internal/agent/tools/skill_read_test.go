package tools

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// The snippet `use` frame instructs a shell_exec by-path run on the path actionUse resolved, and
// names shell_exec and no other tool (the escalation clause it used to carry named a tool deleted on
// 2026-06-10, amendment #96). This was TestRenderSnippetUseHostFrame and fed it an export-dir HOST
// path: shell_exec runs only in the box, so that file is real and unreachable — the two-filesystem
// defect the one-path collapse exists to remove. The subject is the in-box path now; the assertions
// are unchanged.
func TestRenderSnippetUseFrame(t *testing.T) {
	t.Parallel()
	const (
		instr   = "Adds two numbers."
		boxPath = "/skills/calc/calc.py"
		interp  = "python3"
	)
	frame := renderSnippetUse(instr, boxPath, interp)

	if !strings.Contains(frame, "shell_exec") {
		t.Fatalf("frame must name shell_exec: %q", frame)
	}
	if !strings.Contains(frame, interp) || !strings.Contains(frame, boxPath) {
		t.Fatalf("frame must carry the interpreter %q and in-box path %q: %q", interp, boxPath, frame)
	}
	if !strings.Contains(frame, instr) {
		t.Fatalf("frame must carry the docs instructions: %q", frame)
	}
	// WR-04: the rendered command single-quotes the path so spaces are shell-safe.
	if !strings.Contains(frame, "'"+boxPath+"'") {
		t.Fatalf("frame must single-quote the path for shell safety: %q", frame)
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
	frame := renderSnippetUse("Adds two numbers.", "/skills/calc/calc.py", "python3")

	for _, tok := range regexp.MustCompile(`[a-z][a-z0-9]*(?:_[a-z0-9]+)+`).FindAllString(frame, -1) {
		if !allowed[tok] {
			t.Fatalf("frame names %q, which no registry serves; the frame may name only %v: %q", tok, allowed, frame)
		}
	}
}

// TestRenderSnippetUseQuotesTheBoxPath is what remains of the WR-04 regression. The old test
// pinned normalizing a Windows host path (C:\Program Files\...) to forward slashes, because
// action=use used to hand the model the host export-dir copy. It renders the in-box POSIX path
// now, so the only quoting that matters is the space/quote safety shellQuoteArg gives every other
// box path.
func TestRenderSnippetUseQuotesTheBoxPath(t *testing.T) {
	t.Parallel()
	frame := renderSnippetUse("Adds two numbers.", "/skills/my calc/my calc.py", "python3")
	if !strings.Contains(frame, `'/skills/my calc/my calc.py'`) {
		t.Fatalf("frame must single-quote the box path so a space cannot word-split: %q", frame)
	}
}

// TestActionUseSnippetEmitsBoxFrame asserts action=use on an active snippet hands the model the
// shell_exec by-path frame naming the IN-BOX path. It used to assert the host export-dir path;
// that file is real but unreachable from shell_exec, which is the defect the one-path collapse
// removed rather than a preference.
func TestActionUseSnippetEmitsBoxFrame(t *testing.T) {
	t.Parallel()
	loader := newFakeLoader()
	loader.snippets = map[string]fakeSnippet{
		"calc": {
			instructions: "Adds two numbers.",
			sandboxPath:  "/skills/calc/calc.py",
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
		t.Fatalf("snippet use must emit a shell_exec frame: %q", res.Preview)
	}
	if !strings.Contains(res.Preview, "/skills/calc/calc.py") {
		t.Fatalf("snippet use must carry the in-box path: %q", res.Preview)
	}
}
