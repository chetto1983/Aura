// Unit tier (no build tag): pure projection + path-safety + numeric-boundary logic
// that needs no database. The DB round-trip paths are exercised under db_integration
// in store_test.go.
package conversations

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestNumericFloatRoundTrip(t *testing.T) {
	t.Parallel()
	cases := []float64{0, 0.0001, 0.1966, 1.2345, 12.5, 9999.9999, 0.00005}
	for _, in := range cases {
		n, err := numericFromFloat(in)
		if err != nil {
			t.Fatalf("numericFromFloat(%v): %v", in, err)
		}
		got := floatFromNumeric(n)
		// numeric(10,4) keeps 4 decimals; allow a half-ulp at that scale.
		if diff := got - roundTo4(in); diff > 5e-5 || diff < -5e-5 {
			t.Errorf("round-trip %v -> %v (want ~%v)", in, got, roundTo4(in))
		}
	}
}

func roundTo4(f float64) float64 {
	scaled := f * 1e4
	if scaled >= 0 {
		scaled += 0.5
	} else {
		scaled -= 0.5
	}
	return float64(int64(scaled)) / 1e4
}

func TestFloatFromNumeric_NullAndNaN(t *testing.T) {
	t.Parallel()
	if got := floatFromNumeric(pgtype.Numeric{}); got != 0 {
		t.Errorf("invalid numeric: want 0, got %v", got)
	}
	if got := floatFromNumeric(pgtype.Numeric{NaN: true, Valid: true}); got != 0 {
		t.Errorf("NaN numeric: want 0, got %v", got)
	}
}

func TestDisplayTitle(t *testing.T) {
	t.Parallel()
	set := Conversation{Title: "Refactor the loop", TitleSet: true}
	if got := set.DisplayTitle(); got != "Refactor the loop" {
		t.Errorf("set title: got %q", got)
	}
	null := Conversation{TitleSet: false, CreatedAt: "2026-05-30T12:00:00Z"}
	if got := null.DisplayTitle(); got != "(untitled 2026-05-30T12:00:00Z)" {
		t.Errorf("null title: got %q", got)
	}
	// Empty-but-set falls back to untitled too (defensive).
	empty := Conversation{Title: "", TitleSet: true, CreatedAt: "2026-05-30T12:00:00Z"}
	if got := empty.DisplayTitle(); !strings.HasPrefix(got, "(untitled") {
		t.Errorf("empty set title: got %q", got)
	}
}

func TestValidateID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id      string
		wantErr bool
	}{
		{"00000000-0000-0000-0000-000000000001", false},
		{"", true},
		{"..", true},
		{"a/../b", true},
		{"a/b", true},
		{"a\\b", true},
	}
	for _, tc := range tests {
		err := validateID("conversation_id", tc.id)
		if (err != nil) != tc.wantErr {
			t.Errorf("validateID(%q): err=%v want wantErr=%v", tc.id, err, tc.wantErr)
		}
	}
}

func TestTurnToMessage(t *testing.T) {
	t.Parallel()
	// Plain assistant text turn.
	m, err := turnToMessage(Turn{Seq: 2, Role: llm.RoleAssistant, Content: "hi"})
	if err != nil {
		t.Fatalf("turnToMessage: %v", err)
	}
	if m.Role != llm.RoleAssistant || m.Content != "hi" || len(m.ToolCalls) != 0 {
		t.Errorf("plain turn projection wrong: %+v", m)
	}

	// Assistant turn carrying tool_calls jsonb.
	raw := []byte(`[{"id":"call_1","type":"function","function":{"name":"text_response","arguments":"{}"}}]`)
	mt, err := turnToMessage(Turn{Seq: 3, Role: llm.RoleAssistant, ToolCalls: raw})
	if err != nil {
		t.Fatalf("turnToMessage(tool_calls): %v", err)
	}
	if len(mt.ToolCalls) != 1 || mt.ToolCalls[0].ID != "call_1" || mt.ToolCalls[0].Function.Name != "text_response" {
		t.Errorf("tool_calls projection wrong: %+v", mt.ToolCalls)
	}

	// Tool-role turn with a tool_call_id.
	mtool, err := turnToMessage(Turn{Seq: 4, Role: llm.RoleTool, Content: "result", ToolCallID: "call_1"})
	if err != nil {
		t.Fatalf("turnToMessage(tool): %v", err)
	}
	if mtool.Role != llm.RoleTool || mtool.ToolCallID != "call_1" {
		t.Errorf("tool turn projection wrong: %+v", mtool)
	}

	// Malformed tool_calls jsonb is an error (not a silent drop).
	if _, err := turnToMessage(Turn{Role: llm.RoleAssistant, ToolCalls: []byte(`{bad`)}); err == nil {
		t.Error("malformed tool_calls: want error, got nil")
	}
}

