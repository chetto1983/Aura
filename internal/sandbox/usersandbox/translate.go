// translate.go is the SINGLE crossing from Aura's SandboxSpec to a moby HostConfig: the
// dangerous moby fields (Privileged, Binds, host NetworkMode, AutoRemove, the docker
// socket) are pinned to safe constants HERE and appear as literals in NO other file —
// that pin-in-one-place is the SBX-02 containment mechanism.

package usersandbox

import (
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

// The box's sanctioned mount targets. The workspace volume + tmpfs scratch + the shared
// uv warm-cache volume are the ONLY mounts toHostConfig builds; no host path (and so no
// docker socket) can be mounted.
const (
	workspaceTarget = "/workspace"
	scratchTarget   = "/workspace/.scratch"
	uvCacheVolume   = "aura-uv-cache"
	uvCacheTarget   = "/root/.cache/uv"
)

// toHostConfig builds the moby container.HostConfig for a SandboxSpec, pinning every
// host-exposure field to a safe constant unconditionally. These dangerous literals live
// in this function ALONE:
//   - Privileged  false        no privileged escalation
//   - NetworkMode ""           never "host" (egress is enforced by the sidecar netns)
//   - Binds       nil          no host bind-mounts, volumes only (D-10)
//   - AutoRemove  false        the box is suspendable; --rm would destroy it (Pitfall 5)
//   - CapDrop     empty        keep default caps (D-12: the box is not a jail)
//
// Mounts is built only from the per-identity workspace volume, a tmpfs scratch, and the
// shared uv warm-cache volume. The docker socket is a host path whose only mount vector is
// a bind — which never appears here — so the socket is unrepresentable.
func toHostConfig(s SandboxSpec) *container.HostConfig {
	pids := s.Limits.PidsLimit
	return &container.HostConfig{
		Privileged:  false,
		NetworkMode: container.NetworkMode(""),
		Binds:       nil,
		AutoRemove:  false,
		CapDrop:     []string{},
		Runtime:     s.Runtime.dockerRuntime(),
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: s.WorkspaceVol, Target: workspaceTarget},
			{Type: mount.TypeTmpfs, Target: scratchTarget},
			{Type: mount.TypeVolume, Source: uvCacheVolume, Target: uvCacheTarget},
		},
		Resources: container.Resources{
			NanoCPUs:  s.Limits.NanoCPUs,
			Memory:    s.Limits.MemoryBytes,
			PidsLimit: &pids,
		},
	}
}
