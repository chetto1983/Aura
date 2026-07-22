package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/google/uuid"
)

// runner_delete.go is the single conversation-delete lifecycle entry point (MUSR-05 / D-22).
// ALL conversation deletion — AG-UI DELETE, Telegram /clear, the CLI — routes through
// DeleteConversationLifecycle so exactly one ordered teardown owns it: cancel active work →
// expire pending pauses → evict session tools → terminate background jobs → revoke shares
// (D-15) → THEN delete persistence. No surface performs a raw store delete; that would skip
// the teardown and leak live session state (tracked tool cwd, todo lists, a running shell)
// into a long-lived `serve` daemon after its conversation is gone.

// ShareRevoker is the consumer-declared seam step 4.5 drives (the IdentityPurger pattern,
// internal/cron/handlers/identity_purge.go:20-27): the live *share.Service satisfies it via
// RevokeConversationShares, so this package does NOT import internal/share (the reverse-import
// rule). RevokeConversationShares revokes every still-live share for conversationID, owner-
// scoped to identityID, dropping each one's Garage bytes before it returns.
type ShareRevoker interface {
	RevokeConversationShares(ctx context.Context, conversationID, identityID string) error
}

type reservedConversationDelete interface {
	ReserveDeleteForIdentityIfVersion(context.Context, string, string, int64, string) (int64, error)
	ClaimDeleteTeardown(context.Context, string, string, string, string, time.Time) (int64, error)
	ReleaseDeleteLease(context.Context, string, string, string, string) (int64, error)
	DeleteForIdentityIfReservation(context.Context, string, string, string, string) (int64, error)
}

const conversationDeleteFinalizeTimeout = 2 * time.Minute

// SetShareRevoker wires step 4.5's ShareRevoker seam after construction — required because
// chat.run (New) is assembled in the shared boot (chat_boot.go, BEFORE objectStore/chat.assets
// exist), while the live *share.Service needs both (share_service_wiring.go). Mirrors the
// agui.Server SetAssetService/SetShareService late-bound-setter pattern (D-A2-02 narrow seam);
// unset (nil) leaves step 4.5 the documented silent skip, exactly Deps.ShareRevoker's contract.
func (r *Runner) SetShareRevoker(sr ShareRevoker) { r.shareRevoker = sr }

// DeleteConversationLifecycle tears down and deletes one conversation, owner-scoped (D-06).
// It returns rows-affected (0 = the caller does not own the id — the surface maps that to 403
// for a known-foreign id or 404 for an absent one, exactly as DeleteForIdentity) and a
// non-nil error only for a real failure.
//
// The owner gate (plain delete) or atomic owner+version reservation (export-delete)
// runs FIRST, before any destructive step. Foreign, absent, and stale-snapshot calls
// short-circuit with (0, nil), so they cannot alter another process's live state.
//
// Ordering (MUSR-05 / D-22), each keyed to the (identity, session) so it never reaches a
// co-tenant:
//  1. cancel active work   — fire the in-flight turn's ctx-cancel for (identity, session)
//  2. expire pending pauses — askuser auto-resolve (best-effort; the cascade delete backstops it)
//  3. evict session tools   — SessionEvictor.Evict + the gateway approval ledger
//  4. terminate bg jobs     — the plan-03 owner-scoped kill of this session's live shells
//     4.5. revoke shares       — drop each live share's Garage bytes before the row cascades (D-15)
//  5. delete persistence    — the owner-scoped DeleteConversationForIdentity (+ sidecar purge)
func (r *Runner) DeleteConversationLifecycle(ctx context.Context, identityID, convID string) (int64, error) {
	return r.deleteConversationLifecycle(ctx, identityID, convID, 0)
}

// DeleteConversationLifecycleIfVersion reserves the version held by export-delete
// before it runs the otherwise-identical ordered teardown.
func (r *Runner) DeleteConversationLifecycleIfVersion(ctx context.Context, identityID, convID string, expected int64) (int64, error) {
	if expected <= 0 {
		return 0, errors.New("delete lifecycle: expected conversation version is required")
	}
	return r.deleteConversationLifecycle(ctx, identityID, convID, expected)
}

func (r *Runner) deleteConversationLifecycle(ctx context.Context, identityID, convID string, expected int64) (int64, error) {
	owner := resolveOwnerIdentity(identityID)
	if expected > 0 {
		reserved, ok := r.Conv.(reservedConversationDelete)
		if !ok {
			return 0, errors.New("delete lifecycle: reserved conversation delete unavailable")
		}
		reservation, err := exportDeleteReservation(ctx, owner, convID, expected)
		if err != nil {
			return 0, err
		}
		affected, err := reserved.ReserveDeleteForIdentityIfVersion(ctx, convID, owner, expected, reservation)
		if err != nil {
			return 0, fmt.Errorf("delete lifecycle: reserve persistence: %w", err)
		}
		if affected == 0 {
			return 0, nil
		}
		finalizeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), conversationDeleteFinalizeTimeout)
		defer cancel()
		return r.resumeReservedConversationDelete(finalizeCtx, owner, convID, reservation)
	}

	// Owner gate (D-06): resolve the conversation owner-scoped. A foreign/absent id is
	// ErrConversationNotFound → (0, nil), so the surface's 403/404 split runs and NO teardown
	// touches another identity's live state.
	if _, err := r.Conv.GetForIdentity(ctx, convID, owner); err != nil {
		if errors.Is(err, conversations.ErrConversationNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("delete lifecycle: owner gate: %w", err)
	}
	return r.teardownConversation(ctx, owner, convID, func(ctx context.Context) (int64, error) {
		return r.Conv.DeleteForIdentity(ctx, convID, owner)
	})
}

