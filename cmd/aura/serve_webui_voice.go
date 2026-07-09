package main

// serve_webui_voice.go carries the 37C web-voice parent-mux mounts, kept OUT of
// serve_webui.go so that file stays under the 600-LOC ceiling (mirrors the
// serve_webui_musr.go extraction). It mounts the three WEBVOICE routes registered on
// the agui Server.Mux (voice_api.go):
//
//   - POST /api/tts, POST /api/stt — cost-bearing (each consumes OpenRouter budget), so
//     interposed with RequireCapability(agentRunCapability) exactly like POST /agent/run
//     (the seeded `local` identity holds the '*' wildcard, so dev is unaffected; the name
//     becomes load-bearing once real grants arrive).
//   - GET /api/voice/capabilities — a SELF-scoped presence probe the SPA reads once on
//     load; bare aguiHandler, so it inherits the whole-origin RequireAuth from the
//     parent-mux wrap with NO capability gate (like meRoute in serve_webui_musr.go).
//
// Method+path-specific patterns win Go 1.22 longest-pattern precedence over the bare
// "/api/" exclusion carve-out and the "/" embed catch-all; the "/api/" fallback
// exclusion already returns them as backend routes (no fallback change needed).

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const (
	ttsRoute               = "POST /api/tts"
	sttRoute               = "POST /api/stt"
	voiceCapabilitiesRoute = "GET /api/voice/capabilities"
)

// registerVoiceRoutes mounts the web-voice routes on the parent mux. Each delegates to
// the AG-UI handler (routes live on Server.Mux). The two POSTs are capability-gated
// (cost-bearing); the capabilities read is RequireAuth-only.
func registerVoiceRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(ttsRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(sttRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(voiceCapabilitiesRoute, aguiHandler)
}
