//go:build db_integration

package conversations

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

type workerCommand struct {
	Action, DSN, OperationID, ConversationID, BranchID, IdempotencyKey, OwnerID string
	BaseGeneration, CapturedWatermark                                           int
	GovernanceWatermark                                                         int64
	Priority                                                                    ClaimPriority
	LeaseUntil                                                                  time.Time
}
type workerEvent struct{ State, OperationID, OwnerID, Error string }

func compactionDB(t *testing.T) (*pgxpool.Pool, string, string) {
	t.Helper()
	_ = godotenv.Load(filepath.Join("..", "..", ".env"))
	pwd := os.Getenv("POSTGRES_PASSWORD")
	if pwd == "" {
		t.Fatal("POSTGRES_PASSWORD is required for db_integration")
	}
	host := os.Getenv("POSTGRES_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("POSTGRES_PORT")
	if port == "" {
		port = "5432"
	}
	name := os.Getenv("POSTGRES_DB")
	if name == "" {
		name = "aura"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/%s?sslmode=disable", pwd, host, port, name)
	if err := db.EnsureRoles(t.Context(), bootstrap, pwd); err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("postgres://aura_migrate:%s@%s:%s/%s?sslmode=disable", pwd, host, port, name)
	if _, err := db.Migrate(t.Context(), dsn); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	identity, conv := uuid.New(), uuid.New()
	if _, err = pool.Exec(t.Context(), `INSERT INTO aura.identities(id,name,kind) VALUES($1,$2,'user')`, identity, "compaction-"+identity.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(t.Context(), `INSERT INTO aura.conversations(id,identity_id) VALUES($1,$2)`, conv, identity); err != nil {
		t.Fatal(err)
	}
	return pool, dsn, conv.String()
}

func runWorker(t *testing.T, c workerCommand) workerEvent {
	t.Helper()
	cmd := exec.Command("go", "run", "../../cmd/compaction-test-worker")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(in).Encode(c); err != nil {
		t.Fatal(err)
	}
	_ = in.Close()
	var got workerEvent
	if err = json.NewDecoder(bufio.NewReader(out)).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	return got
}
func baseWorker(dsn, conv, op, owner string, lease time.Time) workerCommand {
	return workerCommand{Action: "claim", DSN: dsn, OperationID: op, ConversationID: conv, BranchID: "main", IdempotencyKey: op, OwnerID: owner, CapturedWatermark: 1, Priority: ClaimPriorityAutomatic, LeaseUntil: lease}
}

func TestCompactionMultiProcessDuplicate(t *testing.T) {
	_, dsn, conv := compactionDB(t)
	op := uuid.NewString()
	a := runWorker(t, baseWorker(dsn, conv, op, "a", time.Now().Add(time.Minute)))
	b := runWorker(t, baseWorker(dsn, conv, op, "b", time.Now().Add(time.Minute)))
	if a.Error != "" || b.Error != "" || a.OperationID != b.OperationID || b.OwnerID != "a" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}
func TestCompactionMultiProcessLeaseExpiry(t *testing.T) {
	_, dsn, conv := compactionDB(t)
	op := uuid.NewString()
	a := runWorker(t, baseWorker(dsn, conv, op, "dead", time.Now().Add(-time.Second)))
	b := runWorker(t, baseWorker(dsn, conv, op, "takeover", time.Now().Add(time.Minute)))
	if a.Error != "" || b.Error != "" || b.OwnerID != "takeover" {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
}
func TestCompactionMultiProcessProcessDeath(t *testing.T) {
	_, dsn, conv := compactionDB(t)
	op := uuid.NewString()
	cmd := exec.Command("go", "run", "../../cmd/compaction-test-worker")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err = json.NewEncoder(in).Encode(baseWorker(dsn, conv, op, "killed", time.Now().Add(500*time.Millisecond))); err != nil {
		t.Fatal(err)
	}
	var got workerEvent
	if err = json.NewDecoder(bufio.NewReader(out)).Decode(&got); err != nil || got.Error != "" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	time.Sleep(600 * time.Millisecond)
	takeover := runWorker(t, baseWorker(dsn, conv, op, "recovery", time.Now().Add(time.Minute)))
	if takeover.Error != "" || takeover.OwnerID != "recovery" {
		t.Fatalf("%+v", takeover)
	}
}
func TestCompactionMultiProcessStaleCompletion(t *testing.T) {
	pool, dsn, conv := compactionDB(t)
	op := uuid.NewString()
	_ = runWorker(t, baseWorker(dsn, conv, op, "old", time.Now().Add(-time.Second)))
	if got := runWorker(t, baseWorker(dsn, conv, op, "new", time.Now().Add(time.Minute))); got.Error != "" {
		t.Fatal(got.Error)
	}
	store := New(pool, Config{})
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1, Content: "x"}})
	p := finalizeFixture(op, "old", m)
	if _, err := store.FinalizeCompaction(t.Context(), p); !errors.Is(err, ErrCompactionSuperseded) {
		t.Fatalf("stale err=%v", err)
	}
	p.OwnerID = "new"
	if _, err := store.FinalizeCompaction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
}
func TestCompactionMultiProcessRestoreRace(t *testing.T) {
	pool, dsn, conv := compactionDB(t)
	store := New(pool, Config{})
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1}})
	op1 := uuid.NewString()
	if got := runWorker(t, baseWorker(dsn, conv, op1, "one", time.Now().Add(time.Minute))); got.Error != "" {
		t.Fatal(got.Error)
	}
	p1 := finalizeFixture(op1, "one", m)
	cp1, err := store.FinalizeCompaction(t.Context(), p1)
	if err != nil {
		t.Fatal(err)
	}
	op2 := uuid.NewString()
	c2 := baseWorker(dsn, conv, op2, "two", time.Now().Add(time.Minute))
	c2.BaseGeneration = 1
	if got := runWorker(t, c2); got.Error != "" {
		t.Fatal(got.Error)
	}
	p2 := finalizeFixture(op2, "two", m)
	pool2, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool2.Close()
	errs := make(chan error, 2)
	go func() {
		restoreStore := New(pool2, Config{})
		e := restoreStore.RestoreCompaction(t.Context(), conv, "main", cp1, uuid.NewString(), "operator")
		if isSerializationFailure(e) {
			e = restoreStore.RestoreCompaction(t.Context(), conv, "main", cp1, uuid.NewString(), "operator")
		}
		errs <- e
	}()
	go func() { _, e := store.FinalizeCompaction(t.Context(), p2); errs <- e }()
	for range 2 {
		e := <-errs
		if e != nil && !errors.Is(e, ErrCompactionSuperseded) && !isSerializationFailure(e) {
			t.Fatal(e)
		}
	}
	var pointers, audits int
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM aura.compaction_active_pointers WHERE conversation_id=$1`, conv).Scan(&pointers); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(t.Context(), `SELECT count(*) FROM aura.compaction_restore_events WHERE conversation_id=$1`, conv).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if pointers != 1 || audits != 1 {
		t.Fatalf("pointers=%d audits=%d", pointers, audits)
	}
}

func isSerializationFailure(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40001"
}
func TestCompactionMultiProcessGovernanceAfterCaptureInvalidatesFinalize(t *testing.T) {
	pool, dsn, conv := compactionDB(t)
	op := uuid.NewString()
	c := baseWorker(dsn, conv, op, "owner", time.Now().Add(time.Minute))
	c.GovernanceWatermark = 3
	if got := runWorker(t, c); got.Error != "" {
		t.Fatal(got.Error)
	}
	m := NewCompactionManifest(1, []int{1}, nil, nil, nil, []ManifestTurn{{Seq: 1}})
	p := finalizeFixture(op, "owner", m)
	p.CurrentGovernanceWatermark = 4
	if _, err := New(pool, Config{}).FinalizeCompaction(t.Context(), p); !errors.Is(err, ErrCompactionSuperseded) {
		t.Fatalf("got %v", err)
	}
}
func TestCompactionMultiProcessManualOutranksUnstartedAuto(t *testing.T) {
	_, dsn, conv := compactionDB(t)
	auto := baseWorker(dsn, conv, uuid.NewString(), "auto", time.Now().Add(time.Minute))
	if got := runWorker(t, auto); got.Error != "" {
		t.Fatal(got.Error)
	}
	manual := baseWorker(dsn, conv, uuid.NewString(), "manual", time.Now().Add(time.Minute))
	manual.IdempotencyKey = manual.OperationID
	manual.Priority = ClaimPriorityManual
	if got := runWorker(t, manual); got.Error != "" || got.OwnerID != "manual" {
		t.Fatalf("%+v", got)
	}
}
func TestCompactionMultiProcessManualDoesNotPreemptActiveInference(t *testing.T) {
	_, dsn, conv := compactionDB(t)
	auto := baseWorker(dsn, conv, uuid.NewString(), "auto", time.Now().Add(time.Minute))
	if got := runWorker(t, auto); got.Error != "" {
		t.Fatal(got.Error)
	}
	start := auto
	start.Action = "start"
	if got := runWorker(t, start); got.Error != "" {
		t.Fatal(got.Error)
	}
	manual := baseWorker(dsn, conv, uuid.NewString(), "manual", time.Now().Add(time.Minute))
	manual.Priority = ClaimPriorityManual
	if got := runWorker(t, manual); !strings.Contains(got.Error, "in progress") {
		t.Fatalf("%+v", got)
	}
}
func TestMigration0036(t *testing.T) {
	pool, dsn, conv := compactionDB(t)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_schema='aura' AND table_name='compaction_checkpoints')`).Scan(&exists); err != nil || !exists {
		t.Fatalf("exists=%v err=%v conv=%s", exists, err, conv)
	}
	pool.Close()
	if err := db.MigrateSteps(t.Context(), dsn, -1); err != nil {
		t.Fatal(err)
	}
	check, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	var old string
	if err = check.QueryRow(t.Context(), `SELECT id::text FROM aura.conversations WHERE id=$1`, conv).Scan(&old); err != nil {
		t.Fatalf("old row unreadable after down: %v", err)
	}
	check.Close()
	if err = db.MigrateSteps(t.Context(), dsn, 1); err != nil {
		t.Fatal(err)
	}
}

func finalizeFixture(op, owner string, m VersionedCompactionManifest) FinalizeCompactionParams {
	return FinalizeCompactionParams{OperationID: op, OwnerID: owner, CheckpointID: uuid.NewString(), CapturedWatermarkSeq: m.CapturedWatermarkSeq, SummarizedTurnSeqs: m.SummarizedTurnSeqs, TailTurnSeqs: m.TailTurnSeqs, ProtectedTurnSeqs: m.ProtectedTurnSeqs, ExcludedTurnSeqs: m.ExcludedTurnSeqs, ManifestDigest: m.Digest, CompleteCaptureDigest: m.CompleteCaptureDigest, DigestAlgorithm: m.DigestAlgorithm, DigestVersion: m.DigestVersion, StructuredSummary: json.RawMessage(`{"summary":"ok"}`), SchemaVersion: 1, PromptVersion: 1, ProjectionVersion: 1, QualityState: "valid", RolloutMode: "shadow", RetentionUntil: time.Now().Add(time.Hour)}
}
