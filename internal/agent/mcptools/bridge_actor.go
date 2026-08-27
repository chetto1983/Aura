package mcptools

import (
	"context"

	"github.com/chetto1983/aura/internal/agent/tools"
)

const (
	actorRoleParent = "parent"
	actorRoleWorker = "worker"

	// actorRunIDHeader/actorRoleHeader are the connection-scoped HTTP headers
	// cmd/arcadedb-mcp reads beside the bearer's `sub` (D-10, Phase 51) --
	// never MCP _meta (bridge_identity.go's IdentityBindingMiddleware
	// deliberately writes nothing proprietary there), and never a tool
	// argument the model supplies. Mirrors LibreChat's per-request header
	// substitution for user identity (packages/api/src/utils/env.ts,
	// processUserPlaceholders' isHeader branch), applied here to an actor
	// instead of a user.
	actorRunIDHeader = "X-Aura-Actor-Run-Id"
	actorRoleHeader  = "X-Aura-Actor-Role"
)

// mcpActor identifies who is making an outbound MCP call, host-derived from
// ctx so the model can never assert it:
//
//   - Role is tools.IsDelegatedDispatch(ctx) -- set ONLY by internal/swarm's
//     runChild on a worker's own InvocationContext, absent everywhere else
//     (relocated from internal/gateway to internal/agent/tools so this
//     package can read it without an import cycle: internal/gateway already
//     imports internal/agent/mcptools via classify.go).
//   - RunID is tools.RequestIDFromContext(ctx), the SAME per-invocation id
//     tools.WithRequestID already stamps at the top of every agent run --
//     parent turn or worker -- never minted separately here (D-00: mirrors
//     LibreChat's tenantContext ambient actor; Aura's equivalent is
//     context.Context itself).
type mcpActor struct {
	RunID string
	Role  string
}

func actorFromContext(ctx context.Context) mcpActor {
	role := actorRoleParent
	if tools.IsDelegatedDispatch(ctx) {
		role = actorRoleWorker
	}
	return mcpActor{RunID: tools.RequestIDFromContext(ctx), Role: role}
}

// actorSessionKey extends identitySessionPool's key from bare identity to
// identity+actor, but ONLY for a delegated (worker) dispatch. The parent's own
// session stays keyed by identity alone, unchanged in cost and lifecycle:
// SWARM-07/D-10's hazard is a WORKER's write being misattributed, not the
// operator's own, so only the worker path pays for a fresh, per-worker-run
// session -- an identity with no active worker sees zero behavior change.
func actorSessionKey(owner string, actor mcpActor) string {
	if actor.Role != actorRoleWorker || actor.RunID == "" {
		return owner
	}
	return owner + "|actor|" + actor.RunID
}

// actorHeaders is what an outbound MCP request carries for its actor. Empty
// when no run id was derivable (e.g. a mount-time ctx with no turn yet
// active, or a call outside any agent run), so the request still goes out --
// the write boundary (cmd/arcadedb-mcp) is what enforces "no actor, no
// write", not the transport.
func actorHeaders(actor mcpActor) map[string]string {
	if actor.RunID == "" {
		return nil
	}
	return map[string]string{actorRunIDHeader: actor.RunID, actorRoleHeader: actor.Role}
}

// actorHeaderFunc is internal/mcp.SessionOptions.HeaderFunc's value for every
// identity-scoped mount: internal/mcp stays actor-oblivious (it only knows
// "call this function with the request's own ctx, apply what it returns"),
// while this package owns what an actor IS.
//
// Read per REQUEST, not fixed once when a session opens (mount.go). That
// distinction is load-bearing: the operator's own identity session is shared
// and long-lived, reused across MANY different turns, each with its OWN
// accurate host-derived actor -- a header baked in at connect time would be
// accurate for the first turn and silently stale for every one after it. A
// per-actor WORKER session (bridge_identity_sessions.go's actorSessionKey)
// still benefits from this: it does not need a separate static-header path
// of its own, since this function derives the same answer correctly whether
// the session it rides is shared or dedicated.
func actorHeaderFunc(ctx context.Context) map[string]string {
	return actorHeaders(actorFromContext(ctx))
}
