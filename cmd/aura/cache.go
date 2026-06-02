// cache subcommands for `aura cache-stats` (operator-facing, advertised) and the
// hidden `aura cache-audit` (the runtime-faithful KV-prefix invariant gate, D-06).
// Both are top-level main.go switch cases mirroring `aura db`; cache-audit is
// NOT listed in usage() — it is a CI/maintenance gate, not a user command.
//
// The two implementations live in cache_stats.go and cache_audit.go so neither
// file approaches the 600-LOC ceiling. Each exposes a pure core that returns an
// exit code; the os.Exit boundary stays in these thin wrappers so the cores are
// table-testable without a re-exec subprocess.
package main

import (
	"context"
	"os"
)

// runCacheStats is the `aura cache-stats --since=<dur>` entry point.
func runCacheStats(args []string) {
	os.Exit(cacheStatsMain(context.Background(), args, os.Stdout, os.Stderr))
}
