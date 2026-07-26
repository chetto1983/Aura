package idempotency

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

var errFakeQuery = errors.New("fake idempotency query failure")

type fakeOperationQueries struct {
	tryStart      tryStartQuery
	recover       recoverExpiredOperationQuery
	get           getOperationQuery
	complete      completeOperationQuery
	indeterminate markIndeterminateQuery
	listExpired   listExpiredQuery
	clearExpired  clearExpiredQuery
}

type tryStartQuery func(context.Context, sqlc.TryStartOperationParams) (int64, error)
type recoverExpiredOperationQuery func(context.Context, sqlc.TryRecoverExpiredOperationParams) (int64, error)
type getOperationQuery func(context.Context, sqlc.GetOperationParams) (sqlc.GetOperationRow, error)
type completeOperationQuery func(context.Context, sqlc.CompleteOperationParams) (int64, error)
type markIndeterminateQuery func(context.Context, sqlc.MarkOperationIndeterminateParams) (int64, error)
type listExpiredQuery func(context.Context, sqlc.ListExpiredReplayBodiesParams) ([]sqlc.ListExpiredReplayBodiesRow, error)
type clearExpiredQuery func(context.Context, sqlc.ClearExpiredReplayBodyParams) (int64, error)

func (f *fakeOperationQueries) TryStartOperation(ctx context.Context, arg sqlc.TryStartOperationParams) (int64, error) {
	if f.tryStart == nil {
		return 0, errors.New("unexpected TryStartOperation")
	}
	return f.tryStart(ctx, arg)
}

func (f *fakeOperationQueries) TryRecoverExpiredOperation(
	ctx context.Context,
	arg sqlc.TryRecoverExpiredOperationParams,
) (int64, error) {
	if f.recover == nil {
		return 0, errors.New("unexpected TryRecoverExpiredOperation")
	}
	return f.recover(ctx, arg)
}

func (f *fakeOperationQueries) GetOperation(ctx context.Context, arg sqlc.GetOperationParams) (sqlc.GetOperationRow, error) {
	if f.get == nil {
		return sqlc.GetOperationRow{}, errors.New("unexpected GetOperation")
	}
	return f.get(ctx, arg)
}

func (f *fakeOperationQueries) CompleteOperation(ctx context.Context, arg sqlc.CompleteOperationParams) (int64, error) {
	if f.complete == nil {
		return 0, errors.New("unexpected CompleteOperation")
	}
	return f.complete(ctx, arg)
}

func (f *fakeOperationQueries) MarkOperationIndeterminate(ctx context.Context, arg sqlc.MarkOperationIndeterminateParams) (int64, error) {
	if f.indeterminate == nil {
		return 0, errors.New("unexpected MarkOperationIndeterminate")
	}
	return f.indeterminate(ctx, arg)
}

func (f *fakeOperationQueries) ListExpiredReplayBodies(ctx context.Context, arg sqlc.ListExpiredReplayBodiesParams) ([]sqlc.ListExpiredReplayBodiesRow, error) {
	if f.listExpired == nil {
		return nil, errors.New("unexpected ListExpiredReplayBodies")
	}
	return f.listExpired(ctx, arg)
}

func (f *fakeOperationQueries) ClearExpiredReplayBody(ctx context.Context, arg sqlc.ClearExpiredReplayBodyParams) (int64, error) {
	if f.clearExpired == nil {
		return 0, errors.New("unexpected ClearExpiredReplayBody")
	}
	return f.clearExpired(ctx, arg)
}

