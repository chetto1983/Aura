package runner

import (
	"context"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
)

// ProfileProvider serves the operator profile the runtime applies per turn.
//
// It is an interface here rather than the concrete store so the runner keeps its one-way
// dependency on Postgres through conversations, and so a test can hand it two strings.
type ProfileProvider interface {
	// Timezone is the identity's IANA zone, or "" when it has none.
	Timezone(ctx context.Context, identityID string) string
	// ProfileBlock is the deterministic profile as it rides messages[1], or "".
	ProfileBlock(ctx context.Context, identityID string) string
}

// turnLocation resolves the zone for THIS turn: the identity's own, else the deployment's.
//
// Per identity, not per process, because the timezone belongs to the person. Two identities
// on one daemon can sit in different zones, and until 2026-08-16 both got UTC and the model
// was left to convert -- which it did wrong (it read 15:49:04Z, called Rome "+1 ora" in
// August, and answered with the UTC figure unchanged).
func (r *Runner) turnLocation(ctx context.Context) *time.Location {
	identityID := identityctx.IdentityID(ctx)
	if r.profiles == nil || identityID == "" {
		return r.location
	}
	if zone := strings.TrimSpace(r.profiles.Timezone(ctx, identityID)); zone != "" {
		return tools.LocationOrUTC(zone)
	}
	return r.location
}

// turnProfileBlock is the operator profile for THIS turn, appended to the always-block.
//
// Rendered every turn rather than retrieved: a profile fact competes for rank against real
// memories when it lives in the graph, and a veto the agent only sometimes recalls is not a
// veto. An unavailable store costs the block, never the turn.
func (r *Runner) turnProfileBlock(ctx context.Context) string {
	identityID := identityctx.IdentityID(ctx)
	if r.profiles == nil || identityID == "" {
		return ""
	}
	return strings.TrimSpace(r.profiles.ProfileBlock(ctx, identityID))
}
