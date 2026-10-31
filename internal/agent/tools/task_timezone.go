package tools

import (
	"context"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

// taskTimezones is the identity's own zone, the seam the runner's ProfileProvider also reads.
type taskTimezones interface {
	Timezone(ctx context.Context, identityID string) string
}

// scheduleZone is the zone a schedule with no tz runs in: the identity's own, else the
// deployment's. It is the order runner.turnLocation reads the clock in, so a task fires at the
// hour the operator reads. Empty leaves cron.ParseSchedule's last resort, Europe/Rome. A zone
// that does not load is skipped, as LocationOrUTC skips it for the clock.
func (t *TaskTool) scheduleZone(ctx context.Context) string {
	if t.Timezones != nil {
		if id := identityctx.IdentityID(ctx); id != "" {
			if zone := loadableZone(t.Timezones.Timezone(ctx, id)); zone != "" {
				return zone
			}
		}
	}
	return loadableZone(t.DeploymentZone)
}

// loadableZone is name when it loads, else "". An empty name loads as UTC and stays empty.
func loadableZone(name string) string {
	name = strings.TrimSpace(name)
	if _, err := time.LoadLocation(name); err != nil {
		return ""
	}
	return name
}
