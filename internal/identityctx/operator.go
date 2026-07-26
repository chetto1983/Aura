package identityctx

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ErrOperatorAmbiguous is returned when more than one operator is enrolled. A call that
// carries no principal cannot be attributed to a human by guessing which one, so the
// caller must supply an identity instead of receiving a default.
var ErrOperatorAmbiguous = errors.New("identityctx: more than one operator enrolled; a no-principal call cannot pick one")

// ErrNoOperator is returned when neither an enrolled operator nor the seeded local
// identity exists — the deployment has no one to attribute a no-principal call to.
var ErrNoOperator = errors.New("identityctx: no operator identity exists")

// Querier is the read surface OperatorIdentity needs. Both *pgxpool.Pool and pgx.Tx
// satisfy it.
type Querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// operatorIdentitySQL answers all three questions in one round-trip: how many operators
// are enrolled, which one came first, and whether the seeded local identity still exists.
const operatorIdentitySQL = `
WITH enrolled AS (
    SELECT i.id::text AS id, ial.created_at
    FROM aura.identity_auth_links ial
    JOIN aura.identities i ON i.id = ial.identity_id
    WHERE i.kind = 'user'
)
SELECT (SELECT count(*) FROM enrolled)::int,
       (SELECT id FROM enrolled ORDER BY created_at, id LIMIT 1),
       EXISTS (SELECT 1 FROM aura.identities WHERE id = $1::uuid)
`

// OperatorIdentity resolves who a call with no authenticated principal belongs to.
//
// LocalOperatorIdentity is a seed, not an answer. serve_auth.go retires it the first time
// an operator logs in: it migrates every Postgres reference onto the enrolled identity and
// DELETES the row. Callers that kept using the constant as their fallback were therefore
// attributing data to a tenant that no longer exists — which is why `aura memory facts
// Davide` answered `fact_count: 0` while three facts sat in the graph, owned by the
// enrolled identity. Neither side was broken; they were writing and reading as two
// different people.
//
// aura.identity_auth_links is the authoritative answer: one row per enrolled operator.
// Exactly one means an unambiguous human. None means nobody has enrolled yet, the seed has
// not been retired, and it is still the right owner. More than one means the deployment is
// multi-user, and guessing is worse than failing.
func OperatorIdentity(ctx context.Context, q Querier) (string, error) {
	if q == nil {
		return "", fmt.Errorf("resolve operator identity: %w", ErrNoOperator)
	}
	var enrolled int
	var firstEnrolled *string
	var seedExists bool
	if err := q.QueryRow(ctx, operatorIdentitySQL, LocalOperatorIdentity).
		Scan(&enrolled, &firstEnrolled, &seedExists); err != nil {
		return "", fmt.Errorf("resolve operator identity: %w", err)
	}
	switch {
	case enrolled > 1:
		return "", ErrOperatorAmbiguous
	case enrolled == 1 && firstEnrolled != nil:
		return *firstEnrolled, nil
	case seedExists:
		return LocalOperatorIdentity, nil
	default:
		return "", ErrNoOperator
	}
}
