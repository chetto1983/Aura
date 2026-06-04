package main

import (
	"testing"
	"time"
)

func TestTriadToSpecCron(t *testing.T) {
	kind, runAt := triadToSpec("30 9 * * 1-5", "", 0)
	if kind != "cron" {
		t.Errorf("schedule_kind = %q, want cron", kind)
	}
	if !runAt.IsZero() {
		t.Errorf("cron must not carry a run_at, got %v", runAt)
	}
}

func TestTriadToSpecAt(t *testing.T) {
	kind, runAt := triadToSpec("", "2030-01-01T09:30:00Z", 0)
	if kind != "at" {
		t.Errorf("schedule_kind = %q, want at", kind)
	}
	want := time.Date(2030, 1, 1, 9, 30, 0, 0, time.UTC)
	if !runAt.Equal(want) {
		t.Errorf("run_at = %v, want %v", runAt, want)
	}
}

func TestTriadToSpecEvery(t *testing.T) {
	kind, runAt := triadToSpec("", "", 15)
	if kind != "every" {
		t.Errorf("schedule_kind = %q, want every", kind)
	}
	if !runAt.IsZero() {
		t.Errorf("every must not carry a run_at, got %v", runAt)
	}
}

func TestPayloadOrEmptyDefault(t *testing.T) {
	if got := string(payloadOrEmpty("")); got != "{}" {
		t.Errorf("empty payload = %q, want {}", got)
	}
	if got := string(payloadOrEmpty(`{"text":"hi"}`)); got != `{"text":"hi"}` {
		t.Errorf("valid payload not passed through: %q", got)
	}
}

func TestDefaultSchedulerTZ(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_TZ", "")
	if got := defaultSchedulerTZ(); got != "Europe/Rome" {
		t.Errorf("default tz = %q, want Europe/Rome", got)
	}
	t.Setenv("AURA_SCHEDULER_TZ", "UTC")
	if got := defaultSchedulerTZ(); got != "UTC" {
		t.Errorf("env tz = %q, want UTC", got)
	}
}

func TestNullableHelpers(t *testing.T) {
	if nullableText("") != nil {
		t.Error("empty string must be NULL")
	}
	if got := nullableText("x"); got == nil || *got != "x" {
		t.Error("non-empty string must round-trip")
	}
	if nullableInt(0) != nil || nullableInt(-1) != nil {
		t.Error("non-positive int must be NULL")
	}
	if got := nullableInt(5); got == nil || *got != 5 {
		t.Error("positive int must round-trip")
	}
	if nullableTime(time.Time{}) != nil {
		t.Error("zero time must be NULL")
	}
	now := time.Now()
	if got := nullableTime(now); got == nil || !got.Equal(now.UTC()) {
		t.Error("non-zero time must round-trip as UTC")
	}
}

func TestFmtTimePtr(t *testing.T) {
	if got := fmtTimePtr(nil); got != "—" {
		t.Errorf("nil time = %q, want em-dash", got)
	}
	zero := time.Time{}
	if got := fmtTimePtr(&zero); got != "—" {
		t.Errorf("zero time = %q, want em-dash", got)
	}
	at := time.Date(2030, 1, 1, 9, 30, 0, 0, time.UTC)
	if got := fmtTimePtr(&at); got != "2030-01-01T09:30:00Z" {
		t.Errorf("time = %q, want RFC-3339", got)
	}
}
