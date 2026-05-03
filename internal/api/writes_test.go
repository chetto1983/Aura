package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/source"
)

// doLocal mirrors testEnv.do but pins RemoteAddr to a loopback so requests
// pass requireLoopback. httptest.NewRequest defaults to 192.0.2.1:1234,
// which is non-loopback and would return 403 from every write endpoint.
func (e *testEnv) doLocal(method, path string, body []byte) *httptest.ResponseRecorder {
	e.t.Helper()
	var req *http.Request
	if body == nil {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.RemoteAddr = "127.0.0.1:54321"
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

// ---- POST /wiki/index/rebuild ------------------------------------------

func TestWikiRebuild_OK(t *testing.T) {
	e := newTestEnv(t)
	e.seedPage("Alpha", "body", "notes", nil)
	rr := e.doLocal("POST", "/wiki/index/rebuild", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	// index.md should now exist on disk under the wiki dir.
	rr2 := e.do("GET", "/wiki/page?slug=alpha")
	if rr2.Code != http.StatusOK {
		t.Fatalf("post-rebuild GET wiki/page status %d", rr2.Code)
	}
}

// ---- POST /wiki/log ----------------------------------------------------

func TestWikiAppendLog_HappyPath(t *testing.T) {
	e := newTestEnv(t)
	rr := e.doLocal("POST", "/wiki/log", []byte(`{"action":"test","slug":"alpha"}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestWikiAppendLog_RejectsBadInput(t *testing.T) {
	e := newTestEnv(t)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"empty body", ``, http.StatusBadRequest},
		{"no action", `{}`, http.StatusBadRequest},
		{"action with space", `{"action":"two words"}`, http.StatusBadRequest},
		{"action too long", `{"action":"` + strings.Repeat("a", 33) + `"}`, http.StatusBadRequest},
		{"slug with caps", `{"action":"x","slug":"NotCanonical"}`, http.StatusBadRequest},
		{"unknown field", `{"action":"x","extra":1}`, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := e.doLocal("POST", "/wiki/log", []byte(c.body))
			if rr.Code != c.want {
				t.Errorf("status %d, want %d; body=%s", rr.Code, c.want, rr.Body)
			}
		})
	}
}

// ---- POST /sources/{id}/ingest -----------------------------------------

func TestSourceIngest_DisabledWhenIngestNil(t *testing.T) {
	e := newTestEnv(t)
	src := e.seedSource([]byte("pdf-bytes"), source.KindPDF, "a.pdf")
	rr := e.doLocal("POST", "/sources/"+src.ID+"/ingest", nil)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503; body=%s", rr.Code, rr.Body)
	}
}

func TestSourceIngest_RejectsBadID(t *testing.T) {
	e := newTestEnv(t)
	rr := e.doLocal("POST", "/sources/not-a-real-id/ingest", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rr.Code)
	}
}

// ---- POST /sources/{id}/reocr ------------------------------------------

func TestSourceReocr_DisabledWhenOCRNil(t *testing.T) {
	e := newTestEnv(t)
	src := e.seedSource([]byte("pdf-bytes"), source.KindPDF, "a.pdf")
	rr := e.doLocal("POST", "/sources/"+src.ID+"/reocr", nil)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503; body=%s", rr.Code, rr.Body)
	}
}

func TestSourceReocr_RejectsBadID(t *testing.T) {
	e := newTestEnv(t)
	rr := e.doLocal("POST", "/sources/bad/reocr", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rr.Code)
	}
}

// ---- POST /tasks (upsert) ----------------------------------------------

func TestTaskUpsert_HappyPath_At(t *testing.T) {
	e := newTestEnv(t)
	at := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	body := `{"name":"new-task","kind":"reminder","recipient_id":"u123","payload":"hello","at":"` + at + `"}`
	rr := e.doLocal("POST", "/tasks", []byte(body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got Task
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "new-task" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Status != string(scheduler.StatusActive) {
		t.Errorf("status = %q, want active", got.Status)
	}
	if got.Kind != string(scheduler.KindReminder) {
		t.Errorf("kind = %q", got.Kind)
	}
	if got.ScheduleKind != string(scheduler.ScheduleAt) {
		t.Errorf("schedule_kind = %q", got.ScheduleKind)
	}
	if got.Payload != "hello" {
		t.Errorf("payload = %q", got.Payload)
	}
}

func TestTaskUpsert_HappyPath_Daily(t *testing.T) {
	e := newTestEnv(t)
	body := `{"name":"daily-task","kind":"wiki_maintenance","daily":"03:00","weekdays":"mon,tue,wed,thu,fri"}`
	rr := e.doLocal("POST", "/tasks", []byte(body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got Task
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ScheduleDaily != "03:00" {
		t.Errorf("schedule_daily = %q", got.ScheduleDaily)
	}
	if got.ScheduleKind != string(scheduler.ScheduleDaily) {
		t.Errorf("schedule_kind = %q, want daily", got.ScheduleKind)
	}
	if got.ScheduleWeekdays != "mon,tue,wed,thu,fri" {
		t.Errorf("schedule_weekdays = %q", got.ScheduleWeekdays)
	}
}

func TestTaskUpsert_HappyPath_EveryMinutes(t *testing.T) {
	e := newTestEnv(t)
	body := `{"name":"every-task","kind":"wiki_maintenance","every_minutes":60}`
	rr := e.doLocal("POST", "/tasks", []byte(body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got Task
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ScheduleKind != string(scheduler.ScheduleEvery) {
		t.Errorf("schedule_kind = %q, want every", got.ScheduleKind)
	}
	if got.ScheduleEveryMinutes != 60 {
		t.Errorf("schedule_every_minutes = %d, want 60", got.ScheduleEveryMinutes)
	}
}

func TestTaskUpsert_RejectsBadInput(t *testing.T) {
	e := newTestEnv(t)
	at := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"missing name", `{"kind":"reminder","recipient_id":"u","at":"` + at + `"}`, http.StatusBadRequest},
		{"bad name", `{"name":"bad name","kind":"reminder","recipient_id":"u","at":"` + at + `"}`, http.StatusBadRequest},
		{"bad kind", `{"name":"x","kind":"unknown","recipient_id":"u","at":"` + at + `"}`, http.StatusBadRequest},
		{"both at and daily", `{"name":"x","kind":"wiki_maintenance","at":"` + at + `","daily":"03:00"}`, http.StatusBadRequest},
		{"daily and every", `{"name":"x","kind":"wiki_maintenance","daily":"03:00","every_minutes":60}`, http.StatusBadRequest},
		{"neither at nor daily", `{"name":"x","kind":"wiki_maintenance"}`, http.StatusBadRequest},
		{"weekdays without daily", `{"name":"x","kind":"wiki_maintenance","every_minutes":60,"weekdays":"mon"}`, http.StatusBadRequest},
		{"bad weekday", `{"name":"x","kind":"wiki_maintenance","daily":"03:00","weekdays":"moonday"}`, http.StatusBadRequest},
		{"reminder without recipient", `{"name":"x","kind":"reminder","at":"` + at + `"}`, http.StatusBadRequest},
		{"past at", `{"name":"x","kind":"wiki_maintenance","at":"` + past + `"}`, http.StatusBadRequest},
		{"bad daily format", `{"name":"x","kind":"wiki_maintenance","daily":"3am"}`, http.StatusBadRequest},
		{"empty body", ``, http.StatusBadRequest},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rr := e.doLocal("POST", "/tasks", []byte(c.body))
			if rr.Code != c.want {
				t.Errorf("status %d, want %d; body=%s", rr.Code, c.want, rr.Body)
			}
		})
	}
}

// ---- POST /tasks/{name}/cancel ----------------------------------------

func TestTaskCancel_HappyPath(t *testing.T) {
	e := newTestEnv(t)
	now := time.Now().UTC()
	e.seedTask("active-task", scheduler.KindReminder, scheduler.StatusActive, now.Add(time.Hour))
	rr := e.doLocal("POST", "/tasks/active-task/cancel", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got Task
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != string(scheduler.StatusCancelled) {
		t.Errorf("status = %q, want cancelled", got.Status)
	}
}

func TestTaskCancel_NotFound(t *testing.T) {
	e := newTestEnv(t)
	rr := e.doLocal("POST", "/tasks/missing/cancel", nil)
	if rr.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404; body=%s", rr.Code, rr.Body)
	}
}

func TestTaskCancel_AlreadyTerminal(t *testing.T) {
	e := newTestEnv(t)
	now := time.Now().UTC()
	e.seedTask("done-task", scheduler.KindReminder, scheduler.StatusDone, now.Add(-time.Hour))
	rr := e.doLocal("POST", "/tasks/done-task/cancel", nil)
	if rr.Code != http.StatusConflict {
		t.Errorf("status %d, want 409; body=%s", rr.Code, rr.Body)
	}
}

func TestTaskCancel_RejectsBadName(t *testing.T) {
	e := newTestEnv(t)
	rr := e.doLocal("POST", "/tasks/bad@name/cancel", nil)
	if rr.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rr.Code)
	}
}
