package testenv

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// recorder is a testing.TB whose Skip and Fatal record their message instead of stopping the
// goroutine, so a gate's outcome can be asserted from the test that calls it.
type recorder struct {
	testing.TB
	skipped, failed string
}

func (r *recorder) Helper()                        {}
func (r *recorder) Skip(args ...any)               { r.skipped = fmt.Sprint(args...) }
func (r *recorder) Fatal(args ...any)              { r.failed = fmt.Sprint(args...) }
func (r *recorder) Fatalf(format string, a ...any) { r.failed = fmt.Sprintf(format, a...) }
func (r *recorder) ran() bool                      { return r.skipped == "" && r.failed == "" }

func outcomes(t *testing.T, gate func(testing.TB)) (local, ci *recorder) {
	t.Helper()
	local, ci = &recorder{}, &recorder{}
	t.Setenv("CI", "")
	gate(local)
	t.Setenv("CI", "true")
	gate(ci)
	return local, ci
}

func TestCI(t *testing.T) {
	for value, want := range map[string]bool{"": false, " \t": false, "true": true, "1": true} {
		t.Setenv("CI", value)
		if got := CI(); got != want {
			t.Errorf("CI=%q: CI() = %v, want %v", value, got, want)
		}
	}
}

func TestUnavailableSkipsLocallyAndFailsUnderCI(t *testing.T) {
	local, ci := outcomes(t, func(tb testing.TB) { Unavailable(tb, "no %s here", "widget") })
	if local.skipped != "no widget here" || local.failed != "" {
		t.Errorf("locally: skipped %q, failed %q; want a skip with the reason", local.skipped, local.failed)
	}
	if !strings.HasPrefix(ci.failed, "no widget here; $CI is set") {
		t.Errorf("under CI: failed %q, want the reason and why it fails", ci.failed)
	}
}

func TestRequire(t *testing.T) {
	t.Run("set", func(t *testing.T) {
		t.Setenv("TESTENV_KEY", " secret ")
		var got string
		local, ci := outcomes(t, func(tb testing.TB) { got = Require(tb, "TESTENV_KEY") })
		if !local.ran() || !ci.ran() || got != " secret " {
			t.Errorf("a set key: got %q, local %+v, ci %+v; want the value untrimmed and the test running", got, local, ci)
		}
	})
	for name, value := range map[string]string{"unset": "", "blank": "  "} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("TESTENV_KEY", value)
			local, ci := outcomes(t, func(tb testing.TB) { Require(tb, "TESTENV_KEY") })
			if local.skipped != "TESTENV_KEY is unset" || !strings.HasPrefix(ci.failed, "TESTENV_KEY is unset") {
				t.Errorf("%s key: local skip %q, ci fail %q", name, local.skipped, ci.failed)
			}
		})
	}
}

func TestExecutable(t *testing.T) {
	var path string
	local, ci := outcomes(t, func(tb testing.TB) { path = Executable(tb, "go") })
	if !local.ran() || !ci.ran() || path == "" {
		t.Errorf("go on PATH: path %q, local %+v, ci %+v", path, local, ci)
	}
	local, ci = outcomes(t, func(tb testing.TB) { Executable(tb, "testenv-no-such-program") })
	if !strings.HasPrefix(local.skipped, "testenv-no-such-program is not installed") || ci.failed == "" {
		t.Errorf("a missing program: local skip %q, ci fail %q", local.skipped, ci.failed)
	}
}

func TestSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ok := &recorder{}
	Symlink(ok, target, filepath.Join(dir, "link"))
	if !ok.ran() {
		t.Fatalf("a symlink in a writable directory: %+v", ok)
	}
	if got, err := os.Readlink(filepath.Join(dir, "link")); err != nil || got != target {
		t.Fatalf("link points at %q, %v; want %q", got, err, target)
	}
	local, ci := outcomes(t, func(tb testing.TB) { Symlink(tb, target, filepath.Join(dir, "missing", "link")) })
	if !strings.HasPrefix(local.skipped, "this host cannot make symlinks") || ci.failed == "" {
		t.Errorf("a link the host refuses: local skip %q, ci fail %q", local.skipped, ci.failed)
	}
}

func TestDockerEndpoint(t *testing.T) {
	for host, want := range map[string][2]string{
		"":                        {"unix", "/var/run/docker.sock"},
		"unix:///run/docker.sock": {"unix", "/run/docker.sock"},
		"tcp://127.0.0.1:2375":    {"tcp", "127.0.0.1:2375"},
		"npipe:////./pipe/docker": {"", ""},
	} {
		t.Setenv("DOCKER_HOST", host)
		if network, addr := dockerEndpoint(); network != want[0] || addr != want[1] {
			t.Errorf("DOCKER_HOST=%q: (%q, %q), want %q", host, network, addr, want)
		}
	}
}

func TestDockerd(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	t.Setenv("DOCKER_HOST", "tcp://"+listener.Addr().String())
	local, ci := outcomes(t, Dockerd)
	if !local.ran() || !ci.ran() {
		t.Errorf("a listening endpoint: local %+v, ci %+v", local, ci)
	}

	closed := listener.Addr().String()
	_ = listener.Close()
	for _, host := range []string{"tcp://" + closed, "npipe:////./pipe/docker"} {
		t.Setenv("DOCKER_HOST", host)
		local, ci := outcomes(t, Dockerd)
		if !strings.Contains(local.skipped, "needs a reachable Docker daemon") || ci.failed == "" {
			t.Errorf("DOCKER_HOST=%q: local skip %q, ci fail %q", host, local.skipped, ci.failed)
		}
	}
}

func TestEnforcingBridge(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the gate's native-Linux branches are what this asserts")
	}
	t.Setenv("AURA_EGRESS_ENFORCE", "")
	local, ci := outcomes(t, EnforcingBridge)
	if !strings.HasPrefix(local.skipped, "egress DROP enforcement runs on native Linux only") || !ci.ran() {
		t.Errorf("not opted in: local skip %q, ci %+v; want a local skip and CI running", local.skipped, ci)
	}
	t.Setenv("AURA_EGRESS_ENFORCE", "1")
	if local, _ := outcomes(t, EnforcingBridge); !local.ran() {
		t.Errorf("opted in on Linux: %+v, want it to run", local)
	}
}
