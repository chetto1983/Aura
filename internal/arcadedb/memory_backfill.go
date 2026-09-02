package arcadedb

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// The scheduled caller EmbedMissingFacts never had.
//
// A fact is written without a vector whenever the embedder is absent or merely
// slow, and that is by design: a write must not fail because a sidecar is down.
// What was missing is the other half — something that comes back later and fills
// the gap in. Without it "fail soft" meant "fail forever", and the dense leg of
// retrieval answered on whatever subset of the corpus happened to be embedded at
// write time.
//
// The sweep keys on the ABSENCE OF A VECTOR and nothing else. Not on a session,
// not on a recency window, not on an onboarding marker: those all describe one
// writer's habits, and the corpus has several (the MCP tool, the CLI, onboarding,
// Aura mid-conversation). Absence of a vector is the only condition that is always
// true of the work, which is why it catches every writer without knowing any of
// them.

const (
	// backfillBatch is how many facts one round embeds. The sidecar amortises well —
	// measured on this host a single sentence costs 85-105ms while a batch of nine
	// costs ~400ms (≈45ms each) — so batching is the difference between a sweep that
	// scales and one that does not.
	backfillBatch = 32

	// backfillRoundsPerTenant bounds ONE tenant's share of ONE sweep, so a large
	// backlog cannot starve the tenants queued behind it or overrun the handler's
	// budget. Whatever is left is picked up by the next tick — the sweep is
	// idempotent by construction, since a fact that got its vector is no longer
	// selected.
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
	embedder    Embedder
}

// NewTenantBackfill wires the sweep. base carries the server address only: the
// database is chosen per identity, and a default here would be a fallback that
// writes one tenant's vectors into another's memory.
func NewTenantBackfill(
	identities MemoryIdentities,
	base Config,
	credentials *TenantCredentials,
	embedder Embedder,
) *TenantBackfill {
	return &TenantBackfill{identities: identities, base: base, credentials: credentials, embedder: embedder}
}

// EmbedMissing sweeps every identity and returns how many facts it embedded. The
// sweep clock is unused — "has no vector" is not a question about time, which is
// exactly why this sweep catches stragglers a recency window would miss. The
// argument stays for the cron sweep seam every other sweep shares.
//
// Embedding needs the sidecar, so this sweep additionally requires an embedder —
// unlike LinkMentions below, which does not. See sweepTenants for the rest of the
// tenant-walk contract (skip vs. fail, counts, logging).
func (b *TenantBackfill) EmbedMissing(ctx context.Context, _ time.Time) (int, error) {
	if b == nil || b.embedder == nil {
		return 0, fmt.Errorf("arcadedb: memory embed backfill is not configured")
	}
	return b.sweepTenants(ctx, "embed backfill",
		func(ctx context.Context, client *Client, _ string) (int, error) {
			return embedMissingInBatches(ctx, client.WithEmbedder(b.embedder))
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
	for _, identityID := range identities {
		count, provisioned, err := b.sweepTenant(ctx, identityID, work)
		total += count
		switch {
		case err != nil:
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

// embedMissingInBatches drains one database a batch at a time. A short round means
// there was nothing more to take, so it stops; a full one means there may be, so it
// goes again up to the per-tenant bound.
//
// A round that embeds NOTHING also stops, and that case is not hypothetical: a fact
// whose vector comes back the wrong width is left alone by EmbedMissingFacts, so it
// is selected again on the next round forever. Stopping on zero turns that into a
// bounded no-op instead of a spin.
func embedMissingInBatches(ctx context.Context, client *Client) (int, error) {
	total := 0
	for range backfillRoundsPerTenant {
		written, err := client.EmbedMissingFacts(ctx, backfillBatch)
		total += written
		if err != nil {
			return total, err
		}
		if written < backfillBatch {
			return total, nil
		}
	}
	return total, nil
}
