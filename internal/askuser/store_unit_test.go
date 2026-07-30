package askuser

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestEncodeAnswer_ValidActions accepts the three MCP actions and round-trips the
// {action, content} jsonb (AM-02).
func TestEncodeAnswer_ValidActions(t *testing.T) {
	for _, action := range []string{ActionAccept, ActionDecline, ActionCancel} {
		b, err := encodeAnswer(ResumeAnswer{Action: action, Content: "hi"})
		if err != nil {
			t.Fatalf("encodeAnswer(%q): %v", action, err)
		}
		var got ResumeAnswer
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Action != action || got.Content != "hi" {
			t.Errorf("round-trip: want {%s hi}, got %+v", action, got)
		}
	}
}

// TestEncodeAnswer_RejectsUnknownAction classifies a bad action as ErrInvalidAnswer.
func TestEncodeAnswer_RejectsUnknownAction(t *testing.T) {
	for _, bad := range []string{"", "approve", "yes", "ACCEPT"} {
		if _, err := encodeAnswer(ResumeAnswer{Action: bad}); !errors.Is(err, ErrInvalidAnswer) {
			t.Errorf("encodeAnswer(%q): want ErrInvalidAnswer, got %v", bad, err)
		}
	}
}

func TestDecodeResumedAnswerRejectsNonObject(t *testing.T) {
	_, err := decodeResumedAnswer("tok", []byte(`42`))
	if err == nil {
		t.Fatal("decodeResumedAnswer(non-object): want error, got nil")
	}
	if !strings.Contains(err.Error(), "decode resumed_answer") {
		t.Fatalf("error should name resumed_answer decode, got %v", err)
	}
}

