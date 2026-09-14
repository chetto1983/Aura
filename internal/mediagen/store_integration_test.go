//go:build db_integration

// The live-Postgres half of the video job store: row-level security proven with raw
// statements (not just the store's WHERE clauses), the table's CHECKs, the one-winner
// delivery claim and its rollback, and the resume query's ordering.
//
//	go test -tags db_integration ./internal/mediagen -run TestMediaJob -count=1
package mediagen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
)

func mediaJobEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("media job store integration requires %s under CI", key)
		}
		t.Skipf("media job store integration requires %s", key)
	}
	return v
}

// migratedMediaJobPool opens the nonowner aura_app runtime pool, the role row-level security
// applies to, after migrating the disposable database.
func migratedMediaJobPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pwd := mediaJobEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, mediaJobEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := mediaJobEnvOrSkip(t, "AURA_DB_URL")
	host, port := os.Getenv("PGHOST"), os.Getenv("PGPORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatalf("open app pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedMediaIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.identities(id,name,kind) VALUES($1,$2,'user')`, id, "media-job-"+id[:8]); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

type seededAsset struct {
	thread, modality, status string
	deleted                  bool
}

var acceptedVideo = seededAsset{thread: "thread-a", modality: "video", status: "accepted"}

// seedAsset writes an agent asset inside its owner's transaction, as the watcher's ingest
// leaves it: thread_id is the job's conversation (ruling R6).
func seedAsset(t *testing.T, pool *pgxpool.Pool, owner string, asset seededAsset) string {
	t.Helper()
	id := uuid.NewString()
	err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `
INSERT INTO aura.assets (id, identity_id, source_kind, source_ref, thread_id, scope, modality, status,
                         file_name, mime_type, declared_size_bytes, object_bucket, object_key, deleted_at)
VALUES ($1, $2, 'agent', $3, $4, 'thread', $5, $6, 'clip.mp4', 'video/mp4', 32,
        'media-job-test', $7, CASE WHEN $8 THEN now() END)`,
			id, owner, "media-job:"+id, asset.thread, asset.modality, asset.status, "media/"+id, asset.deleted)
		return err
	})
	if err != nil {
		t.Fatalf("seed asset: %v", err)
	}
	return id
}

// seedJob writes a job row directly, so a test can place it in any state the store would only
// reach through a watcher, and at an explicit creation time.
func seedJob(t *testing.T, pool *pgxpool.Pool, owner, status, assetID string, createdAgo time.Duration, delivered bool) Job {
	t.Helper()
	var id string
	err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `
INSERT INTO aura.media_job (identity_id, conversation_id, tool_call_id, provider_job_id, model, request,
                            status, asset_id, created_at, updated_at, completed_at, delivered_at)
VALUES ($1, 'thread-a', 'call-submit', $2, 'minimax/hailuo-3-max', '{"model":"minimax/hailuo-3-max"}',
        $3, NULLIF($4, '')::uuid, now() - $5::interval, now() - $5::interval,
        CASE WHEN $3 IN ('completed','failed') THEN now() END, CASE WHEN $6 THEN now() END)
RETURNING id`,
			owner, "vid_"+uuid.NewString(), status, assetID, createdAgo.String(), delivered).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seed %s job: %v", status, err)
	}
	job, err := NewStore(pool).Get(context.Background(), owner, id)
	if err != nil {
		t.Fatalf("read seeded job: %v", err)
	}
	return job
}

func newJob(owner string) Job {
	return Job{
		IdentityID: owner, ConversationID: "thread-a", ToolCallID: "call-submit",
		ProviderJobID: "vid_" + uuid.NewString(), Model: "minimax/hailuo-3-max",
		Request: []byte(`{"model":"minimax/hailuo-3-max","_aura":{"origin":"https://openrouter.ai/api/v1"}}`),
		Status:  StatusPending,
	}
}

func assetToolCall(t *testing.T, pool *pgxpool.Pool, owner, assetID string) string {
	t.Helper()
	var call string
	err := db.WithIdentityTxRaw(context.Background(), pool, owner, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT tool_call_id FROM aura.assets WHERE id = $1`, assetID).Scan(&call)
	})
	if err != nil {
		t.Fatalf("read asset tool call: %v", err)
	}
	return call
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

