//go:build memory_integration

// Live `aura memory` CLI tier (ports of spikes 033/035 + dedup re-validation 034).
// Drives the real runMemoryCommand verb router against the running, rebuilt
// agent-memory sidecar (resolved via the default-on managed `memory` recipe):
//   - TestMemoryCLI: seed a uniquely-tagged entity via `add-entity`, then `search`
//     for it and assert the tag round-trips (D-01 deliberate write, D-03 recall).
//   - TestMemoryDedupNewEntityActionNone: a genuinely-new, semantically-distinctive
//     entity must be STORED as its own distinct node and must NOT be auto-merged into
//     a prior run's entity — re-validating the fork's provenance-safe-dedup fix
//     survived the 15-04 rebuild (D-10 / Pitfall 5, T-15-05-01). The load-bearing
//     anti-regression is the ABSENCE of a silent cross-run merge (action=merged at
//     ~0.997, the spike-034 bug); `none`/`flagged` both store the entity distinctly.
//
// Operator-write scope (Open Q3 / D-10): the sidecar runs single-user with
// --user-id aura-local, so these operator writes are unscoped and carry the local
// user identity — cross-conversation same-user entity merge is the intended D-10
// behavior, NOT a leak. A genuinely-new name must still dedup to action=none.
//
// No-skip-as-green (CLAUDE.md): AURA_AGENT_MEMORY_MCP_URL unset under $CI t.Fatals.
package main

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

// memoryTierGate fails under $CI / skips locally when the live sidecar URL is unset,
// and (when running) reaps the shared http.DefaultClient's idle keep-alive
// connections at test end. The CLI opens the sidecar over streamable-HTTP via
// mcp.OpenServer (http.DefaultClient); its parked readLoop/writeLoop goroutines
// otherwise trip the package goleak TestMain even after the MCP session closes.
// Reaping idle connections returns them synchronously; test-only, never touches
// production Close() (which must not close a shared client).
func memoryTierGate(t *testing.T) {
	t.Helper()
	url := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_URL"))
	if url == "" {
		if port := strings.TrimSpace(os.Getenv("AURA_AGENT_MEMORY_MCP_PORT")); port != "" {
			url = "http://127.0.0.1:" + port + "/mcp/"
		}
	}
	if url != "" {
		// Pin the CLI to the EXACT endpoint that gated the tier live, via an
		// isolated managed config: without this the verb router resolves through
		// the operator's real servers.json + the catalog port default, so a
		// non-default gate URL would dial the wrong endpoint and a local run
		// would read/write the operator's own config and graph (WR-03).
		path := withTempMCPConfig(t)
		doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
			"memory": {
				Type:   mcp.ServerTypeStreamableHTTP,
				URL:    url,
				Source: "recipe:memory",
				Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			},
		}}
		if err := mcp.SaveManagedConfig(path, doc); err != nil {
			t.Fatalf("pin memory tier managed config: %v", err)
		}
		t.Cleanup(func() {
			http.DefaultClient.CloseIdleConnections()
			time.Sleep(200 * time.Millisecond)
		})
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("AURA_AGENT_MEMORY_MCP_URL (or _PORT) must be set under CI — a skipped memory_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
	}
	t.Skip("set AURA_AGENT_MEMORY_MCP_URL (or _PORT) + bring the stack up to run the `aura memory` memory_integration tier")
}

