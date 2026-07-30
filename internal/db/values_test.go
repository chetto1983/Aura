package db

import (
	"strings"
	"testing"
)

func TestParseUUID(t *testing.T) {
	t.Parallel()

	got, err := ParseUUID("conversation_id", "00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("ParseUUID(valid): %v", err)
	}
	if !got.Valid || got.Bytes[15] != 1 {
		t.Fatalf("ParseUUID(valid) = %+v", got)
	}

	if _, err := ParseUUID("conversation_id", "not-a-uuid"); err == nil ||
		!strings.Contains(err.Error(), `invalid conversation_id "not-a-uuid"`) {
		t.Fatalf("ParseUUID(invalid) error = %v", err)
	}
}

func TestPostgresTextSafe(t *testing.T) {
	t.Parallel()

	if got := PostgresTextSafe("plain"); got != "plain" {
		t.Fatalf("PostgresTextSafe(plain) = %q", got)
	}
	if got := PostgresTextSafe("before\x00after"); got != "before[NUL]after" {
		t.Fatalf("PostgresTextSafe(NUL) = %q", got)
	}
}
