//go:build docker_integration

package usersandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestBoxReapsOrphans pins what Init=true buys: a process whose parent exits is reparented to
// PID 1, and PID 1 must reap it when it ends. The keep-alive `tail` never did, so each closed
// browser session left its Chromium children behind as zombies that counted against PidsLimit.
func TestBoxReapsOrphans(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	backend := NewDockerBackend(cli, testBoxImage(), testLimits())
	ctx := context.Background()
	h, err := backend.Resolve(ctx, SandboxSpec{IdentityID: strings.ReplaceAll("reap-"+time.Now().Format("150405.000000"), ".", "")})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })

	// Five orphans: each subshell exits at once and leaves its sleep to PID 1.
	if _, code := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c", "for i in 1 2 3 4 5; do (sleep 0.2 &); done"}); code != 0 {
		t.Fatalf("spawn orphans: exit %d", code)
	}
	time.Sleep(1500 * time.Millisecond)
	out, _ := rawExec(t, cli, h.ContainerID, []string{"/bin/sh", "-c",
		`z=0; for s in /proc/[0-9]*/stat; do set -- $(cat "$s" 2>/dev/null); [ "$3" = Z ] && z=$((z+1)); done; echo "zombies=$z"`})
	if !strings.Contains(out, "zombies=0") {
		t.Fatalf("orphans were not reaped: %s", strings.TrimSpace(out))
	}
}