func TestStoreBeginFirstAcquisition(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	req := testBeginRequest(t, "first-acquisition")
	queries := &fakeOperationQueries{tryStart: func(_ context.Context, arg sqlc.TryStartOperationParams) (int64, error) {
		if arg.OperationScope != string(req.Operation.Scope) || arg.OperationKey != req.Operation.Key {
			t.Fatalf("operation identity mismatch: %+v", arg)
		}
		if !bytes.Equal(arg.PayloadHash, req.Fingerprint[:]) {
			t.Fatal("payload hash was not persisted")
		}
		if got, want := arg.LeaseExpiresAt.Time, now.Add(2*time.Minute); !got.Equal(want) {
			t.Fatalf("lease expiry=%v, want %v", got, want)
		}
		if got, want := arg.RetryAfter.Time, now.Add(3*time.Second); !got.Equal(want) {
			t.Fatalf("retry_after=%v, want %v", got, want)
		}
		return 7, nil
	}}
	store := newStore(queries, Config{Now: func() time.Time { return now }, LeaseDuration: 2 * time.Minute, RetryAfter: 3 * time.Second})

	got, err := store.Begin(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != DecisionAcquired || got.ClaimToken != ClaimToken(7) ||
		got.Replay != nil || got.RetryAfter != 0 {
		t.Fatalf("Begin()=%+v, want acquired", got)
	}
}

func TestStoreBeginExistingOperationDecisions(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	req := testBeginRequest(t, "existing")
	expires := now.Add(time.Hour)
	tests := []struct {
		name       string
		row        sqlc.GetOperationRow
		want       Decision
		wantRetry  time.Duration
		wantErr    error
		wantReplay bool
	}{
		{name: "completed replay before deadline", row: testRow(req, StateCompleted, now, expires), want: DecisionReplay, wantReplay: true},
		{name: "completed replay at deadline retains bytes but is expired", row: testRow(req, StateCompleted, now, now), want: DecisionResultExpired},
		{name: "completed replay after deadline retains bytes but is expired", row: testRow(req, StateCompleted, now, now.Add(-time.Nanosecond)), want: DecisionResultExpired},
		{name: "completed replay expired", row: func() sqlc.GetOperationRow {
			row := testRow(req, StateCompleted, now, expires)
			row.ReplayBody, row.ReplayPreview, row.ReplaySidecarRef = nil, pgtype.Text{}, pgtype.Text{}
			row.ReplayStatusCode, row.ReplayHeaders = pgtype.Int2{}, nil
			row.ReplayClearedAt = pgtype.Timestamptz{Time: now, Valid: true}
			return row
		}(), want: DecisionResultExpired},
		{name: "completed replay missing representations", row: func() sqlc.GetOperationRow {
			row := testRow(req, StateCompleted, now, expires)
			row.ReplayBody, row.ReplayPreview, row.ReplaySidecarRef = nil, pgtype.Text{}, pgtype.Text{}
			row.ReplayStatusCode, row.ReplayHeaders = pgtype.Int2{}, nil
			return row
		}(), want: DecisionResultExpired},
		{name: "fresh in progress", row: testRow(req, StateInProgress, now.Add(17*time.Second), time.Time{}), want: DecisionInProgress, wantRetry: 17 * time.Second},
		{name: "terminal indeterminate", row: testRow(req, StateIndeterminate, now, time.Time{}), want: DecisionIndeterminate},
		{name: "different fingerprint conflicts", row: func() sqlc.GetOperationRow {
			row := testRow(req, StateInProgress, now, time.Time{})
			row.PayloadHash[0] ^= 0xff
			return row
		}(), want: DecisionConflict, wantErr: ErrConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			queries := &fakeOperationQueries{
				tryStart: func(context.Context, sqlc.TryStartOperationParams) (int64, error) {
					return 0, pgx.ErrNoRows
				},
				get: func(context.Context, sqlc.GetOperationParams) (sqlc.GetOperationRow, error) {
					return tc.row, nil
				},
			}
			store := newStore(queries, Config{Now: func() time.Time { return now }})
			got, err := store.Begin(context.Background(), req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error=%v, want %v", err, tc.wantErr)
			}
			if got.Decision != tc.want || got.RetryAfter != tc.wantRetry {
				t.Fatalf("decision=%+v, want %s retry %v", got, tc.want, tc.wantRetry)
			}
			if (got.Replay != nil) != tc.wantReplay {
				t.Fatalf("replay presence=%t, want %t", got.Replay != nil, tc.wantReplay)
			}
			if got.Replay != nil && (!bytes.Equal(got.Replay.Body, tc.row.ReplayBody) || got.Replay.Preview != "ok" || got.Replay.StatusCode != 202 || got.Replay.Headers["Location"] != "/operations/one") {
				t.Fatalf("replay=%+v, row=%+v", got.Replay, tc.row)
			}
		})
	}
}

func TestStoreBeginFailedConflictReadNeverInfersOwnership(t *testing.T) {
	req := testBeginRequest(t, "read-failure")
	queries := &fakeOperationQueries{
		tryStart: func(context.Context, sqlc.TryStartOperationParams) (int64, error) {
			return 0, pgx.ErrNoRows
		},
		get: func(context.Context, sqlc.GetOperationParams) (sqlc.GetOperationRow, error) {
			return sqlc.GetOperationRow{}, errFakeQuery
		},
	}
	got, err := newStore(queries, Config{}).Begin(context.Background(), req)
	if !errors.Is(err, errFakeQuery) {
		t.Fatalf("error=%v, want wrapped query failure", err)
	}
	if got.Decision != "" {
		t.Fatalf("failed durable read returned decision %q", got.Decision)
	}
}

