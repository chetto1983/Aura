package testenv

import (
	"net"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Dockerd ends the test unless the Docker daemon accepts a connection. It dials the socket
// DOCKER_HOST names (tcp:// or unix://, the conventional unix socket when unset); it does not
// call the API, which the tests that need it do themselves. An npipe:// endpoint (Windows Docker
// Desktop) cannot be dialled from the standard library and counts as unreachable: the
// docker_integration tier runs on native Linux.
func Dockerd(t testing.TB) {
	t.Helper()
	network, addr := dockerEndpoint()
	if network != "" {
		if conn, err := net.DialTimeout(network, addr, 2*time.Second); err == nil {
			_ = conn.Close()
			return
		}
	}
	Unavailable(t, "docker_integration needs a reachable Docker daemon (DOCKER_HOST=%q)", os.Getenv("DOCKER_HOST"))
}

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

// EnforcingBridge gates the egress DROP assertions on a native-Linux, non-masquerading bridge.
// Under CI it is mandatory, so a daemon that is not native Linux fails the test. Elsewhere it
// skips unless a developer on such a host opts in with AURA_EGRESS_ENFORCE=1: Docker Desktop's
// and WSL's vpnkit NAT the bridge, so a DROP there proves nothing.
func EnforcingBridge(t testing.TB) {
	t.Helper()
	if CI() {
		if runtime.GOOS != "linux" {
			t.Fatalf("egress enforcement must run on native-Linux dockerd under CI, but GOOS=%s "+
				"(Docker Desktop's vpnkit NATs the bridge)", runtime.GOOS)
		}
		return
	}
	if runtime.GOOS == "linux" && strings.TrimSpace(os.Getenv("AURA_EGRESS_ENFORCE")) == "1" {
		return
	}
	t.Skip("egress DROP enforcement runs on native Linux only; set AURA_EGRESS_ENFORCE=1 on a " +
		"native-Linux, non-masquerading host to run it")
}
