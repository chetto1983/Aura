package settings

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	benchmarkSettingsAdvisoryKey int64 = 4707759426009125972
	maxBenchmarkOverrideLease          = 30 * time.Minute
)

var (
	// ErrInvalidBenchmarkOverride reports a structurally invalid override request.
	ErrInvalidBenchmarkOverride = errors.New("invalid benchmark settings override")
	// ErrBenchmarkOverrideActive reports an unexpired override or held session lock.
	ErrBenchmarkOverrideActive = errors.New("benchmark settings override is active")
)

// BenchmarkModelSettings is the complete model routing tuple changed by the
// portability benchmark.
type BenchmarkModelSettings struct {
	Provider string
	Model    string
	BaseURL  string
}

// QwenPortabilitySettings is the frozen PRD binding for the local llama.cpp run.
var QwenPortabilitySettings = BenchmarkModelSettings{
	Provider: "llamacpp",
	Model:    "qwen",
	BaseURL:  "http://127.0.0.1:18081/v1",
}

var benchmarkSettingKeys = [...]string{
	"AURA_LLM_PROVIDER",
	"AURA_LLM_MODEL",
	"AURA_LLM_BASE_URL",
}

// BenchmarkOverrideRequest identifies and bounds one temporary Qwen override.
type BenchmarkOverrideRequest struct {
	RunID         uuid.UUID
	OwnerID       uuid.UUID
	LeaseDuration time.Duration
}

// BenchmarkOverride keeps the advisory lock on one dedicated database session
// until exact restoration succeeds.
type BenchmarkOverride struct {
	conn          *pgxpool.Conn
	runID         uuid.UUID
	leaseDuration time.Duration
	mu            sync.Mutex
	restored      bool
}

