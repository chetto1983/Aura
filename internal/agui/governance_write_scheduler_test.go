package agui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/cron"
)

const schedTaskID = "11111111-1111-1111-1111-111111111111"

// scriptedSchedulerBoard is the shared SchedulerBoardProvider fake (used by BOTH the
// read-board tests in governance_api_test.go and the write-verb tests here): canned tasks +
// run history with recorded pagination args (proving the default limit/offset + that a
// non-UUID id never reaches the store), plus mutation scripting — the task GetTask returns,
// per-verb errors, and the id/params each verb was called with.
type scriptedSchedulerBoard struct {
	tasks       []cron.Task
	tasksErr    error
	runs        []cron.Run
	runsErr     error
	gotLimit    int
	gotOffset   int
	runsReached bool

	getTask     cron.Task
	getErr      error
	approveErr  error
	runErr      error
	cancelErr   error
	updateErr   error
	pauseErr    error
	resumeErr   error
	resumeNext  time.Time
	approvedID  string
	ranID       string
	pausedID    string
	resumedID   string
	cancelledID string
	updatedID   string
	updated     cron.UpdateTaskParams
}

func (b *scriptedSchedulerBoard) ListManageableTasks(context.Context) ([]cron.Task, error) {
	return b.tasks, b.tasksErr
}

func (b *scriptedSchedulerBoard) GetTask(context.Context, string) (cron.Task, error) {
	return b.getTask, b.getErr
}

func (b *scriptedSchedulerBoard) ApproveTask(_ context.Context, id string) error {
	b.approvedID = id
	return b.approveErr
}

func (b *scriptedSchedulerBoard) RunTaskNow(_ context.Context, id string) error {
	b.ranID = id
	return b.runErr
}

func (b *scriptedSchedulerBoard) PauseTask(_ context.Context, id string) error {
	b.pausedID = id
	return b.pauseErr
}

func (b *scriptedSchedulerBoard) ResumeTask(_ context.Context, id string, _ time.Time) (time.Time, error) {
	b.resumedID = id
	return b.resumeNext, b.resumeErr
}

func (b *scriptedSchedulerBoard) CancelTask(_ context.Context, id string) error {
	b.cancelledID = id
	return b.cancelErr
}

func (b *scriptedSchedulerBoard) UpdateTask(_ context.Context, id string, p cron.UpdateTaskParams) error {
	b.updatedID = id
	b.updated = p
	return b.updateErr
}

func (b *scriptedSchedulerBoard) ListRunsForTask(_ context.Context, _ string, limit, offset int) ([]cron.Run, error) {
	b.runsReached = true
	b.gotLimit = limit
	b.gotOffset = offset
	return b.runs, b.runsErr
}

// doGovBody issues a governance request with a body (the PATCH edit path doGov cannot cover).
func doGovBody(t *testing.T, s *Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := withPrincipal(httptest.NewRequest(method, target, strings.NewReader(body)), govOperator)
	rec := httptest.NewRecorder()
	s.Mux().ServeHTTP(rec, req)
	return rec
}

func TestSchedulerApproveManageable(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindReminder, Status: "pending_approval"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/approve")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if board.approvedID != schedTaskID {
		t.Fatalf("ApproveTask id = %q, want %q", board.approvedID, schedTaskID)
	}
}

// A system-seeded sweep is never operator-mutable: the guard returns 403 BEFORE any mutate.
func TestSchedulerApproveSystemKindForbidden(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindIdentityPurge, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/approve")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if board.approvedID != "" {
		t.Fatal("a system task must not reach ApproveTask")
	}
}

func TestSchedulerApproveNotFound(t *testing.T) {
	board := &scriptedSchedulerBoard{getErr: cron.ErrTaskNotFound}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/approve")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// Approve on a task that exists + is manageable but is no longer pending → 409 (the store
// returns ErrTaskNotFound for the zero-rows UPDATE, mapped to conflict after the guard).
func TestSchedulerApproveConflict(t *testing.T) {
	board := &scriptedSchedulerBoard{
		getTask:    cron.Task{Kind: cron.KindReminder, Status: "active"},
		approveErr: cron.ErrTaskNotFound,
	}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/approve")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestSchedulerApproveBadUUID(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindReminder}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/not-a-uuid/approve")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (non-uuid never reaches the store)", rec.Code)
	}
	if board.approvedID != "" {
		t.Fatal("a non-uuid id must not reach ApproveTask")
	}
}

func TestSchedulerRunNow(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindAgentJob, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/run")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if board.ranID != schedTaskID {
		t.Fatalf("RunTaskNow id = %q, want %q", board.ranID, schedTaskID)
	}
}

func TestSchedulerCancel(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindReminder, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodDelete, "/api/governance/scheduler/"+schedTaskID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if board.cancelledID != schedTaskID {
		t.Fatalf("CancelTask id = %q, want %q", board.cancelledID, schedTaskID)
	}
}

// The database backup may be run or edited but never cancelled: 403 before the store is touched.
func TestSchedulerCancelBackupForbidden(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindBackupPostgres, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodDelete, "/api/governance/scheduler/"+schedTaskID)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if board.cancelledID != "" {
		t.Fatal("the database backup must not reach CancelTask")
	}
}

// The row tells the board where to offer Delete, so the backup rule is not mirrored client-side.
func TestSchedulerRowsMarkTheBackupNotCancellable(t *testing.T) {
	rows := schedulerRowsFor(t, []cron.Task{
		{ID: "r1", Kind: cron.KindReminder, Status: "active"},
		{ID: "b1", Kind: cron.KindBackupPostgres, Status: "active"},
	})
	want := map[string]bool{"r1": true, "b1": false}
	if len(rows) != len(want) {
		t.Fatalf("want %d rows, got %d", len(want), len(rows))
	}
	for _, row := range rows {
		id, _ := row["ID"].(string)
		if got, ok := row["Cancellable"].(bool); !ok || got != want[id] {
			t.Errorf("row %s Cancellable = %v, want %v", id, row["Cancellable"], want[id])
		}
	}
}

