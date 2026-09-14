package usersandbox

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func TestCacheMountsAreIdentityScoped(t *testing.T) {
	a := toHostConfig(SandboxSpec{IdentityID: "identity-a", WorkspaceVol: "workspace-a"})
	b := toHostConfig(SandboxSpec{IdentityID: "identity-b", WorkspaceVol: "workspace-b"})
	for _, target := range []string{"/root/.cache/uv", "/root/.npm", "/root/.cache/pip"} {
		var sourceA, sourceB string
		for _, m := range a.Mounts {
			if m.Target == target && m.Type == mount.TypeVolume && !m.ReadOnly {
				sourceA = m.Source
			}
		}
		for _, m := range b.Mounts {
			if m.Target == target && m.Type == mount.TypeVolume && !m.ReadOnly {
				sourceB = m.Source
			}
		}
		if sourceA == "" || sourceB == "" || sourceA == sourceB {
			t.Errorf("cache %s must have distinct writable volumes: A=%q B=%q", target, sourceA, sourceB)
		}
	}
}

func TestExistingCacheMountsMustBelongToIdentity(t *testing.T) {
	valid := []container.MountPoint{
		{Type: mount.TypeVolume, Name: "aura-box-a-uv-cache", Destination: "/root/.cache/uv", RW: true},
		{Type: mount.TypeVolume, Name: "aura-box-a-npm-cache", Destination: "/root/.npm", RW: true},
		{Type: mount.TypeVolume, Name: "aura-box-a-pip-cache", Destination: "/root/.cache/pip", RW: true},
	}
	if !hasIdentityCacheMounts(valid, "a") || hasIdentityCacheMounts(valid, "b") {
		t.Fatal("existing cache mounts must be reusable only by their owner")
	}
	if hasIdentityCacheMounts(nil, "a") || hasIdentityCacheMounts(valid[:2], "a") {
		t.Fatal("missing cache mounts must require recreation")
	}
	for _, change := range []func(*container.MountPoint){
		func(m *container.MountPoint) { m.Name = "aura-pip-cache" },
		func(m *container.MountPoint) { m.Type = mount.TypeBind },
		func(m *container.MountPoint) { m.RW = false },
		func(m *container.MountPoint) { m.Destination = "/another-cache" },
	} {
		modified := append([]container.MountPoint(nil), valid...)
		change(&modified[2])
		if hasIdentityCacheMounts(modified, "a") {
			t.Fatalf("unsafe or incomplete cache configuration accepted: %+v", modified[2])
		}
	}
}
