package handlers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/identityctx"
)

type fakeJobTimezones map[string]string

func (f fakeJobTimezones) Timezone(_ context.Context, identityID string) string { return f[identityID] }

// A scheduled agent reads the clock in the identity's zone, then the deployment's: the order the
// interactive turn reads it in (runner.turnLocation). Measured 2026-08-16: a model handed a UTC
// clock answered Rome's time wrong.
func TestAScheduledAgentReadsTheClockInTheIdentitysZone(t *testing.T) {
	t.Parallel()
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	newYork, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct {
		profiles fakeJobTimezones
		want     *time.Location
	}{
		"the identity's zone":                    {fakeJobTimezones{"identity-ny": "America/New_York"}, newYork},
		"the deployment's, with no profile zone": {fakeJobTimezones{}, tokyo},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			payload, err := json.Marshal(agentJobPayload{Goal: "Write my daily briefing"})
			if err != nil {
				t.Fatal(err)
			}
			client := agenttest.NewFakeClient(agenttest.TextChunks("stop", "briefing"))
			h := AgentJobHandler{Deps: AgentDeps{Client: client, Registry: jobRegistry(), Timezones: tc.profiles, Location: tokyo}}
			ctx := identityctx.WithIdentityID(context.Background(), "identity-ny")
			if _, err := h.Run(ctx, Job{Payload: payload, StepBudget: 4, RunID: "run-clock"}); err != nil {
				t.Fatal(err)
			}
			var sent strings.Builder
			for _, m := range client.RecordedRequests()[0].Messages {
				sent.WriteString(m.Content)
			}
			offset := time.Now().In(tc.want).Format("-07:00")
			if !strings.Contains(sent.String(), offset) {
				t.Errorf("no clock at %s (%s) in the request", tc.want, offset)
			}
		})
	}
}
