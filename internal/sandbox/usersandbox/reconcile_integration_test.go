//go:build docker_integration

package usersandbox

import (
	"context"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// TestResolveReplacesAStaleBox builds the box an older Aura left behind — another image, no
// init — and requires Resolve to replace it with the current spec while the workspace survives.
// A box that is already current must then be reused as is.
func TestResolveReplacesAStaleBox(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	ctx := context.Background()
	const oldImage, newImage = "busybox:stable", "python:3-slim"
	b := NewDockerBackend(cli, newImage, testLimits())
	id := uniqueIdentity(t, "stale-box")
	name := boxName(id)
	spec := SandboxSpec{IdentityID: id, WorkspaceVol: name, Image: newImage}
	for _, ref := range []string{oldImage, newImage} {
		if err := b.ensureImage(ctx, ref); err != nil {
			t.Fatal(err)
		}
	}
	hc := toHostConfig(spec)
	hc.Init = nil
	old, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name, Config: &container.Config{Image: oldImage, Cmd: keepAliveCmd}, HostConfig: hc,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Stop(context.Background(), BoxHandle{ContainerID: name, IdentityID: id}) })
	if _, err := cli.ContainerStart(ctx, old.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatal(err)
	}
	assertExecWrite(t, cli, BoxHandle{ContainerID: old.ID, IdentityID: id}, "/workspace/keep", "retained-work")

	h, err := b.Resolve(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if h.ContainerID == old.ID || containerExists(t, cli, old.ID) {
		t.Fatal("Resolve reused a box built from an older image without an init")
	}
	ins, err := cli.ContainerInspect(ctx, h.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	img, err := cli.ImageInspect(ctx, newImage)
	if err != nil {
		t.Fatal(err)
	}
	if ins.Container.Image != img.ID || ins.Container.HostConfig.Init == nil || !*ins.Container.HostConfig.Init {
		t.Fatalf("recreated box: image %s init %v, want %s with init", ins.Container.Image, ins.Container.HostConfig.Init, img.ID)
	}
	if ins.Container.Config.Hostname != "aura-box" {
		t.Fatalf("recreated box hostname = %q, want aura-box, which every recreate keeps", ins.Container.Config.Hostname)
	}
	assertBoxFile(t, cli, h.ContainerID, "/workspace/keep", "retained-work")

	again, err := b.Resolve(ctx, spec)
	if err != nil || again.ContainerID != h.ContainerID {
		t.Fatalf("a current box was not reused: %v", err)
	}
}