func TestMediaJobClaimsOnceAndIsolatesOwners(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	ownerA, ownerB := seedMediaIdentity(t, pool), seedMediaIdentity(t, pool)
	assetID := seedAsset(t, pool, ownerA, acceptedVideo)
	jobID := seedJob(t, pool, ownerA, "completed", assetID, 0, false).ID

	var won atomic.Int32
	var winner atomic.Value
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			call := uuid.NewString()
			job, claimed, err := store.ClaimDelivery(ctx, ownerA, jobID, "thread-a", call)
			if err != nil {
				t.Error(err)
				return
			}
			if job.DeliveredAt == nil {
				t.Error("both the winner and the loser must see the job delivered")
			}
			if claimed {
				won.Add(1)
				winner.Store(call)
			}
		})
	}
	workers.Wait()
	if won.Load() != 1 {
		t.Fatalf("delivery winners = %d", won.Load())
	}
	if _, err := store.Get(ctx, ownerB, jobID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("foreign job must look missing")
	}

	if got := assetToolCall(t, pool, ownerA, assetID); got != winner.Load() {
		t.Fatalf("asset tool_call_id = %q, want the winning delivery call %q", got, winner.Load())
	}
	job, err := store.Get(ctx, ownerA, jobID)
	if err != nil || job.ToolCallID != "call-submit" {
		t.Fatalf("job tool_call_id = %q (%v), want the submitting call kept", job.ToolCallID, err)
	}
	if _, claimed, err := store.ClaimDelivery(ctx, ownerB, jobID, "thread-a", "call-b"); claimed || !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign claim = %v, %v; want a missing job", claimed, err)
	}
	for owner, want := range map[string]int{ownerA: 0, ownerB: 0} {
		jobs, err := store.Recoverable(ctx, owner)
		if err != nil || len(jobs) != want {
			t.Fatalf("Recoverable = %d jobs (%v), want %d: a delivered job is done", len(jobs), err, want)
		}
	}
}

