// mcp_audit_actor.go derives the cli:<os-username> audit-actor principal every mutating
// `aura mcp` CLI subcommand records into aura.mcp_audit.actor_identity_id (D-12/D-13,
// MCPH-07), and hosts the single choke-point every mutating verb writes through
// (mcpWriteManagedConfig). It is the CLI-side counterpart of the web governance-write
// path's principalIdentityID(r) — the actor is NEVER empty (the column is NOT NULL) and
// NEVER a bare "cli:" (an empty/whitespace username falls through to the next source).
package main

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/jackc/pgx/v5/pgxpool"
)

// currentOSUser is a seam over os/user.Current so mcp_audit_actor_test.go can exercise the
// error-fallback branch deterministically, without depending on the real OS environment.
var currentOSUser = user.Current

// mcpAuditActor derives the cli:<os-username> audit actor (D-13): os/user.Current()
// succeeding with a non-blank Username wins; on error (or a blank/whitespace-only
// Username) it falls back to USER then USERNAME; with every source absent/blank it
// returns the literal "cli:unknown". Never returns "" and never a bare "cli:".
func mcpAuditActor() string {
	if u, err := currentOSUser(); err == nil {
		if name := strings.TrimSpace(u.Username); name != "" {
			return "cli:" + name
		}
	}
	if name := strings.TrimSpace(os.Getenv("USER")); name != "" {
		return "cli:" + name
	}
	if name := strings.TrimSpace(os.Getenv("USERNAME")); name != "" {
		return "cli:" + name
	}
	return "cli:unknown"
}

// mcpPoolRequiredErr is the pure production-disallow gate (MCPH-07's literal "audited OR
// disallowed" requirement): under server_production a CLI mutation may never proceed
// without a live audited pool. It takes no I/O so it is directly unit-testable
// (TestMCPCLIProdDisallow) independent of any env var or DB state.
func mcpPoolRequiredErr(profile config.RuntimeProfile, action string) error {
	if profile != config.ProfileServerProduction {
		return nil
	}
	return fmt.Errorf("mcp %s: a live audited database pool is required under server_production (no unaudited CLI write is permitted)", action)
}

// mcpAuditInsertFor is the pure MCPAuditInsert builder every mutating verb funnels
// through: the cli:<os-username> actor is always attached here, so any caller of
// mcpWriteManagedConfig gets a correctly-shaped audit row without repeating the
// ActorIdentityID wiring at each of the ~10 call sites.
func mcpAuditInsertFor(action, name, reason string) mcpmanager.MCPAuditInsert {
	return mcpmanager.MCPAuditInsert{
		ActorIdentityID: mcpAuditActor(),
		Action:          action,
		ServerName:      name,
		Reason:          reason,
	}
}

// mcpWriteManagedConfig is the ONE choke point every mutating `aura mcp` CLI verb calls
// (D-12/D-13, MCPH-07): main.go's runMCPDispatch opens a live *pgxpool.Pool for every
// mutating subcommand before it ever reaches here, so in real CLI use this call is
// ALWAYS audited via manager.WriteConfigWithAudit with the cli:<os-username> actor. A nil
// pool can only reach here from a pool-free caller (e.g. a unit test exercising a
// mutating function directly, never main.go's real dispatch) — under server_production
// that is an explicit hard error (mcpPoolRequiredErr); under every other profile it
// degrades to the pre-existing plain atomic write so a pool-free caller still gets its
// config change, just without the audit row. This is the CLI write surface's single
// remaining direct SaveManagedConfig call — deliberately isolated here (not in
// mcp.go/mcp_profile.go) so those files' own mutation bodies read as "always audited."
func mcpWriteManagedConfig(ctx context.Context, pool *pgxpool.Pool, path string, doc mcp.ManagedConfig, action, name, reason string) error {
	if pool == nil {
		profile := config.ParseProfile(os.Getenv("AURA_PROFILE"))
		if err := mcpPoolRequiredErr(profile, action); err != nil {
			return err
		}
		return mcp.SaveManagedConfig(path, doc)
	}
	return mcpmanager.WriteConfigWithAudit(ctx, pool, path, doc, mcpAuditInsertFor(action, name, reason))
}
