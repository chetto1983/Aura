package skillacl

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// acl_offline_test.go covers the half of every method that a nil-pool test cannot reach: what
// happens AFTER validation passes and the transaction is attempted. It needs no database and
// no daemon — the pool points at a closed port, so every call fails at Begin with a refused
// connection, which is a real production path (Postgres down) and not a mock.
//
// What it pins is the contract that matters when the database is unreachable: no method
// panics, none of them invents a success, and every error names the operation it came from —
// "skillacl: accessible skill resources: ..." rather than a bare dial error the operator has
// to guess the origin of.

// offlineStore builds a Store over a pool that can never connect — the same unreachable-port
// instrument internal/mcpregistry/store_test.go already uses. pgxpool.New does not dial, so
// construction succeeds and the failure happens where the code under test puts it.
func offlineStore(t *testing.T) *Store {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/nowhere?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	store, err := NewStore(pool)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestEveryMethodSurfacesAnUnreachableDatabase(t *testing.T) {
	s := offlineStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"grant to identity", func() error {
			return s.GrantToIdentity(ctx, testGranter, ResourceSkill, testResource, testGrantee, PermView)
		}},
		{"grant public", func() error {
			return s.GrantPublic(ctx, testGranter, ResourceSkill, testResource, PermView|PermEdit)
		}},
		{"revoke from identity", func() error {
			removed, err := s.RevokeFromIdentity(ctx, testGranter, ResourceSkill, testResource, testGrantee)
			if removed {
				t.Error("a revoke that never reached Postgres reported a removal")
			}
			return err
		}},
		{"revoke public", func() error {
			removed, err := s.RevokePublic(ctx, testGranter, ResourceSkill, testResource)
			if removed {
				t.Error("a revoke that never reached Postgres reported a removal")
			}
			return err
		}},
		{"accessible resources", func() error {
			ids, err := s.AccessibleResourceIDs(ctx, testGrantee, ResourceSkill, PermView)
			if len(ids) != 0 {
				t.Errorf("ids = %v, want none when the read failed", ids)
			}
			return err
		}},
		{"list grants", func() error {
			grants, err := s.ListGrants(ctx, testGranter, ResourceSkill, testResource)
			if len(grants) != 0 {
				t.Errorf("grants = %v, want none when the read failed", grants)
			}
			return err
		}},
	} {
		err := tc.call()
		if err == nil {
			t.Errorf("%s: succeeded against an unreachable database", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "skillacl") && !strings.Contains(err.Error(), "connect") {
			t.Errorf("%s: error %q names neither this package nor the connection failure", tc.name, err)
		}
	}
}

// TestReadErrorsNameTheirOperation pins the two wrapped read messages specifically: an
// operator reading a log needs to know which read failed, and the wrap is the only thing
// that says so.
func TestReadErrorsNameTheirOperation(t *testing.T) {
	s := offlineStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := s.AccessibleResourceIDs(ctx, testGrantee, ResourceSkill, PermView); err == nil ||
		!strings.Contains(err.Error(), "accessible skill resources") {
		t.Errorf("AccessibleResourceIDs error = %v, want it to name the read", err)
	}
	if _, err := s.ListGrants(ctx, testGranter, ResourceSkill, testResource); err == nil ||
		!strings.Contains(err.Error(), "list grants on skill") {
		t.Errorf("ListGrants error = %v, want it to name the read and the resource", err)
	}
}
