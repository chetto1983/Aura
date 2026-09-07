package agui

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/chetto1983/aura/internal/cron"
)

// governance_api_scheduler_payload_test.go pins the one thing the board could not say: WHAT a
// scheduled task will do.
//
// The row projection stripped Payload as "private prompt/context material", and the edit dialog
// says so in its own header — "the payload is server-side, so its field is blank". The result,
// reported by the operator on 2026-09-07: a reminder they dictated themselves was invisible
// until it fired on Telegram, and the edit dialog offered an empty box that would silently keep
// the old text. You could not read your own reminder, and you could not change it without
// retyping it blind.
//
// The board is behind governance.read and shows the deployment's own schedule. Handing an
// operator back the text they dictated is not a disclosure; withholding it made the surface
// useless for the one person it is for.

// schedulerRowsFor drives GET /api/governance/scheduler over a scripted board.
func schedulerRowsFor(t *testing.T, tasks []cron.Task) []map[string]any {
	t.Helper()
	board := &scriptedSchedulerBoard{tasks: tasks}
	rec := doGov(t, govServer(GovernanceProviders{Scheduler: board}), http.MethodGet, "/api/governance/scheduler")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Tasks []map[string]any `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	return payload.Tasks
}

// TestSchedulerRowCarriesTheReminderText is the regression: the row must say what the task will
// do, so the list can show it and the edit dialog can prefill it.
func TestSchedulerRowCarriesTheReminderText(t *testing.T) {
	rows := schedulerRowsFor(t, []cron.Task{{
		ID:      "01a07ac7",
		Kind:    "reminder",
		Status:  "active",
		Payload: []byte(`{"text":"Telefona ad Andrea"}`),
	}})

	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	raw, err := json.Marshal(rows[0]["Payload"])
	if err != nil {
		t.Fatalf("re-encode payload: %v", err)
	}
	if got := string(raw); got == "null" || got == `""` {
		t.Fatalf("the row carries no payload (%s): the operator cannot read their own reminder", got)
	}
	if !jsonContains(raw, "Telefona ad Andrea") {
		t.Fatalf("payload = %s, want the dictated text", raw)
	}
}

// TestSchedulerRowWithoutAPayloadOmitsTheKey keeps the wire clean for the kinds that carry none
// (a backup takes no payload), so the client renders nothing rather than an empty box.
func TestSchedulerRowWithoutAPayloadOmitsTheKey(t *testing.T) {
	rows := schedulerRowsFor(t, []cron.Task{{ID: "b1", Kind: "backup_postgres", Status: "active"}})

	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if v, present := rows[0]["Payload"]; present && v != nil {
		t.Fatalf("a payload-free task carried %v, want the key absent", v)
	}
}

// jsonContains reports whether the encoded payload carries the substring, after unquoting the
// JSON escaping the encoder applies.
func jsonContains(raw []byte, want string) bool {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return false
	}
	again, err := json.Marshal(decoded)
	if err != nil {
		return false
	}
	return len(again) > 0 && containsString(string(again), want)
}

func containsString(haystack, needle string) bool {
	return len(needle) == 0 || len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
