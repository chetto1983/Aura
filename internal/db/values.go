package db

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const postgresTextNULReplacement = "[NUL]"

// ParseUUID validates a caller-supplied UUID at the Postgres boundary.
func ParseUUID(field, value string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid %s %q: %w", field, value, err)
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

// PostgresTextSafe replaces NUL, which PostgreSQL text cannot store.
func PostgresTextSafe(value string) string {
	if !strings.ContainsRune(value, '\x00') {
		return value
	}
	return strings.ReplaceAll(value, "\x00", postgresTextNULReplacement)
}
