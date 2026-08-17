package agui

import (
	"context"
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/conversations"
)

// conversations_compaction_api.go carries the two routes behind the composer's `/compact`
// command: the POST that condenses the thread's earlier turns into its durable summary, and
// the GET the chat lane reads to draw the marker showing where that summary now reaches.
//
// Both are thin in the conversations_api.go sense: owner-gate, ONE call, project. The
// decision about WHAT gets condensed lives in conversations.Store.Compact, and the summary
// the ladder replays afterwards is the same row — there is no second notion of "compacted"
// on this side of the wire.
//
// The two capabilities are read off s.run / s.conv by type assertion rather than being added
// to the Runner / ConversationStore interfaces, the same shape threadTryLocker uses: a
// server wired with a driver that cannot compact answers "unavailable" instead of failing to
// compile every scripted fake in the package.

// conversationCompactor is the operator-requested compaction seam; *runner.Runner satisfies it.
type conversationCompactor interface {
	CompactConversation(ctx context.Context, conversationID string) (conversations.CompactionResult, error)
}

// compactionReader is the durable-summary read seam; *conversations.Store satisfies it. The
// empty branch id is the canonical branch — the one Compact and LoadManagedHistory both file
// under.
type compactionReader interface {
	LoadCompaction(ctx context.Context, conversationID, branchID string) (conversations.Compaction, bool, error)
}

// compactionDTO is what the cockpit draws the in-chat marker from. covers_through_seq is the
// position: it is a conversation_turns.seq, the same number the message snapshot carries as
// backendSeq, so the client places the marker without a second lookup. ZERO means this
// conversation has never been compacted — the absence is a value, not a 404, because "no
// marker" is the normal state of most threads and an error code would make the client treat
// it as a failure.
//
// tokens_before/after are populated only by the POST (the row does not store them): a fresh
// compaction can say how much room it made, a re-read cannot invent it.
type compactionDTO struct {
	CoversThroughSeq int    `json:"covers_through_seq"`
	SourceTurns      int    `json:"source_turns"`
	Summary          string `json:"summary"`
	TokensBefore     int    `json:"tokens_before,omitempty"`
	TokensAfter      int    `json:"tokens_after,omitempty"`
}

// registerCompactionRoutes mounts the two routes on the /api/conversations/{id} subtree,
// inheriting its whole-origin RequireAuth from the parent mux (serve_webui.go) exactly as
// its siblings do.
//
// The patterns are LITERALS, not the named constants this package uses for its read-only
// routes: TestEveryRegisteredUnsafeHTTPRouteIsClassified greps these files for a mux
// registration whose pattern is a quoted unsafe method, to prove every state-changing route
// carries idempotency metadata — and a route registered through a constant is one that sweep
// cannot see. The compact route writes the compaction row, so it belongs in
// httpMutationRoutes AND in the sweep's field of view.
func (s *Server) registerCompactionRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/conversations/{id}/compact", s.handleCompactConversation)
	mux.HandleFunc("GET /api/conversations/{id}/compaction", s.handleGetCompaction)
}

// ownedConversation resolves {id} and proves the caller owns it (MUSR-01 / D-06): a foreign
// or absent id is 404 either way, so neither route discloses another identity's thread.
func (s *Server) ownedConversation(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := parseConvID(w, r)
	if !ok {
		return "", false
	}
	if _, err := s.conv.GetForIdentity(r.Context(), id, scopedIdentityID(r.Context())); err != nil {
		writeStoreErr(w, err)
		return "", false
	}
	return id, true
}

// handleCompactConversation condenses the thread's earlier turns into its durable summary
// and returns what that changed.
//
// A refusal is a status, not an error body on a 200: the operator asked for a state change
// that did not happen, and a 200 carrying a zero result reads, in the composer, exactly like
// a compaction that worked.
//
// The refusals get DIFFERENT statuses because they are different sentences to the person
// reading them — "there is nothing behind this turn yet" (409, a fact about the thread),
// "a summary of this would be longer than the thread" (422, a fact about the trade), and
// "the summarizer did not answer" (502, the only one that is a malfunction, and an upstream's
// rather than this server's). One status would leave the client sniffing prose to tell them
// apart.
func (s *Server) handleCompactConversation(w http.ResponseWriter, r *http.Request) {
	compactor, ok := s.run.(conversationCompactor)
	if !ok {
		http.Error(w, "compaction unavailable", http.StatusServiceUnavailable)
		return
	}
	id, ok := s.ownedConversation(w, r)
	if !ok {
		return
	}
	result, err := compactor.CompactConversation(scopedCtx(r.Context()), id)
	switch {
	case errors.Is(err, conversations.ErrCompactionUnavailable):
		http.Error(w, "compaction unavailable", http.StatusServiceUnavailable)
		return
	case errors.Is(err, conversations.ErrNothingToCompact):
		http.Error(w, conversations.ErrNothingToCompact.Error(), http.StatusConflict)
		return
	case errors.Is(err, conversations.ErrCompactionNotWorthwhile):
		http.Error(w, conversations.ErrCompactionNotWorthwhile.Error(), http.StatusUnprocessableEntity)
		return
	// The summarizer is an upstream this server called and did not get an answer from, so
	// 502 rather than 500: nothing here is broken, and nothing in the conversation changed.
	case errors.Is(err, conversations.ErrCompactionFailed):
		http.Error(w, conversations.ErrCompactionFailed.Error(), http.StatusBadGateway)
		return
	case err != nil:
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, compactionDTO{
		CoversThroughSeq: result.CoversThroughSeq,
		SourceTurns:      result.SourceTurns,
		Summary:          result.Summary,
		TokensBefore:     result.TokensBefore,
		TokensAfter:      result.TokensAfter,
	})
}

// handleGetCompaction returns the thread's stored summary, or the zero DTO when it has
// never been compacted. A store that cannot answer the question at all (no compaction read
// seam wired) reports the same zero DTO: the marker is an annotation on the transcript, and
// failing the whole thread read because an annotation is unavailable would be a worse answer
// than drawing no marker.
func (s *Server) handleGetCompaction(w http.ResponseWriter, r *http.Request) {
	id, ok := s.ownedConversation(w, r)
	if !ok {
		return
	}
	reader, ok := s.conv.(compactionReader)
	if !ok {
		writeJSON(w, compactionDTO{})
		return
	}
	stored, found, err := reader.LoadCompaction(scopedCtx(r.Context()), id, "")
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if !found {
		writeJSON(w, compactionDTO{})
		return
	}
	writeJSON(w, compactionDTO{
		CoversThroughSeq: stored.CoversThroughSeq,
		SourceTurns:      stored.SourceTurns,
		Summary:          stored.Summary,
	})
}
