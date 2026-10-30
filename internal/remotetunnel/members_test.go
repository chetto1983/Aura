package remotetunnel

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestMembersRefuseEmptyAndLastAdminLockout(t *testing.T) {
	for _, m := range []testMembers{{}, {emails: []string{"user@example.com"}}, {emails: []string{"user@example.com"}, admins: []string{"admin@example.com"}}} {
		h := newHarness(t)
		h.members = m
		h.r = h.newReconciler()
		if err := h.r.Reconcile(t.Context()); !errors.Is(err, ErrMemberLockout) {
			t.Fatalf("err=%v", err)
		}
		if h.cloud.creates != 0 || len(h.projection.applied) != 0 {
			t.Fatal("unsafe publication")
		}
	}
}

func TestMembersRetryConvergesFromDegraded(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	now := time.Unix(1000, 0)
	h.r.now = func() time.Time { return now }
	h.cloud.failure = transientError()
	if h.r.SyncMembers(t.Context()) == nil || h.store.state.Phase != PhaseDegraded {
		t.Fatal("sync failure not degraded")
	}
	h.cloud.failure = nil
	now = now.Add(time.Second)
	if err := h.r.SyncMembers(t.Context()); err != nil {
		t.Fatal(err)
	}
	if h.store.state.Phase != PhaseConnecting || h.store.state.Reason != "" {
		t.Fatal("successful sync remained degraded")
	}
}

func TestMembersSyncAddsAndRemovesEmailsWithConfirmedAdministrator(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	for _, emails := range [][]string{{"ADMIN@example.com", "user@example.com", "user@example.com"}, {"admin@example.com"}} {
		h.members.emails = emails
		h.r = h.newReconciler()
		if err := h.r.SyncMembers(t.Context()); err != nil {
			t.Fatal(err)
		}
		want, _ := normalizedEmails(emails)
		for _, p := range h.cloud.policies {
			got := []string{}
			for _, rule := range p.Include {
				got = append(got, rule.Email.Email)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got=%v want=%v", got, want)
			}
		}
	}
	if h.cloud.creates != 10 || len(h.projection.applied) != 1 {
		t.Fatal("member sync reprovisioned")
	}
}

func TestMembersTerminalFailureStopsAutomaticSync(t *testing.T) {
	h := newHarness(t)
	h.assertResources(t)
	h.store.state.Phase = PhaseError
	calls := h.cloud.calls
	if err := h.r.SyncMembers(t.Context()); !errors.Is(err, ErrTerminal) || h.cloud.calls != calls {
		t.Fatalf("terminal membership retry: %v", err)
	}
}

func TestMembersSyncRequiresAdministratorConfirmedInBothPolicies(t *testing.T) {
	h := newHarness(t)
	if err := h.r.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	h.members = testMembers{emails: []string{"next@example.com"}, admins: []string{"next@example.com"}}
	h.r = h.newReconciler()
	if err := h.r.SyncMembers(t.Context()); !errors.Is(err, ErrMemberLockout) {
		t.Fatalf("err=%v", err)
	}
	if h.cloud.policies["public-policy"].Include[0].Email.Email != "admin@example.com" {
		t.Fatal("last admin removed")
	}
}
