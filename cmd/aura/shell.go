package main

import (
	"context"
	"fmt"
	"os"

	"github.com/chetto1983/aura/internal/identityctx"
)

// runShell starts Aura's primary interactive agent terminal. It deliberately
// reuses the persisted Runner-backed chat REPL: that composition root already
// mounts the full host shell_exec tool plus fs_* tools into the agent toolbelt.
func runShell(args []string) {
	if len(args) != 0 {
		fmt.Fprintln(os.Stderr, "usage: aura shell")
		os.Exit(1)
	}

	ctx := context.Background()
	env := bootChatNamed(ctx, "aura shell")
	defer env.close()

	// Scope the session to the operator, reusing the pool the boot just opened. Without a
	// principal in the context the runner falls back to an identity literally named `local`,
	// which is the pre-Authula seed: an appliance provisioned through onboarding never had
	// one, so `aura shell` died on every real deployment with
	// `resolve owner identity: get identity "local": identity not found`. Resolved AFTER the
	// boot on purpose — config validation (a missing API key above all) must still be the
	// first thing an operator hears about.
	identityID, err := identityctx.OperatorIdentity(ctx, env.pool)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura shell:", err)
		os.Exit(1)
	}
	ctx = identityctx.WithIdentityID(ctx, identityID)

	convID, err := env.run.NewConversation(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura shell:", err)
		os.Exit(1)
	}
	runReplOrExitNamed(ctx, "aura shell", env, convID)
}
