package tools

import (
	"log/slog"
	"time"
)

// BackgroundShellCompletion is the small terminal fact emitted after one
// background shell reaches its final state. Output and command text deliberately
// stay in the owner-authorised shell_poll path; this fact only carries enough
// routing and status data to wake the owning conversation.
type BackgroundShellCompletion struct {
	ShellID   string
	OwnerID   string
	SessionID string
	Status    string
	Duration  time.Duration
}

// BackgroundShellCompletionHook receives one terminal fact per background shell.
// Implementations must return promptly; daemon composition uses it only to hand
// the fact to its lifecycle-owned dispatcher.
type BackgroundShellCompletionHook func(BackgroundShellCompletion)

// SetCompletionHook installs the daemon's completion hook for shells started
// after this call. A nil hook restores explicit-poll-only behaviour (the CLI and
// daemon shutdown paths). The composition root calls this before serving traffic,
// so no live shell can straddle hook installation in production.
func (b *BackgroundShells) SetCompletionHook(hook BackgroundShellCompletionHook) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.completionHook = hook
	b.mu.Unlock()
}

func (s *bgShell) notifyCompletion() {
	s.mu.Lock()
	if !s.done || s.completionSent {
		s.mu.Unlock()
		return
	}
	s.completionSent = true
	hook := s.completionHook
	completion := BackgroundShellCompletion{
		ShellID:   s.id,
		OwnerID:   s.ownerID,
		SessionID: s.sessionID,
		Status:    s.statusLocked(),
		Duration:  time.Since(s.startedAt),
	}
	s.mu.Unlock()

	if completion.Duration < 0 {
		completion.Duration = 0
	}
	if hook == nil {
		return
	}
	// A composition hook must not be able to take down the process reaper or
	// rewrite a successful shell exit as a shell failure. The reaper's own panic
	// guard remains responsible for wait/finish panics.
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("background shell completion hook panicked",
				"shell_id", completion.ShellID, "panic", recovered)
		}
	}()
	hook(completion)
}
