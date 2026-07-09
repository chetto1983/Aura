package agui

import "net/http"

// composer_api.go carries the WEBSKILL-01 composer skill-picker read route. It is the lean
// authenticated sibling of the GOV-02 governance skills board: the SAME global active-skills
// snapshot (loader.List, via SkillsBoardProvider.ActiveSkills), projected with the SAME
// activeSkillRows shape, but mounted behind PLAIN RequireAuth (D-03) instead of
// governance.read — so any authenticated identity can populate the composer picker, while
// only governance-graded identities read the board. One loader snapshot backs the picker,
// the board, and the runtime `skill action=use` (D-04 — no divergence, Pitfall 2). The
// parent-mux mount (bare aguiHandler, RequireAuth-only) lives in cmd/aura/serve_webui_composer.go.

// composerSkillsPath is the composer picker read route, registered on the agui Server mux.
const composerSkillsPath = "GET /api/composer/skills"

// registerComposerRoutes mounts the composer read routes on the agui Server mux, colocated
// with their handlers (called from Mux() beside registerVoiceRoutes/registerGovernanceRoutes).
// The route is NOT capability-gated here; it inherits the whole-origin RequireAuth from the
// parent-mux wrap in cmd/aura (contrast the governance board's RequireCapability).
func (s *Server) registerComposerRoutes(mux *http.ServeMux) {
	mux.HandleFunc(composerSkillsPath, s.handleComposerSkills)
}

// handleComposerSkills serves GET /api/composer/skills (WEBSKILL-01): the global
// active-skills snapshot projected onto the board row shape (name/description/type; the
// body is NEVER carried), read from the SAME provider ActiveSkills the governance board and
// the runtime `skill action=use` resolve against (D-04 — activeSkillRows reused verbatim,
// one source of truth). Unlike handleSkillsList there is no stage switch: the picker is
// active-only. A nil provider → 503 so the client degrades to an empty picker (D-09), never
// a 500.
func (s *Server) handleComposerSkills(w http.ResponseWriter, _ *http.Request) {
	if s.governance.Skills == nil {
		http.Error(w, "skills unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, map[string]any{"skills": activeSkillRows(s.governance.Skills.ActiveSkills())})
}
