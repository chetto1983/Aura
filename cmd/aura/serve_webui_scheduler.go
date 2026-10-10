package main

// serve_webui_scheduler.go carries the GOV-03 scheduler WRITE route wiring, split out of
// serve_webui.go to keep that file under the 600-LOC cap. The routes mount the board's verbs
// (approve / run / pause / resume / cancel / reschedule) behind RequireCapability(governance.
// write) — the SAME gate as the MCP/skills write routes. Each is a SPECIFIC method+path
// sibling under the "/api/" carve-out, which is an exclusion and not a mount: a verb the AG-UI
// mux registers but this file leaves out answers the parent mux's own 404. Go 1.22
// longest-pattern precedence keeps the {id}/<action> patterns authoritative over the {id}
// cancel/edit patterns. Who may act on which task (its owner, or an admin) and the
// system-kind guard live in the handler (internal/agui/governance_write_scheduler.go).

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const (
	governanceSchedApproveRoute = "POST /api/governance/scheduler/{id}/approve"
	governanceSchedRunRoute     = "POST /api/governance/scheduler/{id}/run"
	governanceSchedPauseRoute   = "POST /api/governance/scheduler/{id}/pause"
	governanceSchedResumeRoute  = "POST /api/governance/scheduler/{id}/resume"
	governanceSchedCancelRoute  = "DELETE /api/governance/scheduler/{id}"
	governanceSchedEditRoute    = "PATCH /api/governance/scheduler/{id}"
)

// mountGovernanceSchedulerWriteRoutes interposes the scheduler write verbs with
// RequireCapability(governance.write) and mounts them on the parent mux.
func mountGovernanceSchedulerWriteRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	for _, route := range []string{
		governanceSchedApproveRoute,
		governanceSchedRunRoute,
		governanceSchedPauseRoute,
		governanceSchedResumeRoute,
		governanceSchedCancelRoute,
		governanceSchedEditRoute,
	} {
		mux.Handle(route, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	}
}
