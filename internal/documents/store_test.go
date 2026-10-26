package documents

import (
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestStoreValueHelpers(t *testing.T) {
	parsed, err := db.ParseUUID("job id", uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.String(); got == "" {
		t.Fatal("valid.String() returned empty")
	}
	if _, err := db.ParseUUID("job id", "not-a-uuid"); err == nil {
		t.Fatal("pgUUID accepted invalid uuid")
	}
	if got := (pgtype.UUID{}).String(); got != "" {
		t.Fatalf("invalid.String() = %q", got)
	}
	if got := db.OptionalText("boom"); !got.Valid || got.String != "boom" {
		t.Fatalf("db.OptionalText(non-empty) = %#v", got)
	}
	if got := db.OptionalText(""); got.Valid {
		t.Fatalf("db.OptionalText(empty) = %#v", got)
	}
	if got := textString(pgtype.Text{}); got != "" {
		t.Fatalf("textString(invalid) = %q", got)
	}
	if got := timeValue(pgtype.Timestamptz{}); !got.IsZero() {
		t.Fatalf("timeValue(invalid) = %s", got)
	}
}
