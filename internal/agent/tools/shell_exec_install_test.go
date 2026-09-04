package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// shell_exec's skills-install interception. Measured 2026-09-04 by driving the real agent: told to
// install a skill, it ran `npx skills add … --skill … -y` in the box, the CLI succeeded, and the
// payload landed COMPLETE in /workspace/.agents/skills/<name>/ — scripts, reference, images, the
// lot — plus a symlink under /workspace/data/skills and a /workspace/skills-lock.json. Nothing was
// broken except the directory: the CLI writes relative to its cwd, and the box's cwd is not the
// library. The model was then told "Installation complete" for a skill Aura cannot load, and the
// ~53 agent-layout directories it left behind sit in a persistent volume nobody sweeps.
//
// The fix routes the command to the host install pipeline, which runs THE SAME CLI
// (`npx skills add <source> --copy -y`, installer.go) in a host work dir and then applies the six
// gates the box can never reach: ambiguity refusal, name/dir match, content hash, the NFKC
// injection blocklist, and the audit row that commits before the write. Interception happens at
// the gate shell_exec already has, so nothing new crosses the D-10 boundary — the box still never
// touches the host library.

// recordingInstallHook returns a hook that records the sources it was handed and answers with a
// fixed line, standing in for the host-side Installer.
func recordingInstallHook(got *[]string, reply string) func(context.Context, string) (string, error) {
	return func(_ context.Context, source string) (string, error) {
		*got = append(*got, source)
		return reply, nil
	}
}

// TestShellExecRoutesSkillsInstallToTheHost proves a skills-install command NEVER reaches the box
// and is answered by the host pipeline instead. The box assertion is the load-bearing half: an
// install that also ran in the box would leave the agent-layout litter this exists to stop.
func TestShellExecRoutesSkillsInstallToTheHost(t *testing.T) {
	var sources []string
	tool, be := boxShell(usersandbox.ExecResult{Stdout: []byte("must not run in the box")})
	tool.InstallHook = recordingInstallHook(&sources, `Skill "tushare-finance" installed.`)
	ctx := ctxWith(t, "sess-inst", "call-inst")

	res, err := tool.Execute(ctx, json.RawMessage(
		`{"command":"npx skills add stanleychanh/tushare-finance-skill-for-claude-code --skill tushare-finance -y"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("install hook calls = %d, want 1 (the host pipeline must own the install)", len(sources))
	}
	if want := "stanleychanh/tushare-finance-skill-for-claude-code@tushare-finance"; sources[0] != want {
		t.Errorf("hook source = %q, want %q — the --skill selector must survive as the @suffix the installer takes", sources[0], want)
	}
	if len(be.execCalls) != 0 {
		t.Errorf("the command must NOT run in the box, got %#v", be.execCalls)
	}
	if !strings.Contains(res.Preview, "tushare-finance") {
		t.Errorf("preview must carry the host result, got %q", res.Preview)
	}
}

// TestShellExecLeavesOrdinaryCommandsAlone proves the interception is narrow: anything that is not
// a skills install reaches the box verbatim, hook or no hook. A guard that swallowed neighbouring
// commands would cost more than the litter it prevents.
func TestShellExecLeavesOrdinaryCommandsAlone(t *testing.T) {
	for _, command := range []string{
		"echo hello",
		"npm install lodash",
		"npx prettier --write .",
		"cat /skills/xlsx/SKILL.md",
	} {
		var sources []string
		tool, be := boxShell(usersandbox.ExecResult{Stdout: []byte("ran\n")})
		tool.InstallHook = recordingInstallHook(&sources, "unused")
		ctx := ctxWith(t, "sess-plain", "call-plain")

		if _, err := tool.Execute(ctx, json.RawMessage(`{"command":`+quoteJSON(command)+`}`)); err != nil {
			t.Fatalf("Execute(%q): %v", command, err)
		}
		if len(sources) != 0 {
			t.Errorf("%q must not be treated as an install, hook saw %v", command, sources)
		}
		if len(be.execCalls) != 1 {
			t.Errorf("%q must reach the box, got %#v", command, be.execCalls)
		}
	}
}

// TestShellExecInstallWithoutHookStillRefuses proves the box is protected even where no hook is
// wired: the command is refused with guidance rather than silently running and reporting a success
// Aura cannot honour. Fail-closed matches every other box-boundary decision in this package.
func TestShellExecInstallWithoutHookStillRefuses(t *testing.T) {
	tool, be := boxShell(usersandbox.ExecResult{Stdout: []byte("must not run")})
	ctx := ctxWith(t, "sess-nohook", "call-nohook")

	res, err := tool.Execute(ctx, json.RawMessage(`{"command":"npx skills add owner/repo -y"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(be.execCalls) != 0 {
		t.Errorf("an install must never run in the box, got %#v", be.execCalls)
	}
	if !strings.Contains(res.Preview, "skill_manage") {
		t.Errorf("the refusal must name the way that works, got %q", res.Preview)
	}
}

// quoteJSON renders s as a JSON string literal for the table above.
func quoteJSON(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
