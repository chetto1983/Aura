package agui

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"
)

// server_swarm_transcript.go carries GET
// /api/conversations/{conv}/swarm/{childID}/transcript?offset=N (SWARM-10, Phase 51
// plan 07): the operator-facing read over a backgrounded worker's live JSONL
// transcript (internal/swarm's dumpTranscript / ReadTranscript). It mirrors
// handleGetConversation's owner-scoped 404 ladder exactly (conv resolved through
// s.conv.GetForIdentity BEFORE any transcript read), never a distinguishable 403.
// Every failure past that point — a nil (unwired) reader, an absent childID, or a
// ReadTranscript error (including a rejected/hostile childID) — renders the SAME
// opaque 404 body, so the wire never discloses which of those actually happened
// (T-51-28/29).

// swarmTranscriptReader is the narrow read surface this route consumes (D-A2-02
// narrow seam, mirroring steerPusher/ConversationStore): swarm.ReadTranscript is a
// package-level function, not a method, so the daemon composition root wires a tiny
// adapter (cmd/aura) that closes over RunDir and satisfies this interface — the same
// shape swarmRunner (internal/agent/tools/swarm_spawn.go) already uses to keep this
// package from importing internal/swarm's concrete types.
type swarmTranscriptReader interface {
	ReadTranscript(ctx context.Context, conv, childID string, fromOffset int64) ([]byte, int64, error)
	ListChildTranscripts(ctx context.Context, conv string) ([]string, error)
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

// swarmTranscriptNotFoundBody is the single opaque body every failure branch below
// renders — an ownership miss, a malformed id, an unwired reader, and a
// ReadTranscript error are all indistinguishable on the wire (T-51-28/29).
const swarmTranscriptNotFoundBody = "conversation not found"

func (s *Server) handleSwarmTranscript(w http.ResponseWriter, r *http.Request) {
	conv := r.PathValue("conv")
	if _, err := uuid.Parse(conv); err != nil {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	ctx := r.Context()
	// Owner-scoped resolution BEFORE any transcript read (MUSR-01 / D-06, mirroring
	// handleGetConversation): a foreign or absent conversation both 404, hiding
	// existence rather than a distinguishable 403.
	if _, err := s.conv.GetForIdentity(ctx, conv, scopedIdentityID(ctx)); err != nil {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	if s.swarmTranscripts == nil {
		// Flag-off / not-yet-wired posture, mirroring SetGraphView's best-effort
		// pattern — the route must not accept a request nothing can serve.
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	childID := r.PathValue("childID")
	if childID == "" {
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	offset := parseTranscriptOffset(r.URL.Query().Get("offset"))
	body, newOffset, err := s.swarmTranscripts.ReadTranscript(ctx, conv, childID, offset)
	if err != nil {
		// A rejected/hostile childID and a genuine "not found" render identically —
		// never leak which one occurred (T-51-28/29).
		http.Error(w, swarmTranscriptNotFoundBody, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("X-Transcript-Offset", strconv.FormatInt(newOffset, 10))
	// Defense-in-depth (T-51-30): the payload is raw worker event JSON, never HTML —
	// forbid the browser from sniffing it into an executable type.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body) //nolint:gosec // G705: body is the bounded (maxTranscriptReadBytes-capped) output of swarm.ReadTranscript, served as application/x-ndjson with nosniff, never interpreted as HTML.
}

// parseTranscriptOffset reads ?offset=, falling back to 0 on an absent, unparseable,
// or negative value — a resume cursor is always non-negative by construction
// (ReadTranscript's own returned offset), so an out-of-range client value degrades
// to "read from the start" rather than erroring.
func parseTranscriptOffset(raw string) int64 {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
