package main

import (
	"sync"

	"github.com/chetto1983/aura/internal/config"
)

// newDecideBoot reads the decision model's wiring from the process environment on its first call
// and returns that same read ever after. The first caller is the wiring, which runs after main has
// loaded .env; a package-level initialiser would have run before it. The settings overlay cannot
// move AURA_DECIDE_BASE_URL (it copies only AllowedKeys into the environment, settings.OverlayEnv),
// so the address is the installer's whenever it is read. AURA_DECIDE_MODEL may be overlaid, but the
// route reads that row live (settings.DecideRoute), so the boot read is for the address.
func newDecideBoot() func() config.DecideConfig {
	return sync.OnceValue(config.LoadDecide)
}

// bootDecide is the one read the cockpit's sidecar flag and the mail tick's route share.
var bootDecide = newDecideBoot()
