//go:build docker_integration

package usersandbox

import (
	"context"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/client"
)

func TestResolveReplacesForeignCacheMounts(t *testing.T) {
	skipUnlessDockerd(t)
	cli := newTestDockerClient(t)
	b := NewDockerBackend(cli, testBoxImage(), testLimits())
	ctx := context.Background()
	id := uniqueIdentity(t, "cache-migration")
	name := boxName(id)
	spec := SandboxSpec{IdentityID: id, WorkspaceVol: name, Image: testBoxImage()}
	if err := b.ensureImage(ctx, spec.Image); err != nil {
		t.Fatal(err)
	}
	hc := toHostConfig(spec)
	var oldVolumes []string
	for i := range hc.Mounts {
		if hc.Mounts[i].Type == mount.TypeVolume && hc.Mounts[i].Target != "/workspace" {
			hc.Mounts[i].Source = "old-" + hc.Mounts[i].Source
			oldVolumes = append(oldVolumes, hc.Mounts[i].Source)
		}
	}
	old, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name, Config: &container.Config{Image: spec.Image, Cmd: keepAliveCmd}, HostConfig: hc,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Stop(context.Background(), BoxHandle{ContainerID: name, IdentityID: id})
		for _, v := range oldVolumes {
			_, _ = cli.VolumeRemove(context.Background(), v, client.VolumeRemoveOptions{})
		}
	})
	if _, err := cli.ContainerStart(ctx, old.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatal(err)
	}
	oldHandle := BoxHandle{ContainerID: old.ID, IdentityID: id}
	assertExecWrite(t, cli, oldHandle, "/workspace/keep", "retained-work")
	assertExecWrite(t, cli, oldHandle, "/root/.cache/pip/foreign-wheel", "foreign")
	h, err := b.Resolve(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}
	if h.ContainerID == old.ID || containerExists(t, cli, old.ID) {
		t.Fatal("Resolve reused a box with foreign cache mounts")
	}
	assertBoxFile(t, cli, h.ContainerID, "/workspace/keep", "retained-work")
	if _, code := rawExec(t, cli, h.ContainerID, []string{"test", "!", "-e", "/root/.cache/pip/foreign-wheel"}); code != 0 {
		t.Fatal("foreign cache contents were copied into the private cache")
	}
	again, err := b.Resolve(ctx, spec)
	if err != nil || again.ContainerID != h.ContainerID {
		t.Fatalf("a private-cache box was not reused: %v", err)
	}
}
