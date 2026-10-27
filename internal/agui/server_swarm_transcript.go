package agui

import "context"

// server_swarm_transcript.go is the read seam over a backgrounded worker's JSONL transcript
// (internal/swarm's dumpTranscript / ReadTranscript). The cockpit reads it only through the
// SSE routes (server_swarm_events.go, server_swarm_events_status.go). The pull route
// GET .../swarm/{childID}/transcript that SWARM-10 added first had no client once those
// streams shipped, and was removed on 2026-10-26.

// swarmTranscriptReader is the narrow read surface the worker streams consume (D-A2-02
// narrow seam, mirroring steerPusher/ConversationStore): swarm.ReadTranscript is a
// package-level function, not a method, so the daemon composition root wires a tiny
// adapter (cmd/aura) that closes over RunDir and satisfies this interface — the same
// shape swarmRunner (internal/agent/tools/swarm_spawn.go) already uses to keep this
// package from importing internal/swarm's concrete types.
type swarmTranscriptReader interface {
	ReadTranscript(ctx context.Context, conv, childID string, fromOffset int64) ([]byte, int64, error)
	ListChildTranscripts(ctx context.Context, conv string) ([]string, error)
}

// SetSwarmTranscripts wires the transcript reader. Set by the daemon composition root after
// NewServer; until set (nil), the worker streams answer 404 — mirroring SetGraphView's
// best-effort posture (a missing dependency must not abort boot, and a route must not
// accept a request nothing can serve).
func (s *Server) SetSwarmTranscripts(r swarmTranscriptReader) { s.swarmTranscripts = r }

// swarmTranscriptNotFoundBody is the single opaque body every worker-stream failure renders
// — an ownership miss, a malformed id, an unwired reader, and a ReadTranscript error are all
// indistinguishable on the wire (T-51-28/29).
const swarmTranscriptNotFoundBody = "conversation not found"
