package learning

import (
	"context"
	"fmt"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// WriteApprovedUserFact writes an approved user_memory proposal to
// compact_memory_documents with Kind=user_memory. Returns nil without writing
// when proposal.Kind is not "user_memory". The write is idempotent: the
// document ID equals proposal.SignatureHash (the UserFactHandle), so
// re-approving the same proposal overwrites the existing row (ActionPatch).
func WriteApprovedUserFact(ctx context.Context, store *memoryindex.Store, proposal Proposal) error {
	if proposal.Kind != "user_memory" {
		return nil
	}
	handle := proposal.SignatureHash
	if handle == "" {
		return fmt.Errorf("user memory writer: SignatureHash required")
	}
	title := proposal.Fact
	if len(title) > 80 {
		title = title[:80]
	}
	body := proposal.Fact
	contentHash := memoryindex.ContentHash("user_memory", body, "")
	tags := []string{}
	if proposal.Category != "" {
		tags = []string{proposal.Category}
	}
	doc := memoryindex.Document{
		ID:          handle,
		Kind:        memoryindex.KindUserMemory,
		Title:       title,
		Body:        body,
		Handle:      handle,
		Status:      "active",
		UpdatedAt:   time.Now().UTC(),
		ContentHash: contentHash,
		Tags:        tags,
	}
	return store.Upsert(ctx, doc)
}
