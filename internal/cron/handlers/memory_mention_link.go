package handlers

import (
	"context"
	"time"
)

// KindMemoryMentionLink is the system-seeded memory mention-link TaskKind. Like
// memory_embed_backfill it is NOT model-schedulable — the composition root seeds it
// (seedMemoryMentionLinkSweep) and the dispatcher routes it here. The 0114 migration
// widened the scheduler_tasks.kind CHECK to admit the row.
const KindMemoryMentionLink TaskKind = "memory_mention_link"

// memoryMentionLinkMaxDuration bounds ONE sweep across all tenants. Linking is text
// matching plus edge writes with no sidecar in the path, and each tenant's pass is
// bounded by one corpus scan, so it is far cheaper than the embed sweep's 5 minutes —
// but it still writes per edge, so it is not instant. 2 minutes covers a large corpus
// while still failing a sweep that has hung.
const memoryMentionLinkMaxDuration = 2 * time.Minute

// MemoryMentionLinker is the consumer-declared seam the mention-link sweep drives (the
// SnippetSweeper pattern): the live *arcadedb.TenantBackfill satisfies it via
// LinkMentions, so this package does not import internal/arcadedb. LinkMentions visits
// every identity's memory database and rebuilds that identity's MENTIONS edges,
// returning how many edges changed (created plus removed).
type MemoryMentionLinker interface {
	LinkMentions(ctx context.Context, now time.Time) (changed int, err error)
}

// NewMemoryMentionLinkHandler builds the sweep that gives the mention linker its
// scheduled caller: a MENTIONS edge is only decidable against the whole corpus (the hub
// cap excludes an entity mentioned by more than a configured share of all facts), so no
// single fact-write can evaluate it — without this sweep, facts written after the last
// run stay unreachable from their neighbours. A nil linker yields the disabled no-op
// sweep (harmlessly off, not an error). It never reschedules a missed run: the next
// tick rebuilds the same edges from the same corpus, which is the whole due set.
func NewMemoryMentionLinkHandler(linker MemoryMentionLinker) Handler {
	var seam sweepFn
	if linker != nil {
		seam = linker.LinkMentions
	}
	return newCountingSweep(KindMemoryMentionLink, memoryMentionLinkMaxDuration, seam,
		"memory mention link: disabled (no linker)", "memory mention link",
		"memory mention link ok: changed %d edge(s)")
}
