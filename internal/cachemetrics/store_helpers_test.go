package cachemetrics

import (
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/pgnumeric"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// mustNumeric encodes f, failing the test on the out-of-range error path so the
// in-range fixtures stay one-liners at the call sites.
func mustNumeric(t *testing.T, f float64) pgtype.Numeric {
	t.Helper()
	n, err := pgnumeric.NumericFromFloat(f)
	if err != nil {
		t.Fatalf("NumericFromFloat(%v): %v", f, err)
	}
	return n
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
	if got := pgnumeric.FloatFromNumeric(p.CostUsd); got < 0.0123-1e-9 || got > 0.0123+1e-9 {
		t.Errorf("cost_usd: want 0.0123, got %v", got)
	}

	if _, err := NewInsertParams(MetricParams{ConversationID: "not-a-uuid"}); err == nil {
		t.Fatal("NewInsertParams with a bad UUID: want error, got nil")
	}

	// A cost outside numeric(10,4) range is rejected with a contextual error naming the
	// offending conversation/seq, not silently truncated (WR-01/IN-06).
	_, err = NewInsertParams(MetricParams{
		ConversationID: convID,
		Seq:            9,
		CostUSD:        pgnumeric.DefaultNumericMaxCost + 1,
	})
	if err == nil {
		t.Fatal("NewInsertParams with an out-of-range cost: want error, got nil")
	}
	if got := err.Error(); !strings.Contains(got, "seq 9") || !strings.Contains(got, convID) {
		t.Errorf("cost-overflow error must name conversation/seq, got %q", got)
	}
}

func TestTimestamptzFrom(t *testing.T) {
	t.Parallel()
	ts := timestamptzFrom(time.Now())
	if !ts.Valid {
		t.Error("timestamptzFrom: want Valid, got invalid")
	}
}
