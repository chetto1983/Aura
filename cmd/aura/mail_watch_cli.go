// `aura mail-watch shadow --identity=<uuid> [--days=30]` prints the urgency centroid's counts from
// the verdict rows, on the appliance (docs/superpowers/specs/2026-11-03-urgency-centroid-design.md,
// decision 11). It reads, and prints no sender, subject or id: §5.5 keeps the counts where the
// mail is, and no developer reads mail to make them.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/pimwatch"
)

const (
	mailWatchUsage = "usage: aura mail-watch shadow --identity=<uuid> [--days=30]"
	// mailWatchShadowDays is the default window, the verdicts' default retention.
	mailWatchShadowDays = 30
)

func runMailWatch(args []string) {
	os.Exit(mailWatchMain(context.Background(), args, os.Stdout, os.Stderr))
}

// mailWatchMain is the testable core: a usage error exits 64 before any database work.
func mailWatchMain(ctx context.Context, args []string, out, errOut io.Writer) int {
	if len(args) == 0 || args[0] != "shadow" {
		_, _ = fmt.Fprintln(errOut, mailWatchUsage)
		return exitUsage
	}
	identity, days, ok := parseMailWatchShadow(args[1:])
	if !ok {
		_, _ = fmt.Fprintln(errOut, mailWatchUsage)
		return exitUsage
	}
	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	defer pool.Close()
	store := pimwatch.NewStore(pool)
	lastDigest, err := store.LastDigest(ctx, identity)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	now := time.Now().UTC()
	rows, err := store.ShadowRows(ctx, identity, now.AddDate(0, 0, -days))
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	pimwatch.BuildShadowReport(rows, lastDigest, now).WriteText(out)
	return 0
}

// parseMailWatchShadow reads --identity, required, and --days, a positive count.
func parseMailWatchShadow(args []string) (identity string, days int, ok bool) {
	flags := flag.NewFlagSet("mail-watch shadow", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&identity, "identity", "", "")
	flags.IntVar(&days, "days", mailWatchShadowDays, "")
	if err := flags.Parse(args); err != nil || flags.NArg() > 0 {
		return "", 0, false
	}
	identity = strings.TrimSpace(identity)
	return identity, days, identity != "" && days > 0
}