func TestSchedulerEditValid(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{
		Kind: cron.KindReminder, Status: "active", Payload: []byte(`{"text":"keep me"}`),
	}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGovBody(t, s, http.MethodPatch, "/api/governance/scheduler/"+schedTaskID,
		`{"schedule_kind":"every","every_minutes":15,"notify":"whatsapp"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if board.updatedID != schedTaskID {
		t.Fatalf("UpdateTask id = %q, want %q", board.updatedID, schedTaskID)
	}
	if board.updated.Spec.EveryMinutes != 15 || board.updated.NotifyRoute != "whatsapp" {
		t.Fatalf("UpdateTask spec = %+v notify=%q, want every=15 notify=whatsapp", board.updated.Spec, board.updated.NotifyRoute)
	}
	// An omitted payload keeps the task's current one (schedule-only edit).
	if string(board.updated.Payload) != `{"text":"keep me"}` {
		t.Fatalf("omitted payload must keep current, got %s", board.updated.Payload)
	}
}

func TestSchedulerEditBadSchedule(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindReminder, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGovBody(t, s, http.MethodPatch, "/api/governance/scheduler/"+schedTaskID,
		`{"schedule_kind":"cron","cron":"not a cron expr"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if board.updatedID != "" {
		t.Fatal("an invalid schedule must not reach UpdateTask")
	}
}

func TestSchedulerEditSystemKindForbidden(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindSandboxReap, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGovBody(t, s, http.MethodPatch, "/api/governance/scheduler/"+schedTaskID,
		`{"schedule_kind":"every","every_minutes":15}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if board.updatedID != "" {
		t.Fatal("a system task must not reach UpdateTask")
	}
}

// An unwired scheduler provider answers the write verbs 503 (mirrors the read board).
func TestSchedulerMutateUnwired(t *testing.T) {
	s := govServer(GovernanceProviders{})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/approve")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestSchedulerPauseActiveTask(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindAgentJob, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/pause")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"paused"`) {
		t.Fatalf("pause = %d %s, want 200 paused", rec.Code, rec.Body.String())
	}
	if board.pausedID != schedTaskID {
		t.Fatalf("PauseTask id = %q, want %q", board.pausedID, schedTaskID)
	}
}

func TestSchedulerPauseRefusesTheBackup(t *testing.T) {
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindBackupPostgres, Status: "active"}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/pause")
	if rec.Code != http.StatusForbidden || board.pausedID != "" {
		t.Fatalf("pause backup = %d (store reached: %q), want 403 and no store call", rec.Code, board.pausedID)
	}
}

func TestSchedulerPauseAndResumeRefuseASystemSweep(t *testing.T) {
	for _, verb := range []string{"pause", "resume"} {
		board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.TaskKind("retention_sweep"), Status: "active"}}
		s := govServer(GovernanceProviders{Scheduler: board})
		rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/"+verb)
		if rec.Code != http.StatusForbidden || board.pausedID != "" || board.resumedID != "" {
			t.Fatalf("%s system sweep = %d, want 403 and no store call", verb, rec.Code)
		}
	}
}

func TestSchedulerPauseOfATaskThatIsNotActiveConflicts(t *testing.T) {
	board := &scriptedSchedulerBoard{
		getTask:  cron.Task{Kind: cron.KindReminder, Status: "paused"},
		pauseErr: fmt.Errorf("pause: %w", cron.ErrTaskNotFound),
	}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/pause")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "not active") {
		t.Fatalf("pause of a paused task = %d %s, want 409 not active", rec.Code, rec.Body.String())
	}
}

func TestSchedulerResumeReportsTheNextFire(t *testing.T) {
	next := time.Date(2030, 1, 2, 9, 30, 0, 0, time.UTC)
	board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindReminder, Status: "paused"}, resumeNext: next}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/resume")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "2030-01-02T09:30:00Z") {
		t.Fatalf("resume = %d %s, want 200 with the next fire", rec.Code, rec.Body.String())
	}
	if board.resumedID != schedTaskID {
		t.Fatalf("ResumeTask id = %q, want %q", board.resumedID, schedTaskID)
	}
}

func TestSchedulerResumeConflicts(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"not paused", fmt.Errorf("resume: %w", cron.ErrTaskNotFound), "not paused"},
		{"one-shot in the past", fmt.Errorf("resume: %w", cron.ErrResumePastOneShot), "edit it to a new time"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			board := &scriptedSchedulerBoard{getTask: cron.Task{Kind: cron.KindReminder, Status: "paused"}, resumeErr: tc.err}
			s := govServer(GovernanceProviders{Scheduler: board})
			rec := doGov(t, s, http.MethodPost, "/api/governance/scheduler/"+schedTaskID+"/resume")
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("resume = %d %s, want 409 containing %q", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

func TestSchedulerBoardCarriesWhyATaskIsPaused(t *testing.T) {
	board := &scriptedSchedulerBoard{tasks: []cron.Task{{
		ID: schedTaskID, Kind: cron.KindAgentJob, Status: "paused",
		PausedReason: cron.PausedByFailures, ConsecutiveFailures: 3,
	}}}
	s := govServer(GovernanceProviders{Scheduler: board})
	rec := doGov(t, s, http.MethodGet, "/api/governance/scheduler")
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"PausedReason":"failures"`) || !strings.Contains(body, `"ConsecutiveFailures":3`) {
		t.Fatalf("board = %d %s, want the pause reason and the failure count", rec.Code, body)
	}
}
