package main

// serve_webui_scheduler.go carries the GOV-03 scheduler WRITE route wiring, split out of
// serve_webui.go to keep that file under the 600-LOC cap. The routes mount the operator
// management verbs (approve / run / cancel / reschedule) behind RequireCapability(governance.
// write) — the SAME gate as the MCP/skills write routes. Each is a SPECIFIC method+path
// sibling under the "/api/" carve-out; Go 1.22 longest-pattern precedence keeps the
// {id}/approve + {id}/run action patterns authoritative over the {id} cancel/edit patterns.
// The system-kind guard (a system-seeded sweep is never operator-mutable) lives in the
// handler (internal/agui/governance_write_scheduler.go), not the route.

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const (
	governanceSchedApproveRoute = "POST /api/governance/scheduler/{id}/approve"
	governanceSchedRunRoute     = "POST /api/governance/scheduler/{id}/run"
	governanceSchedCancelRoute  = "DELETE /api/governance/scheduler/{id}"
	governanceSchedEditRoute    = "PATCH /api/governance/scheduler/{id}"
)

// mountGovernanceSchedulerWriteRoutes interposes the four scheduler write verbs with
// RequireCapability(governance.write) and mounts them on the parent mux.
func mountGovernanceSchedulerWriteRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	for _, route := range []string{
		governanceSchedApproveRoute,
		governanceSchedRunRoute,
		governanceSchedCancelRoute,
		governanceSchedEditRoute,
	} {
		mux.Handle(route, agui.RequireCapability(aguiHandler, auth, governanceWriteCapability))
	}
}
