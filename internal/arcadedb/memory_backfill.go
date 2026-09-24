package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// The scheduled pass that keeps every tenant's memory in the daemon's embedding space.
//
// A row is written without a vector whenever the embedder is absent or merely slow, and
// that is by design: a write must not fail because a sidecar is down. And after a route
// change every stored vector is in the old space. Both leave work behind, and without
// something that comes back later "fail soft" meant "fail forever", while the dense leg
// answered on whatever subset happened to be embedded right.
//
// The sweep keys on the row NOT BEING IN THE DAEMON'S SPACE and nothing else (spec §5).
// That covers the facts the old key, the absence of a vector, selected, and the rows a
// route change left behind. Not a session, not a recency window, not an onboarding marker:
// those describe one writer's habits, and the corpus has several (the MCP tool, the CLI,
// onboarding, Aura mid-conversation). A stamp outside the space is always true of the
// work, which is why it catches every writer without knowing any of them.

const (
	// backfillBatch is how many facts one round embeds. The sidecar amortises well —
	// measured on this host a single sentence costs 85-105ms while a batch of nine
	// costs ~400ms (≈45ms each) — so batching is the difference between a sweep that
	// scales and one that does not.
	backfillBatch = 32

	// backfillRoundsPerTenant bounds ReEmbedAllFacts, the operator's same-space repair.
	// The sweep is bounded by its run budget and the rotation of the tenant it starts
	// from, not by rounds: a route change leaves a whole memory behind, and a round cap
	// would keep a large tenant lexical for many runs.
	backfillRoundsPerTenant = 20
)

// MemoryIdentities is the consumer-declared seam over the identity roster: the
// live *identity.Store satisfies it at the composition root, so this package does
// not import Postgres to know who exists.
type MemoryIdentities interface {
	IdentityIDs(ctx context.Context) ([]string, error)
}

// TenantBackfill walks every identity's memory database and runs a sweep against
// each one. EmbedMissing and LinkMentions are two such sweeps: they share the
// identical tenant walk (sweepTenants/sweepTenant below) — enumerate identities,
// build a per-tenant *Client with that tenant's derived credential, skip a tenant
// that has no memory yet — and differ only in the per-tenant work they run.
//
// Memory is one database per identity (tenant.go), so a sweep that held a single
// client would only ever fix whichever tenant it happened to point at. It
// therefore enumerates identities and visits each one's database with that
// tenant's own derived credential — the same credential the sidecar provisions
// with, so the server still refuses anything out of scope.
type TenantBackfill struct {
	identities  MemoryIdentities
	base        Config
	credentials *TenantCredentials
	embedder    DenseEmbedder
	rotation    atomic.Uint64
}

// NewTenantBackfill wires the sweep. base carries the server address only: the
// database is chosen per identity, and a default here would be a fallback that
// writes one tenant's vectors into another's memory.
func NewTenantBackfill(
	identities MemoryIdentities,
	base Config,
	credentials *TenantCredentials,
	embedder DenseEmbedder,
) *TenantBackfill {
	return &TenantBackfill{identities: identities, base: base, credentials: credentials, embedder: embedder}
}

// EmbedMissing runs the pass (memory_embed_pass.go) over every identity's memory and
// returns how many vectors it wrote. Its name is the cron seam's (handlers.MemoryEmbedder),
// and so is the unused clock: "outside the space" is not a question about time.
func (b *TenantBackfill) EmbedMissing(ctx context.Context, _ time.Time) (int, error) {
	if b == nil || b.embedder == nil {
		return 0, fmt.Errorf("arcadedb: memory embed backfill is not configured")
	}
	// No space, no pass: a hosted route without its key, or a sidecar that cannot name its
	// model, would fail every tenant the same way (spec §5).
	if _, err := b.embedder.Space(ctx); err != nil {
		return 0, fmt.Errorf("arcadedb: memory re-embed cannot run: %w", err)
	}
	return b.sweepTenants(ctx, "embed backfill",
		func(ctx context.Context, client *Client, database string) (int, error) {
			tally, err := client.WithEmbedder(b.embedder).reembedMemory(ctx)
			if tally.refused > 0 {
				slog.Warn("memory re-embed: records set aside, refused by the embedding model or with no text",
					"database", database, "refused", tally.refused)
			}
			if tally.failed > 0 {
				slog.Warn("memory re-embed: rows the store would not take (tried again next run)",
					"database", database, "failed", tally.failed, "first", tally.firstFailed, "error", tally.failCause)
			}
			return tally.embedded, err
		})
}

