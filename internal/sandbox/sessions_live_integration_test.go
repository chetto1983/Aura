//go:build sandbox_integration && db_integration

// Live session-persistence + TTL-reap integration tier for the SessionManager
// (ROADMAP CAP-02 criteria 1a/1b/1c + 3). Unlike the db_integration-only boot
// recovery tier (sessions_integration_test.go), these tests drive a REAL
// per-conversation container through a live SessionManager (real dockerCLI + real
// sqlc store) AND the live sidecar exec path (RunPythonSession/RunShellSession over
// AURA_SANDBOX_URL). They therefore require BOTH tags and the full live stack:
//
//	make db-up && make sandbox-up && aura db migrate
//	AURA_SANDBOX_URL + AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set
//	go test -race -tags 'sandbox_integration db_integration' ./internal/sandbox/
//
// No-skip-as-green (CLAUDE.md): the helpers reused here (sidecarURL from
// docker_integration_test.go, migratedPool/envOrSkip from sessions_integration_test.go)
// each t.Fatal under $CI when their env is unset, so a skipped tier never reports
// falsely green. A sub-second runtime is a skip tell — these tests spin a real
// container and exec into it, so a genuine run is multi-second.
package sandbox

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// liveManager builds a SessionManager over the production dockerCLI + the migrated
// sqlc store, with the resolved per-arch runtime (sidecar env). It registers Close
// cleanup so the reaper goroutine is goleak-clean. allowHosts/egressNet/proxyEnv let
// the network test opt into the egress posture; the persistence/TTL tests pass the
// egressless defaults (empty allowlist keeps the 2a posture).
func liveManager(t *testing.T, pool *pgxpool.Pool, ttl, reapEvery time.Duration, allowHosts, egressNet string, proxyEnv []string) (*SessionManager, *sqlc.Queries) {
	t.Helper()
	q := sqlc.New(pool)
	mctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	m := NewSessionManager(mctx, SessionDeps{
		Docker:         NewDockerCLI(),
		Store:          q,
		MaxN:           5,
		TTL:            ttl,
		Runtime:        sessionRuntime(t),
		Image:          sessionImage(t),
		SeccompProfile: sessionSeccomp(t),
		AllowHosts:     allowHosts,
		EgressNetwork:  egressNet,
		ProxyEnv:       proxyEnv,
		ReapEvery:      reapEvery,
	})
	t.Cleanup(m.Close)
	return m, q
}

// liveSessionExec acquires the conversation's live container, execs code in its
// persistent namespace via the sidecar, then releases the per-session lock. It is
// the single live exec helper the persistence tests use so the Acquire/Release
// discipline (D-07) is uniform.
func liveSessionExec(t *testing.T, m *SessionManager, runner *DockerRunner, convID, code string) Result {
	t.Helper()
	ctx := ctxT(t)
	sess, err := m.Acquire(ctx, convID)
	if err != nil {
		t.Fatalf("Acquire(%s): %v", convID, err)
	}
	defer m.Release(sess)
	res, err := runner.RunPythonSession(ctx, convID, code, 20)
	if err != nil {
		t.Fatalf("RunPythonSession(%s): %v", convID, err)
	}
	return res
}

