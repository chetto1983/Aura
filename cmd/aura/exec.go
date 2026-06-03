package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/sandbox"
)

// Exit codes for `aura exec` (D-19). A normal run exits with the sandbox's own
// exit_code; an unreachable sidecar is the distinct infra code 70; any other infra
// failure (malformed protocol etc.) is 71; a usage error is 64 (sysexits EX_USAGE).
const (
	exitUnreachable = 70
	exitInfra       = 71
	exitUsage       = 64
	exitSessionCap  = 75 // sysexits EX_TEMPFAIL — the session cap is reached; retry later
)

// execArgs is the parsed `aura exec [--session <id>] <lang> <code|->` invocation.
// A non-empty session routes through the 2b session-bound runner (persistent
// per-conversation container); an empty session keeps the 2a stateless path.
// codeStdin marks the `-` form so runExec reads the snippet from stdin.
type execArgs struct {
	lang      string
	code      string
	codeStdin bool
	session   string
}

// parseExecArgs hand-parses the exec invocation (no cobra — repo convention). It
// accepts an optional --session <id> anywhere before the positionals, a required
// lang in {python,shell}, and a code positional (a single string, or "-" to read
// the whole snippet from stdin for big/multi-line input).
//
// The flag is consumed via a range loop with a one-shot expectSession latch rather
// than manual index advancement: indexing the slice after an i++ is a textbook
// gosec G602 false positive (the bounds guard is provably correct but gosec can't
// follow increment-then-guard), so we sidestep it by never indexing at all.
func parseExecArgs(args []string) (execArgs, error) {
	var ea execArgs
	var positionals []string
	expectSession := false
	for _, arg := range args {
		switch {
		case expectSession:
			ea.session = arg
			expectSession = false
		case arg == "--session":
			expectSession = true
		default:
			positionals = append(positionals, arg)
		}
	}
	if expectSession {
		return ea, fmt.Errorf("--session requires an id")
	}
	if len(positionals) < 2 {
		return ea, fmt.Errorf("usage: aura exec [--session <id>] <python|shell> <code|->")
	}
	if len(positionals) > 2 {
		return ea, fmt.Errorf("unexpected extra argument %q (quote multi-word code or pass - to read stdin)", positionals[2])
	}
	ea.lang = positionals[0]
	switch ea.lang {
	case "python", "shell":
	default:
		return ea, fmt.Errorf("lang %q must be one of python|shell", ea.lang)
	}
	if positionals[1] == "-" {
		ea.codeStdin = true
	} else {
		ea.code = positionals[1]
	}
	return ea, nil
}

// runExec is the `aura exec` entry point. It exits the process: with the sandbox
// exit_code on a normal run, 70 on ErrSandboxUnreachable, 75 on ErrSessionCapReached,
// 71 on any other infra error, 64 on a usage/arg error. A non-empty --session routes
// through the 2b session-bound runner; otherwise the 2a stateless path (D-19).
func runExec(args []string) {
	ea, err := parseExecArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura exec:", err)
		os.Exit(exitUsage)
	}

	code := ea.code
	if ea.codeStdin {
		b, rerr := io.ReadAll(os.Stdin)
		if rerr != nil {
			fmt.Fprintln(os.Stderr, "aura exec: reading stdin:", rerr)
			os.Exit(exitUsage)
		}
		code = string(b)
	}

	// LoadDB skips the LLM key fail-fast (Load): exec only needs the sandbox fields.
	cfg := config.LoadDB()
	runner := sandbox.NewDockerRunner(cfg)
	ctx := context.Background()

	res, err := execRun(ctx, runner, ea, code)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura exec:", err)
		switch {
		case errors.Is(err, sandbox.ErrSandboxUnreachable):
			os.Exit(exitUnreachable)
		case errors.Is(err, sandbox.ErrSessionCapReached):
			os.Exit(exitSessionCap)
		default:
			os.Exit(exitInfra) // ErrSandboxProtocol or any other infra fault
		}
	}

	// Same lean format as the execute tool (reuse, no drift).
	fmt.Println(tools.FormatLean(res))
	os.Exit(res.ExitCode)
}

// execRun dispatches the lang to the stateless (no --session) or session-bound
// (--session <id>) runner path. timeout 0 → the Runner substitutes
// cfg.SandboxTimeoutSec. lang is pre-validated to python|shell by parseExecArgs.
func execRun(ctx context.Context, runner sandbox.Runner, ea execArgs, code string) (sandbox.Result, error) {
	if ea.session != "" {
		if ea.lang == "python" {
			return runner.RunPythonSession(ctx, ea.session, code, 0)
		}
		return runner.RunShellSession(ctx, ea.session, code, 0)
	}
	if ea.lang == "python" {
		return runner.RunPython(ctx, code, 0)
	}
	return runner.RunShell(ctx, code, 0)
}
