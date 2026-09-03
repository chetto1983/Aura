package agui

// GET /threads/{id}/messages — the persisted conversation, rebuilt for the cockpit.
//
// Extracted from server.go on touch (600-LOC cap). It is one route, but it is the route
// where every DISPLAY-ONLY decoration is merged back onto the history: the re-derived tool
// displays, the per-turn reasoning, and what each user turn was sent with. All three are
// deliberately absent from the llm.Message rebuild itself, so this is the only place they
// meet — and each merges fail-soft, because a decoration that cannot be read is worth less
// than the thread it would otherwise take down with it.

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/google/uuid"
)

// handleMessages resolves the thread (404) and returns the persisted history as a
// MESSAGES_SNAPSHOT JSON body (one-shot read, NOT SSE — OQ2). Each persisted
// llm.Message is projected to an events.Message.
func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	ctx := r.Context()
	// A malformed thread id (non-UUID) is definitionally not an existing thread —
	// 404 before the store round-trip rather than a 500 from the id parse failure
	// (T-12-11; the live smoke's `does-not-exist` chokepoint).
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}
	// Owner-scoped thread resolution (MUSR-01 / D-06): a foreign/absent thread is 404 before
	// the history read, so a MESSAGES_SNAPSHOT never leaks another identity's conversation.
	if _, err := s.conv.GetForIdentity(ctx, id, scopedIdentityID(ctx)); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			http.Error(w, "thread not found", http.StatusNotFound)
			return
		}
		http.Error(w, "thread lookup failed", http.StatusInternalServerError)
		return
	}
	hist, err := s.conv.LoadHistory(scopedCtx(ctx), id)
	if err != nil {
		http.Error(w, sanitizeErr(err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// D-06: emit the display-aware MESSAGES_SNAPSHOT — each tool-result turn re-derives
	// its DisplayPayload through the SAME normalizer the live stream uses, so a reopened
	// thread renders typed displays identically to live. The envelope is byte-compatible
	// with the SDK MESSAGES_SNAPSHOT plus the additive per-tool-call `display` key the
	// cockpit replay reads.
	snap := projectDisplaySnapshot(hist)
	// Amendment #91 (fix-plan 1.12) display rehydration: merge the persisted per-turn
	// CoT onto the assistant answer messages so the ReasoningDrawer survives reload.
	// Fail-soft: reasoning is additive display data — a read failure degrades to a
	// drawer-less snapshot (the NULL-column posture), never a 500 for the whole thread.
	if reasonings, rerr := s.conv.ListTurnReasoning(scopedCtx(ctx), id); rerr != nil {
		slog.Warn("agui: list turn reasoning (serving snapshot without it)", "thread", id, "err", rerr)
	} else {
		attachTurnReasoning(&snap, reasonings)
	}
	// Migration 0116: what each user turn was actually sent with. Fail-soft for the same
	// reason reasoning is — it is additive display data, and the degrade is the cockpit's
	// pre-0116 positional fold rather than a 500 for the whole thread.
	if attached, aErr := s.conv.ListTurnAttachments(scopedCtx(ctx), id); aErr != nil {
		slog.Warn("agui: list turn attachments (serving snapshot without them)", "thread", id, "err", aErr)
	} else {
		attachTurnAttachments(&snap, attached)
	}
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		slog.Warn("agui: encode messages snapshot", "err", err)
	}
}
