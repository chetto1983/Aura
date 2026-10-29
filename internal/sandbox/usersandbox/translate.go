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
	// uv keeps the Pythons it downloads here, outside its cache. Without a volume every box
	// recreation dropped them, and a `uvx` server's first start downloaded one again: 25 s
	// against 7 s for broker-analysis on the lab VM (2026-10-29).
	uvDataTarget = "/root/.local/share/uv"
)

func identityCacheMounts(identityID string) []mount.Mount {
	prefix := boxName(identityID)
	return []mount.Mount{
		{Type: mount.TypeVolume, Source: prefix + "-uv-cache", Target: uvCacheTarget},
		{Type: mount.TypeVolume, Source: prefix + "-uv-data", Target: uvDataTarget},
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
//   - Runtime     ""           the daemon's default runc; gVisor was retired 2026-10-11 (prd.md §12)
//   - Init        true         docker-init (tini) is PID 1 and reaps orphans. The keep-alive
//     `tail` never waits on anyone, so every process a closed browser session left behind
//     stayed a zombie and counted against PidsLimit: measured 2026-09-26, 177 zombies after
//     four agent-browser sessions opened and closed, in a box capped at 512 pids.
//
// Mounts is built only from the per-identity workspace volume, a tmpfs scratch, and the
// identity's uv (cache and managed Pythons) / npm / pip warm-cache volumes. The docker socket is a host path whose only
// mount vector is a bind — which never appears here — so the socket is unrepresentable.
func toHostConfig(s SandboxSpec) *container.HostConfig {
	pids := s.Limits.PidsLimit
	reapOrphans := true
	mounts := []mount.Mount{
		{Type: mount.TypeVolume, Source: workspaceName(s.IdentityID, s.Kind), Target: workspaceTarget},
		{Type: mount.TypeTmpfs, Target: scratchTarget},
	}
	if s.Kind == Shell {
		mounts = append(mounts, identityCacheMounts(s.IdentityID)...)
	}
	return &container.HostConfig{
		Init:        &reapOrphans,
		Privileged:  false,
		NetworkMode: container.NetworkMode(""),
		Binds:       nil,
		AutoRemove:  false,
		CapDrop:     []string{},
		Runtime:     "",
		Mounts:      mounts,
		NanoCPUs:    s.Limits.NanoCPUs,
		Memory:      s.Limits.MemoryBytes,
		PidsLimit:   &pids,
	}
}
