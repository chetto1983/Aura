package skills

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ResumeHandler is the D-03 activation channel for a model-proposed skill mutation:
// the human's ask_user answer (accept/decline/cancel) on a skill-approval pause is
// routed here. There is NO model-facing approve action (D-03) — a skill becomes
// active ONLY through this handler (a human resume) or the `aura skills approve`
// CLI. On accept it calls Writer.Activate (audit row: ask_user + non-NULL token +
// gate_recommended=true + gate_taken=true); on decline/cancel it DISCARDS the
// pending skill and records the rejection (an ask_user row that did not take the
// gate). The handler is the seam the Runner's resume orchestration invokes when a
// resolved pause was a skill-approval pause.
type ResumeHandler struct {
	writer *Writer
}

// NewResumeHandler builds a ResumeHandler over the live Writer.
func NewResumeHandler(w *Writer) *ResumeHandler {
	return &ResumeHandler{writer: w}
}

// Resume actions mirror askuser.Action{Accept,Decline,Cancel} as plain strings so
// internal/skills does not import internal/askuser (the boundary the askuser package
// itself holds in reverse). The caller maps the resume answer's action onto these.
const (
	ResumeAccept  = "accept"
	ResumeDecline = "decline"
	ResumeCancel  = "cancel"
)

// Resume applies a human's resume answer to a pending skill mutation. name is the
// skill the approval pause was about; pausedToken is the canonical UUID of the
// paused_states row (the audit's NOT-NULL token for the ask_user matrix row, D-29).
//
//   - action=accept → Writer.Activate(ApprovalAskUser, pausedToken): pending → active,
//     materialize, audit the approved tuple.
//   - action=decline|cancel → discard the pending skill + audit the ask_user rejection
//     (gate_taken=false: the human declined the gate).
//
// An unknown action is an error (the caller validated the answer shape; this is a
// defensive guard).
func (h *ResumeHandler) Resume(ctx context.Context, action, name, pausedToken string, actor AuditActor) error {
	switch action {
	case ResumeAccept:
		return h.writer.Activate(ctx, name, ApprovalAskUser, pausedToken, actor)
	case ResumeDecline, ResumeCancel:
		return h.writer.DiscardPending(ctx, name, ApprovalAskUser, pausedToken, actor)
	default:
		return fmt.Errorf("skill resume %q: unknown action %q", name, action)
	}
}

// DiscardPending removes a pending (not-yet-activated) skill and records the
// rejection in the audit ledger. It is the decline/cancel half of the ask_user
// approval channel (and the `aura skills` reject path): the pending dir is removed
// and ONE audit row is appended marking the gate as recommended-but-not-taken
// (gate_taken=false) with the supplied approval source. src=ApprovalAskUser carries
// the non-NULL pausedToken (the D-29 ask_user reject row); src=ApprovalCLI carries
// an empty token.
func (w *Writer) DiscardPending(ctx context.Context, name string, src ApprovalSource, pausedToken string, actor AuditActor) error {
	if err := SanitizeName(name, name); err != nil {
		return fmt.Errorf("discard pending %q: %w", name, err)
	}
	pendingDir := filepath.Join(w.pendingDir, name)
	hash, _ := HashSkillDir(pendingDir) // best-effort content_hash for the recovery path

	if w.pendingDir != "" {
		if err := os.RemoveAll(pendingDir); err != nil {
			return fmt.Errorf("discard pending %q: remove pending: %w", name, err)
		}
	}

	if err := w.auditRejection(ctx, name, hash, src, pausedToken, actor); err != nil {
		return fmt.Errorf("discard pending %q: audit: %w", name, err)
	}
	return nil
}
