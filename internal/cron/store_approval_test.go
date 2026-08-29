//go:build db_integration

// Integration tests for the pending-approval reminder store queries (fix-plan 1.7 /
// Amendment #92). Requires the Phase-10 + 0051 migrations applied. Run via:
//
//	go test -tags db_integration -race ./internal/cron -count=1
//
// No-skip-as-green: migratedPool's envOrSkip t.Fatals under $CI when the DSN is unset.
package cron

import (
	"context"
	"testing"
	"time"
)

// TestListDueApprovalReminders_FiltersAndThrottle proves the SQL contract (Amendment #92
// revised): every pending_approval row WITH an origin conversation is returned — including a
// WebUI/local-origin row (it surfaces via the pull /api/approvals) — while a row with NO origin
// conversation (CLI-origin) and non-pending rows are excluded; NULL-stamp rows are always due;
// MarkApprovalReminded stamps now() so the cutoff throttle then excludes the row until it ages
// past the cadence.
func TestListDueApprovalReminders_FiltersAndThrottle(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(pool)

	spec, err := ParseSchedule("cron", "30 9 * * *", 0, time.Time{}, "Europe/Rome")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	next, err := NextRunAt(spec, time.Now())
	if err != nil {
		t.Fatalf("NextRunAt: %v", err)
	}

	mk := func(identity, status, originConv string) string {
		task, err := s.CreateTask(ctx, CreateTaskParams{
			Kind: KindAgentJob, Spec: spec, Payload: []byte(`{"goal":"x"}`),
			NextRunAt: next, IdentityID: identity, Status: status, OriginConversationID: originConv, NotifyRoute: "none",
		})
		if err != nil {
			t.Fatalf("CreateTask(%s,%s): %v", identity, status, err)
		}
		t.Cleanup(func() { cleanupTask(t, pool, task.ID) })
		return task.ID
	}

	idA := "11111111-1111-1111-1111-111111111111"
	idB := "22222222-2222-2222-2222-222222222222"
	convA := "aaaa1111-1111-1111-1111-111111111111"
	convB := "bbbb2222-2222-2222-2222-222222222222"
	convW := "cccc3333-3333-3333-3333-333333333333"
	pendingA := mk(idA, "pending_approval", convA)
	pendingB := mk(idB, "pending_approval", convB)
	pendingWebUI := mk("local", "pending_approval", convW)      // INCLUDED now: WebUI-origin (pulls the pause)
	noOrigin := mk(idA, "pending_approval", "")                 // excluded: CLI-origin (NULL origin conversation)
	mk("33333333-3333-3333-3333-333333333333", "active", convA) // excluded: not pending

	ids := func(tasks []Task) map[string]bool {
		m := map[string]bool{}
		for _, tk := range tasks {
			m[tk.ID] = true
		}
		return m
	}

	// Never-reminded (NULL stamp) rows are due regardless of cutoff; the WebUI-origin row is
	// now included, the CLI-origin (no-conversation) + active rows excluded.
	due, err := s.ListDueApprovalReminders(ctx, time.Now(), 50)
	if err != nil {
		t.Fatalf("ListDueApprovalReminders: %v", err)
	}
	got := ids(due)
	if !got[pendingA] || !got[pendingB] {
		t.Fatalf("channel-owned pending_approval rows must be due, got %v", got)
	}
	if !got[pendingWebUI] {
		t.Fatalf("a WebUI/local-origin pending row WITH an origin conversation must be due (it pulls the pause), got %v", got)
	}
	if got[noOrigin] {
		t.Fatalf("a CLI-origin row with NO origin conversation must be excluded, got %v", got)
	}
	for _, tk := range due {
		if tk.Status != "pending_approval" {
			t.Fatalf("non-pending row selected: %s status=%q", tk.ID, tk.Status)
		}
		if tk.OriginConversationID == "" {
			t.Fatalf("a row with no origin conversation must never be selected: %s", tk.ID)
		}
	}

	// Stamp pendingA → it re-nudges at most once per cadence.
	if err := s.MarkApprovalReminded(ctx, pendingA); err != nil {
		t.Fatalf("MarkApprovalReminded: %v", err)
	}

	// Past cutoff (now-1h): the freshly-stamped pendingA (stamp=now) is NOT due; pendingB
	// (still NULL) is.
	duePast, err := s.ListDueApprovalReminders(ctx, time.Now().Add(-time.Hour), 50)
	if err != nil {
		t.Fatalf("ListDueApprovalReminders(past): %v", err)
	}
	gotPast := ids(duePast)
	if gotPast[pendingA] {
		t.Fatalf("freshly-stamped row must be throttled out at a past cutoff")
	}
	if !gotPast[pendingB] {
		t.Fatalf("NULL-stamp row must always be due")
	}

	// Future cutoff (now+1h): the stamped pendingA is due again (stamp <= future cutoff).
	dueFuture, err := s.ListDueApprovalReminders(ctx, time.Now().Add(time.Hour), 50)
	if err != nil {
		t.Fatalf("ListDueApprovalReminders(future): %v", err)
	}
	if !ids(dueFuture)[pendingA] {
		t.Fatalf("a stamped row must re-nudge once its stamp ages past the cadence cutoff")
	}
}
