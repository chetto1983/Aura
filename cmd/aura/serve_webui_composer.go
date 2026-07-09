package main

// serve_webui_composer.go carries the 37D WEBSKILL-01 composer skill-picker parent-mux
// mount, kept OUT of serve_webui.go so that file stays under the 600-LOC ceiling (mirrors
// the serve_webui_voice.go / serve_webui_musr.go extractions). It mounts the one composer
// route registered on the agui Server.Mux (composer_api.go):
//
//   - GET /api/composer/skills — the global active-skills snapshot the composer picker
//     reads. Bare aguiHandler, so it inherits the whole-origin RequireAuth from the
//     parent-mux wrap with NO capability gate (like voiceCapabilitiesRoute / imageProxyRoute
//     / meRoute). This is the deliberate D-03 choice: any authenticated identity populates
//     the picker, so the route MUST NOT wrap RequireCapability(governanceReadCapability) —
//     that is the 403 trap the governance skills board sibling (serve_webui.go) sets, and it
//     would deny an ordinary identity its own picker list.
//
// Method+path-specific patterns win Go 1.22 longest-pattern precedence over the bare "/api/"
// exclusion carve-out and the "/" embed catch-all; the "/api/" fallback exclusion already
// returns it as a backend route (no fallback change needed).

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const composerSkillsRoute = "GET /api/composer/skills"

// registerComposerRoutes mounts the composer read route on the parent mux. It delegates to
// the AG-UI handler (the route lives on Server.Mux) with NO capability gate — RequireAuth-only,
// so an ordinary authenticated identity gets the global picker list (D-03). auth is accepted
// for signature parity with registerVoiceRoutes/registerMUSRRoutes and future gated routes.
func registerComposerRoutes(mux *http.ServeMux, aguiHandler http.Handler, _ agui.AuthDeps) {
	mux.Handle(composerSkillsRoute, aguiHandler)
}