// TestSessions_PythonStatePersists is ROADMAP CAP-02 criterion 1a: x=42 set in
// exec call 1 is readable in call 2 in the SAME conversation (the sidecar's
// per-session namespace dict outlives the call — D-01). A fresh conversation must
// NOT see the variable (cross-conversation isolation, T-08-05-INFO-XCONV).
func TestSessions_PythonStatePersists(t *testing.T) {
	pool := migratedPool(t)
	runner := integrationRunner(t)
	m, _ := liveManager(t, pool, 1800*time.Second, time.Hour, "", "", nil)

	convID := newConversationRow(t, pool).String()

	if res := liveSessionExec(t, m, runner, convID, "x = 42"); res.ExitCode != 0 {
		t.Fatalf("set x=42: exit %d stderr=%q", res.ExitCode, res.Stderr)
	}
	res := liveSessionExec(t, m, runner, convID, "print(x)")
	if res.ExitCode != 0 {
		t.Fatalf("print(x): exit %d stderr=%q", res.ExitCode, res.Stderr)
	}
	if strings.TrimSpace(res.Stdout) != "42" {
		t.Fatalf("persisted x must read 42 in call 2, got stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}

	// Cross-conversation isolation: a different conversation must NOT see x.
	other := newConversationRow(t, pool).String()
	res = liveSessionExec(t, m, runner, other, "print('x' in dir())")
	if strings.TrimSpace(res.Stdout) != "False" {
		t.Fatalf("conversation %s leaked x from %s: stdout=%q", other, convID, res.Stdout)
	}
}

// TestSessions_WorkspacePersists is ROADMAP CAP-02 criterion 1b: a file written to
// the bind-mounted /workspace in call 1 is visible in call 2 (the workspace dir
// outlives the call — it is the host-mounted per-conversation tree, D-04).
func TestSessions_WorkspacePersists(t *testing.T) {
	pool := migratedPool(t)
	runner := integrationRunner(t)
	m, _ := liveManager(t, pool, 1800*time.Second, time.Hour, "", "", nil)

	convID := newConversationRow(t, pool).String()
	marker := "persist-" + uuid.NewString()

	write := liveSessionExec(t, m, runner,
		convID, "open('/workspace/marker.txt','w').write('"+marker+"')")
	if write.ExitCode != 0 {
		t.Fatalf("write marker: exit %d stderr=%q", write.ExitCode, write.Stderr)
	}
	read := liveSessionExec(t, m, runner,
		convID, "print(open('/workspace/marker.txt').read())")
	if read.ExitCode != 0 {
		t.Fatalf("read marker: exit %d stderr=%q", read.ExitCode, read.Stderr)
	}
	if strings.TrimSpace(read.Stdout) != marker {
		t.Fatalf("workspace file must persist across calls, want %q got %q", marker, read.Stdout)
	}
}

// TestSessions_ConcurrentSerialized_Live is ROADMAP CAP-02 criterion 1c (live): two
// goroutines driving the SAME conversation serialize via the per-session lock (D-07),
// so no data race fires under -race and both execs land on the one live container. Run
// under `-race`; a missing lock would surface as a race on the session map / a double
// container-create. (The deterministic synctest leg is the unit-tier
// TestSessions_ConcurrentSerialized in sessions_test.go.)
func TestSessions_ConcurrentSerialized_Live(t *testing.T) {
	pool := migratedPool(t)
	runner := integrationRunner(t)
	m, _ := liveManager(t, pool, 1800*time.Second, time.Hour, "", "", nil)

	convID := newConversationRow(t, pool).String()
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			ctx := ctxT(t)
			sess, err := m.Acquire(ctx, convID)
			if err != nil {
				errs <- err
				return
			}
			defer m.Release(sess)
			if _, err := runner.RunPythonSession(ctx, convID, "x = 1", 20); err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent same-session exec failed: %v", err)
		}
	}
	// Exactly one container was created for the conversation (the cap counts one).
	if got := m.liveCount(); got != 1 {
		t.Fatalf("same-conversation concurrency must create ONE container, liveCount=%d", got)
	}
}

// TestReaper_LiveContainerRemoved is ROADMAP CAP-02 criterion 3 (live): after the
// idle TTL elapses with no activity, the reaper stops + removes the real container,
// marks the registry row terminated, and drops the live count to 0. It uses a tiny
// real TTL + fast reap tick (NOT synctest — this is the live-container leg; the
// deterministic virtual-clock leg is the unit-tier TestReaper_EvictsAfterTTL).
func TestReaper_LiveContainerRemoved(t *testing.T) {
	pool := migratedPool(t)
	m, q := liveManager(t, pool, 2*time.Second, 500*time.Millisecond, "", "", nil)

	convID := newConversationRow(t, pool).String()
	ctx := ctxT(t)
	sess, err := m.Acquire(ctx, convID)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	m.Release(sess) // release so the reaper can TryLock + evict
	if m.liveCount() != 1 {
		t.Fatalf("want 1 live session after Acquire, got %d", m.liveCount())
	}

	// Wait past TTL + a couple of reap ticks for the live docker rm to complete.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if m.liveCount() == 0 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if m.liveCount() != 0 {
		t.Fatalf("idle session must be reaped after TTL, liveCount=%d", m.liveCount())
	}
	// The registry must no longer list it active (MarkTerminated ran).
	active, err := q.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, a := range active {
		if a.ConversationID.String() == convID {
			t.Fatalf("reaped session %s still active in registry", convID)
		}
	}
}

// TestSessions_BootRecovery_LazyRecreate is the boot-recovery + lazy-recreate leg of
// CAP-02 (D-06): after RecoverOnBoot marks a prior 'active' row terminated, the next
// Acquire transparently recreates a fresh live container (the session is usable
// again without an operator step).
func TestSessions_BootRecovery_LazyRecreate(t *testing.T) {
	pool := migratedPool(t)
	runner := integrationRunner(t)
	m, q := liveManager(t, pool, 1800*time.Second, time.Hour, "", "", nil)
	ctx := ctxT(t)

	convID := newConversationRow(t, pool).String()
	sess, err := m.Acquire(ctx, convID)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	m.Release(sess)

	if err := m.RecoverOnBoot(ctx); err != nil {
		t.Fatalf("RecoverOnBoot: %v", err)
	}

	// Lazy recreate: Acquire after recovery rebuilds the container + a fresh row.
	res := liveSessionExec(t, m, runner, convID, "print(2+2)")
	if strings.TrimSpace(res.Stdout) != "4" {
		t.Fatalf("lazy-recreated session must exec, got stdout=%q stderr=%q", res.Stdout, res.Stderr)
	}
	active, err := q.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	found := false
	for _, a := range active {
		if a.ConversationID.String() == convID {
			found = true
		}
	}
	if !found {
		t.Fatalf("lazy-recreate must register a fresh active row for %s", convID)
	}
}
