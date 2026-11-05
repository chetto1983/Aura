package main

import (
	"sync"

	"github.com/chetto1983/aura/internal/config"
)

// newDecideBoot reads the decision model's wiring from the process environment on its first call
// and returns that same read ever after. The first caller is the boot, after main has loaded .env
// and before the settings overlay runs (resolveConfigAndPoolWithSettings); a package-level
// initialiser would have run before .env. The overlay cannot move AURA_DECIDE_BASE_URL (it copies
// only AllowedKeys into the environment, settings.OverlayEnv), so the address is the installer's.
// AURA_DECIDE_MODEL's row is overlaid, and the route reads it live (settings.DecideRoute): the boot
// read is what the route falls back to once the row is deleted.
func newDecideBoot() func() config.DecideConfig {
	return sync.OnceValue(config.LoadDecide)
}

// bootDecide is the one read the cockpit's sidecar flag and the mail tick's route share.
var bootDecide = newDecideBoot()
