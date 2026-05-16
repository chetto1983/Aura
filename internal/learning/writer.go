package learning

import (
	"context"
	"fmt"
	"time"

	"github.com/aura/aura/internal/storage/memoryindex"
)

// Proposal is the subset of a proposed_updates row needed by WriteApprovedLesson.
// For operational_memory proposals: ToolName is stored in target_slug and
// ErrorClass is stored in category by the SQLProposalStore adapter.
type Proposal struct {
	Kind          string
	Fact          string
	SignatureHash string
	ToolName      string
	ErrorClass    string
}

// WriteApprovedLesson writes an approved operational_memory proposal to
// compact_memory_documents with Kind=operational. Returns nil without writing
// when proposal.Kind is not "operational_memory". The write is idempotent:
// the document ID is derived from SignatureHash so re-approving the same
// proposal overwrites the existing row rather than creating a duplicate.
func WriteApprovedLesson(ctx context.Context, store *memoryindex.Store, proposal Proposal) error {
	if proposal.Kind != "operational_memory" {
		return nil
	}
	sig := proposal.SignatureHash
	title := fmt.Sprintf("Lesson: %s %s", proposal.ToolName, proposal.ErrorClass)
	body := proposal.Fact
	contentHash := memoryindex.ContentHash("operational", body, "")
	doc := memoryindex.Document{
		ID:          "operational:" + sig,
		Kind:        memoryindex.KindOperational,
		Title:       title,
		Body:        body,
		Handle:      "operational:" + sig,
		Status:      "active",
		UpdatedAt:   time.Now().UTC(),
		ContentHash: contentHash,
	}
	return store.Upsert(ctx, doc)
}
