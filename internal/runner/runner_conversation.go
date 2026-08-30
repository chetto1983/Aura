package runner

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// runner_conversation.go holds the conversation-lifecycle helpers split out of runner.go
// (deep-refactor-on-touch / ≤600 LOC, CLAUDE.md): minting + lazily ensuring the
// conversation row the turn loop appends to. No behavior change from the prior in-file
// definitions.

// NewConversation creates a new active conversation and returns its id. Ownership
// follows the Authula-era identity model: prefer a real user identity, with `local`
// retained only as a legacy fallback.
func (r *Runner) NewConversation(ctx context.Context) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("mint conversation id: %w", err)
	}
	return r.NewConversationWithID(ctx, id.String())
}

// NewConversationWithID creates a conversation with a caller-supplied id (the REPL
// mints the id so it can key the sidecar dir before the row exists).
func (r *Runner) NewConversationWithID(ctx context.Context, conversationID string) (string, error) {
	owner, err := r.defaultConversationOwner(ctx)
	if err != nil {
		return "", fmt.Errorf("new conversation: resolve owner identity: %w", err)
	}
	if _, err := r.Conv.Create(ctx, conversations.CreateParams{
		ID:         conversationID,
		IdentityID: owner.ID,
		Model:      r.llmSnapshot(ctx).Config.Model,
	}); err != nil {
		return "", fmt.Errorf("new conversation: %w", err)
	}
	return conversationID, nil
}

// defaultConversationOwner resolves the owner of a NEW conversation (MUSR-02). The
// authenticated principal on ctx — threaded from agui.withPrincipal →
// identityctx.WithIdentityID — is the owner: a Web conversation created by B is owned by B,
// validated via GetIdentityByID so a stale/bogus principal fails loudly instead of minting
// an orphan row. This replaces the pre-Phase-36 "prefer the FIRST user identity" scan,
// which mis-attributed B's conversation to A once more than one user existed. With no
// principal (the CLI / no-auth path, D-25) `local` owns the conversation.
func (r *Runner) defaultConversationOwner(ctx context.Context) (identity.Identity, error) {
	if principal := identityctx.IdentityID(ctx); principal != "" {
		owner, err := r.identity.GetIdentityByID(ctx, principal)
		if err != nil {
			return identity.Identity{}, fmt.Errorf("resolve principal owner %s: %w", principal, err)
		}
		return owner, nil
	}
	id, err := r.identity.GetIdentityByName(ctx, localIdentityName)
	if err != nil {
		return identity.Identity{}, err
	}
	return id, nil
}

// EnsureConversation lazily creates the conversation row when it is absent and is
// a no-op when it already exists. Channels that key a stable conversation id off
// an external identifier (e.g. a Telegram chat id via a deterministic UUIDv5)
// have no explicit "new conversation" step like the REPL, so the first inbound
// message must create the row before Turn appends to it (appendUserTurn's
// AppendTurn FK-references the conversation). A Get short-circuits the common
// path; a concurrent first-message race that loses the Create is reconciled by a
// re-Get rather than surfaced as an error.
func (r *Runner) EnsureConversation(ctx context.Context, convID string) error {
	if _, err := r.Conv.Get(ctx, convID); err == nil {
		return nil
	}
	if _, err := r.NewConversationWithID(ctx, convID); err != nil {
		// Only a 23505 unique_violation is the benign lost-create race (a concurrent
		// first-message creator won); classify by SQLSTATE, never by the row's mere
		// presence — that would mask a real create failure (FK/constraint/connection)
		// as "already exists" (M-08).
		if isUniqueViolation(err) {
			return nil // a concurrent creator won the race — the row now exists
		}
		return err
	}
	return nil
}

// isUniqueViolation classifies a pgx error as SQLSTATE 23505 via errors.As +
// pgErr.Code — never string-matching the message (mirrors identity/cron/telegram).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
