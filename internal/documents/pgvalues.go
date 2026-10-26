package documents

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// The conversions between Go's zero values and pgtype's explicit NULL, in one place.
//
// They lived in store.go beside the per-document job ledger until migration 0098 retired
// that ledger. The writing halves are shared now: db.ParseUUID and db.OptionalText go in,
// pgtype.UUID.String comes out, so a round-trip stays lossless without a private copy of
// either half.

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
