//go:build db_integration

package documents

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedDocumentTestIdentity creates a throwaway identity so a catalog row's identity_id
// foreign key is satisfied without borrowing a real one. Both reasons matter: the seeded
// operator identity is deleted by coverage runs, so a test that assumes it exists fails
// with an unrelated FK violation; and these tiers run against the real deployment, where a
// test document hanging off the operator's identity would surface in their document list.
func seedDocumentTestIdentity(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	name := "documents-test+" + id[:8] + "@aura.local"
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1::uuid, $2, 'service')`, id, name); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id = $1", id)
	})
	return id
}
