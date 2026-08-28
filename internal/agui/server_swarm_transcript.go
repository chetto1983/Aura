package agui

import (
	"context"
	"net/http"
)

// server_swarm_transcript.go carries GET
// /api/conversations/{conv}/swarm/{childID}/transcript?offset=N (SWARM-10, Phase 51
// plan 07): the operator-facing read over a backgrounded worker's live JSONL
// transcript (internal/swarm's dumpTranscript / ReadTranscript). It mirrors
// handleGetConversation's owner-scoped 404 ladder exactly (conv resolved through
// s.conv.GetForIdentity BEFORE any transcript read), never a distinguishable 403.
//
// STUB (RED phase): the handler answers 501 unconditionally so
// server_swarm_transcript_test.go fails for the right reason before the GREEN
// commit lands the real ladder.

// swarmTranscriptReader is the narrow read surface this route consumes (D-A2-02
// narrow seam, mirroring steerPusher/ConversationStore): swarm.ReadTranscript is a
// package-level function, not a method, so the daemon composition root wires a tiny
// adapter (cmd/aura) that closes over RunDir and satisfies this interface — the same
// shape swarmRunner (internal/agent/tools/swarm_spawn.go) already uses to keep this
// package from importing internal/swarm's concrete types.
type swarmTranscriptReader interface {
	ReadTranscript(ctx context.Context, conv, childID string, fromOffset int64) ([]byte, int64, error)
}

// SetSwarmTranscripts wires the SWARM-10 transcript reader. Set by the daemon
// composition root after NewServer; until set (nil), the route answers 404 —
// mirroring SetGraphView's best-effort posture (a missing dependency must not abort
// boot, and the route must not accept a request nothing can serve).
func (s *Server) SetSwarmTranscripts(r swarmTranscriptReader) { s.swarmTranscripts = r }

// registerSwarmTranscriptRoutes mounts the SWARM-10 route on the supplied mux.
func (s *Server) registerSwarmTranscriptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/conversations/{conv}/swarm/{childID}/transcript", s.handleSwarmTranscript)
}

func (s *Server) handleSwarmTranscript(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
