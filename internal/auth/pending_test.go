package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequestAccess_Fresh(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	fresh, err := s.RequestAccess(ctx, "u1", "alice")
	if err != nil {
		t.Fatalf("request access: %v", err)
	}
	if !fresh {
		t.Fatal("fresh=false on first request, want true")
	}
	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].UserID != "u1" || pending[0].Username != "alice" {
		t.Fatalf("pending = %+v, want one row {u1, alice}", pending)
	}
}

func TestRequestAccess_RepeatNotFresh(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.RequestAccess(ctx, "u1", "alice"); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.RequestAccess(ctx, "u1", "alice2")
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Fatal("repeat /start while still pending should not be fresh")
	}
	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Username should NOT have been bumped on a still-pending row, since
	// the SQL guards the UPDATE branch with decision IS NOT NULL.
	if len(pending) != 1 || pending[0].Username != "alice" {
		t.Fatalf("pending = %+v, want one row {u1, alice} (no username bump on still-pending)", pending)
	}
}

func TestRequestAccess_AfterDenyIsFresh(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.RequestAccess(ctx, "u1", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Deny(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	// Same user retries → re-opens the row, fresh = true again.
	fresh, err := s.RequestAccess(ctx, "u1", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !fresh {
		t.Fatal("re-request after deny should be fresh")
	}
	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending = %+v, want one re-opened row", pending)
	}
}

func TestRequestAccess_RejectsEmptyUser(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.RequestAccess(context.Background(), "", "x"); err == nil {
		t.Fatal("expected error for empty user id")
	}
}

func TestApprove_GrantsAccess(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.RequestAccess(ctx, "u1", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, "u1"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	ok, err := s.IsUserAllowed(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("u1 should be allowed after approve")
	}
	// Pending list now empty — decision was set.
	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after approve = %+v, want empty", pending)
	}
}

func TestApprove_NoPendingRequest(t *testing.T) {
	s := newTestStore(t)
	if err := s.Approve(context.Background(), "ghost"); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestApprove_DoubleApproveRejected(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.RequestAccess(ctx, "u1", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, "u1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("second approve err = %v, want ErrInvalid", err)
	}
}

func TestDeny_RejectsRequest(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.RequestAccess(ctx, "u1", "alice"); err != nil {
		t.Fatal(err)
	}
	if err := s.Deny(ctx, "u1"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	ok, err := s.IsUserAllowed(ctx, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("u1 should NOT be allowed after deny")
	}
}

func TestDeny_NoPendingRequest(t *testing.T) {
	s := newTestStore(t)
	if err := s.Deny(context.Background(), "ghost"); !errors.Is(err, ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestListPending_OrderedByRequestedAt(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// Drive deterministic timestamps by stubbing s.now. Each tick is 1
	// second apart so the RFC3339 strings sort correctly.
	base := mustNow(t, "2026-01-01T00:00:00Z")
	tick := 0
	s.now = func() time.Time {
		out := base.Add(time.Duration(tick) * time.Second)
		tick++
		return out
	}
	if _, err := s.RequestAccess(ctx, "u_b", "b"); err != nil { // tick 0
		t.Fatal(err)
	}
	if _, err := s.RequestAccess(ctx, "u_a", "a"); err != nil { // tick 1
		t.Fatal(err)
	}
	if _, err := s.RequestAccess(ctx, "u_c", "c"); err != nil { // tick 2
		t.Fatal(err)
	}
	pending, err := s.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 3 {
		t.Fatalf("len(pending) = %d, want 3", len(pending))
	}
	if pending[0].UserID != "u_b" || pending[1].UserID != "u_a" || pending[2].UserID != "u_c" {
		t.Errorf("order = [%s,%s,%s], want [u_b,u_a,u_c] (insertion order = oldest requested_at first)",
			pending[0].UserID, pending[1].UserID, pending[2].UserID)
	}
}

func mustNow(t *testing.T, s string) time.Time {
	t.Helper()
	out, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return out
}

func TestAllowedUserIDs_IncludesBootstrapAndApproved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if claimed, err := s.BootstrapUser(ctx, "owner"); err != nil || !claimed {
		t.Fatalf("bootstrap claimed=%v err=%v", claimed, err)
	}
	if _, err := s.RequestAccess(ctx, "guest", "g"); err != nil {
		t.Fatal(err)
	}
	if err := s.Approve(ctx, "guest"); err != nil {
		t.Fatal(err)
	}
	ids, err := s.AllowedUserIDs(ctx)
	if err != nil {
		t.Fatalf("allowed user ids: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("ids = %v, want 2 entries", ids)
	}
}
