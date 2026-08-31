//go:build db_integration

package db

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"
)

// identityDeleteExceptions are the ONLY references to aura.identities allowed not to
// cascade, each with the reason it is exempt. Everything else must be CASCADE.
//
// 'r' = RESTRICT, 'n' = SET NULL, 'a' = NO ACTION, 'c' = CASCADE (pg_constraint.confdeltype).
var identityDeleteExceptions = map[string]struct {
	rule   byte
	reason string
}{
	// An audit trail is supposed to outlive its subject: cascading it would destroy the
	// record of what someone did as a side effect of deleting them. Still a landmine —
	// arming it means dropping the FK or anonymizing the actor on purge, a retention
	// decision — but a DORMANT one: zero rows and no writer outside tests (2026-08-31).
	"audit_logs.actor_identity_id": {'r', "an audit trail must outlive its subject"},
	// A registered MCP server belongs to the deployment, not to whoever added it, so the
	// server survives and forgets its author.
	"mcp_server.created_by": {'n', "the server outlives its author"},
}

// TestIdentityReferencesCascadeOrAreExplicitlyExempt is the regression this schema
// actually needed. The de-provisioning saga deletes the identity row LAST, after that
// identity's ArcadeDB memory database, Garage bucket and filesystem roots are already
// gone, so a reference that blocks the delete does not protect anything: it leaves a
// half-erased person plus orphans nothing sweeps.
//
// Measured live 2026-08-31, deleting one real identity: telegram_accounts and
// telegram_setup_pending each refused in turn, both had to be removed by hand, and the
// two ArcadeDB tenant databases stranded by that detour had to be dropped by hand too —
// the exact failure this test exists to stop, produced by accident while hitting it.
// Migration 0113 cascaded those two plus the benchmark run lease.
//
// It queries the LIVE schema rather than a migration file on purpose. A text contract
// pins what one file says; only the catalog answers "did anything, ever, add a reference
// that blocks a delete" — which is the question, since the next such FK will arrive in a
// migration nobody thought to check.
func TestIdentityReferencesCascadeOrAreExplicitlyExempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := Open(ctx, &Config{URL: envOrSkip(t, "AURA_DB_URL")})
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	defer pool.Close()

	rows, err := pool.Query(ctx, `
		SELECT c.conrelid::regclass::text AS child,
		       a.attname                  AS column_name,
		       c.confdeltype              AS delete_rule
		FROM pg_constraint c
		JOIN unnest(c.conkey) WITH ORDINALITY AS k(attnum, ord) ON true
		JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = k.attnum
		WHERE c.contype = 'f'
		  AND c.confrelid = (
		      SELECT oid FROM pg_class
		      WHERE relname = 'identities' AND relnamespace = 'aura'::regnamespace)`)
	if err != nil {
		t.Fatalf("read identity foreign keys: %v", err)
	}
	defer rows.Close()

	var offenders, seen []string
	for rows.Next() {
		var child, column string
		var rule byte
		if err := rows.Scan(&child, &column, &rule); err != nil {
			t.Fatalf("scan foreign key: %v", err)
		}
		key := strings.TrimPrefix(child, "aura.") + "." + column
		seen = append(seen, key)
		if rule == 'c' {
			continue
		}
		exempt, ok := identityDeleteExceptions[key]
		if !ok {
			offenders = append(offenders, key+" is "+string(rule)+", want CASCADE")
			continue
		}
		if exempt.rule != rule {
			offenders = append(offenders, key+" is "+string(rule)+", but its exemption declares "+string(exempt.rule))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate foreign keys: %v", err)
	}
	if len(seen) == 0 {
		t.Fatal("no foreign keys reference aura.identities — the query is wrong, not the schema")
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("references to aura.identities that block a delete:\n  %s\n\n"+
			"De-provisioning deletes the identity row LAST, so this fails with the memory "+
			"database and object-store bucket already erased. Cascade it, or add it to "+
			"identityDeleteExceptions with the reason it must outlive its owner.",
			strings.Join(offenders, "\n  "))
	}

	// An exemption for a reference that no longer exists is stale documentation claiming
	// to be a decision; drop it when the column goes.
	for key := range identityDeleteExceptions {
		if !contains(seen, key) {
			t.Errorf("identityDeleteExceptions still lists %q, which no longer references aura.identities", key)
		}
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
