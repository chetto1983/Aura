package agui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/cron"
)

// The scheduler board shows a member only their own tasks, and refuses the others as if they
// did not exist (prd.md §3, 2026-10-10). Measured before the fix: a member listed every identity's
// tasks, rewrote and ran the admin's agent_job (it ran as the admin) and made the nightly
// backup yearly (docs/superpowers/verification/2026-10-10-member-scheduler-and-skills.md).

const schedulerMember = "00000000-0000-0000-0000-0000000000b2"

func doSchedulerAsMember(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := withPrincipal(httptest.NewRequest(method, target, strings.NewReader(body)), schedulerMember)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

func TestSchedulerListShowsAMemberOnlyTheirOwnTasks(t *testing.T) {
	board := &scriptedSchedulerBoard{tasks: []cron.Task{
		{ID: "01a12555-e469-73cd-9181-68a5405d47c8", Kind: cron.KindBackupPostgres, IdentityID: "local"},
		{ID: "11111111-1111-1111-1111-111111111111", Kind: cron.KindAgentJob, IdentityID: govOperator},
		{ID: "22222222-2222-2222-2222-222222222222", Kind: cron.KindReminder, IdentityID: schedulerMember},
	}}
	rec := doSchedulerAsMember(t, govServer(GovernanceProviders{Scheduler: board}), http.MethodGet, "/api/governance/scheduler", "")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "22222222-2222-2222-2222-222222222222") {
		t.Fatalf("member list = %d %s, want their own reminder", rec.Code, body)
	}
	for _, other := range []string{"01a12555-e469-73cd-9181-68a5405d47c8", "11111111-1111-1111-1111-111111111111"} {
		if strings.Contains(body, other) {
			t.Errorf("member list shows another identity's task %s: %s", other, body)
		}
	}
}

func TestSchedulerRefusesAMemberAnotherIdentitysTask(t *testing.T) {
	const target = "/api/governance/scheduler/11111111-1111-1111-1111-111111111111"
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodGet, target + "/runs", ""},
		{http.MethodPost, target + "/run", ""},
		{http.MethodPost, target + "/approve", ""},
		{http.MethodPost, target + "/pause", ""},
		{http.MethodPost, target + "/resume", ""},
		{http.MethodDelete, target, ""},
		{http.MethodPatch, target, `{"schedule_kind":"every","every_minutes":5,"tz":"Europe/Rome","payload":{"goal":"x"},"notify":"none"}`},
	} {
		board := &scriptedSchedulerBoard{getTask: cron.Task{
			ID: "11111111-1111-1111-1111-111111111111", Kind: cron.KindAgentJob, Status: "active", IdentityID: govOperator,
		}}
		rec := doSchedulerAsMember(t, govServer(GovernanceProviders{Scheduler: board}), tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s = %d, want 404", tc.method, tc.path, rec.Code)
		}
		if board.ranID+board.approvedID+board.pausedID+board.resumedID+board.cancelledID+board.updatedID != "" || board.runsReached {
			t.Errorf("%s %s reached the store: %+v", tc.method, tc.path, board)
		}
	}
}

func TestSchedulerLetsAMemberRunTheirOwnTask(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{
		ID: "22222222-2222-2222-2222-222222222222", Kind: cron.KindReminder, Status: "active", IdentityID: schedulerMember,
	}}
	rec := doSchedulerAsMember(t, govServer(GovernanceProviders{Scheduler: board}), http.MethodPost, "/api/governance/scheduler/22222222-2222-2222-2222-222222222222/run", "")
	if rec.Code != http.StatusOK || board.ranID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("own run = %d (ran %q), want 200 and the run queued", rec.Code, board.ranID)
	}
}
