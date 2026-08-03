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

// memoryEmbedBackfillMaxDuration bounds ONE sweep across all tenants. Embedding costs
// ~45ms per fact in a batch, and the sweep is bounded per tenant
// (arcadedb.backfillRoundsPerTenant), so 5 minutes covers a large backlog while still
// failing a sweep that has hung on an unresponsive sidecar.
const memoryEmbedBackfillMaxDuration = 5 * time.Minute

// MemoryEmbedder is the consumer-declared seam the backfill drives (the SnippetSweeper
// pattern): the live *arcadedb.TenantBackfill satisfies it via EmbedMissing, so this
// package does not import internal/arcadedb. EmbedMissing visits every identity's memory
// database and embeds the facts that have no vector, returning the count embedded.
type MemoryEmbedder interface {
	EmbedMissing(ctx context.Context, now time.Time) (embedded int, err error)
}

// NewMemoryEmbedBackfillHandler builds the sweep that gives the memory embedder its
// scheduled caller: without it a fact written while the embedding sidecar was absent
// or slow stays vector-less forever, and the dense leg of retrieval answers on a
// corpus with holes in it. A nil embedder yields the disabled no-op sweep (harmlessly
// off, not an error). It never reschedules a missed run: the next tick re-evaluates
// the same "has no vector" set, which is the whole due set.
func NewMemoryEmbedBackfillHandler(embedder MemoryEmbedder) Handler {
	var seam sweepFn
	if embedder != nil {
		seam = embedder.EmbedMissing
	}
	return newCountingSweep(KindMemoryEmbedBackfill, memoryEmbedBackfillMaxDuration, seam,
		"memory embed backfill: disabled (no embedder)", "memory embed backfill",
		"memory embed backfill ok: embedded %d fact(s)")
}