// BeginQwenPortabilityOverride recovers an expired predecessor and atomically
// journals then applies the three frozen Qwen settings.
func BeginQwenPortabilityOverride(
	ctx context.Context,
	pool *pgxpool.Pool,
	request BenchmarkOverrideRequest,
) (*BenchmarkOverride, error) {
	if pool == nil {
		return nil, ErrInvalidBenchmarkOverride
	}
	if err := validateBenchmarkOverrideRequest(request); err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire benchmark settings session: %w", err)
	}
	if _, err = conn.Exec(ctx, "SELECT pg_advisory_lock($1)", benchmarkSettingsAdvisoryKey); err != nil {
		releaseBenchmarkSettingsConn(ctx, conn)
		return nil, fmt.Errorf("lock benchmark settings session: %w", err)
	}
	release := true
	defer func() {
		if release {
			releaseBenchmarkSettingsConn(ctx, conn)
		}
	}()

	err = withSettingsConnTx(ctx, conn, func(q *sqlc.Queries) error {
		if recoverErr := recoverExpiredLocked(ctx, q); recoverErr != nil {
			return recoverErr
		}
		runID := pgUUID(request.RunID)
		if _, insertErr := q.InsertBenchmarkSettingsOverride(
			ctx,
			sqlc.InsertBenchmarkSettingsOverrideParams{
				RunID:        runID,
				OwnerID:      pgUUID(request.OwnerID),
				LeaseSeconds: request.LeaseDuration.Seconds(),
			},
		); insertErr != nil {
			return fmt.Errorf("insert benchmark settings journal: %w", insertErr)
		}
		values := map[string]string{
			"AURA_LLM_PROVIDER": QwenPortabilitySettings.Provider,
			"AURA_LLM_MODEL":    QwenPortabilitySettings.Model,
			"AURA_LLM_BASE_URL": QwenPortabilitySettings.BaseURL,
		}
		actor := pgtype.Text{String: request.OwnerID.String(), Valid: true}
		for _, key := range benchmarkSettingKeys {
			if captureErr := q.CaptureBenchmarkSetting(
				ctx,
				sqlc.CaptureBenchmarkSettingParams{RunID: runID, Key: key},
			); captureErr != nil {
				return fmt.Errorf("capture benchmark setting %s: %w", key, captureErr)
			}
			if applyErr := q.UpsertBenchmarkSetting(
				ctx,
				sqlc.UpsertBenchmarkSettingParams{
					Key: key, Value: values[key], UpdatedBy: actor,
				},
			); applyErr != nil {
				return fmt.Errorf("apply benchmark setting %s: %w", key, applyErr)
			}
		}
		count, countErr := q.CountBenchmarkSettingsOverrideRows(ctx, runID)
		if countErr != nil {
			return fmt.Errorf("count benchmark settings journal: %w", countErr)
		}
		if count != int32(len(benchmarkSettingKeys)) {
			return fmt.Errorf("%w: captured %d settings rows", ErrInvalidBenchmarkOverride, count)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	release = false
	return &BenchmarkOverride{
		conn: conn, runID: request.RunID, leaseDuration: request.LeaseDuration,
	}, nil
}

// RecoverExpiredBenchmarkOverride restores a crashed predecessor before boot
// overlays settings. An active holder or unexpired lease fails closed.
func RecoverExpiredBenchmarkOverride(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return ErrInvalidBenchmarkOverride
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire benchmark recovery session: %w", err)
	}
	var locked bool
	if err := conn.QueryRow(
		ctx, "SELECT pg_try_advisory_lock($1)", benchmarkSettingsAdvisoryKey,
	).Scan(&locked); err != nil {
		releaseBenchmarkSettingsConn(ctx, conn)
		return fmt.Errorf("try benchmark recovery lock: %w", err)
	}
	if !locked {
		conn.Release()
		return ErrBenchmarkOverrideActive
	}
	defer releaseBenchmarkSettingsConn(ctx, conn)
	return withSettingsConnTx(ctx, conn, func(q *sqlc.Queries) error {
		return recoverExpiredLocked(ctx, q)
	})
}

// Heartbeat extends the journal lease while retaining the same session lock.
func (o *BenchmarkOverride) Heartbeat(ctx context.Context) error {
	if o == nil {
		return ErrInvalidBenchmarkOverride
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.restored || o.conn == nil {
		return ErrInvalidBenchmarkOverride
	}
	affected, err := sqlc.New(o.conn).HeartbeatBenchmarkSettingsOverride(
		ctx,
		sqlc.HeartbeatBenchmarkSettingsOverrideParams{
			RunID: pgUUID(o.runID), LeaseSeconds: o.leaseDuration.Seconds(),
		},
	)
	if err != nil {
		return fmt.Errorf("heartbeat benchmark settings override: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: heartbeat journal is not active", ErrInvalidBenchmarkOverride)
	}
	return nil
}

// Restore reinstates every captured field, verifies equality, completes the
// audit, and releases the session lock. A failed restore remains retryable.
func (o *BenchmarkOverride) Restore(ctx context.Context) error {
	if o == nil {
		return ErrInvalidBenchmarkOverride
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.restored {
		return nil
	}
	if o.conn == nil {
		return ErrInvalidBenchmarkOverride
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := withSettingsConnTx(cleanupCtx, o.conn, func(q *sqlc.Queries) error {
		return restoreLocked(cleanupCtx, q, pgUUID(o.runID), "completed")
	}); err != nil {
		return err
	}
	releaseBenchmarkSettingsConn(cleanupCtx, o.conn)
	o.conn = nil
	o.restored = true
	return nil
}

func recoverExpiredLocked(ctx context.Context, q *sqlc.Queries) error {
	open, err := q.GetOpenBenchmarkSettingsOverride(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read open benchmark settings journal: %w", err)
	}
	expired, err := q.BenchmarkSettingsOverrideExpired(ctx, open.RunID)
	if err != nil {
		return fmt.Errorf("check benchmark settings lease: %w", err)
	}
	if !expired {
		return ErrBenchmarkOverrideActive
	}
	return restoreLocked(ctx, q, open.RunID, "recovered")
}

func restoreLocked(
	ctx context.Context, q *sqlc.Queries, runID pgtype.UUID, finalState string,
) error {
	count, err := q.CountBenchmarkSettingsOverrideRows(ctx, runID)
	if err != nil {
		return fmt.Errorf("count benchmark restore rows: %w", err)
	}
	if count != int32(len(benchmarkSettingKeys)) {
		return fmt.Errorf("%w: restore journal has %d rows", ErrInvalidBenchmarkOverride, count)
	}
	affected, err := q.MarkBenchmarkSettingsOverrideRestoring(ctx, runID)
	if err != nil {
		return fmt.Errorf("mark benchmark settings restoring: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: restore journal is not open", ErrInvalidBenchmarkOverride)
	}
	for _, key := range benchmarkSettingKeys {
		if err := q.RestoreBenchmarkSetting(
			ctx,
			sqlc.RestoreBenchmarkSettingParams{RunID: runID, SettingKey: key},
		); err != nil {
			return fmt.Errorf("restore benchmark setting %s: %w", key, err)
		}
	}
	mismatches, err := q.CountBenchmarkSettingsRestoreMismatches(ctx, runID)
	if err != nil {
		return fmt.Errorf("verify benchmark settings restore: %w", err)
	}
	if mismatches != 0 {
		return fmt.Errorf("%w: restore has %d mismatches", ErrInvalidBenchmarkOverride, mismatches)
	}
	affected, err = q.CompleteBenchmarkSettingsOverride(
		ctx,
		sqlc.CompleteBenchmarkSettingsOverrideParams{RunID: runID, State: finalState},
	)
	if err != nil {
		return fmt.Errorf("complete benchmark settings journal: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("%w: restore audit did not complete", ErrInvalidBenchmarkOverride)
	}
	return nil
}

func withSettingsConnTx(
	ctx context.Context, conn *pgxpool.Conn, fn func(*sqlc.Queries) error,
) (err error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin benchmark settings transaction: %w", err)
	}
	defer func() {
		if panicValue := recover(); panicValue != nil {
			_ = tx.Rollback(ctx)
			panic(panicValue)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return
		}
		err = tx.Commit(ctx)
	}()
	return fn(sqlc.New(tx))
}

func releaseBenchmarkSettingsConn(_ context.Context, conn *pgxpool.Conn) {
	if conn == nil {
		return
	}
	unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var unlocked bool
	err := conn.QueryRow(
		unlockCtx, "SELECT pg_advisory_unlock($1)", benchmarkSettingsAdvisoryKey,
	).Scan(&unlocked)
	if err != nil || !unlocked {
		_ = conn.Hijack().Close(unlockCtx)
		return
	}
	conn.Release()
}

func pgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func validateBenchmarkOverrideRequest(request BenchmarkOverrideRequest) error {
	if request.RunID == uuid.Nil || request.OwnerID == uuid.Nil ||
		request.LeaseDuration <= 0 || request.LeaseDuration > maxBenchmarkOverrideLease {
		return ErrInvalidBenchmarkOverride
	}
	return nil
}
