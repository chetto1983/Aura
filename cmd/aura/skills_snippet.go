// `aura skills snippet {save|exec}` (Slice 7e, plan 11-07). save stages an
// executable snippet (a type:snippet skill) as pending, gated like any mutation
// (the operator then `approve`s it). exec is the DETERMINISTIC operator path that
// runs an ACTIVE snippet BY PATH through the sandbox_exec seam (python3
// /skills/<name>/<name>.py — never the exec bit, spike 005) AND stamps the usage
// sidecar (D-04/D-19). There is NO bespoke run code: exec resolves the in-sandbox
// path via Writer.UseSnippet and calls the same sandboxagent client the model's
// sandbox_exec tool uses.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/chetto1983/aura/internal/sandboxagent"
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

// skillsSnippetExec runs an ACTIVE snippet by path through the sandbox_exec seam and
// stamps the usage sidecar (SC#4 deterministic operator path, D-04/D-19). It resolves
// the in-sandbox /skills path + interpreter via Writer.UseSnippet, calls the same
// sandboxagent client the model's sandbox_exec uses, prints stdout/stderr/exit, and
// bumps use_count on success. Any trailing args after the name are passed to the
// snippet (interpreter path arg1 arg2 ...).
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

	client := sandboxagent.New(env.cfg.SandboxAgent)
	runArgs := append([]string{use.SandboxPath}, extra...)
	res, runErr := client.Run(ctx, sandboxagent.RunRequest{
		Command: use.Interpreter,
		Args:    runArgs,
	})
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "snippet exec %q: %v\n", name, runErr)
		os.Exit(1)
	}

	if res.Stdout != "" {
		fmt.Print(res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Fprint(os.Stderr, res.Stderr)
	}

	// Stamp usage on a clean (exit 0) run — the deterministic operator stamp (D-19).
	exit := 0
	if res.ExitCode != nil {
		exit = *res.ExitCode
	}
	if exit == 0 {
		if serr := env.w.StampUsage(name, time.Now()); serr != nil {
			fmt.Fprintf(os.Stderr, "snippet exec %q: usage stamp failed: %v\n", name, serr)
		}
	}
	if exit != 0 {
		os.Exit(exit)
	}
}
