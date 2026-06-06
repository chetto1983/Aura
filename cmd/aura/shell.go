package main

import (
	"context"
	"fmt"
	"os"
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

	convID, err := env.run.NewConversation(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura shell:", err)
		os.Exit(1)
	}
	runReplOrExitNamed(ctx, "aura shell", env, convID)
}
