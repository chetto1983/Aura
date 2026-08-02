package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
)

var (
	_ agui.ConversationPurger = conversationPurgeAdapter{}
	_ agui.MemoryPurger       = arcadeMemoryPurgeAdapter{}
)

type conversationPurgeStore interface {
	PurgeConversations(context.Context, string) error
}

type conversationPurgeAdapter struct {
	store conversationPurgeStore
}

func (a conversationPurgeAdapter) PurgeConversations(ctx context.Context, identityID string) error {
	if a.store == nil {
		return fmt.Errorf("conversation purge is not configured")
	}
	return a.store.PurgeConversations(ctx, identityID)
}

// arcadeMemoryPurgeAdapter erases one identity's long-term memory. Memory is one
// ArcadeDB database and one server user per identity, so the purge is two server
// commands rather than a sweep that has to find everything memory ever wrote.
//
// It carries no client for the tenant: the probe below is built per call, because
// its whole purpose is to be REFUSED, and a cached client would prove nothing
// about the credential the server holds now.
type arcadeMemoryPurgeAdapter struct {
	admin *arcadedb.Client
	// base has no credential. The admin one drops; the tenant one is derived per
	// call to be refused. Storing either in base would invite using the wrong one.
	base        arcadedb.Config
	credentials *arcadedb.TenantCredentials
}

// PurgeMemory drops the identity's database and its server user, then PROVES both.
//
// The commands' exit codes are deliberately not the assertion. ArcadeDB refuses a
// drop of something already gone — dropUser throws "User 'x' not found on server"
// — and the saga journal re-runs a step that failed, so a resumed purge would fail
// forever on work it had already finished. Nor is a successful exit proof: it says
// the server accepted a command, not that the data is unreachable. Both legs
// therefore run the command, keep its error only for the message, and assert the
// postcondition by reading the server back.
func (a arcadeMemoryPurgeAdapter) PurgeMemory(ctx context.Context, identityID string) error {
	if a.admin == nil || a.credentials == nil {
		return fmt.Errorf("memory purge is not configured")
	}
	database, err := arcadedb.DatabaseFor(identityID)
	if err != nil {
		return fmt.Errorf("memory purge: %w", err)
	}
	if err := a.dropDatabase(ctx, database); err != nil {
		return err
	}
	return a.dropUser(ctx, database)
}

func (a arcadeMemoryPurgeAdapter) dropDatabase(ctx context.Context, database string) error {
	_, dropErr := a.admin.DropDatabase(ctx, database)
	exists, err := a.admin.DatabaseExists(ctx, database)
	if err != nil {
		return fmt.Errorf("verify memory database %s erased: %w", database, err)
	}
	if exists {
		return fmt.Errorf("verify memory database %s erased: the server still holds it%s",
			database, becauseOf(dropErr))
	}
	return nil
}

// dropUser removes the tenant's server user and proves it by BINDING as that user
// and being refused. ArcadeDB publishes no list-users endpoint, so there is no
// read that answers "does this user exist"; a negative bind is the only proof
// available.
//
// It is not ceremony. The user name and its password are both derived — from the
// identity UUID and from the database name — so a surviving credential regains
// full access the moment that UUID's database comes back, and at least one
// identity UUID is fixed (localSeededIdentityID). A restore from backup or an
// operator re-seed would make a stale credential live again.
func (a arcadeMemoryPurgeAdapter) dropUser(ctx context.Context, database string) error {
	user := arcadedb.TenantUserFor(database)
	dropErr := a.admin.DropUser(ctx, user)

	probeCfg := a.base
	// The database is already gone; this names the credential's own database only
	// so the config reads honestly. The probe hits /api/v1/ready, which is a
	// server endpoint and never dereferences it.
	probeCfg.Database = database
	probeCfg.User = user
	probeCfg.Password = a.credentials.PasswordFor(database)
	probe, err := arcadedb.New(probeCfg)
	if err != nil {
		return fmt.Errorf("build memory credential probe for %s: %w", user, err)
	}
	accepted, err := probe.CredentialAccepted(ctx)
	if err != nil {
		return fmt.Errorf("verify memory credential %s revoked: %w", user, err)
	}
	if accepted {
		return fmt.Errorf("verify memory credential %s revoked: the server still accepts it%s",
			user, becauseOf(dropErr))
	}
	return nil
}

// becauseOf appends a failed command's own error to a postcondition failure. The
// command error alone is not a verdict, but when the postcondition also fails it
// is usually the reason.
func becauseOf(err error) string {
	if err == nil {
		return ""
	}
	return " (drop: " + err.Error() + ")"
}

// buildArcadeMemoryPurger wires the purger, or returns nil when the server-rights
// credential or the tenant derivation secret is missing.
//
// Nil, never a no-op: a purger that silently did nothing would let the identity
// row be deleted — cascading the whole Postgres catalog — while the ArcadeDB
// database survived with no owner row left pointing at it. The de-provisioning
// preflight refuses identity deletion when this is nil, which is the loud failure
// an operator can act on.
func buildArcadeMemoryPurger(cfg *config.Config) agui.MemoryPurger {
	if cfg == nil {
		return nil
	}
	server := cfg.ArcadeDB
	if strings.TrimSpace(server.AdminUser) == "" || strings.TrimSpace(server.AdminPassword) == "" {
		slog.Warn("aura serve: no ArcadeDB server credential — memory purge disabled, de-provisioning will refuse identity deletion")
		return nil
	}
	base := arcadedb.Config{BaseURL: server.BaseURL, Database: server.Database}
	adminCfg := base
	adminCfg.User = server.AdminUser
	adminCfg.Password = server.AdminPassword
	admin, err := arcadedb.New(adminCfg)
	if err != nil {
		slog.Warn("aura serve: ArcadeDB server credential unusable — memory purge disabled", "error", err)
		return nil
	}
	// Without the derivation secret the purge could still drop the user, but it
	// could not prove the drop took — and an unprovable erasure is not one.
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		slog.Warn("aura serve: no ArcadeDB tenant secret — memory purge disabled", "error", err)
		return nil
	}
	return arcadeMemoryPurgeAdapter{admin: admin, base: base, credentials: credentials}
}