// TestMediaJobRLSFailsClosedWithoutIdentity drives raw statements, so only the policies can be
// what refuses them.
func TestMediaJobRLSFailsClosedWithoutIdentity(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	owner := seedMediaIdentity(t, pool)
	job := seedJob(t, pool, owner, "in_progress", "", 0, false)

	fresh, err := pgx.Connect(ctx, os.Getenv("AURA_DB_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.Close(ctx) })
	var setting *string
	if err := fresh.QueryRow(ctx, `SELECT current_setting('app.current_identity', true)`).Scan(&setting); err != nil {
		t.Fatal(err)
	}
	if setting != nil {
		t.Fatalf("fresh connection carries app.current_identity = %q, want it absent", *setting)
	}

	type session struct {
		exec  func(sql string, args ...any) (pgconn.CommandTag, error)
		count func() (int, error)
	}
	absent := session{
		exec: func(sql string, args ...any) (pgconn.CommandTag, error) { return fresh.Exec(ctx, sql, args...) },
		count: func() (n int, err error) {
			return n, fresh.QueryRow(ctx, `SELECT count(*) FROM aura.media_job`).Scan(&n)
		},
	}
	inEmptyIdentity := func(run func(pgx.Tx) error) error { return db.WithIdentityTxRaw(ctx, pool, "", run) }
	empty := session{
		exec: func(sql string, args ...any) (tag pgconn.CommandTag, err error) {
			err = inEmptyIdentity(func(tx pgx.Tx) error { tag, err = tx.Exec(ctx, sql, args...); return err })
			return tag, err
		},
		count: func() (n int, err error) {
			return n, inEmptyIdentity(func(tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM aura.media_job`).Scan(&n)
			})
		},
	}
	for name, s := range map[string]session{"absent identity": absent, "empty identity": empty} {
		t.Run(name, func(t *testing.T) {
			if n, err := s.count(); err != nil || n != 0 {
				t.Fatalf("SELECT sees %d rows (%v), want 0", n, err)
			}
			tag, err := s.exec(`UPDATE aura.media_job SET model = 'hijacked' WHERE id = $1`, job.ID)
			if err != nil || tag.RowsAffected() != 0 {
				t.Fatalf("UPDATE affected %d rows (%v), want 0", tag.RowsAffected(), err)
			}
			_, err = s.exec(`
INSERT INTO aura.media_job (identity_id, conversation_id, tool_call_id, provider_job_id, model, request, status)
VALUES ($1, 'thread-a', 'call', $2, 'm', '{}', 'pending')`, owner, "vid_"+uuid.NewString())
			if sqlState(err) != "42501" {
				t.Fatalf("INSERT err = %v, want SQLSTATE 42501", err)
			}
		})
	}
	got, err := NewStore(pool).Get(ctx, owner, job.ID)
	if err != nil || got.Model != "minimax/hailuo-3-max" {
		t.Fatalf("owner reads model %q (%v), want the row intact", got.Model, err)
	}
}

func TestMediaJobChecksRejectInconsistentRows(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	owner := seedMediaIdentity(t, pool)
	assetID := seedAsset(t, pool, owner, acceptedVideo)
	cases := map[string]struct {
		status, asset, cost string
		delivered           bool
	}{
		"unknown status":             {status: "running", cost: "0"},
		"completed without an asset": {status: "completed", cost: "0"},
		"delivered before completed": {status: "in_progress", asset: assetID, cost: "0", delivered: true},
		"negative cost":              {status: "failed", cost: "-0.01"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
INSERT INTO aura.media_job (identity_id, conversation_id, tool_call_id, provider_job_id, model, request,
                            status, asset_id, cost_usd, delivered_at)
VALUES ($1, 'thread-a', 'call', $2, 'm', '{}', $3, NULLIF($4, '')::uuid, $5::numeric, CASE WHEN $6 THEN now() END)`,
					owner, "vid_"+uuid.NewString(), tc.status, tc.asset, tc.cost, tc.delivered)
				return err
			})
			if sqlState(err) != "23514" {
				t.Fatalf("err = %v, want SQLSTATE 23514 (check_violation)", err)
			}
		})
	}
	if _, err := NewStore(pool).Progress(ctx, owner, seedJob(t, pool, owner, "pending", "", 0, false).ID,
		StatusInProgress, func() *float64 { v := -1.0; return &v }(), nil); sqlState(err) != "23514" {
		t.Fatalf("Progress with a negative cost err = %v, want the CHECK to refuse it", err)
	}
}

