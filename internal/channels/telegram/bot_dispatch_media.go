// Package telegram — native-media projection for attachment turns. The channel does
// not read image or audio bytes itself: it arms the SAME llm.ContentProjection seam
// the AG-UI gateway arms, and the provider client decides (via its capability probe)
// whether the active model receives the bytes. Without this, a Telegram photo reached
// the model as a file name in the attachment catalog and the model confabulated the
// content (amendment #198).
package telegram

import (
	"context"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/llm"
)

// withTurnMediaProjection arms the content projection for this turn's image and audio
// attachments. Non-media attachments (documents) stay catalog-only. The principal is
// the assets' owner — every attachment of a Telegram turn belongs to the linked
// identity that ingested it.
func (t *Telegram) withTurnMediaProjection(ctx context.Context, chatID int64, attachments []assets.Asset) context.Context {
	if t.deps.Assets == nil {
		return ctx
	}
	mediaIDs := make([]string, 0, len(attachments))
	allowed := make(map[string]bool, len(attachments))
	ownerID := ""
	for _, attachment := range attachments {
		if attachment.Modality != assets.ModalityImage && attachment.Modality != assets.ModalityAudio {
			continue
		}
		mediaIDs = append(mediaIDs, attachment.ID)
		allowed[attachment.ID] = true
		ownerID = attachment.IdentityID
	}
	if len(mediaIDs) == 0 || ownerID == "" {
		return ctx
	}
	return llm.WithContentProjection(ctx, llm.ContentProjection{
		Loader:       assets.TurnMediaLoader{Opener: t.deps.Assets, ThreadID: convID(chatID), Allowed: allowed},
		Principal:    llm.ProjectionPrincipal{OwnerID: ownerID},
		ReferenceIDs: mediaIDs,
	})
}
