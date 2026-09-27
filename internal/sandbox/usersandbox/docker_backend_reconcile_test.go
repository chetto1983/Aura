package usersandbox

import (
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

func TestStaleBoxReason(t *testing.T) {
	t.Parallel()
	const id = "id-1"
	var mounts []container.MountPoint
	for _, m := range identityCacheMounts(id) {
		mounts = append(mounts, container.MountPoint{Type: mount.TypeVolume, Name: m.Source, Destination: m.Target, RW: true})
	}
	yes, no := true, false
	current := container.InspectResponse{Image: "sha256:new", Mounts: mounts, HostConfig: &container.HostConfig{Init: &yes}}

	for name, tc := range map[string]struct {
		mutate  func(*container.InspectResponse)
		imageID string
		want    string
	}{
		"current box is kept":                      {func(*container.InspectResponse) {}, "sha256:new", ""},
		"unknown configured image never recreates": {func(i *container.InspectResponse) { i.Image = "sha256:old" }, "", ""},
		"older image":                              {func(i *container.InspectResponse) { i.Image = "sha256:old" }, "sha256:new", "an older image"},
		"init off":                                 {func(i *container.InspectResponse) { i.HostConfig = &container.HostConfig{Init: &no} }, "sha256:new", "no init to reap orphans"},
		"init unset":                               {func(i *container.InspectResponse) { i.HostConfig = &container.HostConfig{} }, "sha256:new", "no init to reap orphans"},
		"no host config":                           {func(i *container.InspectResponse) { i.HostConfig = nil }, "sha256:new", "no init to reap orphans"},
		"shared caches win over everything else":   {func(i *container.InspectResponse) { i.Mounts = nil; i.Image = "sha256:old" }, "sha256:new", "non-private cache mounts"},
	} {
		t.Run(name, func(t *testing.T) {
			ins := current
			tc.mutate(&ins)
			if got := staleBoxReason(ins, id, tc.imageID); got != tc.want {
				t.Fatalf("staleBoxReason = %q, want %q", got, tc.want)
			}
		})
	}
}