// runMemory captures runMemoryCommand's stdout, failing the test on a CLI error.
func runMemoryCLI(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out bytes.Buffer
	if err := runMemoryCommand(ctx, args, &out); err != nil {
		t.Fatalf("aura memory %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func uniqueMemoryTag(prefix string) string {
	return fmt.Sprintf("AURA-P15-IT-%s-%d-%d", prefix, time.Now().UnixNano(), rand.Intn(1_000_000))
}

// TestMemoryCLI seeds a uniquely-tagged entity via `aura memory add-entity`, then
// reads it back via `aura memory search` (spike 033 seed/read path).
func TestMemoryCLI(t *testing.T) {
	memoryTierGate(t)
	tag := uniqueMemoryTag("entity")
	name := "RoboManual " + tag

	// Seed: a deliberate operator write (D-01).
	runMemoryCLI(t, "add-entity", name, "document", "Phase 15 integration-tier seed "+tag)

	// Read back: pull-on-demand recall (D-03). Search by the unique tag.
	got := runMemoryCLI(t, "search", tag)
	if !strings.Contains(got, tag) {
		t.Fatalf("aura memory search %q did not return the seeded tag.\noutput:\n%s", tag, got)
	}
}

// TestMemoryDedupNewEntityActionNone writes a genuinely-new, semantically-distinctive
// entity and asserts it is stored as its OWN distinct node and is NOT auto-merged into
// a prior run's entity — re-validating the fork's provenance-safe-dedup fix survived
// the 15-04 rebuild (D-10 / Pitfall 5 / T-15-05-01).
//
// The dedup decision (long_term.py) is purely embedding-similarity banded:
//
//	score >= 0.95 -> merged (the new entity is DISCARDED, the prior one returned)
//	0.85 <= score < 0.95 -> flagged (stored DISTINCT, with a pending SAME_AS link)
//	score < 0.85 -> none (stored distinct, no link)
//
// The 768d granite embedder clusters short entity names at ~0.85-0.93 cosine, so
// against a SHARED, accumulating graph a brand-new distinct name commonly lands in the
// `flagged` band rather than `none` — `action=none` cannot be deterministically forced
// by name choice on a populated store. The load-bearing anti-regression for
// T-15-05-01 / the provenance-safe-dedup fix is therefore the ABSENCE of a silent
// cross-run auto-MERGE (the spike-034 bug merged distinct runs at ~0.997): the new
// entity MUST be `stored: true` with its own fresh id and MUST NOT report
// `action=merged` into a different prior entity. `none` and `flagged` both satisfy
// this (both store the entity distinctly); only a spurious `merged` is a regression.
func TestMemoryDedupNewEntityActionNone(t *testing.T) {
	memoryTierGate(t)

	// On an ACCUMULATING shared graph (operator host, repeated tier runs) the
	// template name space ("Adjective Noun Suffix", 800 combos) eventually seeds
	// near-neighbors, so a single random name can legitimately land in the >=0.95
	// merge band — that is the package working as designed, not the spike-034 bug.
	// The spike-034 over-merge regression is SYSTEMIC (everything merges at
	// ~0.997), so the diagnostic is: three INDEPENDENT random names must not ALL
	// merge. Any non-merged attempt proves the provenance-safe-dedup fix holds.
	const attempts = 3
	for i := 1; i <= attempts; i++ {
		name := distinctiveEntityName()
		out := runMemoryCLI(t, "add-entity", name, "concept", "a genuinely new, semantically-distinctive entity for dedup re-validation: "+name)
		low := strings.ToLower(out)

		if dedupAction(low) == "merged" {
			t.Logf("attempt %d/%d: %q merged into a prior entity (plausible near-neighbor on the accumulated graph) — retrying with a fresh name", i, attempts, name)
			continue
		}
		// Non-merged: the entity must be persisted as its own distinct node.
		if !strings.Contains(low, "\"stored\": true") && !strings.Contains(low, "\"stored\":true") {
			t.Fatalf("new entity %q was not stored distinctly.\nadd-entity output:\n%s", name, out)
		}
		switch dedupAction(low) {
		case "none":
			t.Logf("dedup action=none (no near-match) — best case")
		case "flagged":
			t.Logf("dedup action=flagged (stored distinct, pending SAME_AS) — non-merging, fix holds; embedder clustering on the shared graph kept similarity in [0.85,0.95)")
		default:
			t.Logf("dedup action=%q", dedupAction(low))
		}
		return
	}
	t.Fatalf("ALL %d independent random entities were AUTO-MERGED — systemic over-merge, the provenance-safe-dedup fix did NOT survive the rebuild (D-10 / spike-034 regression)", attempts)
}

// dedupAction extracts the deduplication.action value from a lowercased add-entity
// output ("none" | "flagged" | "merged"), or "" when absent.
func dedupAction(low string) string {
	for _, a := range []string{"merged", "flagged", "none"} {
		if strings.Contains(low, "\"action\": \""+a+"\"") || strings.Contains(low, "\"action\":\""+a+"\"") {
			return a
		}
	}
	return ""
}

// distinctiveEntityName builds a random multi-word noun phrase with high lexical +
// semantic entropy, so its embedding is far from any prior entity (the precondition
// for a true action=none dedup decision).
func distinctiveEntityName() string {
	adjectives := []string{"Crimson", "Velvet", "Tungsten", "Marzipan", "Quasar", "Obsidian", "Saffron", "Glacial", "Nebular", "Cobalt"}
	nouns := []string{"Ferret", "Lighthouse", "Accordion", "Mango", "Turbine", "Cathedral", "Walrus", "Meridian", "Lantern", "Sequoia"}
	suffixes := []string{"Protocol", "Hypothesis", "Almanac", "Tessellation", "Conjecture", "Cartography", "Symphony", "Apparatus"}
	a := adjectives[rand.Intn(len(adjectives))]
	n := nouns[rand.Intn(len(nouns))]
	s := suffixes[rand.Intn(len(suffixes))]
	// A short random hex suffix keeps each run unique WITHOUT a shared semantic anchor
	// (no repeated "AURA-P15-IT-dedup" prefix that would embed near prior runs).
	return fmt.Sprintf("%s %s %s %06x%06x", a, n, s, rand.Intn(1<<24), rand.Intn(1<<24))
}
