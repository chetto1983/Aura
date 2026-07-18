// mcp_audit_actor.go derives the cli:<os-username> audit-actor principal every mutating
// `aura mcp` CLI subcommand records into aura.mcp_audit.actor_identity_id (D-12/D-13,
// MCPH-07). It is the CLI-side counterpart of the web governance-write path's
// principalIdentityID(r) — the actor is NEVER empty (the column is NOT NULL) and NEVER a
// bare "cli:" (an empty/whitespace username falls through to the next source).
package main

import (
	"os/user"
)

// currentOSUser is a seam over os/user.Current so mcp_audit_actor_test.go can exercise the
// error-fallback branch deterministically, without depending on the real OS environment.
var currentOSUser = user.Current

// mcpAuditActor derives the cli:<os-username> audit actor (D-13).
//
// TODO(RED stub): this minimal compiling body ignores currentOSUser/env entirely; GREEN
// replaces it with the real os/user.Current() -> USER -> USERNAME -> "cli:unknown" chain.
func mcpAuditActor() string {
	return "cli:unknown"
}
