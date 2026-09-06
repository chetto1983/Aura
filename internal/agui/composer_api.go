package agui

import (
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
)

// composer_api.go carries the WEBSKILL-01 composer skill-picker read route. It is the lean
// authenticated sibling of the GOV-02 governance skills board: the SAME caller-scoped
// active-skills snapshot (loader.List, via SkillsBoardProvider.ActiveSkills), projected with the SAME
// activeSkillRows shape, but mounted behind PLAIN RequireAuth (D-03) instead of
// governance.read — so any authenticated identity can populate the composer picker, while
// only governance-graded identities read the board. One loader snapshot backs the picker,
// the board, and the runtime `skill action=use` (D-04 — no divergence, Pitfall 2). The
// parent-mux mount (bare aguiHandler, RequireAuth-only) lives in cmd/aura/serve_webui_composer.go.

// composerSkillsPath is the composer picker read route, registered on the agui Server mux.
const composerSkillsPath = "GET /api/composer/skills"

// composerReasoningCapsPath is the 37E composer reasoning-effort capability read route
// (WEBMODEL-01 / D-13): the levels the active model advertises, so the Composer renders exactly
// those (never a placebo). Same plain-RequireAuth posture as the skills picker (D-03).
const composerReasoningCapsPath = "GET /api/composer/reasoning-capabilities"

// registerComposerRoutes mounts the composer read routes on the agui Server mux, colocated
// with their handlers (called from Mux() beside registerVoiceRoutes/registerGovernanceRoutes).
// The routes are NOT capability-gated here; they inherit the whole-origin RequireAuth from the
// parent-mux wrap in cmd/aura (contrast the governance board's RequireCapability).
func (s *Server) registerComposerRoutes(mux *http.ServeMux) {
	mux.HandleFunc(composerSkillsPath, s.handleComposerSkills)
	mux.HandleFunc(composerReasoningCapsPath, s.handleReasoningCapabilities)
}

// reasoningCapabilitiesDTO is the composer reasoning-effort capability surface (37E / D-13). It
// carries ONLY the allowed UI symbols + the default + the non-secret backend kind + a detected
// flag — NEVER the model id, base URL, or API key (Threat T-37E-06-LEAK). Levels are always
// auto-first; off is present only when the model advertises it (a mandatory model omits it).
type reasoningCapabilitiesDTO struct {
	Levels   []string `json:"levels"`
	Default  string   `json:"default"`
	Backend  string   `json:"backend"`
	Detected bool     `json:"detected"`
}

// handleReasoningCapabilities serves GET /api/composer/reasoning-capabilities (WEBMODEL-01): the
// active model's advertised effort levels mapped to UI symbols (auto prepended; none→off,
// low→low, medium→mid, high→high, xhigh→extra, max→max), read from the SAME TTL-cached
// ReasoningCapabilitySource the /agent/run Stage-2 validator uses (no per-request fetch). A nil
// source OR a failed detection degrades to the safe floor {auto,off} at HTTP 200 (NOT 503) — the
// composer must degrade, never break (37D D-09) — and Stage-2 rejects any graduated level while
// undetected, so the floor is honest.
func (s *Server) handleReasoningCapabilities(w http.ResponseWriter, r *http.Request) {
	caps, backend := s.reasoningCapabilitySnapshot()
	if caps == nil {
		writeJSON(w, reasoningCapabilitiesDTO{Levels: []string{"auto", "off"}, Default: "auto", Backend: backend, Detected: false})
		return
	}
	efforts, _, detected := caps.AllowedEfforts(r.Context())
	if !detected {
		writeJSON(w, reasoningCapabilitiesDTO{Levels: []string{"auto", "off"}, Default: "auto", Backend: backend, Detected: false})
		return
	}
	levels := make([]string, 0, len(efforts)+1)
	levels = append(levels, "auto")
	for _, e := range efforts {
		if sym := effortToUISymbol(e); sym != "" {
			levels = append(levels, sym)
		}
	}
	writeJSON(w, reasoningCapabilitiesDTO{Levels: levels, Default: "auto", Backend: backend, Detected: true})
}

// effortToUISymbol maps an internal llm.ReasoningEffort to the Composer UI symbol (the inverse
// of parseEffortSymbol's fixed-level cases). An unmodelled effort (e.g. minimal) maps to "" and
// is dropped by the caller, so the endpoint never emits a symbol the selector cannot render.
func effortToUISymbol(e llm.ReasoningEffort) string {
	switch e {
	case llm.ReasoningEffortNone:
		return "off"
	case llm.ReasoningEffortLow:
		return "low"
	case llm.ReasoningEffortMedium:
		return "mid"
	case llm.ReasoningEffortHigh:
		return "high"
	case llm.ReasoningEffortXHigh:
		return "extra"
	case llm.ReasoningEffortMax:
		return "max"
	default:
		return ""
	}
}

// handleComposerSkills serves GET /api/composer/skills (WEBSKILL-01): the CALLER'S
// active-skills snapshot projected onto the board row shape (name/description/type; the
// body is NEVER carried), read from the SAME provider ActiveSkills the governance board and
// the runtime `skill action=use` resolve against (D-04 — activeSkillRows reused verbatim,
// one source of truth). Unlike handleSkillsList there is no stage switch: the picker is
// active-only. A nil provider → 503 so the client degrades to an empty picker (D-09), never
// a 500.
func (s *Server) handleComposerSkills(w http.ResponseWriter, r *http.Request) {
	if s.governance.Skills == nil {
		http.Error(w, "skills unavailable", http.StatusServiceUnavailable)
		return
	}
	// The picker is scoped to the caller for the same reason the board is: a picker that
	// offered a name the run pin cannot then resolve — or that hid a person's own skill —
	// would be the divergence Pitfall 2 exists to forbid, just spread across two routes.
	//
	// The picker resolves ownership from the same scope even though it never offers a write
	// verb: the row shape is shared with the board, and a row that says `owned:false`
	// everywhere would be a wrong answer on the wire waiting for the first client to read it.
	ctx := scopedCtx(r.Context())
	writeJSON(w, map[string]any{"skills": activeSkillRows(s.governance.Skills.ActiveSkills(ctx), s.governance.Skills.WritableRoot(ctx))})
}
