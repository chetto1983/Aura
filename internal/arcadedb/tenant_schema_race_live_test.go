//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// aura serve builds a resolver per subsystem (tenant reconcile, conversation projection,
// document index) and the memory sidecar has its own, so a new tenant's first boot opens it
// from several resolvers at once. The per-resolver gate cannot see the others. Measured on
// 2026-10-10: the boot reconcile lost the race with
// "Cannot create the property 'name' in type 'Entity' because it already exists" and aura
// serve exited.
func TestIndependentResolversOpenANewTenantTogetherLive(t *testing.T) {
	base := os.Getenv("ARCADEDB_URL")
	if base == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("ARCADEDB_URL must be set in CI: a skipped integration tier is a falsely-green job")
		}
		t.Skip("ARCADEDB_URL not set")
	}
	admin, err := New(Config{
		BaseURL: base, Database: "unused", User: envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	credentials := &TenantCredentials{secret: []byte(strings.Repeat("r", 32))}

	const rounds, resolvers = 5, 4
	for range rounds {
		identity := uuid.NewString()
		database, err := DatabaseFor(identity)
		if err != nil {
			t.Fatalf("DatabaseFor: %v", err)
		}
		t.Cleanup(func() {
			_, _ = admin.DropDatabase(context.Background(), database)
			_ = admin.DropUser(context.Background(), TenantUserFor(database))
		})

		start := make(chan struct{})
		errs := make(chan error, resolvers)
		var wait sync.WaitGroup
		for range resolvers {
			resolver := NewTenantClients(Config{BaseURL: base}, admin, nil, credentials)
			wait.Go(func() {
				<-start
				_, err := resolver.For(context.Background(), identity)
				errs <- err
			})
		}
		close(start)
		wait.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Errorf("tenant %s: %v", identity, err)
			}
		}
	}
}
