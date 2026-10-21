//go:build db_integration

package identity

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// createUser runs CreateUserTx in its own transaction and commits it, deleting the identity
// (and its cascaded grants) when the test ends.
func createUser(t *testing.T, pool *pgxpool.Pool, name string, capabilities []string) (string, error) {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE name = $1", name)
	})
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	id, err := CreateUserTx(ctx, tx, name, capabilities)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return id.String(), nil
}

func TestCreateUserTx_WritesUserWithGrants(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	const name = "test_create_user_tx@example.test"
	id, err := createUser(t, pool, name, All())
	if err != nil {
		t.Fatalf("CreateUserTx: %v", err)
	}

	s := New(pool)
	got, err := s.GetIdentityByName(ctx, name)
	if err != nil {
		t.Fatalf("GetIdentityByName: %v", err)
	}
	if got.ID != id || got.Kind != "user" {
		t.Fatalf("identity = {%s %s}, want {%s user}", got.ID, got.Kind, id)
	}
	caps, err := s.ListCapabilities(ctx, id)
	if err != nil {
		t.Fatalf("ListCapabilities: %v", err)
	}
	want := All()
	slices.Sort(caps)
	slices.Sort(want)
	if !slices.Equal(caps, want) {
		t.Fatalf("capabilities = %v, want %v", caps, want)
	}
}

func TestCreateUserTx_DuplicateNameIsUniqueViolation(t *testing.T) {
	pool := migratedPool(t)

	const name = "test_create_user_tx_dup@example.test"
	if _, err := createUser(t, pool, name, nil); err != nil {
		t.Fatalf("first CreateUserTx: %v", err)
	}
	_, err := createUser(t, pool, name, nil)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("second CreateUserTx err = %v, want SQLSTATE 23505", err)
	}
}

func TestCreateUserTx_InvalidCapabilityWritesNothing(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const name = "test_create_user_tx_bad_cap@example.test"
	_, err := createUser(t, pool, name, []string{CapAgentRun, "Not A Capability"})
	if !errors.Is(err, ErrInvalidCapability) {
		t.Fatalf("CreateUserTx err = %v, want ErrInvalidCapability", err)
	}
	if _, err := New(pool).GetIdentityByName(ctx, name); !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("GetIdentityByName err = %v, want ErrIdentityNotFound", err)
	}
}
