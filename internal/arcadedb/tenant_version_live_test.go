//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/testenv"
	"github.com/google/uuid"
)

// The version floor runs on the tenant client when no admin is configured (cmd/arcadedb-mcp
// can run that way), so a tenant's own credential must be allowed to read the server
// version. ArcadeDB's documentation does not say either way for a non-root user; this
// measures it on the pinned engine.
func TestTenantCredentialReadsTheServerVersionLive(t *testing.T) {
	base := testenv.Require(t, "ARCADEDB_URL")
	ctx := context.Background()
	admin, err := New(Config{
		BaseURL: base, Database: "unused", User: envOr("ARCADEDB_USER", "root"),
		Password: os.Getenv("ARCADEDB_PASSWORD"),
	})
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	credentials := &TenantCredentials{secret: []byte(strings.Repeat("v", 32))}
	identity := uuid.NewString()
	database, err := DatabaseFor(identity)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.DropDatabase(context.Background(), database)
		_ = admin.DropUser(context.Background(), TenantUserFor(database))
	})

	tenant, err := NewTenantClients(Config{BaseURL: base}, admin, nil, credentials).For(ctx, identity)
	if err != nil {
		t.Fatalf("provision the tenant: %v", err)
	}
	if err := tenant.VerifySecureVersion(ctx); err != nil {
		t.Fatalf("a tenant credential cannot read the server version: %v", err)
	}
	if _, err := NewTenantClients(Config{BaseURL: base}, nil, nil, credentials).For(ctx, identity); err != nil {
		t.Fatalf("a resolver without an admin refused the provisioned tenant: %v", err)
	}
}
