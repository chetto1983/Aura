package agui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/hostupdate"
	"github.com/chetto1983/aura/internal/identity"
)

const (
	updateAdminID  = "448ddbe1-96ea-405d-8219-4a3d52a425c0"
	updateMemberID = "5a1c7c1e-0000-4000-8000-000000000002"
)

var updateNow = time.Unix(1790320000, 0).UTC()

func updateServer(t *testing.T, statusBody string) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	if statusBody != "" {
		if err := os.WriteFile(filepath.Join(dir, hostupdate.StatusFile), []byte(statusBody), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{idAdmin: &fakeIdentityAdmin{caps: map[string][]string{updateAdminID: {identity.CapIdentityCreate}}}}
	s.SetHostUpdate(dir)
	s.hostUpdateNow = func() time.Time { return updateNow }
	return s, dir
}

const pendingStatus = "state=pending\nrunning_rev=4de507676c6b\navailable_rev=7886200e5abc\navailable_built=1790316300\n" +
	"pending_since=1790316600\ndeadline=1790403000\nhandled_request=0123456789abcdef0123456789abcdef\n" +
	"error=\nchecked_at=1790319900\n"

func getUpdate(t *testing.T, s *Server, principal string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleSystemUpdateGet(rec, withPrincipal(httptest.NewRequest(http.MethodGet, "/api/system/update", nil), principal))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status %d body %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestSystemUpdateWithoutAHostUpdaterIsUnmanaged(t *testing.T) {
	s, _ := updateServer(t, "")
	body := getUpdate(t, s, updateAdminID)
	if body["managed"] != false || body["state"] != nil {
		t.Fatalf("body %v, want managed=false and nothing else", body)
	}
}

func TestSystemUpdateTellsAMemberOnlyWhatIsHappening(t *testing.T) {
	s, dir := updateServer(t, pendingStatus+"error=pull failed\n")
	writeTestActivity(t, dir)
	body := getUpdate(t, s, updateMemberID)
	if body["state"] != "pending" || body["can_decide"] != false || body["available_rev"] != "7886200e5abc" {
		t.Fatalf("member body %v", body)
	}
	for _, adminOnly := range []string{"error", "last_activity_at", "live_runs"} {
		if _, ok := body[adminOnly]; ok {
			t.Fatalf("a member was shown %q: %v", adminOnly, body)
		}
	}
}

func TestSystemUpdateShowsAnAdminWhatANowWouldInterrupt(t *testing.T) {
	s, dir := updateServer(t, pendingStatus)
	writeTestActivity(t, dir)
	body := getUpdate(t, s, updateAdminID)
	want := map[string]any{
		"managed":            true,
		"state":              "pending",
		"running_rev":        "4de507676c6b",
		"available_rev":      "7886200e5abc",
		"available_built_at": rfc(1790316300),
		"pending_since":      rfc(1790316600),
		"deadline":           rfc(1790403000),
		"deferred_until":     nil,
		"checked_at":         rfc(1790319900),
		"can_decide":         true,
		"error":              "",
		"last_activity_at":   rfc(1790317200),
		"live_runs":          float64(2),
	}
	for key, value := range want {
		if body[key] != value {
			t.Errorf("%s = %#v, want %#v", key, body[key], value)
		}
	}
}

func TestSystemUpdateReportsARequestTheUpdaterHasNotHandledYet(t *testing.T) {
	s, dir := updateServer(t, pendingStatus)
	if err := hostupdate.WriteRequest(dir, hostupdate.Request{ID: "fedcba9876543210fedcba9876543210", Action: hostupdate.ActionApply, By: updateAdminID}); err != nil {
		t.Fatal(err)
	}
	if got := getUpdate(t, s, updateMemberID)["state"]; got != "requested" {
		t.Fatalf("state = %v, want requested while the updater has not picked the request up", got)
	}
}

func TestSystemUpdateShowsAnUnhandledDeferralAsTheDeferral(t *testing.T) {
	s, dir := updateServer(t, pendingStatus)
	until := updateNow.Add(time.Hour)
	if err := hostupdate.WriteRequest(dir, hostupdate.Request{ID: "fedcba9876543210fedcba9876543210", Action: hostupdate.ActionDefer, Until: until, By: updateAdminID}); err != nil {
		t.Fatal(err)
	}
	body := getUpdate(t, s, updateAdminID)
	if body["state"] != "pending" || body["deferred_until"] != until.Format(time.RFC3339) {
		t.Fatalf("body %v, want the deferral shown before the updater records it", body)
	}
}

func TestSystemUpdateAnUnreadableStatusIsUnknownNotAnError(t *testing.T) {
	s, _ := updateServer(t, "state=exploded\n")
	if got := getUpdate(t, s, updateAdminID)["state"]; got != "unknown" {
		t.Fatalf("state = %v, want unknown", got)
	}
}

func postUpdate(t *testing.T, s *Server, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := withPrincipal(httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)), updateAdminID)
	rec := httptest.NewRecorder()
	switch path {
	case "/api/system/update/apply":
		s.handleSystemUpdateApply(rec, req)
	case "/api/system/update/defer":
		s.handleSystemUpdateDefer(rec, req)
	}
	return rec
}

