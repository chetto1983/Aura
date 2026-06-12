package documents

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestStoreValueHelpers(t *testing.T) {
	parsed, err := pgUUID("job id", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if got := uuidString(parsed); got == "" {
		t.Fatal("uuidString(valid) returned empty")
	}
	if _, err := pgUUID("job id", "not-a-uuid"); err == nil {
		t.Fatal("pgUUID accepted invalid uuid")
	}
	if got := uuidString(pgtype.UUID{}); got != "" {
		t.Fatalf("uuidString(invalid) = %q", got)
	}
	if got := pgText("boom"); !got.Valid || got.String != "boom" {
		t.Fatalf("pgText(non-empty) = %#v", got)
	}
	if got := pgText(""); got.Valid {
		t.Fatalf("pgText(empty) = %#v", got)
	}
	if got := textString(pgtype.Text{}); got != "" {
		t.Fatalf("textString(invalid) = %q", got)
	}
	if got := timeValue(pgtype.Timestamptz{}); !got.IsZero() {
		t.Fatalf("timeValue(invalid) = %s", got)
	}
}
