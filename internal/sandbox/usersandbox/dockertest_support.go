//go:build docker_integration

// dockertest_support.go is the shared docker_integration test scaffolding for the
// usersandbox package. It is compiled ONLY under `-tags docker_integration` (verified by
// `go build -tags docker_integration ./internal/sandbox/usersandbox/`) and holds the
// no-skip-as-green dockerd gate the tier's tests call before touching a live daemon.
//
// It deliberately does NOT import the moby client: the client binding (and a real API
// Ping) is owned by the DockerBackend plan (37-04). Here a stdlib socket-reach probe is
// sufficient to gate the tier — an unreachable daemon skips locally but t.Fatals under
// $CI, which is the only contract downstream integration tests rely on.

package usersandbox

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// dockerEndpoint resolves the (network, address) to dial for the local Docker daemon from
// DOCKER_HOST (tcp:// or unix://), defaulting to the conventional unix socket. An npipe://
// endpoint (Windows Docker Desktop) is not stdlib-dialable and resolves to ("", ""), which
// the caller treats as unreachable — a LOCAL skip only, never a CI pass: the
// docker_integration tier is a native-Linux gate (37-RESEARCH Pitfall 3).
func dockerEndpoint() (network, addr string) {
	host := strings.TrimSpace(os.Getenv("DOCKER_HOST"))
	switch {
	case host == "":
		return "unix", "/var/run/docker.sock"
	case strings.HasPrefix(host, "unix://"):
		return "unix", strings.TrimPrefix(host, "unix://")
	case strings.HasPrefix(host, "tcp://"):
		return "tcp", strings.TrimPrefix(host, "tcp://")
	default:
		return "", ""
	}
}

// dockerdReachable reports whether the resolved Docker daemon endpoint accepts a
// connection. It is a lightweight reachability probe, not an API call — the real
// moby-client Ping lives in the DockerBackend plan's docker_integration smoke (37-04).
func dockerdReachable() bool {
	network, addr := dockerEndpoint()
	if network == "" {
		return false
	}
	conn, err := net.DialTimeout(network, addr, 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// skipUnlessDockerd gates a docker_integration test on a reachable Docker daemon. When the
// daemon is unreachable it t.Skips locally (the one legitimate local skip) but t.Fatals
// under $CI — a skipped integration tier must never pass green (CLAUDE.md no-skip-as-green).
func skipUnlessDockerd(t *testing.T) {
	t.Helper()
	if dockerdReachable() {
		return
	}
	if strings.TrimSpace(os.Getenv("CI")) != "" {
		t.Fatal("docker_integration requires a reachable Docker daemon, but none was found under CI — " +
			"a skipped docker_integration tier is never a silent pass (CLAUDE.md no-skip-as-green); wire dockerd into ci.yml")
	}
	t.Skip("docker_integration requires a reachable Docker daemon; start Docker and re-run " +
		"(e.g. `go test -tags docker_integration ./internal/sandbox/usersandbox`)")
}
