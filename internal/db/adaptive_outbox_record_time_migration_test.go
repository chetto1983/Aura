package db

import (
	"strings"
	"testing"
)

func TestAdaptiveOutboxRecordTimeMigrationSourceContract(t *testing.T) {
	upBytes, err := migrationsFS.ReadFile(
		"migrations/0074_adaptive_outbox_record_time.up.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	up := string(upBytes)
	for _, want := range []string{
		"ADD COLUMN recorded_at timestamptz",
		"CREATE FUNCTION aura.set_adaptive_outbox_recorded_at()",
		"NEW.recorded_at := statement_timestamp();",
		"CREATE TRIGGER adaptive_outbox_set_recorded_at",
		"BEFORE INSERT ON aura.adaptive_outbox",
		"SET search_path = pg_catalog",
		"REVOKE ALL ON FUNCTION aura.set_adaptive_outbox_recorded_at() FROM PUBLIC",
		"NEW.payload_hash, NEW.created_at, NEW.recorded_at",
		"OLD.payload_hash, OLD.created_at, OLD.recorded_at",
	} {
		if !strings.Contains(up, want) {
			t.Errorf("0074 up migration missing %q", want)
		}
	}
	if strings.Contains(up, "SECURITY DEFINER") {
		t.Error("0074 record-time trigger must not use SECURITY DEFINER")
	}
	if strings.Contains(up, "DEFAULT") || strings.Contains(up, "UPDATE aura.adaptive_outbox") {
		t.Error("0074 must not default or backfill recorded_at")
	}

	downBytes, err := migrationsFS.ReadFile(
		"migrations/0074_adaptive_outbox_record_time.down.sql",
	)
	if err != nil {
		t.Fatal(err)
	}
	down := string(downBytes)
	for _, want := range []string{
		"DROP TRIGGER IF EXISTS adaptive_outbox_set_recorded_at",
		"DROP FUNCTION IF EXISTS aura.set_adaptive_outbox_recorded_at()",
		"CREATE OR REPLACE FUNCTION aura.reject_adaptive_outbox_fact_update()",
		"DROP COLUMN recorded_at",
	} {
		if !strings.Contains(down, want) {
			t.Errorf("0074 down migration missing %q", want)
		}
	}
	if strings.Contains(down, "NEW.recorded_at") || strings.Contains(down, "OLD.recorded_at") {
		t.Error("0074 down migration must restore prior fact immutability")
	}
}
