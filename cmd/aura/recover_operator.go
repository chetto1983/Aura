// Break-glass operator recovery for `aura identity recover-operator` (Phase 43,
// R1/R3/R4, D-05). This is the OFFLINE, HOST-ONLY operator credential reset: it
// resolves the single operator identity, resets its Authula password (argon2id),
// invalidates its sessions, and — unless --no-recovery — re-seeds aura.identity_recovery
// so the online forgot-password flow works again. It is a SIBLING of `recover <name>`
// (recovery.go), which is a different mechanism (minting a short-lived reset token to
// hand a user); this path never mints a token and never touches that flow.
//
// Secret discipline (T-43-01): the new password + recovery answer are sourced via
// internal/breakglass.Sourcer three ways (AURA_RECOVERY_* env, --generate, hidden TTY
// prompt). The ONLY place a secret is ever emitted is the single --generate stdout line
// written by Sourcer.Source; the ok/success line and every error name NO secret, and the
// password is never a CLI argument. Runtime requires AURA_AUTHULA_SECRET (+ an Authula
// DSN) for the offline Authula provider (breakglass.NewAuthula enforces it) — an empty
// secret fails fast with a clear, secret-free stderr error rather than a panic (A3/T-43-10).
//
// Flags are parsed with a std flag.FlagSet, NOT cobra: go.mod has no spf13/cobra and
// CLAUDE.md mandates following the existing runIdentity/runDB switch-tree pattern.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/chetto1983/aura/internal/breakglass"
	"github.com/chetto1983/aura/internal/config"
)

const recoverOperatorUsage = "usage: aura identity recover-operator [--generate] [--no-recovery]\n" +
	"  offline, host-only operator password reset + session-kill + identity_recovery re-seed (no running server).\n" +
	"  password/answer source: AURA_RECOVERY_PASSWORD / AURA_RECOVERY_QUESTION / AURA_RECOVERY_ANSWER env,\n" +
	"    --generate (one strong random secret printed once to stdout), or a hidden interactive prompt.\n" +
	"  --no-recovery skips the recovery re-seed (online forgot-password stays unavailable until reconfigured).\n" +
	"  requires AURA_AUTHULA_SECRET (64 hex) plus AURA_AUTHULA_DATABASE_URL or AURA_DB_URL for the offline Authula reset."

// identityRecoverOperator implements `aura identity recover-operator`: parse the two
// boolean flags, source the secrets (env / --generate / hidden prompt), build the
// offline Authula CoreServices, and call breakglass.RecoverOperator. Every failure
// path prints a secret-free message to stderr and exits non-zero; the offline provider
// is always Close()d (T-43-09 goleak-clean, even on the error paths after construction).
func identityRecoverOperator(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, args []string) {
	generate, noRecovery, err := parseRecoverOperatorFlags(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, recoverOperatorUsage)
		os.Exit(1)
	}

	sourcer := breakglass.Sourcer{
		Getenv:     os.Getenv,
		IsTTY:      func() bool { return term.IsTerminal(int(os.Stdin.Fd())) },
		ReadHidden: readHiddenFromStdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}
	// Source resolves + validates the secrets (conflict / non-TTY / empty) and, on
	// --generate, writes the ONE secret line to stdout. Its errors never carry a secret.
	secrets, err := sourcer.Source(generate, noRecovery)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// The offline Authula provider needs its HMAC secret + a DSN; webauth forces
	// ?search_path=authula, so the aura DB URL is a valid fallback (mirror authula_seed_e2e.go).
	if cfg.AuthulaSecret == "" {
		fmt.Fprintln(os.Stderr, "recover-operator requires AURA_AUTHULA_SECRET (64 hex) for the offline Authula reset")
		os.Exit(1)
	}
	authulaDSN := cfg.AuthulaDatabaseURL
	if authulaDSN == "" {
		authulaDSN = cfg.DB.URL
	}

	provider, err := breakglass.NewAuthula(authulaDSN, cfg.AuthulaSecret)
	if err != nil {
		// NewAuthula validates the DSN/secret; it never receives the operator password.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = provider.Close() }()

	id, err := breakglass.RecoverOperator(ctx, breakglass.Deps{
		Pool:       pool,
		Core:       provider.CoreServices(),
		NoRecovery: noRecovery,
	}, secrets)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// stdout ok line — names the operator id, NEVER a secret (mirror recovery.go:53-54).
	if noRecovery {
		fmt.Printf("ok: recovered operator %s; sessions invalidated; recovery re-seed SKIPPED\n", id)
		fmt.Fprintln(os.Stderr, "warning: --no-recovery set — online forgot-password stays unavailable until identity_recovery is reconfigured")
		return
	}
	fmt.Printf("ok: recovered operator %s; sessions invalidated; recovery re-seeded\n", id)
}

// parseRecoverOperatorFlags hand-parses the two boolean flags via a std flag.FlagSet
// (flag.ContinueOnError, output discarded so the caller owns the usage print), NOT cobra.
// An unknown flag or any bare positional argument returns an error so the caller prints
// usage and exits non-zero — the password is never accepted as an argv element.
func parseRecoverOperatorFlags(args []string) (generate, noRecovery bool, err error) {
	fs := flag.NewFlagSet("recover-operator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVar(&generate, "generate", false, "generate one strong random secret and print it once to stdout")
	fs.BoolVar(&noRecovery, "no-recovery", false, "skip the identity_recovery re-seed")
	if err = fs.Parse(args); err != nil {
		return false, false, err
	}
	if fs.NArg() > 0 {
		return false, false, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	return generate, noRecovery, nil
}

// readHiddenFromStdin is the real golang.org/x/term-backed ReadHidden seam the Sourcer
// invokes for the interactive path (only after its own canPrompt() TTY gate). It writes
// the prompt to stderr — keeping stdout clean for the single --generate line — reads the
// secret with no echo, and emits a trailing newline so the cursor advances past the entry.
func readHiddenFromStdin(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
