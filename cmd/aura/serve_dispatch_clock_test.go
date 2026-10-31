package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// A scheduled agent's clock reads the identity's zone through the profile store, and the
// deployment's zone when the identity has none.
func TestCronAgentDepsCarryTheClockZones(t *testing.T) {
	t.Parallel()
	deps := newCronAgentDeps(&chatEnv{cfg: &config.Config{Timezone: "Asia/Tokyo"}})
	if deps.Timezones == nil {
		t.Error("the agent_job deps read no identity zone")
	}
	if deps.Location == nil || deps.Location.String() != "Asia/Tokyo" {
		t.Errorf("Location = %v, want the deployment's Asia/Tokyo", deps.Location)
	}
}
