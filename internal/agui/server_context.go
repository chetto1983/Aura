package agui

import (
	"context"
	"net/http"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/llm"
)

// buildTurnUserMessage prepends the per-turn context blocks to the user message: this
// turn's attachments (detailed) plus a catalog of the thread's other searchable documents
// so the agent calls document_search even with no attachment this turn (Item 2 / spike
// 077) without over-triggering on unrelated turns. Both blocks ride the user turn — a
// cache-safe tail that leaves messages[0]/[1] byte-stable.
//
// It returns the (possibly augmented) message plus an HTTP status to fail the request
// (status 0 = proceed). The attachment path keeps its strict 503/401/404 semantics; the
// catalog path is best-effort (no docs or no identity -> no catalog, no error).
func (s *Server) buildTurnUserMessage(ctx context.Context, r *http.Request, threadID string, attachmentIDs []string, userMsg *string) (context.Context, *string, int, string) {
	identityID, identityOK := principalIdentityID(r)
	var attachments []assets.Asset
	if len(attachmentIDs) > 0 {
		if s.assets == nil {
			return ctx, userMsg, http.StatusServiceUnavailable, "asset service unavailable"
		}
		if !identityOK {
			return ctx, userMsg, http.StatusUnauthorized, "unauthorized"
		}
		attachments = make([]assets.Asset, 0, len(attachmentIDs))
		for _, id := range attachmentIDs {
			asset, err := s.assets.GetForIdentity(ctx, id, identityID)
			if err != nil {
				return ctx, userMsg, http.StatusNotFound, "attachment not found"
			}
			if asset.ThreadID != "" && asset.ThreadID != threadID {
				return ctx, userMsg, http.StatusNotFound, "attachment not found"
			}
			attachments = append(attachments, asset)
		}
	}
	if s.assets == nil {
		return ctx, userMsg, 0, ""
	}
	mediaIDs := make([]string, 0, len(attachments))
	allowed := make(map[string]bool, len(attachments))
	for _, attachment := range attachments {
		if attachment.Modality != assets.ModalityImage && attachment.Modality != assets.ModalityAudio {
			continue
		}
		mediaIDs = append(mediaIDs, attachment.ID)
		allowed[attachment.ID] = true
	}
	if len(mediaIDs) > 0 {
		ctx = llm.WithContentProjection(ctx, llm.ContentProjection{
			Loader:       assetContentPartLoader{assets: s.assets, threadID: threadID, allowed: allowed},
			Principal:    llm.ProjectionPrincipal{OwnerID: identityID},
			ReferenceIDs: mediaIDs,
		})
	}
	// Compose via the shared, channel-agnostic seam — the same path the Telegram channel
	// uses — instead of duplicating the catalog/attachment composition here. The catalog is
	// keyed by the authenticated identity, so an unauthenticated principal gets none.
	catalogIdentity := ""
	if identityOK {
		catalogIdentity = identityID
	}
	text := ""
	if userMsg != nil {
		text = *userMsg
	}
	combined := s.assets.BuildTurnContext(ctx, catalogIdentity, threadID, attachments, text)
	if combined == text {
		return ctx, userMsg, 0, ""
	}
	return ctx, &combined, 0, ""
}
