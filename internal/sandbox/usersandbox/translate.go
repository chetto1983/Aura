// translate.go is the SINGLE crossing from Aura's SandboxSpec to a moby HostConfig: the
// dangerous moby fields (Privileged, Binds, host NetworkMode, AutoRemove, the docker
// socket) are pinned to safe constants HERE and appear as literals in NO other file —
// that pin-in-one-place is the SBX-02 containment mechanism.

package usersandbox

import (
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
)

const (
	workspaceTarget = "/workspace"
	scratchTarget   = "/workspace/.scratch"
	uvCacheTarget   = "/root/.cache/uv"
	npmCacheTarget  = "/root/.npm"
	pipCacheTarget  = "/root/.cache/pip"
)

func identityCacheMounts(identityID string) []mount.Mount {
	prefix := boxName(identityID)
	return []mount.Mount{
		{Type: mount.TypeVolume, Source: prefix + "-uv-cache", Target: uvCacheTarget},
		{Type: mount.TypeVolume, Source: prefix + "-npm-cache", Target: npmCacheTarget},
		{Type: mount.TypeVolume, Source: prefix + "-pip-cache", Target: pipCacheTarget},
	}
}

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
// identity's uv / npm / pip warm-cache volumes. The docker socket is a host path whose only
// mount vector is a bind — which never appears here — so the socket is unrepresentable.
func toHostConfig(s SandboxSpec) *container.HostConfig {
	pids := s.Limits.PidsLimit
	return &container.HostConfig{
		Privileged:  false,
		NetworkMode: container.NetworkMode(""),
		Binds:       nil,
		AutoRemove:  false,
		CapDrop:     []string{},
		Runtime:     s.Runtime.dockerRuntime(),
		Mounts: append([]mount.Mount{
			{Type: mount.TypeVolume, Source: s.WorkspaceVol, Target: workspaceTarget},
			{Type: mount.TypeTmpfs, Target: scratchTarget},
		}, identityCacheMounts(s.IdentityID)...),
		NanoCPUs:  s.Limits.NanoCPUs,
		Memory:    s.Limits.MemoryBytes,
		PidsLimit: &pids,
	}
}
