//go:build docker_integration

package usersandbox

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"
)

// TestBoxFilesLandAndComeBackAfterRecreate proves the lifecycle the browser key depends on: the
// file is written 0600 at create, is still there after Suspend/Resume, and a recreated box gets
// it back from Resolve rather than from anything stored in the old container.
func TestBoxFilesLandAndComeBackAfterRecreate(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	const path, body = "/run/aura/test.key", "per-identity-key"
	calls := 0
	backend := NewDockerBackend(cli, testBoxImage(), testLimits(), WithBoxFiles(func(string) ([]BoxFile, error) {
		calls++
		return []BoxFile{{Path: path, Content: []byte(body), Mode: 0o600}}, nil
	}))

	ctx := context.Background()
	id := strings.ReplaceAll("bf-"+time.Now().Format("150405.000000"), ".", "")
	h, err := backend.Resolve(ctx, SandboxSpec{IdentityID: id})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Cleanup(func() { _ = backend.Stop(context.Background(), h) })

	check := func(stage, containerID string) {
		t.Helper()
		out, code := rawExec(t, cli, containerID, []string{"/bin/sh", "-c", "stat -c %a " + path + " && cat " + path})
		if code != 0 || !strings.Contains(out, "600") || !strings.Contains(out, body) {
			t.Fatalf("%s: code=%d out=%q, want mode 600 and the body", stage, code, out)
		}
	}
	check("create", h.ContainerID)

	if err := backend.Suspend(ctx, h); err != nil {
		t.Fatalf("suspend: %v", err)
	}
	if err := backend.Resume(ctx, h); err != nil {
		t.Fatalf("resume: %v", err)
	}
	check("resume", h.ContainerID)

	if _, err := cli.ContainerRemove(ctx, h.ContainerID, client.ContainerRemoveOptions{Force: true}); err != nil {
		t.Fatalf("remove container: %v", err)
	}
	h2, err := backend.Resolve(ctx, SandboxSpec{IdentityID: id})
	if err != nil {
		t.Fatalf("resolve after recreate: %v", err)
	}
	h = h2
	check("recreate", h2.ContainerID)
	if calls != 2 {
		t.Fatalf("source consulted %d times, want once per Resolve (2)", calls)
	}
}
