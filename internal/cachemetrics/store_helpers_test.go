package cachemetrics

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// mustNumeric encodes f, failing the test on the out-of-range error path so the
// in-range fixtures stay one-liners at the call sites.
func mustNumeric(t *testing.T, f float64) pgtype.Numeric {
	t.Helper()
	n, err := numericFromFloat(f)
	if err != nil {
		t.Fatalf("numericFromFloat(%v): %v", f, err)
	}
	return n
}

func TestNumericFromFloat_RoundTrip(t *testing.T) {
	t.Parallel()
	cases := []float64{0, 0.0001, 0.1234, 1.5, 12.3456, -0.0050, 123.4567, 999999.9999, -999999.9999}
	for _, want := range cases {
		got := floatFromNumeric(mustNumeric(t, want))
		if diff := got - want; diff > 1e-9 || diff < -1e-9 {
			t.Errorf("numericFromFloat(%v) round-trip: got %v", want, got)
		}
	}
}

func TestNumericFromFloat_OutOfRange(t *testing.T) {
	t.Parallel()
	cases := []float64{numericMaxCost + 1, -numericMaxCost - 1, 1e9, -1e9, math.NaN(), math.Inf(1), math.Inf(-1)}
	for _, in := range cases {
		if _, err := numericFromFloat(in); err == nil {
			t.Errorf("numericFromFloat(%v): want out-of-range error, got nil", in)
		}
	}
}

func TestFloatFromNumeric_InvalidAndNaN(t *testing.T) {
	t.Parallel()
	if v := floatFromNumeric(pgtype.Numeric{Valid: false}); v != 0 {
		t.Errorf("invalid numeric: want 0, got %v", v)
	}
	if v := floatFromNumeric(pgtype.Numeric{NaN: true, Valid: true}); v != 0 {
		t.Errorf("NaN numeric: want 0, got %v", v)
	}
}

func TestAnyInt64_DecodeShapes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   any
		want int64
	}{
		{int64(42), 42},
		{int32(7), 7},
		{int(9), 9},
		{float64(13), 13},
		{"100", 100},
		{[]byte("250"), 250},
	}
	for _, c := range cases {
		got, err := anyInt64(c.in)
		if err != nil {
			t.Errorf("anyInt64(%#v): unexpected error %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("anyInt64(%#v): want %d, got %d", c.in, c.want, got)
		}
	}
	got, err := anyInt64(mustNumeric(t, 5))
	if err != nil {
		t.Fatalf("anyInt64(numeric 5): %v", err)
	}
	if got != 5 {
		t.Errorf("anyInt64(numeric 5): want 5, got %d", got)
	}
}

// TestAnyInt64_UnparseableErrors proves WR-02: an unmodeled driver shape or an
// unparseable text aggregate fails loud rather than reporting a fabricated 0.
func TestAnyInt64_UnparseableErrors(t *testing.T) {
	t.Parallel()
	bad := []any{nil, struct{}{}, "not-a-number", []byte("xx")}
	for _, in := range bad {
		if _, err := anyInt64(in); err == nil {
			t.Errorf("anyInt64(%#v): want error, got nil", in)
		}
	}
}

func TestAnyNumericFloat_DecodeShapes(t *testing.T) {
	t.Parallel()
	mustFloat := func(v any) float64 {
		t.Helper()
		f, err := anyNumericFloat(v)
		if err != nil {
			t.Fatalf("anyNumericFloat(%#v): %v", v, err)
		}
		return f
	}
	if got := mustFloat(mustNumeric(t, 1.25)); got < 1.25-1e-9 || got > 1.25+1e-9 {
		t.Errorf("anyNumericFloat(numeric 1.25): got %v", got)
	}
	if got := mustFloat(float64(2.5)); got != 2.5 {
		t.Errorf("anyNumericFloat(float 2.5): got %v", got)
	}
	if got := mustFloat(int64(3)); got != 3 {
		t.Errorf("anyNumericFloat(int64 3): got %v", got)
	}
	if got := mustFloat("4.5"); got != 4.5 {
		t.Errorf("anyNumericFloat(string 4.5): got %v", got)
	}
	if got := mustFloat([]byte("6.75")); got != 6.75 {
		t.Errorf("anyNumericFloat([]byte 6.75): got %v", got)
	}
}

// TestAnyNumericFloat_UnparseableErrors proves WR-02: an unmodeled shape or an
// unparseable text cost aggregate errors rather than reporting a fabricated $0.00.
func TestAnyNumericFloat_UnparseableErrors(t *testing.T) {
	t.Parallel()
	bad := []any{nil, struct{}{}, "not-a-number", []byte("xx")}
	for _, in := range bad {
		if _, err := anyNumericFloat(in); err == nil {
			t.Errorf("anyNumericFloat(%#v): want error, got nil", in)
		}
	}
}

func TestNewInsertParams(t *testing.T) {
	t.Parallel()
	convID := uuid.Must(uuid.NewV7()).String()
	p, err := NewInsertParams(MetricParams{
		ConversationID: convID,
		Seq:            3,
		PromptTokens:   1000,
		CachedTokens:   800,
		CostUSD:        0.0123,
	})
	if err != nil {
		t.Fatalf("NewInsertParams: %v", err)
	}
	if uuid.UUID(p.ConversationID.Bytes).String() != convID {
		t.Errorf("conversation_id: want %s, got %s", convID, uuid.UUID(p.ConversationID.Bytes).String())
	}
	if p.Seq != 3 || p.PromptTokens != 1000 || p.CachedTokens != 800 {
		t.Errorf("scalar params: got seq=%d prompt=%d cached=%d", p.Seq, p.PromptTokens, p.CachedTokens)
	}
	if got := floatFromNumeric(p.CostUsd); got < 0.0123-1e-9 || got > 0.0123+1e-9 {
		t.Errorf("cost_usd: want 0.0123, got %v", got)
	}

	if _, err := NewInsertParams(MetricParams{ConversationID: "not-a-uuid"}); err == nil {
		t.Fatal("NewInsertParams with a bad UUID: want error, got nil")
	}
}

func TestTimestamptzFrom(t *testing.T) {
	t.Parallel()
	ts := timestamptzFrom(time.Now())
	if !ts.Valid {
		t.Error("timestamptzFrom: want Valid, got invalid")
	}
}
