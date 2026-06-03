// Unit tier for the SessionEndpoint resolver (the BLOCKER-1 process-scoped shared
// routing table) + the docker-port multi-line/IPv6 parse filter. No daemon, no
// Postgres. Shares the package-level goleak TestMain in docker_test.go
// (//go:build !sandbox_integration) — this file MUST NOT declare its own TestMain.
package sandbox

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/config"
)

// bareRunner builds a DockerRunner exactly as the integration helper integrationRunner
// does — NewDockerRunner(&config.Config{...}) with NO explicit resolver — so the
// BLOCKER-1 test proves the bare runner defaults its resolver to the package var.
func bareRunner() *DockerRunner {
	return NewDockerRunner(&config.Config{SandboxURL: "http://127.0.0.1:1", SandboxTimeoutSec: 30})
}

// TestSessionEndpoint_RoundTrip is the basic Register/URLFor/Unregister contract on
// a PRIVATE instance (contamination guard: never the shared defaultSessionEndpoint).
func TestSessionEndpoint_RoundTrip(t *testing.T) {
	e := NewSessionEndpoint()
	id := uuid.NewString()

	if _, ok := e.URLFor(id); ok {
		t.Fatalf("URLFor before Register must miss")
	}
	e.Register(id, "http://127.0.0.1:18999")
	got, ok := e.URLFor(id)
	if !ok || got != "http://127.0.0.1:18999" {
		t.Fatalf("URLFor after Register = %q,%v; want the registered URL", got, ok)
	}
	e.Unregister(id)
	if _, ok := e.URLFor(id); ok {
		t.Fatalf("URLFor after Unregister must miss")
	}
}

// TestSessionEndpoint_ConcurrentRace hammers Register/URLFor/Unregister from N
// goroutines on a PRIVATE instance so -race proves concurrency safety.
func TestSessionEndpoint_ConcurrentRace(t *testing.T) {
	e := NewSessionEndpoint()
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := uuid.NewString()
			e.Register(id, "http://127.0.0.1:1")
			_, _ = e.URLFor(id)
			e.Unregister(id)
		}(i)
	}
	wg.Wait()
}

// TestSessionEndpoint_BlockerOneSharedDefault is THE BLOCKER-1 GUARANTEE: with NO
// SessionDeps.Endpoint injected, SessionManager.create registers into the package
// defaultSessionEndpoint; a DockerRunner built with the BARE NewDockerRunner (no
// explicit resolver, exactly as integrationRunner builds it) resolves that SAME
// convID via URLFor — proving the test's independently-built runner+manager share
// routing state BY CONSTRUCTION.
func TestSessionEndpoint_BlockerOneSharedDefault(t *testing.T) {
	dk := &fakeDocker{}
	m, _ := testManager(t, dk, &fakeStore{}, nil)

	conv := newConvID(21)
	sess, err := m.Acquire(context.Background(), conv)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	m.Release(sess)

	// A bare NewDockerRunner — exactly the shape integrationRunner builds — must
	// DEFAULT its resolver to the package var, so it sees create's registration.
	runner := bareRunner()
	if runner.SessionEndpoint == nil {
		t.Fatalf("NewDockerRunner must default SessionEndpoint to the package singleton, got nil")
	}
	got, ok := runner.SessionEndpoint.URLFor(conv)
	if !ok {
		t.Fatalf("bare runner could not resolve convID %s registered by create — shared default broken", conv)
	}
	if !strings.HasPrefix(got, "http://127.0.0.1:") {
		t.Fatalf("registered per-conv URL must be a loopback HTTP base, got %q", got)
	}
}

// TestSessionEndpoint_LookupMissTyped asserts sessionExec on an UNREGISTERED
// sessionID returns ErrSessionEndpointUnknown (no silent fallthrough to r.url). The
// CONTAMINATION GUARD: it builds a runner over a PRIVATE resolver instance keyed by
// a uuid, so no sibling test's create-registration into the shared default can
// satisfy this miss path.
func TestSessionEndpoint_LookupMissTyped(t *testing.T) {
	r := NewDockerRunnerWithEndpoint("http://127.0.0.1:1", 30, NewSessionEndpoint())
	_, err := r.RunPythonSession(context.Background(), uuid.NewString(), "x=1", 5)
	if !errors.Is(err, ErrSessionEndpointUnknown) {
		t.Fatalf("unregistered session exec: want ErrSessionEndpointUnknown, got %v", err)
	}
}

// TestSessionEndpoint_NilResolverDefensive: a runner with a nil resolver returns
// ErrSessionEndpointUnknown rather than panicking.
func TestSessionEndpoint_NilResolverDefensive(t *testing.T) {
	r := NewDockerRunnerWithEndpoint("http://127.0.0.1:1", 30, nil)
	_, err := r.RunPythonSession(context.Background(), uuid.NewString(), "x=1", 5)
	if !errors.Is(err, ErrSessionEndpointUnknown) {
		t.Fatalf("nil-resolver session exec: want ErrSessionEndpointUnknown, got %v", err)
	}
}

// TestParsePublishedHostPort_IPv6Multiline is FLAG 1: a dual-stack `docker port`
// stdout (a 127.0.0.1 line AND an IPv6 [::] line) picks the 127.0.0.1 line; zero
// 127.0.0.1 lines errors.
func TestParsePublishedHostPort_IPv6Multiline(t *testing.T) {
	cases := []struct {
		name    string
		stdout  string
		want    string
		wantErr bool
	}{
		{
			name:   "dual-stack picks the 127.0.0.1 line",
			stdout: "0.0.0.0:49154\n[::]:49155",
			want:   "",
			// 0.0.0.0 is not loopback; neither is [::] — both rejected.
			wantErr: true,
		},
		{
			name:   "loopback present among ipv6",
			stdout: "127.0.0.1:49154\n[::1]:49155",
			want:   "127.0.0.1:49154",
		},
		{
			name:   "mapping arrow form",
			stdout: "18901/tcp -> 127.0.0.1:49160\n18901/tcp -> [::]:49161",
			want:   "127.0.0.1:49160",
		},
		{
			name:    "no loopback line errors",
			stdout:  "[::]:49155",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePublishedHostPort(tc.stdout)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStatelessPathUsesFixedURL asserts the 2a stateless RunPython/RunShell path
// still POSTs to the fixed r.url (the resolver is consulted ONLY on the session
// path). It points the runner at a closed port and asserts an unreachable error
// (transport), NOT ErrSessionEndpointUnknown — proving the stateless path never
// consults the resolver.
func TestStatelessPathUsesFixedURL(t *testing.T) {
	r := NewDockerRunnerWithEndpoint("http://127.0.0.1:1", 1, NewSessionEndpoint())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := r.RunPython(ctx, "x=1", 1)
	if errors.Is(err, ErrSessionEndpointUnknown) {
		t.Fatalf("stateless path must NOT consult the resolver, got ErrSessionEndpointUnknown")
	}
	if err == nil {
		t.Fatalf("stateless path against a closed port must error")
	}
}
