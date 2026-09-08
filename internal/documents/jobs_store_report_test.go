package documents

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

func TestDelegationJobRowRetainsFullSavedReport(t *testing.T) {
	summary := strings.Repeat("observed detail ", 200) + "LAST_RESULT=abcdef0123456789abcdef01"
	payload, err := json.Marshal(map[string]any{
		"goal": "collect results", "child_id": "w-real",
		"pending_delivery": map[string]any{"report": map[string]any{"child_id": "w-real", "summary": summary}},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, err := delegationJobRowFromSQL(sqlc.ListDelegationJobsForConversationRow{Payload: payload, Status: "succeeded"})
	if err != nil {
		t.Fatal(err)
	}
	var report struct{ Summary string }
	if err := json.Unmarshal(row.Report, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary != summary {
		t.Fatal("saved report was reduced to the notification or UI preview budget")
	}
}
