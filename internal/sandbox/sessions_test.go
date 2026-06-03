// Unit tier for the SessionManager control plane. Shares the package-level goleak
// TestMain declared in docker_test.go (//go:build !sandbox_integration) — this
// file MUST NOT declare its own TestMain. It injects fake docker + store
// dependencies so the cap/reaper/serialization invariants run with no Docker
// daemon and no Postgres (the live boot-recovery tier lives in
// sessions_integration_test.go behind db_integration).
package sandbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeDocker records lifecycle calls and never shells out — unit tests assert the
// argv shape (runtime + hardening flags) without a daemon.
type fakeDocker struct {
	mu      sync.Mutex
	runArgv []string
	runErr  error
	stopped []string
	removed []string
	started atomic.Int64
}

func (f *fakeDocker) run(_ context.Context, name string, argv []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runErr != nil {
		return "", f.runErr
	}
	f.runArgv = argv
	f.started.Add(1)
	return name, nil
}

func (f *fakeDocker) stop(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *fakeDocker) remove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeDocker) listStray(_ context.Context) ([]string, error) { return nil, nil }

// fakeStore satisfies sessionStore with in-memory rows; it never touches Postgres.
type fakeStore struct {
	mu         sync.Mutex
	inserted   int
	terminated []pgtype.UUID
	active     []sqlc.AuraSandboxSessions
	insertErr  error
}

func (s *fakeStore) InsertSession(_ context.Context, arg sqlc.InsertSessionParams) (sqlc.AuraSandboxSessions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertErr != nil {
		return sqlc.AuraSandboxSessions{}, s.insertErr
	}
	s.inserted++
	id := pgtype.UUID{Bytes: [16]byte{byte(s.inserted)}, Valid: true}
	return sqlc.AuraSandboxSessions{ID: id, ConversationID: arg.ConversationID, ContainerID: arg.ContainerID, Status: "active"}, nil
}

func (s *fakeStore) TouchLastUsed(_ context.Context, _ pgtype.UUID) error { return nil }

func (s *fakeStore) MarkTerminated(_ context.Context, id pgtype.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminated = append(s.terminated, id)
	return nil
}

func (s *fakeStore) ListActive(_ context.Context) ([]sqlc.AuraSandboxSessions, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, nil
}

func (s *fakeStore) terminatedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.terminated)
}

// newConvID returns a distinct canonical UUID string for index i.
func newConvID(i int) string {
	return pgtype.UUID{Bytes: [16]byte{0xab, byte(i)}, Valid: true}.String()
}

func testManager(t *testing.T, dk dockerClient, st sessionStore, opts func(*SessionManager)) (*SessionManager, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	m := NewSessionManager(ctx, SessionDeps{
		Docker:  dk,
		Store:   st,
		MaxN:    5,
		TTL:     1800 * time.Second,
		Runtime: "runsc",
		Image:   "aura-sandbox:latest",
	})
	if opts != nil {
		opts(m)
	}
	t.Cleanup(func() {
		m.Close()
		cancel()
	})
	return m, cancel
}

func TestSessions_HardCapEnforced(t *testing.T) {
	dk := &fakeDocker{}
	st := &fakeStore{}
	m, _ := testManager(t, dk, st, nil)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		s, err := m.Acquire(ctx, newConvID(i))
		if err != nil {
			t.Fatalf("Acquire %d: %v", i, err)
		}
		m.Release(s)
	}
	if _, err := m.Acquire(ctx, newConvID(99)); !errors.Is(err, ErrSessionCapReached) {
		t.Fatalf("6th distinct Acquire: want ErrSessionCapReached, got %v", err)
	}
	if dk.started.Load() != 5 {
		t.Fatalf("docker run called %d times, want 5 (no LRU eviction)", dk.started.Load())
	}
}

