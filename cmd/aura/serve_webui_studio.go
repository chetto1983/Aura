package main

// serve_webui_studio.go carries the cockpit Studio's parent-mux mounts, kept out of
// serve_webui.go so that file stays under the 600-LOC ceiling (mirrors serve_webui_voice.go).
// It mounts the six Studio routes registered on the agui Server.Mux (studio_api.go):
//
//   - POST /api/studio/videos, /api/studio/images and /api/studio/uploads/{id}/finalize are
//     cost-bearing or asset-mutating, so they are interposed with
//     RequireCapability(agentRunCapability) exactly like the asset mutations and POST /agent/run.
//   - the three reads are bare aguiHandler: they inherit the whole-origin RequireAuth from the
//     parent-mux wrap and are already scoped to the caller's own identity.

import (
	"net/http"

	"github.com/chetto1983/aura/internal/agui"
)

const (
	studioModelsRoute   = "GET /api/studio/models"
	studioHistoryRoute  = "GET /api/studio/history"
	studioLibraryRoute  = "GET /api/studio/library"
	studioVideosRoute   = "POST /api/studio/videos"
	studioImagesRoute   = "POST /api/studio/images"
	studioFinalizeRoute = "POST /api/studio/uploads/{id}/finalize"
)

func registerStudioWebRoutes(mux *http.ServeMux, aguiHandler http.Handler, auth agui.AuthDeps) {
	mux.Handle(studioModelsRoute, aguiHandler)
	mux.Handle(studioHistoryRoute, aguiHandler)
	mux.Handle(studioLibraryRoute, aguiHandler)
	mux.Handle(studioVideosRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(studioImagesRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
	mux.Handle(studioFinalizeRoute, agui.RequireCapability(aguiHandler, auth, agentRunCapability))
}