func TestSystemUpdateApplyHandsTheUpdaterANewRequest(t *testing.T) {
	s, dir := updateServer(t, pendingStatus)
	rec := postUpdate(t, s, "/api/system/update/apply", "")
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"state":"requested"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
	req, ok, err := hostupdate.ReadRequest(dir)
	if err != nil || !ok || req.Action != hostupdate.ActionApply || req.By != updateAdminID {
		t.Fatalf("request %+v ok=%v err=%v", req, ok, err)
	}
	if req.ID == "0123456789abcdef0123456789abcdef" {
		t.Fatal("the request reused the id the updater already handled, so it would be ignored")
	}
}

func TestSystemUpdateApplyRefusesWhenThereIsNothingToApply(t *testing.T) {
	for name, status := range map[string]string{
		"unmanaged": "",
		"current":   "state=current\nrunning_rev=7886200e5abc\n",
		"applying":  "state=applying\nrunning_rev=4de507676c6b\n",
	} {
		t.Run(name, func(t *testing.T) {
			s, dir := updateServer(t, status)
			if rec := postUpdate(t, s, "/api/system/update/apply", ""); rec.Code != http.StatusConflict {
				t.Fatalf("status %d, want 409", rec.Code)
			}
			if _, ok, _ := hostupdate.ReadRequest(dir); ok {
				t.Fatal("a refused apply still reached the updater")
			}
		})
	}
}

func TestSystemUpdateRetriesAFailedApply(t *testing.T) {
	s, _ := updateServer(t, "state=failed\nrunning_rev=4de507676c6b\navailable_rev=7886200e5abc\nerror=pull failed\n")
	if rec := postUpdate(t, s, "/api/system/update/apply", ""); rec.Code != http.StatusAccepted {
		t.Fatalf("status %d, want 202", rec.Code)
	}
}

func TestSystemUpdateDeferIsClampedToTheDeadline(t *testing.T) {
	s, dir := updateServer(t, pendingStatus)
	rec := postUpdate(t, s, "/api/system/update/defer", untilBody(updateNow.Add(72*time.Hour)))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"deferred_until":"`+rfc(1790403000)+`"`) {
		t.Fatalf("status %d body %s, want 202 clamped to the deadline", rec.Code, rec.Body)
	}
	req, _, err := hostupdate.ReadRequest(dir)
	if err != nil || req.Action != hostupdate.ActionDefer || !req.Until.Equal(time.Unix(1790403000, 0)) {
		t.Fatalf("request %+v err=%v", req, err)
	}
}

func TestSystemUpdateDeferKeepsAnUntilInsideTheDeadline(t *testing.T) {
	s, _ := updateServer(t, pendingStatus)
	until := updateNow.Add(2 * time.Hour)
	rec := postUpdate(t, s, "/api/system/update/defer", untilBody(until))
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"deferred_until":"`+until.Format(time.RFC3339)+`"`) {
		t.Fatalf("status %d body %s", rec.Code, rec.Body)
	}
}

func TestSystemUpdateDeferRefusesWhatCannotBeADeferral(t *testing.T) {
	for name, tc := range map[string]struct {
		status string
		body   string
		code   int
	}{
		"not json":          {pendingStatus, `until=tomorrow`, http.StatusBadRequest},
		"no until":          {pendingStatus, `{}`, http.StatusBadRequest},
		"until in the past": {pendingStatus, untilBody(updateNow.Add(-time.Hour)), http.StatusBadRequest},
		"deadline passed":   {"state=pending\ndeadline=1790319000\n", untilBody(updateNow.Add(time.Hour)), http.StatusConflict},
		"nothing pending":   {"state=current\n", untilBody(updateNow.Add(time.Hour)), http.StatusConflict},
		"unmanaged":         {"", untilBody(updateNow.Add(time.Hour)), http.StatusConflict},
	} {
		t.Run(name, func(t *testing.T) {
			s, dir := updateServer(t, tc.status)
			if rec := postUpdate(t, s, "/api/system/update/defer", tc.body); rec.Code != tc.code {
				t.Fatalf("status %d body %s, want %d", rec.Code, rec.Body, tc.code)
			}
			if _, ok, _ := hostupdate.ReadRequest(dir); ok {
				t.Fatal("a refused deferral still reached the updater")
			}
		})
	}
}

func TestSystemUpdateMutationsNeedAPrincipal(t *testing.T) {
	s, dir := updateServer(t, pendingStatus)
	for _, handler := range []http.HandlerFunc{s.handleSystemUpdateApply, s.handleSystemUpdateDefer} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodPost, "/api/system/update/apply", strings.NewReader(`{"until":"2026-09-24T06:00:00Z"}`)))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status %d, want 401", rec.Code)
		}
	}
	if _, ok, _ := hostupdate.ReadRequest(dir); ok {
		t.Fatal("an anonymous request reached the updater")
	}
}

func TestSystemUpdateRoutesAreInTheIdempotencyInventory(t *testing.T) {
	for _, route := range []string{"POST /api/system/update/apply", "POST /api/system/update/defer"} {
		if _, ok := httpMutationRoutes[route]; !ok {
			t.Errorf("%s is absent from httpMutationRoutes", route)
		}
	}
}

func rfc(epoch int64) string { return time.Unix(epoch, 0).UTC().Format(time.RFC3339) }

func untilBody(until time.Time) string { return `{"until":"` + until.Format(time.RFC3339) + `"}` }

func writeTestActivity(t *testing.T, dir string) {
	t.Helper()
	if err := hostupdate.WriteActivity(dir, hostupdate.Activity{
		WrittenAt:      updateNow,
		LastActivityAt: time.Unix(1790317200, 0),
		LiveRuns:       2,
	}); err != nil {
		t.Fatal(err)
	}
}