func TestFromRowCarriesResumeContext(t *testing.T) {
	tok, err := db.ParseUUID("token", "11111111-1111-1111-1111-111111111111")
	if err != nil {
		t.Fatal(err)
	}
	conv, err := db.ParseUUID("conversation_id", "22222222-2222-2222-2222-222222222222")
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"type":"skill_approval","skill_name":"calc"}`)
	got := fromRow(sqlc.AuraPausedStates{
		Token:          tok,
		ConversationID: conv,
		Kind:           "approval",
		Question:       "approve?",
		ResumeContext:  raw,
		ToolCallID:     "tc1",
	})
	if string(got.ResumeContext) != string(raw) {
		t.Fatalf("ResumeContext = %s, want %s", got.ResumeContext, raw)
	}
}

// TestParseUUID_RejectsMalformed covers the boundary parse failure each Store
// method funnels through before any DB round-trip.
func TestParseUUID_RejectsMalformed(t *testing.T) {
	if _, err := db.ParseUUID("token", "not-a-uuid"); err == nil {
		t.Fatal("parseUUID(bad): want error, got nil")
	}
	good := "00000000-0000-0000-0000-000000000001"
	pg, err := db.ParseUUID("token", good)
	if err != nil {
		t.Fatalf("parseUUID(good): %v", err)
	}
	if !pg.Valid {
		t.Error("parsed pgtype.UUID must be Valid")
	}
}

// TestStore_BadUUID_FailsBeforeDB drives every UUID-taking method with a malformed
// id so the parse branch returns before any pool call — exercised with a nil-pool
// Store to prove no DB access happens on the error path.
func TestStore_BadUUID_FailsBeforeDB(t *testing.T) {
	s := New(nil) // nil pool: a DB touch would panic, proving parse short-circuits
	ctx := context.Background()
	bad := "nope"

	if err := s.Insert(ctx, InsertParams{Token: bad, ConversationID: bad, Kind: "approval"}); err == nil {
		t.Error("Insert(bad token): want error, got nil")
	}
	if err := s.Insert(ctx, InsertParams{Token: "00000000-0000-0000-0000-000000000001", ConversationID: bad}); err == nil {
		t.Error("Insert(bad conv): want error, got nil")
	}
	if _, err := s.GetByToken(ctx, bad); err == nil {
		t.Error("GetByToken(bad): want error, got nil")
	}
	if _, err := s.ListPending(ctx, bad); err == nil {
		t.Error("ListPending(bad): want error, got nil")
	}
	if err := s.MarkResumed(ctx, bad, ResumeAnswer{Action: ActionAccept}); err == nil {
		t.Error("MarkResumed(bad): want error, got nil")
	}
	if err := s.AutoResolveForConversation(ctx, bad); err == nil {
		t.Error("AutoResolveForConversation(bad): want error, got nil")
	}
}

// TestMarkResumed_RejectsInvalidAnswerBeforeDB: an invalid action fails encoding
// before any DB call (nil-pool Store proves it).
func TestMarkResumed_RejectsInvalidAnswerBeforeDB(t *testing.T) {
	s := New(nil)
	tok := "11111111-1111-1111-1111-111111111111"
	if err := s.MarkResumed(context.Background(), tok, ResumeAnswer{Action: "bogus"}); !errors.Is(err, ErrInvalidAnswer) {
		t.Errorf("MarkResumed(bad action): want ErrInvalidAnswer, got %v", err)
	}
}

// TestMarkResumedBatch_EmptyIsNoOp: an empty map returns nil without touching the
// DB (nil-pool Store proves it).
func TestMarkResumedBatch_EmptyIsNoOp(t *testing.T) {
	s := New(nil)
	if err := s.MarkResumedBatch(context.Background(), nil); err != nil {
		t.Errorf("empty batch must be a no-op, got %v", err)
	}
}

// TestStore_ParseFailWrapMessages pins the DISTINCT error-context prefix each
// method stamps onto a parse/encode failure (nil-pool: the parse/encode guard
// returns before any DB call). Asserting the exact prefix kills the survivors
// that remove each `return fmt.Errorf("<context>: %w", ...)` wrap — without a
// per-guard message the wrap-removal mutants survive on a bare err!=nil check.
func TestStore_ParseFailWrapMessages(t *testing.T) {
	s := New(nil)
	ctx := context.Background()
	const bad = "nope"
	const goodTok = "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		run    func() error
		prefix string
	}{
		{"insert-token-parse", func() error {
			return s.Insert(ctx, InsertParams{Token: bad, ConversationID: goodTok, Kind: "approval"})
		}, "insert paused state: invalid token"},
		{"insert-conv-parse", func() error {
			return s.Insert(ctx, InsertParams{Token: goodTok, ConversationID: bad, Kind: "approval"})
		}, "insert paused state: invalid conversation_id"},
		{"get-parse", func() error {
			_, err := s.GetByToken(ctx, bad)
			return err
		}, "get paused state: invalid token"},
		{"listpending-parse", func() error {
			_, err := s.ListPending(ctx, bad)
			return err
		}, "list pending: invalid conversation_id"},
		{"markresumed-parse", func() error {
			return s.MarkResumed(ctx, bad, ResumeAnswer{Action: ActionAccept})
		}, "mark resumed: invalid token"},
		{"autoresolve-parse", func() error {
			return s.AutoResolveForConversation(ctx, bad)
		}, "auto-resolve: invalid conversation_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatalf("%s: want error, got nil", tc.name)
			}
			if !strings.HasPrefix(err.Error(), tc.prefix) {
				t.Fatalf("%s: want message prefix %q, got %q", tc.name, tc.prefix, err.Error())
			}
		})
	}
}

// TestStore_EncodeFailWrapMessages pins the per-method context around an
// encodeAnswer (invalid-action) failure, which fires AFTER the UUID parse with a
// valid token (nil-pool: still no DB call). It kills the encodeAnswer wrap
// survivors in MarkResumed and MarkResumedBatch, distinguishing them by the
// token-bearing prefix; it also pins ErrInvalidAnswer propagation.
func TestStore_EncodeFailWrapMessages(t *testing.T) {
	s := New(nil)
	ctx := context.Background()
	const goodTok = "11111111-1111-1111-1111-111111111111"

	mrErr := s.MarkResumed(ctx, goodTok, ResumeAnswer{Action: "bogus"})
	if !errors.Is(mrErr, ErrInvalidAnswer) {
		t.Fatalf("MarkResumed bad action: want ErrInvalidAnswer, got %v", mrErr)
	}
	if got := mrErr.Error(); !strings.HasPrefix(got, "mark resumed "+goodTok+":") {
		t.Fatalf("MarkResumed encode-fail wrap: want prefix %q, got %q", "mark resumed "+goodTok+":", got)
	}
}

// recordingDBTX is an in-memory sqlc.DBTX that records the positional args of each
// Exec/Query so a unit test can assert the order/values the Store passed to the
// generated queries — without a live Postgres. Exec reports RowsAffected==1 (a claimed
// row) so the MarkResumedBatchTx loop proceeds through every token; Query returns zero
// rows so the ListRecent read loop returns cleanly.
type recordingDBTX struct {
	execTokens []string // canonical UUIDs captured from MarkPausedStateResumed's $1
	queryLimit int32    // the int32 LIMIT captured from ListRecentPausedStates' $1
	queryCalls int
}

func (r *recordingDBTX) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	if len(args) > 0 {
		if u, ok := args[0].(pgtype.UUID); ok {
			r.execTokens = append(r.execTokens, uuid.UUID(u.Bytes).String())
		}
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (r *recordingDBTX) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	r.queryCalls++
	if len(args) > 0 {
		if lim, ok := args[0].(int32); ok {
			r.queryLimit = lim
		}
	}
	return &emptyRows{}, nil
}

// QueryRow is unused by the methods under test (they use Exec/Query); the nil row is
// never dereferenced.
func (r *recordingDBTX) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

// emptyRows is a pgx.Rows that yields no rows (Next→false immediately), letting the
// sqlc read loop return an empty slice cleanly.
type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(...any) error                            { return nil }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }

// TestMarkResumedBatchTx_ClaimsInSortedTokenOrder asserts the batch claims pauses in
// SORTED token order (RESEARCH landmine #2 / T-34-B) so two concurrent overlapping
// batches lock rows in the same order and cannot deadlock (Postgres 40P01). The input
// is a map (random iteration order) and the recording fake captures the actual UPDATE
// order; a non-sorting implementation would surface a non-sorted order and fail. Eight
// tokens make an accidental in-order map iteration astronomically unlikely.
func TestMarkResumedBatchTx_ClaimsInSortedTokenOrder(t *testing.T) {
	rec := &recordingDBTX{}
	q := sqlc.New(rec)
	s := New(nil) // pool unused: the Tx method operates on the supplied Queries only

	tokens := []string{
		"88888888-8888-8888-8888-888888888888",
		"22222222-2222-2222-2222-222222222222",
		"66666666-6666-6666-6666-666666666666",
		"11111111-1111-1111-1111-111111111111",
		"55555555-5555-5555-5555-555555555555",
		"33333333-3333-3333-3333-333333333333",
		"77777777-7777-7777-7777-777777777777",
		"44444444-4444-4444-4444-444444444444",
	}
	answers := make(map[string]ResumeAnswer, len(tokens))
	for _, tok := range tokens {
		answers[tok] = ResumeAnswer{Action: ActionAccept, Content: "ok"}
	}

	if err := s.MarkResumedBatchTx(context.Background(), q, answers); err != nil {
		t.Fatalf("MarkResumedBatchTx: %v", err)
	}

	want := []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
		"33333333-3333-3333-3333-333333333333",
		"44444444-4444-4444-4444-444444444444",
		"55555555-5555-5555-5555-555555555555",
		"66666666-6666-6666-6666-666666666666",
		"77777777-7777-7777-7777-777777777777",
		"88888888-8888-8888-8888-888888888888",
	}
	if !reflect.DeepEqual(rec.execTokens, want) {
		t.Errorf("batch must claim tokens in sorted order (deadlock-free), got %v want %v", rec.execTokens, want)
	}
}

// TestListRecent_Int32Guard pins the QUAL-04a int32 narrowing guard: the LIMIT bound to
// ListRecentPausedStates falls back to 50 for a non-positive OR int32-overflowing input
// and never wraps to a negative LIMIT. A recording fake captures the int32 actually
// passed to the generated query.
func TestListRecent_Int32Guard(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int32
	}{
		{"zero", 0, 50},
		{"negative", -5, 50},
		{"fifty", 50, 50},
		{"maxint32", math.MaxInt32, math.MaxInt32},
		{"overflow", int(int64(math.MaxInt32) + 1), 50},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &recordingDBTX{}
			s := &Store{q: sqlc.New(rec)} // pool unused: ListRecent reads via s.q only
			if _, err := s.ListRecent(context.Background(), tc.in); err != nil {
				t.Fatalf("ListRecent(%d): %v", tc.in, err)
			}
			if rec.queryCalls != 1 {
				t.Fatalf("ListRecent(%d): want exactly 1 query, got %d", tc.in, rec.queryCalls)
			}
			if rec.queryLimit != tc.want {
				t.Errorf("ListRecent(%d): bound LIMIT = %d, want %d (int32 guard)", tc.in, rec.queryLimit, tc.want)
			}
		})
	}
}