// LinkMentions sweeps every identity and returns how many MENTIONS edges changed
// (created plus removed). It runs as a sweep, never a write hook, for the same
// reason (*Client).LinkMentions does (memory_mentions_link.go): the hub cap is a
// property of the WHOLE corpus, so no single write can decide an edge on its own.
//
// Unlike EmbedMissing, linking is pure text matching against facts already in
// memory — it needs no sidecar, so its configuration guard does not require an
// embedder.
func (b *TenantBackfill) LinkMentions(ctx context.Context, _ time.Time) (int, error) {
	return b.sweepTenants(ctx, "mention link",
		func(ctx context.Context, client *Client, database string) (int, error) {
			result, err := client.LinkMentions(ctx)
			if err != nil {
				return 0, err
			}
			if !result.Covered {
				// A partial scan describes only a prefix of this tenant's memory, not
				// the whole of it — see MentionLinkResult.Covered.
				slog.Warn("memory mention link: corpus larger than one scan", "database", database)
			}
			return result.Created + result.Removed, nil
		})
}

// sweepTenants is the tenant walk EmbedMissing and LinkMentions share. work does
// one tenant's share of the sweep and returns how much it changed; sweep names
// the caller only for the log lines, so the two sweeps stay distinguishable.
//
// A tenant with no memory yet is SKIPPED, not failed: databases are provisioned
// lazily on first use, so a registered identity that has never stored a fact
// legitimately has neither database nor credential. A tenant that fails for any
// other reason is logged and the sweep continues to the next one — one broken
// tenant must not stop the other tenants' work. If NO tenant could be swept and
// at least one failed hard, that error is returned, so a misconfiguration (wrong
// tenant secret, unreachable server) is reported by the scheduler instead of
// looking like an empty, healthy sweep.
//
// The walk starts one identity later on each run, and a run whose budget ends mid-walk
// reports what it did rather than failing. A failure of the embedding route itself ends the
// walk: it is not the tenant's, and every tenant after it would fail the same way (spec §5).
func (b *TenantBackfill) sweepTenants(
	ctx context.Context,
	sweep string,
	work func(ctx context.Context, client *Client, database string) (int, error),
) (int, error) {
	if b == nil || b.identities == nil || b.credentials == nil {
		return 0, fmt.Errorf("arcadedb: memory %s sweep is not configured", sweep)
	}
	identities, err := b.identities.IdentityIDs(ctx)
	if err != nil {
		return 0, fmt.Errorf("arcadedb: list identities for %s: %w", sweep, err)
	}
	total, swept, skipped := 0, 0, 0
	var firstErr error
	for _, identityID := range rotated(identities, b.rotation.Add(1)-1) {
		if ctx.Err() != nil {
			slog.Info("memory "+sweep+": run budget reached; the rest resumes next run",
				"count", total, "tenants", swept)
			return total, nil
		}
		count, provisioned, err := b.sweepTenant(ctx, identityID, work)
		total += count
		switch {
		case err != nil:
			if ctx.Err() != nil {
				continue
			}
			if errors.Is(err, errEmbeddingRoute) {
				slog.Warn("memory "+sweep+": the embedding route failed; the run ends here",
					"count", total, "tenants", swept, "error", err)
				return total, err
			}
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("memory "+sweep+": tenant failed (retried next sweep)", "error", err)
		case !provisioned:
			skipped++
		default:
			swept++
		}
	}
	if swept == 0 && firstErr != nil {
		return total, firstErr
	}
	slog.Info("memory "+sweep,
		"count", total, "tenants", swept, "without_memory", skipped, "identities", len(identities))
	return total, nil
}

// sweepTenant runs work against one identity's memory database. The bool reports
// whether the tenant HAS memory: false means it has never been provisioned, which
// is a skip rather than an error.
func (b *TenantBackfill) sweepTenant(
	ctx context.Context,
	identityID string,
	work func(ctx context.Context, client *Client, database string) (int, error),
) (int, bool, error) {
	database, err := DatabaseFor(identityID)
	if err != nil {
		return 0, false, fmt.Errorf("memory backfill: %w", err)
	}
	cfg := b.base
	cfg.Database = database
	cfg.User = TenantUserFor(database)
	cfg.Password = b.credentials.PasswordFor(database)
	client, err := New(cfg)
	if err != nil {
		return 0, false, fmt.Errorf("memory backfill for %s: %w", database, err)
	}
	// The existence probe is a BIND, not a database read. A tenant's database and
	// its server user are created together, so a refused credential is the exact,
	// documented signal that this identity has no memory yet — and unlike matching
	// on a query's error text it cannot mistake a syntax error for an absence.
	// (The (bool, error) split is load-bearing: false is REFUSED, an error is
	// "unknown", and a server that is merely down must not read as "no memory".)
	provisioned, err := client.CredentialAccepted(ctx)
	if err != nil {
		return 0, false, fmt.Errorf("memory backfill for %s: %w", database, err)
	}
	if !provisioned {
		return 0, false, nil
	}
	count, err := work(ctx, client, database)
	if err != nil {
		return count, true, fmt.Errorf("memory backfill for %s: %w", database, err)
	}
	return count, true, nil
}

// rotated starts the walk one identity later on each run, so a tenant whose backlog outlasts
// one run's budget cannot starve the ones behind it.
func rotated(ids []string, turn uint64) []string {
	if len(ids) == 0 {
		return ids
	}
	start := int(turn % uint64(len(ids)))
	return append(append(make([]string, 0, len(ids)), ids[start:]...), ids[:start]...)
}
