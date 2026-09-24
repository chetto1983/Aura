package handlers

import (
	"context"
	"time"
)

// KindMemoryEmbedBackfill is the system-seeded memory embedding backfill TaskKind.
// Like identity_purge and sandbox_reap it is NOT model-schedulable — the composition
// root seeds it (seedMemoryEmbedBackfillSweep) and the dispatcher routes it here. The
// 0091 migration widened the scheduler_tasks.kind CHECK to admit the row.
const KindMemoryEmbedBackfill TaskKind = "memory_embed_backfill"

// memoryEmbedBackfillMaxDuration is ONE run's budget across all tenants. A backlog it
// does not finish continues on the next run, and the tenant a run starts from rotates, so
// no tenant waits behind another's backlog for ever.
const memoryEmbedBackfillMaxDuration = 5 * time.Minute

// MemoryEmbedder is the consumer-declared seam the backfill drives (the SnippetSweeper
// pattern): the live *arcadedb.TenantBackfill satisfies it via EmbedMissing, so this
// package does not import internal/arcadedb. EmbedMissing visits every identity's memory
// database and re-embeds every memory row not in the daemon's embedding space, returning
// the count embedded.
type MemoryEmbedder interface {
	EmbedMissing(ctx context.Context, now time.Time) (embedded int, err error)
}

// NewMemoryEmbedBackfillHandler builds the sweep that gives the memory embedder its
// scheduled caller: without it a row written while the embedding route was absent or
// slow stays vector-less forever, a route change leaves every vector in the old space,
// and memory stays lexical. A nil embedder yields the disabled no-op sweep (harmlessly
// off, not an error). It never reschedules a missed run: the next tick re-evaluates
// the same "outside the space" set, which is the whole due set.
func NewMemoryEmbedBackfillHandler(embedder MemoryEmbedder) Handler {
	var seam sweepFn
	if embedder != nil {
		seam = embedder.EmbedMissing
	}
	return newCountingSweep(KindMemoryEmbedBackfill, memoryEmbedBackfillMaxDuration, seam,
		"memory embed backfill: disabled (no embedder)", "memory embed backfill",
		"memory embed backfill ok: embedded %d record(s)")
}