func (r *Runner) resumeReservedConversationDelete(ctx context.Context, owner, convID, reservation string) (int64, error) {
	reserved, ok := r.Conv.(reservedConversationDelete)
	if !ok {
		return 0, errors.New("delete lifecycle: reserved conversation delete unavailable")
	}
	worker := uuid.NewString()
	affected, err := reserved.ClaimDeleteTeardown(ctx, convID, owner, reservation, worker, time.Now().UTC().Add(conversationDeleteFinalizeTimeout+time.Minute))
	if err != nil {
		return 0, fmt.Errorf("delete lifecycle: claim teardown: %w", err)
	}
	if affected == 0 {
		return 0, nil
	}
	affected, err = r.teardownConversation(ctx, owner, convID, func(ctx context.Context) (int64, error) {
		return reserved.DeleteForIdentityIfReservation(ctx, convID, owner, reservation, worker)
	})
	if err == nil && affected == 1 {
		return affected, nil
	}
	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, releaseErr := reserved.ReleaseDeleteLease(releaseCtx, convID, owner, reservation, worker); releaseErr != nil {
		return affected, errors.Join(err, fmt.Errorf("delete lifecycle: release failed attempt: %w", releaseErr))
	}
	if err != nil {
		return affected, err
	}
	return affected, errors.New("delete lifecycle: final delete lost durable ownership")
}

func (r *Runner) teardownConversation(ctx context.Context, owner, convID string, finalize func(context.Context) (int64, error)) (int64, error) {

	// 1. Cancel active work: abort the owner's in-flight turn for this session (a no-op when
	// idle). Its ctx cancels, so the round unwinds before the row it appends to is deleted.
	r.cancelSession(owner, convID)

	// 2. Expire pending pauses (askuser auto-resolve). Best-effort: the paused_states rows
	// FK-cascade with the conversation on delete, so a transient failure here is a WARN, never
	// a blocked delete — the mandated lifecycle step still runs first.
	if err := r.pause.AutoResolveForConversation(ctx, convID); err != nil {
		slog.Warn("delete lifecycle: expire pauses failed (cascade delete will reconcile)",
			"conv", convID, "err", err)
	}

	// 3. Evict session tool state (todo list, tracked shell cwd, shell-approval + gateway
	// ledgers) BEFORE the persistence delete, so a finished conversation leaves nothing behind
	// in the daemon (R-41 / AP-16). sessionID == conversationID (D-26).
	r.evictSessionToolState(convID)

	// 4. Terminate the (identity, session)'s live background jobs (plan-03 owner-scoped kill),
	// so a detached shell started in this conversation does not outlive its deletion.
	r.terminateSessionJobs(owner, convID)

	// 4.5. Revoke the conversation's shares, dropping their Garage bytes (D-15): the
	// shared_links ROW cascades with the conversation at step 5, but the BYTES do not (R-10).
	if r.shareRevoker != nil {
		if err := r.shareRevoker.RevokeConversationShares(ctx, convID, owner); err != nil {
			slog.Warn("delete lifecycle: revoke shares failed (Garage bytes may be orphaned)", "conv", convID, "err", err)
		}
	}

	// 5. Delete persistence, owner-scoped (rows-affected drives the surface's 403/404/204).
	affected, err := finalize(ctx)
	if err != nil {
		return affected, err
	}
	return affected, nil
}

func exportDeleteReservation(ctx context.Context, owner, convID string, expected int64) (string, error) {
	operation, ok := idempotency.OperationFromContext(ctx)
	if !ok {
		return "", errors.New("delete lifecycle: stable operation identity is required")
	}
	operationIdentity := operation.Key.IdentityID + "\x00" + string(operation.Key.Scope) + "\x00" + operation.Key.Key
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%s", owner, convID, expected, operationIdentity)))
	return hex.EncodeToString(digest[:]), nil
}

// resolveOwnerIdentity maps an empty identity id — the CLI / no-principal path (D-25) — to
// the seeded `local` id so the owner-scoped delete and the (identity, session) key both
// resolve deterministically (matching sessionIdentity's bare-ctx fallback).
func resolveOwnerIdentity(identityID string) string {
	if identityID == "" {
		return localSessionIdentity
	}
	return identityID
}

// terminateSessionJobs terminates the live background jobs owned by (identityID, convID)
// during a conversation delete (MUSR-05 step 4). It ranges the tool registry for the
// owner-scoped session-job terminator (ShellPoll over the shared BackgroundShells): the
// plan-03 (identity, session) owner binding means only THIS identity's jobs for THIS session
// are killed, never a co-tenant's that happens to share a session id. Idempotent + nil-safe.
func (r *Runner) terminateSessionJobs(identityID, convID string) {
	if r.registry == nil {
		return
	}
	for _, t := range r.registry.All() {
		if term, ok := t.(tools.SessionJobTerminator); ok {
			term.TerminateSession(identityID, convID)
		}
	}
}
