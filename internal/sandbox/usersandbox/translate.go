// translate.go is the SINGLE crossing from Aura's SandboxSpec to a moby HostConfig. The
// dangerous moby fields (Privileged, Binds, host NetworkMode, AutoRemove, the docker
// socket) are pinned to safe constants HERE and appear as literals in NO other file —
// that pin-in-one-place is the SBX-02 containment mechanism.
package usersandbox

import "github.com/moby/moby/api/types/container"

// toHostConfig builds the moby container.HostConfig for a SandboxSpec, pinning every
// host-exposure field to a safe constant unconditionally. The dangerous literals live in
// this function alone.
func toHostConfig(s SandboxSpec) *container.HostConfig {
	// STUB (RED): the safe pins + sanctioned mounts are not yet built — see GREEN.
	return &container.HostConfig{}
}
