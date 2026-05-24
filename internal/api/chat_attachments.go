package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/chat"
)

const maxChatAttachments = 16

type chatAttachmentsContextKey struct{}

// ChatAttachmentsFromContext returns source-backed attachments parsed by the
// chat API boundary. The web Hub adapter turns these into InboundMessage
// attachments so core chat code never reads HTTP payloads directly.
func ChatAttachmentsFromContext(ctx context.Context) []chat.AttachmentRef {
	if ctx == nil {
		return nil
	}
	refs, _ := ctx.Value(chatAttachmentsContextKey{}).([]chat.AttachmentRef)
	return append([]chat.AttachmentRef(nil), refs...)
}

// WithChatAttachments stores already-validated source attachment references
// on the request context. API handlers normally call the unexported parser;
// cmd/aura tests and adapters use this when they already own AttachmentRef
// values.
func WithChatAttachments(ctx context.Context, refs []chat.AttachmentRef) context.Context {
	return withChatAttachments(ctx, refs)
}

func withChatAttachments(ctx context.Context, refs []chat.AttachmentRef) context.Context {
	if len(refs) == 0 {
		return ctx
	}
	return context.WithValue(ctx, chatAttachmentsContextKey{}, append([]chat.AttachmentRef(nil), refs...))
}

type chatAttachmentRequest struct {
	SourceID string `json:"source_id"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

func chatAttachmentsFromRequest(sourceIDs []string, attachments []chatAttachmentRequest) ([]chat.AttachmentRef, error) {
	seen := make(map[string]struct{}, len(sourceIDs)+len(attachments))
	refs := make([]chat.AttachmentRef, 0, len(sourceIDs)+len(attachments))
	add := func(sourceID, filename, mimeType string, size int64) error {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			return nil
		}
		if !sourceIDRe.MatchString(sourceID) {
			return fmt.Errorf("invalid source_id %q", sourceID)
		}
		if _, ok := seen[sourceID]; ok {
			return nil
		}
		if len(refs) >= maxChatAttachments {
			return fmt.Errorf("too many attachments: max %d", maxChatAttachments)
		}
		seen[sourceID] = struct{}{}
		refs = append(refs, chat.AttachmentRef{
			SourceID: sourceID,
			Filename: strings.TrimSpace(filename),
			MimeType: strings.TrimSpace(mimeType),
			Size:     size,
			Status:   "stored",
		})
		return nil
	}
	for _, id := range sourceIDs {
		if err := add(id, "", "", 0); err != nil {
			return nil, err
		}
	}
	for _, attachment := range attachments {
		if err := add(attachment.SourceID, attachment.Filename, attachment.MimeType, attachment.Size); err != nil {
			return nil, err
		}
	}
	return refs, nil
}
