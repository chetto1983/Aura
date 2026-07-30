package cron

// helpers_fake_test.go covers the pure, DB-free helpers the db_integration tier only
// exercises incidentally (or not at all): the pgtype conversion helpers, the row
// projections of NULL columns, the FNV advisory-key hash, the unique-violation
// classifier on non-23505/non-PgError inputs, and the trivial accessors. No pool, no
// fake DBTX — just value-in/value-out assertions against the real boundary contract.

import (
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestTaskHashIsDeterministicAndDistinct(t *testing.T) {
	t.Parallel()
	a := taskHash("11111111-1111-1111-1111-111111111111")
	if a != taskHash("11111111-1111-1111-1111-111111111111") {
		t.Fatal("taskHash must be deterministic for the same UUID (FNV-1a)")
	}
	if a == taskHash("22222222-2222-2222-2222-222222222222") {
		t.Fatal("distinct UUIDs must (almost surely) hash to distinct keys")
	}
	if taskHash("") == 0 {
		// FNV-1a of the empty string is the offset basis (non-zero); a 0 key here would
		// signal a broken hash, not just an empty input.
		t.Fatal("taskHash of the empty string must be the non-zero FNV offset basis")
	}
}

func TestClaimConnAccessor(t *testing.T) {
	t.Parallel()
	// A nil-conn Claim returns nil (no panic); release on a nil Claim is a no-op.
	c := &Claim{RunID: "r"}
	if c.Conn() != nil {
		t.Fatal("a Claim with no held conn must return a nil Conn()")
	}
	c.release(t.Context()) // idempotent-safe on a nil conn
	var nilClaim *Claim
	nilClaim.release(t.Context()) // nil receiver is a no-op
}

func TestIsUniqueViolationClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"23505 unique", &pgconn.PgError{Code: "23505"}, true},
		{"23503 fk (not unique)", &pgconn.PgError{Code: "23503"}, false},
		{"wrapped 23505", errors.Join(errors.New("ctx"), &pgconn.PgError{Code: "23505"}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("isUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestPgtypeConversionHelpers(t *testing.T) {
	t.Parallel()

	// text: empty → invalid (NULL); non-empty → valid.
	if got := text(""); got.Valid {
		t.Fatal("text(\"\") must be a NULL pgtype.Text")
	}
	if got := text("x"); !got.Valid || got.String != "x" {
		t.Fatalf("text(\"x\") = %#v", got)
	}

	// int4OrNull: <=0 → NULL; >0 → valid.
	if got := int4OrNull(0); got.Valid {
		t.Fatal("int4OrNull(0) must be NULL")
	}
	if got := int4OrNull(-3); got.Valid {
		t.Fatal("int4OrNull(-3) must be NULL")
	}
	if got := int4OrNull(7); !got.Valid || got.Int32 != 7 {
		t.Fatalf("int4OrNull(7) = %#v", got)
	}

	// tsOrNull: zero → NULL; non-zero → valid + normalized to UTC.
	if got := tsOrNull(time.Time{}); got.Valid {
		t.Fatal("tsOrNull(zero) must be NULL")
	}
	in := time.Date(2026, 6, 13, 9, 30, 0, 0, time.FixedZone("CEST", 2*3600))
	got := tsOrNull(in)
	if !got.Valid || !got.Time.Equal(in.UTC()) || got.Time.Location() != time.UTC {
		t.Fatalf("tsOrNull must normalize to UTC, got %v", got.Time)
	}

	// payloadOrEmpty: empty/nil → {}; non-empty → passthrough.
	if string(payloadOrEmpty(nil)) != "{}" {
		t.Fatal("payloadOrEmpty(nil) must be {}")
	}
	if string(payloadOrEmpty([]byte{})) != "{}" {
		t.Fatal("payloadOrEmpty(empty) must be {}")
	}
	if string(payloadOrEmpty([]byte(`{"a":1}`))) != `{"a":1}` {
		t.Fatal("payloadOrEmpty must pass non-empty payloads through")
	}
}

func TestUUIDHelpers(t *testing.T) {
	t.Parallel()

	// newUUID is a valid v7 UUID.
	n := newUUID()
	if !n.Valid {
		t.Fatal("newUUID must be Valid")
	}

	// parseUUID: valid string round-trips; invalid errors.
	const valid = "11111111-1111-1111-1111-111111111111"
	pu, err := db.ParseUUID("uuid", valid)
	if err != nil || !pu.Valid {
		t.Fatalf("parseUUID(valid) = %#v, %v", pu, err)
	}
	if uuid.UUID(pu.Bytes).String() != valid {
		t.Fatalf("parseUUID round-trip mismatch: %s", uuid.UUID(pu.Bytes).String())
	}
	if _, err := db.ParseUUID("uuid", "not-a-uuid"); err == nil {
		t.Fatal("parseUUID must reject a malformed string")
	}

	// uuidOrNull: empty → NULL; malformed → NULL (best-effort, never panics); valid → set.
	if uuidOrNull("").Valid {
		t.Fatal("uuidOrNull(\"\") must be NULL")
	}
	if uuidOrNull("garbage").Valid {
		t.Fatal("uuidOrNull(malformed) must degrade to NULL, not panic")
	}
	if got := uuidOrNull(valid); !got.Valid {
		t.Fatal("uuidOrNull(valid) must be set")
	}

	// uuidString: NULL → ""; valid → canonical string.
	if uuidString(pgtype.UUID{}) != "" {
		t.Fatal("uuidString(NULL) must be empty")
	}
	if uuidString(pu) != valid {
		t.Fatalf("uuidString(valid) = %q", uuidString(pu))
	}
}

func TestTaskFromRowProjectsNullColumns(t *testing.T) {
	t.Parallel()
	// A minimal row with the optional columns NULL: the projection must zero-fill, not
	// panic, and leave OriginConversationID empty for a NULL uuid.
	r := sqlc.AuraSchedulerTasks{
		ID:           mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		Kind:         "agent_job",
		ScheduleKind: "every",
		EveryMinutes: pgtype.Int4{Int32: 15, Valid: true},
		Tz:           "UTC",
		Status:       "active",
		IdentityID:   "local",
		// CronExpr, RunAt, NextRunAt, NotifyRoute, OriginConversationID all NULL.
	}
	got := taskFromRow(r)
	if got.Kind != KindAgentJob || got.ScheduleKind != KindEvery || got.EveryMinutes != 15 {
		t.Fatalf("projection mismatch: %+v", got)
	}
	if got.CronExpr != "" || got.NotifyRoute != "" || got.OriginConversationID != "" {
		t.Fatalf("NULL columns must project to empty strings, got %+v", got)
	}
	if !got.RunAt.IsZero() || !got.NextRunAt.IsZero() {
		t.Fatalf("NULL timestamps must project to the zero time, got run_at=%v next=%v", got.RunAt, got.NextRunAt)
	}
}

func TestRunFromRowProjectsNullColumns(t *testing.T) {
	t.Parallel()
	r := sqlc.AuraAgentJobRuns{
		ID:         mustUUID(t, "22222222-2222-2222-2222-222222222222"),
		TaskID:     mustUUID(t, "11111111-1111-1111-1111-111111111111"),
		Status:     "running",
		StepBudget: pgtype.Int4{Int32: 4, Valid: true},
		// CompletedWithHash, Summary, LastError, MissedSince, PausedStateToken, CompletedAt NULL.
	}
	got := runFromRow(r)
	if got.Status != "running" || got.StepBudget != 4 {
		t.Fatalf("projection mismatch: %+v", got)
	}
	if got.CompletedWithHash != "" || got.Summary != "" || got.LastError != "" || got.PausedStateToken != "" {
		t.Fatalf("NULL text/uuid columns must project to empty strings, got %+v", got)
	}
	if !got.MissedSince.IsZero() || !got.CompletedAt.IsZero() {
		t.Fatalf("NULL timestamps must project to the zero time, got %+v", got)
	}
}

func TestPendingNotificationFromRowProjectsNullIdentity(t *testing.T) {
	t.Parallel()
	// A legacy row: NULL identity_id + NULL notify_route project to empty (route fallback).
	r := sqlc.AuraPendingNotifications{
		ID:       mustUUID(t, "44444444-4444-4444-4444-444444444444"),
		RunID:    mustUUID(t, "22222222-2222-2222-2222-222222222222"),
		Body:     "legacy body",
		Attempts: 3,
		Status:   "failed",
		// NotifyRoute, LastError, IdentityID NULL.
	}
	got := pendingNotificationFromRow(r)
	if got.Body != "legacy body" || got.Attempts != 3 || got.Status != "failed" {
		t.Fatalf("projection mismatch: %+v", got)
	}
	if got.NotifyRoute != "" || got.IdentityID != "" || got.LastError != "" {
		t.Fatalf("NULL columns must project to empty (route fallback), got %+v", got)
	}
}

func TestSpecFromTaskRebuildsGrammarUnion(t *testing.T) {
	t.Parallel()
	runAt := time.Date(2026, 6, 13, 17, 30, 0, 0, time.UTC)
	task := Task{
		ScheduleKind: KindAt,
		CronExpr:     "ignored",
		EveryMinutes: 0,
		RunAt:        runAt,
		TZ:           "Europe/Rome",
	}
	spec := specFromTask(task)
	if spec.Kind != KindAt || !spec.RunAt.Equal(runAt) || spec.TZ != "Europe/Rome" {
		t.Fatalf("specFromTask must mirror the persisted task fields, got %+v", spec)
	}
	// The rebuilt spec feeds NextRunAt: an `at` whose RunAt is in the future returns it.
	next, err := NextRunAt(spec, runAt.Add(-time.Hour))
	if err != nil || !next.Equal(runAt.UTC()) {
		t.Fatalf("NextRunAt(specFromTask) = %v, %v; want %v", next, err, runAt.UTC())
	}
}

// mustUUID builds a valid pgtype.UUID fixture or fails the test.
func mustUUID(t *testing.T, s string) pgtype.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", s, err)
	}
	return pgtype.UUID{Bytes: u, Valid: true}
}