func TestStoreRecoverExpiredRequiresExactOperationAndRenewsLease(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	request := testBeginRequest(t, "recover-expired")
	queries := &fakeOperationQueries{
		recover: func(
			_ context.Context,
			arg sqlc.TryRecoverExpiredOperationParams,
		) (int64, error) {
			if arg.OperationScope != string(request.Operation.Scope) ||
				arg.OperationKey != request.Operation.Key ||
				!bytes.Equal(arg.PayloadHash, request.Fingerprint[:]) ||
				!arg.Now.Time.Equal(now) ||
				!arg.LeaseExpiresAt.Time.Equal(now.Add(2*time.Minute)) ||
				!arg.RetryAfter.Time.Equal(now.Add(3*time.Second)) {
				t.Fatalf("recovery parameters = %#v", arg)
			}
			return 8, nil
		},
	}
	store := newStore(queries, Config{
		Now: func() time.Time { return now }, LeaseDuration: 2 * time.Minute,
		RetryAfter: 3 * time.Second,
	})
	claimToken, recovered, err := store.RecoverExpired(t.Context(), request)
	if err != nil {
		t.Fatalf("RecoverExpired: %v", err)
	}
	if !recovered || claimToken != ClaimToken(8) {
		t.Fatalf("RecoverExpired = %d/%t, want 8/true", claimToken, recovered)
	}
}

func TestStoreRecoverExpiredDoesNotAcquireUnexpiredOrChangedOperation(
	t *testing.T,
) {
	t.Parallel()
	request := testBeginRequest(t, "not-recoverable")
	store := newStore(&fakeOperationQueries{
		recover: func(
			context.Context,
			sqlc.TryRecoverExpiredOperationParams,
		) (int64, error) {
			return 0, pgx.ErrNoRows
		},
	}, Config{})
	claimToken, recovered, err := store.RecoverExpired(t.Context(), request)
	if err != nil {
		t.Fatalf("RecoverExpired: %v", err)
	}
	if recovered || claimToken != 0 {
		t.Fatalf("RecoverExpired = %d/%t, want zero/false", claimToken, recovered)
	}
}

func TestStoreRecoverExpiredRejectsInvalidClaimToken(t *testing.T) {
	t.Parallel()
	request := testBeginRequest(t, "invalid-recovery-count")
	store := newStore(&fakeOperationQueries{
		recover: func(
			context.Context,
			sqlc.TryRecoverExpiredOperationParams,
		) (int64, error) {
			return 0, nil
		},
	}, Config{})
	if _, _, err := store.RecoverExpired(t.Context(), request); err == nil {
		t.Fatal("RecoverExpired accepted an invalid claim token")
	}
}

