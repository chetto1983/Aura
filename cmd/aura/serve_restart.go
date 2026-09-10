package main

import (
	"os"

	"github.com/chetto1983/aura/internal/agui"
)

// wireRestartTrigger lets the web console restart the daemon where a supervisor brings
// it back: the image sets AURA_IN_CONTAINER=1, and compose restarts the container after
// a clean exit unless an operator stopped it. Anywhere else a shutdown is only a stop,
// so the trigger stays unwired and POST /api/admin/restart answers 409.
func wireRestartTrigger(server *agui.Server, requestShutdown func()) {
	if os.Getenv("AURA_IN_CONTAINER") != "1" {
		return
	}
	server.SetRestartTrigger(requestShutdown)
}
