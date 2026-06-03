// sandbox subcommand dispatcher for `aura sandbox sessions {list|terminate|prune}`.
// Lives in package main alongside cmd/aura/main.go's switch case "sandbox",
// mirroring runDB: it loads the LLM-free config, opens a pool, drives the
// sandbox.SessionControl operator control plane (registry reads + docker stop/rm
// via the LookPath-gated fixed-argv dockerCLI, NEVER the socket — D-05), prints a
// human-readable result, and exits non-zero on error.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/sandbox"
)

const sandboxUsage = "usage: aura sandbox sessions {list|terminate <conv_id>|prune}"

func runSandbox(args []string) {
	if len(args) < 1 || args[0] != "sessions" {
		fmt.Fprintln(os.Stderr, sandboxUsage)
		os.Exit(exitUsage)
	}
	sub := args[1:]
	if len(sub) < 1 {
		fmt.Fprintln(os.Stderr, sandboxUsage)
		os.Exit(exitUsage)
	}

	cfg := config.LoadDB()
	ctx := context.Background()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura sandbox:", err)
		os.Exit(exitInfra)
	}
	defer pool.Close()
	ctrl := sandbox.NewSessionControl(sqlc.New(pool))

	switch sub[0] {
	case "list":
		sandboxSessionsList(ctx, ctrl)
	case "terminate":
		sandboxSessionsTerminate(ctx, ctrl, sub[1:])
	case "prune":
		sandboxSessionsPrune(ctx, ctrl)
	default:
		fmt.Fprintln(os.Stderr, sandboxUsage)
		os.Exit(exitUsage)
	}
}

func sandboxSessionsList(ctx context.Context, ctrl *sandbox.SessionControl) {
	rows, err := ctrl.List(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura sandbox:", err)
		os.Exit(exitInfra)
	}
	if len(rows) == 0 {
		fmt.Println("ok: no active sandbox sessions")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tCONVERSATION_ID\tCONTAINER_ID\tSTARTED_AT\tLAST_USED_AT\tSTATUS")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.ID, r.ConversationID, r.ContainerID, r.StartedAt, r.LastUsedAt, r.Status)
	}
	_ = w.Flush()
}

func sandboxSessionsTerminate(ctx context.Context, ctrl *sandbox.SessionControl, args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: aura sandbox sessions terminate <conv_id>")
		os.Exit(exitUsage)
	}
	if err := ctrl.Terminate(ctx, args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "aura sandbox:", err)
		if errors.Is(err, sandbox.ErrSessionNotFound) {
			os.Exit(exitUsage)
		}
		os.Exit(exitInfra)
	}
	fmt.Printf("ok: terminated session for conversation %s\n", args[0])
}

func sandboxSessionsPrune(ctx context.Context, ctrl *sandbox.SessionControl) {
	n, err := ctrl.Prune(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura sandbox:", err)
		os.Exit(exitInfra)
	}
	fmt.Printf("ok: pruned %d session(s)\n", n)
}
