package documents

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// The conversions between Go's zero values and pgtype's explicit NULL, in one place.
//
// They lived in store.go beside the per-document job ledger until migration 0098 retired
// that ledger. The ingestion queue that outlived it needs the same conversions, so they
// moved here rather than being copied into it — the pairing (pgUUID/uuidString,
// pgText/textString) is what makes a round-trip through the database lossless, and two
// copies is how one half of a pair drifts from the other.

func pgUUID(field, value string) (pgtype.UUID, error) {
	u, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}

// pgText treats the empty string as NULL. Every column it feeds is an optional free-text
// field (a worker id, an error message), where "" and NULL mean the same absence.
func pgText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func textString(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func timeValue(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}
