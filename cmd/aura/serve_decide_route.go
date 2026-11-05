package main

import "github.com/chetto1983/aura/internal/config"

// bootDecideEnvironment is the decision model's wiring the environment named when the process
// started. Package variables initialise before main, so before the aura.settings overlay: the
// sidecar's address is the installer's, and no row may move it.
var bootDecideEnvironment = config.LoadDecide()
