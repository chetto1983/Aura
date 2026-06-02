// `aura cache-stats --since=<dur>` — a real time-windowed read of
// aura.cache_metrics (D-06, SC#4). It parses the duration with time.ParseDuration
// (rejecting unparseable input BEFORE any query, T-06-02), windows the metrics on
// ts >= now-d via the 06-03 sqlc queries, and prints a tabwriter table of per-turn
// rows plus a summary line. The cache hit-rate ratio is computed client-side from
// the summed integers with a total_prompt==0 guard (never a SQL float divide); the
// 80% target is displayed, never gated (provider-dependent, PRD OQ4).
package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
)

// cacheStatsUsage is the --since invocation help. The exit-64 usage convention
// (sysexits EX_USAGE) is shared with exec.go's exitUsage.
const cacheStatsUsage = "usage: aura cache-stats --since=<dur>  (e.g. --since=1h, --since=24h)"

// cacheStatsMain is the testable core: it parses args, opens the pool, runs the
// window read, and writes the table. It returns a process exit code instead of
// calling os.Exit so the flag-parse contract is unit-testable.
func cacheStatsMain(ctx context.Context, args []string, out, errOut io.Writer) int {
	since, code := parseSince(args, errOut)
	if code != 0 {
		return code
	}

	cfg := config.LoadDB()
	pool, err := db.Open(ctx, &cfg.DB)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	defer pool.Close()

	store := cachemetrics.New(pool)
	agg, err := store.AggregateSince(ctx, since)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}
	rows, err := store.ListSince(ctx, since)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 1
	}

	writeCacheStats(out, since, rows, agg)
	return 0
}

// parseSince validates the --since flag. A missing or unparseable duration is a
// usage error (exit 64) emitted to errOut BEFORE any DB work (T-06-02).
func parseSince(args []string, errOut io.Writer) (time.Time, int) {
	raw, ok := sinceValue(args)
	if !ok || raw == "" {
		_, _ = fmt.Fprintln(errOut, cacheStatsUsage)
		return time.Time{}, exitUsage
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		_, _ = fmt.Fprintf(errOut, "invalid --since %q: %v\n", raw, err)
		return time.Time{}, exitUsage
	}
	if d <= 0 {
		_, _ = fmt.Fprintf(errOut, "invalid --since %q: window must be positive\n", raw)
		return time.Time{}, exitUsage
	}
	return time.Now().Add(-d), 0
}

// writeCacheStats renders the window: a per-turn detail table then a summary line
// carrying the totals + the client-computed hit-rate (mirrors db.go's tabwriter).
func writeCacheStats(out io.Writer, since time.Time, rows []cachemetrics.Metric, agg cachemetrics.Aggregate) {
	_, _ = fmt.Fprintf(out, "cache metrics since %s (window: %d turn(s))\n", since.UTC().Format(time.RFC3339), agg.Turns)
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "CONVERSATION\tSEQ\tTS\tPROMPT\tCACHED\tHIT-RATE\tCOST_USD")
	for _, r := range rows {
		_, _ = fmt.Fprintf(w, "%s\t%d\t%s\t%d\t%d\t%s\t%.4f\n",
			r.ConversationID, r.Seq, r.TS.UTC().Format(time.RFC3339),
			r.PromptTokens, r.CachedTokens, hitRate(int64(r.CachedTokens), int64(r.PromptTokens)), r.CostUSD)
	}
	_, _ = fmt.Fprintf(w, "TOTAL\t\t\t%d\t%d\t%s\t%.4f\n",
		agg.TotalPromptTokens, agg.TotalCachedTokens,
		hitRate(agg.TotalCachedTokens, agg.TotalPromptTokens), agg.TotalCostUSD)
	_ = w.Flush()
}

// sinceValue extracts the --since flag value, accepting both the `--since=<dur>`
// (equals) and `--since <dur>` (space) forms.
func sinceValue(args []string) (string, bool) {
	const flag = "--since"
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, flag+"="); ok {
			return v, true
		}
		if a == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

// hitRate returns the cached/prompt ratio formatted as a percentage, guarding the
// total_prompt==0 case with "n/a" (never a SQL or float divide-by-zero, T-06-03).
func hitRate(cached, prompt int64) string {
	if prompt <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100*float64(cached)/float64(prompt))
}