func TestSessions_ReuseSameConversation(t *testing.T) {
	dk := &fakeDocker{}
	m, _ := testManager(t, dk, &fakeStore{}, nil)
	ctx := context.Background()

	conv := newConvID(1)
	s1, err := m.Acquire(ctx, conv)
	if err != nil {
		t.Fatalf("Acquire 1: %v", err)
	}
	m.Release(s1)
	s2, err := m.Acquire(ctx, conv)
	if err != nil {
		t.Fatalf("Acquire 2: %v", err)
	}
	m.Release(s2)
	if dk.started.Load() != 1 {
		t.Fatalf("docker run called %d times, want 1 (reuse, not recreate)", dk.started.Load())
	}
}

func TestSessions_ConcurrentSerialized(t *testing.T) {
	m, _ := testManager(t, &fakeDocker{}, &fakeStore{}, nil)
	ctx := context.Background()
	conv := newConvID(7)

	var inCrit atomic.Int32
	var maxConc atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := m.Acquire(ctx, conv)
			if err != nil {
				t.Errorf("Acquire: %v", err)
				return
			}
			n := inCrit.Add(1)
			for {
				cur := maxConc.Load()
				if n <= cur || maxConc.CompareAndSwap(cur, n) {
					break
				}
			}
			time.Sleep(time.Millisecond)
			inCrit.Add(-1)
			m.Release(s)
		}()
	}
	wg.Wait()
	if got := maxConc.Load(); got != 1 {
		t.Fatalf("max concurrent holders inside the per-session lock = %d, want 1 (serialized)", got)
	}
}

func TestReaper_EvictsAfterTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dk := &fakeDocker{}
		st := &fakeStore{}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		m := NewSessionManager(ctx, SessionDeps{
			Docker:    dk,
			Store:     st,
			MaxN:      5,
			TTL:       100 * time.Second,
			Runtime:   "runsc",
			Image:     "img",
			ReapEvery: 60 * time.Second,
		})

		s, err := m.Acquire(ctx, newConvID(3))
		if err != nil {
			t.Fatalf("Acquire: %v", err)
		}
		m.Release(s)

		// Advance virtual time well past TTL + a sweep tick.
		time.Sleep(200 * time.Second)
		synctest.Wait()

		if st.terminatedCount() != 1 {
			t.Fatalf("MarkTerminated called %d times, want 1 after TTL", st.terminatedCount())
		}
		if m.liveCount() != 0 {
			t.Fatalf("live session count = %d after reap, want 0", m.liveCount())
		}
		m.Close()
	})
}

func TestSessions_RunArgvHardeningFlags(t *testing.T) {
	dk := &fakeDocker{}
	m, _ := testManager(t, dk, &fakeStore{}, func(m *SessionManager) {
		m.seccompProfile = "/etc/aura/session-seccomp.json"
	})
	if _, err := m.Acquire(context.Background(), newConvID(4)); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	dk.mu.Lock()
	argv := strings.Join(dk.runArgv, " ")
	dk.mu.Unlock()

	// Pitfall 6: gVisor + every 2a hardening flag must be replicated in the ad-hoc
	// docker run argv (they are NOT inherited). D-05: never the socket.
	for _, want := range []string{
		"--runtime=runsc",
		"--user 65532:65532",
		"--read-only",
		"--tmpfs /tmp",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--security-opt seccomp=/etc/aura/session-seccomp.json",
		"--pids-limit 64",
		"--memory 512m",
		"--cpus 1.0",
		"--ulimit nofile=64",
	} {
		if !strings.Contains(argv, want) {
			t.Errorf("docker run argv missing %q\n  full: %s", want, argv)
		}
	}
	if strings.Contains(argv, "docker.sock") {
		t.Fatalf("docker run argv references the docker socket (D-05 violation): %s", argv)
	}
}

func TestSessions_PrivacyModeFailFast(t *testing.T) {
	m, _ := testManager(t, &fakeDocker{}, &fakeStore{}, func(m *SessionManager) {
		m.privacyMode = "local-only"
		m.allowHosts = "pypi.org"
	})
	if _, err := m.Acquire(context.Background(), newConvID(2)); !errors.Is(err, ErrPrivacyModeEgressDenied) {
		t.Fatalf("Acquire under local-only + non-empty allowlist: want ErrPrivacyModeEgressDenied, got %v", err)
	}
}