func TestMediaJobInsertRefusesDuplicateProviderID(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	ownerA, ownerB := seedMediaIdentity(t, pool), seedMediaIdentity(t, pool)
	first := newJob(ownerA)
	inserted, err := store.Insert(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if inserted.ID == "" || inserted.Status != StatusPending || inserted.CreatedAt.IsZero() || inserted.CostUSD != nil {
		t.Fatalf("inserted = %#v", inserted)
	}
	audit, err := inserted.Audit()
	if err != nil || audit.Origin != "https://openrouter.ai/api/v1" {
		t.Fatalf("request read back = %s (%v)", inserted.Request, err)
	}
	for owner, label := range map[string]string{ownerA: "same owner", ownerB: "another owner"} {
		again := newJob(owner)
		again.ProviderJobID = first.ProviderJobID
		if _, err := store.Insert(ctx, again); !db.IsUniqueViolation(err) {
			t.Fatalf("%s: second insert of provider job %s err = %v, want 23505", label, first.ProviderJobID, err)
		}
	}
	jobs, err := store.Recoverable(ctx, ownerA)
	if err != nil || len(jobs) != 1 || jobs[0].ID != inserted.ID {
		t.Fatalf("Recoverable = %#v (%v), want exactly the first job", jobs, err)
	}
}

func TestMediaJobProgressRecordsCostAndTerminalState(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner, stranger := seedMediaIdentity(t, pool), seedMediaIdentity(t, pool)
	job, err := store.Insert(ctx, newJob(owner))
	if err != nil {
		t.Fatal(err)
	}
	zero, charged := 0.0, 0.2475

	absent, err := store.Progress(ctx, owner, job.ID, StatusInProgress, nil, nil)
	if err != nil || absent.CostUSD != nil || absent.Status != StatusInProgress || absent.CompletedAt != nil {
		t.Fatalf("absent cost = %#v (%v), want NULL cost and no terminal time", absent, err)
	}
	free, err := store.Progress(ctx, owner, job.ID, StatusInProgress, &zero, nil)
	if err != nil || free.CostUSD == nil || *free.CostUSD != 0 {
		t.Fatalf("zero cost = %v (%v), want an explicit 0", free.CostUSD, err)
	}
	kept, err := store.Progress(ctx, owner, job.ID, StatusInProgress, nil, nil)
	if err != nil || kept.CostUSD == nil || *kept.CostUSD != 0 {
		t.Fatalf("a later absent cost erased the recorded 0: %v (%v)", kept.CostUSD, err)
	}
	failure := &Error{Code: "job_failed", Message: "Video generation failed."}
	failed, err := store.Progress(ctx, owner, job.ID, StatusFailed, &charged, failure)
	if err != nil || failed.Status != StatusFailed || failed.CompletedAt == nil ||
		failed.CostUSD == nil || *failed.CostUSD != charged || failed.Error == nil || *failed.Error != *failure {
		t.Fatalf("failed = %#v (%v)", failed, err)
	}
	if !failed.CreatedAt.Equal(job.CreatedAt) || failed.UpdatedAt.Before(job.UpdatedAt) {
		t.Fatalf("created_at %v -> %v, updated_at %v -> %v", job.CreatedAt, failed.CreatedAt, job.UpdatedAt, failed.UpdatedAt)
	}

	if _, err := store.Progress(ctx, owner, job.ID, StatusInProgress, nil, nil); !errors.Is(err, ErrJobNotActive) {
		t.Fatalf("Progress on a failed job err = %v, want ErrJobNotActive", err)
	}
	if _, err := store.Complete(ctx, owner, job.ID, seedAsset(t, pool, owner, acceptedVideo), nil); !errors.Is(err, ErrJobNotActive) {
		t.Fatalf("Complete on a failed job err = %v, want ErrJobNotActive", err)
	}
	if _, err := store.Progress(ctx, stranger, job.ID, StatusExpired, nil, nil); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("foreign Progress err = %v, want pgx.ErrNoRows", err)
	}
	if got, _ := store.Get(ctx, owner, job.ID); got.Status != StatusFailed {
		t.Fatalf("a refused update changed the job to %q", got.Status)
	}
}

func TestMediaJobCompleteRequiresOwnedAcceptedVideoAsset(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner, stranger := seedMediaIdentity(t, pool), seedMediaIdentity(t, pool)
	job, err := store.Insert(ctx, newJob(owner))
	if err != nil {
		t.Fatal(err)
	}
	refused := map[string]string{
		"foreign asset":          seedAsset(t, pool, stranger, acceptedVideo),
		"another conversation":   seedAsset(t, pool, owner, seededAsset{thread: "thread-b", modality: "video", status: "accepted"}),
		"image asset":            seedAsset(t, pool, owner, seededAsset{thread: "thread-a", modality: "image", status: "accepted"}),
		"asset not yet ingested": seedAsset(t, pool, owner, seededAsset{thread: "thread-a", modality: "video", status: "uploaded"}),
		"deleted asset":          seedAsset(t, pool, owner, seededAsset{thread: "thread-a", modality: "video", status: "accepted", deleted: true}),
	}
	for name, assetID := range refused {
		if _, err := store.Complete(ctx, owner, job.ID, assetID, nil); ErrorCode(err) != "asset_not_found" {
			t.Errorf("Complete with %s err = %v, want asset_not_found", name, err)
		}
	}
	if got, _ := store.Get(ctx, owner, job.ID); got.Status != StatusPending || got.AssetID != "" {
		t.Fatalf("a refused Complete changed the job: %#v", got)
	}

	assetID := seedAsset(t, pool, owner, acceptedVideo)
	charged := 0.2475
	done, err := store.Complete(ctx, owner, job.ID, assetID, &charged)
	if err != nil || done.Status != StatusCompleted || done.AssetID != assetID || done.CompletedAt == nil ||
		done.DeliveredAt != nil || done.CostUSD == nil || *done.CostUSD != charged {
		t.Fatalf("completed = %#v (%v)", done, err)
	}
	jobs, err := store.Recoverable(ctx, owner)
	if err != nil || len(jobs) != 1 || jobs[0].ID != job.ID {
		t.Fatalf("a completed, undelivered job must stay recoverable: %#v (%v)", jobs, err)
	}
}

