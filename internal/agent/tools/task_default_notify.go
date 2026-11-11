package tools

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
)

// taskDefaults is the operator's default channel, the Profile panel's setting
// (docs/superpowers/specs/2026-11-05-scheduled-delivery-defaults-and-messages-design.md, Part 1).
// onboarding.ProfileStore satisfies it.
type taskDefaults interface {
	DefaultNotify(ctx context.Context, identityID string) string
}

// defaultNotify is the identity's default channel when it is a delivery route, else "". A value
// that is not a route is "not set": the tool then asks, as it does with no default at all.
func (t *TaskTool) defaultNotify(ctx context.Context) string {
	if t.Defaults == nil {
		return ""
	}
	id := identityctx.IdentityID(ctx)
	if id == "" {
		return ""
	}
	route := strings.TrimSpace(t.Defaults.DefaultNotify(ctx, id))
	if !cron.ValidNotifyRoute(route) {
		return ""
	}
	return route
}
