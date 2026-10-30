package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

type fakeTimezones map[string]string

func (f fakeTimezones) Timezone(_ context.Context, identityID string) string { return f[identityID] }

const cronWithoutTZ = `{"action":"schedule","schedule_kind":"cron","cron":"30 7 * * 1-5","kind":"agent_job","payload":{"goal":"Write my daily briefing"},"notify":"stdout"}`

// A schedule with no tz fires at the hour the operator reads, in their own zone, the order the
// interactive turn reads the clock in (runner.turnLocation).
func TestACronWithoutTZRunsInTheIdentitysZone(t *testing.T) {
	t.Parallel()
	ctx := identityctx.WithIdentityID(context.Background(), "identity-ny")
	cases := map[string]struct {
		profiles   taskTimezones
		deployment string
		args       string
		want       string
	}{
		"the identity's zone":                          {fakeTimezones{"identity-ny": "America/New_York"}, "Asia/Tokyo", cronWithoutTZ, "America/New_York"},
		"the deployment's, with no profile zone":       {fakeTimezones{}, "Asia/Tokyo", cronWithoutTZ, "Asia/Tokyo"},
		"the deployment's, past a broken profile zone": {fakeTimezones{"identity-ny": "Not/AZone"}, "Asia/Tokyo", cronWithoutTZ, "Asia/Tokyo"},
		"the last resort, with neither":                {nil, "", cronWithoutTZ, "Europe/Rome"},
		"an explicit tz always wins": {fakeTimezones{"identity-ny": "America/New_York"}, "Asia/Tokyo",
			`{"action":"schedule","schedule_kind":"cron","cron":"30 7 * * 1-5","tz":"Europe/Lisbon","kind":"agent_job","payload":{"goal":"x"},"notify":"stdout"}`, "Europe/Lisbon"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			store := &fakeTaskStore{}
			tool := &TaskTool{Store: store, Timezones: tc.profiles, DeploymentZone: tc.deployment}
			if _, err := tool.Execute(ctx, json.RawMessage(tc.args)); err != nil {
				t.Fatalf("schedule: %v", err)
			}
			if store.created.TZ != tc.want {
				t.Errorf("TZ = %q, want %q", store.created.TZ, tc.want)
			}
		})
	}
}
