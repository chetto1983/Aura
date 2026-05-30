package tools

import (
	"context"
	"testing"
	"time"
)

// Test 4 (Req#14): current_time{} parses as RFC-3339 UTC.
func TestCurrentTime_DefaultUTC(t *testing.T) {
	res, err := CurrentTime{}.Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, res.Preview)
	if err != nil {
		t.Fatalf("not RFC-3339: %q (%v)", res.Preview, err)
	}
	if _, off := parsed.Zone(); off != 0 {
		t.Fatalf("default should be UTC (offset 0), got offset %d for %q", off, res.Preview)
	}
}

// Test 4b: an explicit "UTC" arg behaves like the default.
func TestCurrentTime_ExplicitUTC(t *testing.T) {
	res, err := CurrentTime{}.Execute(context.Background(), []byte(`{"timezone":"UTC"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if _, err := time.Parse(time.RFC3339, res.Preview); err != nil {
		t.Fatalf("not RFC-3339: %q", res.Preview)
	}
}

// Test 4c (Req#14): an IANA timezone returns the correct offset for the same
// instant as time.LoadLocation yields.
func TestCurrentTime_IANAOffset(t *testing.T) {
	res, err := CurrentTime{}.Execute(context.Background(), []byte(`{"timezone":"Europe/Rome"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	parsed, err := time.Parse(time.RFC3339, res.Preview)
	if err != nil {
		t.Fatalf("not RFC-3339: %q", res.Preview)
	}
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	_, gotOff := parsed.Zone()
	_, wantOff := parsed.In(loc).Zone()
	if gotOff != wantOff {
		t.Fatalf("offset = %d, want %d (Europe/Rome for the same instant)", gotOff, wantOff)
	}
}

// Test 5 (Req#14): an invalid IANA tz returns an error, not a panic.
func TestCurrentTime_InvalidTZ(t *testing.T) {
	if _, err := (CurrentTime{}).Execute(context.Background(), []byte(`{"timezone":"Mars/Olympus"}`)); err == nil {
		t.Fatal("want error for an invalid timezone")
	}
}

// Malformed JSON args -> error, not panic.
func TestCurrentTime_MalformedArgs(t *testing.T) {
	if _, err := (CurrentTime{}).Execute(context.Background(), []byte(`{not json`)); err == nil {
		t.Fatal("want error for malformed args")
	}
}

func TestCurrentTime_Deferred(t *testing.T) {
	if (CurrentTime{}).Spec().Deferred {
		t.Fatal("current_time must be Deferred:false")
	}
}

var _ Tool = CurrentTime{}
