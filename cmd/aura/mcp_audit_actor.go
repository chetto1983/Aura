// mcp_audit_actor.go derives the cli:<os-username> audit-actor principal every mutating
// `aura mcp` CLI subcommand records into aura.mcp_audit.actor_identity_id (D-12/D-13,
// MCPH-07). It is the CLI-side counterpart of the web governance-write path's
// principalIdentityID(r) — the actor is NEVER empty (the column is NOT NULL) and NEVER a
// bare "cli:" (an empty/whitespace username falls through to the next source).
package main

import (
	"os"
	"os/user"
	"strings"
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