func TestStoreTerminalTransitionsAreConditional(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	req := testBeginRequest(t, "terminal")
	result := ReplayResult{Body: []byte(`{"ok":true}`), Preview: "ok", SidecarRef: "replays/one", StatusCode: 202, Headers: map[string]string{"Location": "/operations/one"}, ExpiresAt: now.Add(time.Hour)}
	queries := &fakeOperationQueries{
		complete: func(_ context.Context, arg sqlc.CompleteOperationParams) (int64, error) {
			if arg.ClaimToken != 9 ||
				!bytes.Equal(arg.PayloadHash, req.Fingerprint[:]) ||
				arg.ReplayBytes <= int64(len(result.Body)+len(result.Preview)+len(result.SidecarRef)) ||
				arg.ReplayStatusCode.Int16 != 202 ||
				!bytes.Contains(arg.ReplayHeaders, []byte("/operations/one")) {
				t.Fatalf("completion parameters=%+v", arg)
			}
			return 1, nil
		},
		indeterminate: func(_ context.Context, arg sqlc.MarkOperationIndeterminateParams) (int64, error) {
			if arg.ClaimToken != 9 || !bytes.Equal(arg.PayloadHash, req.Fingerprint[:]) {
				t.Fatal("indeterminate transition omitted fingerprint or claim token")
			}
			return 0, nil
		},
	}
	store := newStore(queries, Config{Now: func() time.Time { return now }})
	if err := store.Complete(context.Background(), CompleteRequest{
		Operation: req.Operation, Fingerprint: req.Fingerprint,
		ClaimToken: ClaimToken(9), Result: result,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkIndeterminate(
		context.Background(), req.Operation, req.Fingerprint, ClaimToken(9),
	); !errors.Is(err, ErrStaleTransition) {
		t.Fatalf("MarkIndeterminate error=%v, want stale transition", err)
	}
}

func TestStoreRejectsInvalidInputBeforePersistence(t *testing.T) {
	queries := &fakeOperationQueries{}
	store := newStore(queries, Config{})
	if _, err := store.Begin(context.Background(), BeginRequest{}); err == nil {
		t.Fatal("Begin accepted invalid request")
	}
	if _, _, err := store.RecoverExpired(
		context.Background(),
		BeginRequest{},
	); err == nil {
		t.Fatal("RecoverExpired accepted invalid request")
	}
	if err := store.Complete(context.Background(), CompleteRequest{}); err == nil {
		t.Fatal("Complete accepted invalid request")
	}
	if err := store.MarkIndeterminate(context.Background(), OperationKey{}, [32]byte{}, 0); err == nil {
		t.Fatal("MarkIndeterminate accepted invalid request")
	}
}

func TestStoreExpireReplayBodiesUsesBoundedBatch(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC)
	before := now.Add(-time.Hour)
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	rows := make([]sqlc.ListExpiredReplayBodiesRow, 3)
	for i := range rows {
		rows[i] = sqlc.ListExpiredReplayBodiesRow{
			IdentityID: pgtype.UUID{Bytes: ids[i], Valid: true}, OperationScope: string(ScopeHTTPMutation),
			OperationKey: "expired", ReplayBytes: int64(i + 4), ReplayExpiresAt: pgtype.Timestamptz{Time: before, Valid: true},
		}
	}
	cleared := 0
	queries := &fakeOperationQueries{
		listExpired: func(_ context.Context, arg sqlc.ListExpiredReplayBodiesParams) ([]sqlc.ListExpiredReplayBodiesRow, error) {
			if arg.BatchSize != 2 || !arg.Before.Time.Equal(before) {
				t.Fatalf("list params=%+v", arg)
			}
			return rows[:2], nil
		},
		clearExpired: func(_ context.Context, arg sqlc.ClearExpiredReplayBodyParams) (int64, error) {
			if !arg.Before.Time.Equal(before) || !arg.ClearedAt.Time.Equal(now) {
				t.Fatalf("clear params=%+v", arg)
			}
			cleared++
			return 1, nil
		},
	}
	store := newStore(queries, Config{Now: func() time.Time { return now }, ExpiryBatch: 2})
	report, err := store.ExpireReplayBodies(context.Background(), before)
	if err != nil {
		t.Fatal(err)
	}
	if report.Cleared != 2 || report.Bytes != 9 || cleared != 2 {
		t.Fatalf("report=%+v cleared calls=%d", report, cleared)
	}
}

func testBeginRequest(t *testing.T, key string) BeginRequest {
	t.Helper()
	fingerprint, err := FingerprintTyped(struct {
		Value string `json:"value"`
	}{Value: key})
	if err != nil {
		t.Fatal(err)
	}
	return BeginRequest{Operation: OperationKey{IdentityID: "00000000-0000-0000-0000-000000000001", Scope: ScopeHTTPMutation, Key: key}, Fingerprint: fingerprint}
}

func testRow(req BeginRequest, state State, retry, expires time.Time) sqlc.GetOperationRow {
	row := sqlc.GetOperationRow{
		PayloadHash: append([]byte(nil), req.Fingerprint[:]...), State: string(state),
		RetryAfter: pgtype.Timestamptz{Time: retry, Valid: !retry.IsZero()},
	}
	if state == StateCompleted {
		row.ReplayBody = []byte(`{"ok":true}`)
		row.ReplayPreview = pgtype.Text{String: "ok", Valid: true}
		row.ReplaySidecarRef = pgtype.Text{String: "replays/one", Valid: true}
		row.ReplayStatusCode = pgtype.Int2{Int16: 202, Valid: true}
		row.ReplayHeaders = []byte(`{"Location":"/operations/one"}`)
		row.ReplayExpiresAt = pgtype.Timestamptz{Time: expires, Valid: true}
	}
	return row
}
