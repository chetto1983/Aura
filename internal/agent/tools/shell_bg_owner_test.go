package tools

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

// ctxWithIdentity builds a tool-call ctx carrying BOTH the session key (via
// WithToolCallContext) and the authenticated identity principal (identityctx) — the
// two coordinates the poll/kill authority check reads (MUSR-03).
func ctxWithIdentity(t *testing.T, sessionID, toolCallID, identityID string) context.Context {
	t.Helper()
	ctx := WithToolCallContext(context.Background(), sessionID, toolCallID, t.TempDir(), testCap)
	return identityctx.WithIdentityID(ctx, identityID)
}

// stubCapabilityChecker is the injected admin-capability seam for the D-18 tests.
type stubCapabilityChecker struct {
	allow  bool
	err    error
	gotID  string
	gotCap string
}

func (s *stubCapabilityChecker) HasCapability(_ context.Context, id, capability string) (bool, error) {
	s.gotID, s.gotCap = id, capability
	return s.allow, s.err
}

// TestBackgroundJobID: minted ids are unguessable 128-bit crypto-random hex, never
// the pre-MUSR-03 sequential sh_<int>, and never collide (MUSR-03).
func TestBackgroundJobID(t *testing.T) {
	hexRe := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seqRe := regexp.MustCompile(`^sh_\d+$`)

	seen := make(map[string]bool, 512)
	for i := 0; i < 512; i++ {
		id, err := newBackgroundShellID()
		if err != nil {
			t.Fatalf("newBackgroundShellID: %v", err)
		}
		if !hexRe.MatchString(id) {
			t.Fatalf("id %q is not 128-bit lowercase hex", id)
		}
		if seqRe.MatchString(id) {
			t.Fatalf("id %q is a guessable sequential sh_<int>", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id minted: %q", id)
		}
		seen[id] = true
	}

	// Two real starts must bind distinct crypto ids (not sh_1/sh_2). echo exists on
	// both the POSIX shell and the cmd fallback; Shutdown joins the reapers (goleak).
	t.Run("start binds crypto ids", func(t *testing.T) {
		bg := NewBackgroundShells(nil)
		ctx := ctxWith(t, "sess-id", "call-id")
		id1, err := bg.start(ctx, "echo one", "", mergeEnv(nil))
		if err != nil {
			t.Fatalf("start1: %v", err)
		}
		id2, err := bg.start(ctx, "echo two", "", mergeEnv(nil))
		if err != nil {
			t.Fatalf("start2: %v", err)
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), backgroundKillWaitTimeout())
		defer cancel()
		defer func() { _ = bg.Shutdown(shutCtx) }()
		if id1 == id2 {
			t.Fatalf("two starts minted the same id %q", id1)
		}
		for _, id := range []string{id1, id2} {
			if !hexRe.MatchString(id) || seqRe.MatchString(id) {
				t.Fatalf("start bound a guessable id %q", id)
			}
		}
	})
}

// TestBackgroundJobOwnerDeny: a job is bound to its (identity, session) owner; a
// foreign caller cannot poll (404-shaped miss) or kill (403 refusal) it, while the
// owner and an admin-capability holder can (D-06 read=404/mutate=403, D-18).
func TestBackgroundJobOwnerDeny(t *testing.T) {
	const (
		ownerA = "11111111-1111-1111-1111-111111111111"
		ownerB = "22222222-2222-2222-2222-222222222222"
		jobID  = "job-owned-by-A"
	)
	newReg := func() *BackgroundShells {
		return &BackgroundShells{shells: map[string]*bgShell{
			jobID: {ownerID: ownerA, sessionID: "sess-A", cancel: func() {}, bufCap: 1024},
		}}
	}
	killArgs := mustJSON(t, map[string]string{"shell_id": jobID})
	pollArgs := mustJSON(t, map[string]string{"shell_id": jobID})

	t.Run("foreign session poll is a not-found miss", func(t *testing.T) {
		bg := newReg()
		_, err := (&ShellPoll{Shells: bg}).Execute(ctxWithIdentity(t, "sess-B", "call-B", ownerB), pollArgs)
		if err == nil || !strings.Contains(err.Error(), "unknown shell_id") {
			t.Fatalf("foreign poll must return the not-found shape (D-06 read=404), got %v", err)
		}
	})

	t.Run("foreign session kill is refused and does not terminate", func(t *testing.T) {
		bg := newReg()
		_, err := (&ShellKill{Shells: bg}).Execute(ctxWithIdentity(t, "sess-B", "call-B", ownerB), killArgs)
		if err == nil || !strings.Contains(err.Error(), "not authorized") {
			t.Fatalf("foreign kill must be refused (D-06 mutate=403), got %v", err)
		}
		sh, _ := bg.get(jobID)
		sh.mu.Lock()
		killed := sh.killed
		sh.mu.Unlock()
		if killed {
			t.Fatal("a refused foreign kill still terminated the job (authority bypass)")
		}
	})

	t.Run("same owner may poll and kill", func(t *testing.T) {
		bg := newReg()
		ctx := ctxWithIdentity(t, "sess-A", "call-A", ownerA)
		if _, err := (&ShellPoll{Shells: bg}).Execute(ctx, pollArgs); err != nil {
			t.Fatalf("owner poll should succeed: %v", err)
		}
		if _, err := (&ShellKill{Shells: bg}).Execute(ctx, killArgs); err != nil {
			t.Fatalf("owner kill should succeed: %v", err)
		}
	})

	t.Run("admin capability may poll and kill a foreign job", func(t *testing.T) {
		bg := newReg()
		pollCaps := &stubCapabilityChecker{allow: true}
		if _, err := (&ShellPoll{Shells: bg, Caps: pollCaps}).Execute(ctxWithIdentity(t, "sess-admin", "call-a1", "admin-id"), pollArgs); err != nil {
			t.Fatalf("admin poll should be allowed (D-18): %v", err)
		}
		if pollCaps.gotCap != adminShellCapability || pollCaps.gotID != "admin-id" {
			t.Fatalf("admin path consulted (%q,%q), want (admin-id,%q)", pollCaps.gotID, pollCaps.gotCap, adminShellCapability)
		}
		if _, err := (&ShellKill{Shells: bg, Caps: &stubCapabilityChecker{allow: true}}).Execute(ctxWithIdentity(t, "sess-admin", "call-a2", "admin-id"), killArgs); err != nil {
			t.Fatalf("admin kill should be allowed (D-18): %v", err)
		}
	})

	t.Run("a non-admin foreign caller stays denied even with a Caps checker", func(t *testing.T) {
		bg := newReg()
		caps := &stubCapabilityChecker{allow: false}
		if _, err := (&ShellPoll{Shells: bg, Caps: caps}).Execute(ctxWithIdentity(t, "sess-B", "call-B", ownerB), pollArgs); err == nil {
			t.Fatal("a store-denied foreign caller must still be denied (fail-closed)")
		}
	})
}
