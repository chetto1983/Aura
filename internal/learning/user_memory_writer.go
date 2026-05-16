package learning

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aura/aura/internal/identity"
	"github.com/aura/aura/internal/storage/memoryindex"
)

// Authorizer checks capability grants before a user-memory write.
// Satisfied by *identity.Store in production; test fakes can implement the
// single method.
type Authorizer interface {
	Authorize(ctx context.Context, params identity.AuthorizeParams) (identity.AuthorizationDecision, error)
}

// WriteApprovedUserFactAs is the capability-gated entry point. When authz is
// nil or actor.ID is empty the write proceeds without a check (system-actor
// back-compat for automated pipelines and pre-Q01 callers).
func WriteApprovedUserFactAs(ctx context.Context, store *memoryindex.Store, authz Authorizer, actor identity.Actor, proposal Proposal) error {
	if proposal.Kind != "user_memory" {
		return nil
	}
	if authz != nil && actor.ID != "" {
		dec, err := authz.Authorize(ctx, identity.AuthorizeParams{
			ActorID:    actor.ID,
			Capability: identity.CapabilityMemoryUserWrite,
			Resource:   identity.ResourceRef{Type: "user_memory", ID: proposal.SignatureHash},
		})
		if err != nil {
			return fmt.Errorf("user memory writer: authorize: %w", err)
		}
		if dec.Decision != identity.DecisionAllow {
			slog.WarnContext(ctx, "user memory write denied",
				"actor_id", actor.ID,
				"capability", string(identity.CapabilityMemoryUserWrite),
				"reason", dec.Reason)
			return identity.ErrPermissionDenied
		}
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

// WriteApprovedUserFact writes an approved user_memory proposal without a
// capability check (system-actor back-compat for automated pipelines and tests
// that predate US-Q01). Callers that hold an actor identity should use
// WriteApprovedUserFactAs instead.
func WriteApprovedUserFact(ctx context.Context, store *memoryindex.Store, proposal Proposal) error {
	return WriteApprovedUserFactAs(ctx, store, nil, identity.Actor{}, proposal)
}
