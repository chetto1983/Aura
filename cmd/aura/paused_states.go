// paused-states subcommand dispatcher for `aura paused-states {list|purge}`
// (CORE-02 Req#11 escape hatch). Hand-rolled switch tree mirroring runDB
// (cmd/aura/db.go), NOT cobra — go.mod has no spf13/cobra and the codebase uses
// nested switch dispatchers (the same OQ1 deviation recorded for identity/chat).
//
// `list` shows the most-recent paused_states rows (pending + auto-resolved) with
// their persisted answer; `purge --before <ISO> --confirm` deletes resolved rows
// older than the cutoff, gated by --confirm like dbReset's destructive guard.
package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"text/tabwriter"
	"time"

	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
)

const pausedStatesUsage = "usage: aura paused-states {list|purge --before <ISO8601> --confirm}"

func runPausedStates(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, pausedStatesUsage)
		os.Exit(1)
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "config load:", err)
		os.Exit(1)
	}
	ctx := context.Background()

	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pool.Close()
	store := askuser.New(pool)

	switch args[0] {
	case "list":
		pausedStatesList(ctx, store)
	case "purge":
		pausedStatesPurge(ctx, store, args[1:])
	default:
		fmt.Fprintln(os.Stderr, pausedStatesUsage)
		os.Exit(1)
	}
}

// pausedStatesList prints the most-recent rows with their resolution state +
// auto-terminated/answer content (Req#11 acceptance).
func pausedStatesList(ctx context.Context, store *askuser.Store) {
	records, err := store.ListRecent(ctx, 50)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(records) == 0 {
		fmt.Println("ok: no paused states")
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "TOKEN\tCONVERSATION\tKIND\tSTATE\tANSWER\tQUESTION")
	for _, r := range records {
		state := "pending"
		if r.Resumed {
			state = "resolved"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Token, r.ConversationID, r.Kind, state, truncate(r.ResumedAnswer, 40), truncate(r.Question, 50))
	}
	_ = w.Flush()
}

// pausedStatesPurge deletes resolved rows older than --before <ISO>, gated by
// --confirm (mirrors dbReset's destructive-op guard).
func pausedStatesPurge(ctx context.Context, store *askuser.Store, args []string) {
	before, ok := flagValue(args, "--before")
	if !ok || before == "" {
		fmt.Fprintln(os.Stderr, "purge requires --before <ISO8601>")
		os.Exit(1)
	}
	if !slices.Contains(args, "--confirm") {
		fmt.Fprintln(os.Stderr, "refusing — pass --confirm to delete resolved paused states")
		os.Exit(1)
	}
	cutoff, err := time.Parse(time.RFC3339, before)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --before %q: want ISO8601/RFC3339 (e.g. 2026-01-31T00:00:00Z): %v\n", before, err)
		os.Exit(1)
	}
	if err := store.CleanupResumedOlderThan(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("ok: purged resolved paused states older than %s\n", cutoff.Format(time.RFC3339))
}

// flagValue extracts the value following a `--flag value` token pair from args.
func flagValue(args []string, flag string) (string, bool) {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// truncate clamps a string to n runes for the tabwriter list output.
func truncate(s string, n int) string {
	if s == "" {
		return "-"
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
