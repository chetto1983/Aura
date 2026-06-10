// `aura skills snippet {save|exec}` (Slice 7e, plan 11-07). save stages an
// executable snippet (a type:snippet skill) as pending, gated like any mutation
// (the operator then `approve`s it). exec is the DETERMINISTIC operator path that
// runs an ACTIVE snippet BY PATH on the HOST (the materialized export-dir file) via
// os/exec AND stamps the usage sidecar (D-04/D-19). exec resolves the host path +
// interpreter via Writer.UseSnippet — the same materialized file the model runs
// through shell_exec.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/chetto1983/aura/internal/skills"
)

const snippetUsage = "usage: aura skills snippet {save <name> --lang <python|shell|js> --code <code> " +
	"[--desc <d>] [--needs-network] [--needs-workspace]|exec <name> [args...]}"

// skillsSnippet dispatches the snippet subcommands.
func skillsSnippet(ctx context.Context, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, snippetUsage)
		os.Exit(1)
	}
	switch args[0] {
	case "save":
		skillsSnippetSave(ctx, args[1:])
	case "exec":
		skillsSnippetExec(ctx, args[1:])
	default:
		fmt.Fprintln(os.Stderr, snippetUsage)
		os.Exit(1)
	}
}

// skillsSnippetSave stages a snippet as pending. --lang is the required language enum;
// --code is the executable code; --desc is the manifest description; --needs-network /
// --needs-workspace surface in the SAVE gate (D-20/D-37). The operator then approves it.
func skillsSnippetSave(ctx context.Context, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, snippetUsage)
		os.Exit(1)
	}
	name := args[0]
	lang, _ := flagValue(args[1:], "--lang")
	code, _ := flagValue(args[1:], "--code")
	desc, _ := flagValue(args[1:], "--desc")
	if lang == "" || code == "" {
		fmt.Fprintln(os.Stderr, "skills snippet save: --lang and --code are required")
		os.Exit(1)
	}
	if desc == "" {
		desc = name + " snippet"
	}

	env := bootSkills(ctx)
	defer env.close()

	fm := skills.Frontmatter{
		Name:           name,
		Description:    desc,
		NeedsNetwork:   hasFlag(args[1:], "--needs-network"),
		NeedsWorkspace: hasFlag(args[1:], "--needs-workspace"),
	}
	res, err := env.w.SaveSnippet(ctx, name, lang, code, fm, skills.AuditActor{ActorID: "cli"})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: snippet %q saved (status=%s, lang=%s, tier=%s) — approve with `aura skills approve %s`\n",
		name, res.Status, res.Language, res.Tier, name)
	if res.NeedsNetwork {
		fmt.Println("  needs_network: true (RISKY tier; the enforced egress proxy is Phase-8)")
	}
	if res.NeedsWorkspace {
		fmt.Println("  needs_workspace: true")
	}
}

// skillsSnippetExec runs an ACTIVE snippet by path on the HOST and stamps the usage
// sidecar (SC#4 deterministic operator path, D-04/D-19). It resolves the host export-dir
// path + interpreter via Writer.UseSnippet, execs <interpreter> <hostPath> via os/exec,
// prints stdout/stderr/exit, and bumps use_count on a clean run. Any trailing args after
// the name are passed to the snippet (interpreter path arg1 arg2 ...).
func skillsSnippetExec(ctx context.Context, args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: aura skills snippet exec <name> [args...]")
		os.Exit(1)
	}
	name := args[0]
	extra := args[1:]

	env := bootSkills(ctx)
	defer env.close()

	use, err := env.w.UseSnippet(name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	runArgs := append([]string{use.HostPath}, extra...)
	// use.Interpreter is a fixed language->binary (python3/sh/node) resolved from validated
	// snippet frontmatter; use.HostPath is the writer-materialized export-dir file, not
	// arbitrary input. Host exec IS the deliberate execution surface (amendment #50 / D-15c).
	cmd := exec.CommandContext(ctx, use.Interpreter, runArgs...) // #nosec G204 G702 -- see above
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()

	if stdout.Len() > 0 {
		fmt.Print(stdout.String())
	}
	if stderr.Len() > 0 {
		fmt.Fprint(os.Stderr, stderr.String())
	}

	// A non-zero exit (ExitError) carries an exit code; a spawn failure (interpreter
	// not found) is a hard error, never a stamped run.
	exit := 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exit = ee.ExitCode()
		} else {
			fmt.Fprintf(os.Stderr, "snippet exec %q: %v\n", name, runErr)
			os.Exit(1)
		}
	}

	// Stamp usage on a clean (exit 0) run — the deterministic operator stamp (D-19).
	if exit == 0 {
		if serr := env.w.StampUsage(name, time.Now()); serr != nil {
			fmt.Fprintf(os.Stderr, "snippet exec %q: usage stamp failed: %v\n", name, serr)
		}
	}
	if exit != 0 {
		os.Exit(exit)
	}
}