func TestOptionalText(t *testing.T) {
	t.Parallel()
	if got := optionalText(""); got.Valid {
		t.Error("empty string must be NULL")
	}
	if got := optionalText("call_1"); !got.Valid || got.String != "call_1" {
		t.Errorf("non-empty must be set: %+v", got)
	}
}

func TestNumericFromFloat_Negative(t *testing.T) {
	t.Parallel()
	n, err := numericFromFloat(-1.2345)
	if err != nil {
		t.Fatalf("numericFromFloat(neg): %v", err)
	}
	if got := floatFromNumeric(n); got > -1.2344 || got < -1.2346 {
		t.Errorf("negative round-trip: got %v", got)
	}
}

func TestNumericFromFloat_RejectsNonFiniteAndOverflow(t *testing.T) {
	t.Parallel()
	for _, in := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 1e12, -1e12} {
		if _, err := numericFromFloat(in); err == nil {
			t.Errorf("numericFromFloat(%v): want error, got nil", in)
		}
	}
}

func TestParseTiktokenBPE_Malformed(t *testing.T) {
	t.Parallel()
	// A valid line then a malformed one (missing rank) → error.
	if _, err := parseTiktokenBPE([]byte("aGVsbG8= 0\nbadline")); err == nil {
		t.Error("malformed BPE line must error")
	}
	// Non-base64 token → error.
	if _, err := parseTiktokenBPE([]byte("!!! 0")); err == nil {
		t.Error("non-base64 token must error")
	}
	// Non-integer rank → error.
	if _, err := parseTiktokenBPE([]byte("aGVsbG8= notnum")); err == nil {
		t.Error("non-integer rank must error")
	}
}

func TestNewStore_DefaultCap(t *testing.T) {
	t.Parallel()
	s := New(nil, Config{RunDir: "/x", TurnCapBytes: 0})
	if s.turnCapBytes != 65536 {
		t.Errorf("zero cap must fall back to 65536, got %d", s.turnCapBytes)
	}
}

func TestNormalizeSearchLimitBoundsInt32(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   int
		want int32
	}{
		{name: "default zero", in: 0, want: 20},
		{name: "default negative", in: -1, want: 20},
		{name: "preserve positive", in: 7, want: 7},
	}
	if strconv.IntSize > 32 {
		tooLarge64 := int64(2147483647) + 1
		cases = append(cases, struct {
			name string
			in   int
			want int32
		}{name: "clamp int32 overflow", in: int(tooLarge64), want: 2147483647})
	}
	for _, tc := range cases {
		got := normalizeSearchLimit(tc.in)
		if got != tc.want {
			t.Errorf("%s: normalizeSearchLimit(%d) = %d, want %d", tc.name, tc.in, got, tc.want)
		}
	}
}

// TestLoadManagedHistory_BadUUID exercises the parseUUID error branch without a DB
// (loadTurns rejects the id before any query).
func TestLoadManagedHistory_BadUUID(t *testing.T) {
	t.Parallel()
	s := New(nil, Config{RunDir: "/x"})
	if _, err := s.LoadManagedHistory(context.Background(), "not-a-uuid",
		ContextConfig{ContextWindow: 1000, MaxOutputTokens: 1}); err == nil {
		t.Error("LoadManagedHistory(bad id): want error")
	}
}

// TestInsertContextRotEvent_BadUUID exercises the parseUUID branch of the emitter.
func TestInsertContextRotEvent_BadUUID(t *testing.T) {
	t.Parallel()
	s := New(nil, Config{RunDir: "/x"})
	if err := s.insertContextRotEvent(context.Background(), "bad", 1, 10, 5); err == nil {
		t.Error("insertContextRotEvent(bad id): want error")
	}
}

// TestSidecarDir_TraversalRejected proves the path-traversal guard.
func TestSidecarDir_TraversalRejected(t *testing.T) {
	t.Parallel()
	s := New(nil, Config{RunDir: "/run"})
	if _, err := s.sidecarDir("../escape"); err == nil {
		t.Error("sidecarDir(traversal): want error")
	}
	if _, err := s.turnSidecarPath("../escape", 1); err == nil {
		t.Error("turnSidecarPath(traversal): want error")
	}
}