func TestMediaJobClaimRollsBackWhenTheAssetCannotBeBound(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner, stranger := seedMediaIdentity(t, pool), seedMediaIdentity(t, pool)

	running := seedJob(t, pool, owner, "in_progress", "", 0, false)
	job, claimed, err := store.ClaimDelivery(ctx, owner, running.ID, "thread-a", "call-collect")
	if err != nil || claimed || job.Status != StatusInProgress {
		t.Fatalf("claim on a running job = %v, %q, %v; want the owned row, unclaimed", claimed, job.Status, err)
	}

	assetID := seedAsset(t, pool, owner, acceptedVideo)
	completed := seedJob(t, pool, owner, "completed", assetID, 0, false)
	if _, claimed, err := store.ClaimDelivery(ctx, owner, completed.ID, "thread-b", "call-collect"); claimed || !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claim from another conversation = %v, %v; want a missing job", claimed, err)
	}
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE aura.assets SET deleted_at = now() WHERE id = $1`, assetID)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	foreignAsset := seedAsset(t, pool, stranger, acceptedVideo)
	pointsAway := seedJob(t, pool, owner, "completed", foreignAsset, 0, false)
	for name, jobID := range map[string]string{"deleted asset": completed.ID, "foreign asset": pointsAway.ID} {
		_, claimed, err := store.ClaimDelivery(ctx, owner, jobID, "thread-a", "call-collect")
		if claimed || ErrorCode(err) != "asset_not_found" {
			t.Fatalf("%s: claim = %v, %v; want asset_not_found", name, claimed, err)
		}
		after, err := store.Get(ctx, owner, jobID)
		if err != nil || after.DeliveredAt != nil {
			t.Fatalf("%s: delivered_at = %v (%v), want the claim rolled back", name, after.DeliveredAt, err)
		}
	}
	if got := assetToolCall(t, pool, stranger, foreignAsset); got != "" {
		t.Fatalf("a foreign asset was bound to call %q", got)
	}
}

func TestMediaJobRecoverableOrdersByCreationAndRetainsIt(t *testing.T) {
	pool := migratedMediaJobPool(t)
	ctx := context.Background()
	store := NewStore(pool)
	owner := seedMediaIdentity(t, pool)
	assetID := seedAsset(t, pool, owner, acceptedVideo)

	newest := seedJob(t, pool, owner, "pending", "", 10*time.Minute, false)
	oldest := seedJob(t, pool, owner, "in_progress", "", 30*time.Minute, false)
	middle := seedJob(t, pool, owner, "completed", assetID, 20*time.Minute, false)
	seedJob(t, pool, owner, "failed", "", 40*time.Minute, false)
	seedJob(t, pool, owner, "completed", seedAsset(t, pool, owner, acceptedVideo), 50*time.Minute, true)
	tie := seedJob(t, pool, owner, "pending", "", 0, false)
	if err := db.WithIdentityTxRaw(ctx, pool, owner, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE aura.media_job SET created_at = $2 WHERE id = $1`, tie.ID, oldest.CreatedAt)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	tied := []string{oldest.ID, tie.ID}
	sort.Strings(tied)
	want := append(tied, middle.ID, newest.ID)
	jobs, err := store.Recoverable(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(jobs))
	for _, job := range jobs {
		got = append(got, job.ID)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("recovery order = %v, want %v (created_at, then id)", got, want)
	}

	resumed, err := store.Progress(ctx, owner, oldest.ID, StatusInProgress, nil, nil)
	if err != nil || !resumed.CreatedAt.Equal(oldest.CreatedAt) {
		t.Fatalf("created_at %v after progress (%v), want the original %v", resumed.CreatedAt, err, oldest.CreatedAt)
	}
	if time.Since(resumed.CreatedAt) < 29*time.Minute {
		t.Fatalf("created_at %v lost the job's real age", resumed.CreatedAt)
	}
}
