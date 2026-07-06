// docker_backend.go is the FIRST moby/moby/client v0.4.1 caller in the repo: DockerBackend
// implements the Backend E2B seam (backend.go) over the Docker Engine API. It owns the
// moby client handle, the fat box image ref, and the cgroup caps; the per-verb lifecycle
// (Resolve/Suspend/Resume/Stop) lives in docker_backend_lifecycle.go and Exec in
// docker_backend_exec.go. The dangerous HostConfig fields are pinned safe in translate.go
// (SBX-02) — this file never sets them directly.

package usersandbox

import "github.com/moby/moby/client"

// boxNamePrefix is the deterministic container + per-identity volume name prefix. Both the
// container and the identity's workspace volume share the name aura-box-<identityID> (the
// key the reaper's lastUsedAt tracking and every per-identity scope use); they live in
// separate Docker namespaces (containers vs volumes) so the shared name is not a collision.
const boxNamePrefix = "aura-box-"

// keepAliveCmd is the container's foreground command: a portable idle keep-alive so the box
// stays running (and thus exec-able) with no workload of its own. `tail -f /dev/null` blocks
// forever on both BusyBox and GNU coreutils images, so it works for the fat aura-sandbox box
// AND any lightweight image the docker_integration round-trip pulls. Overriding the image
// CMD here is intentional: the box must never exit on its own — only Suspend/Stop stop it.
var keepAliveCmd = []string{"tail", "-f", "/dev/null"}

// MaterializeSource maps a host source directory to the destination root inside the box it
// is tar-streamed into (docker cp, the replacement for the removed ro bind-mount — D-10).
// For skills the Dest is skills.inSandboxSkillsRoot ("/skills") so the in-box path equals
// the one SnippetSandboxPath renders; Agent.md and pyscripts map to their own roots.
type MaterializeSource struct {
	HostDir string
	Dest    string
}

// SourceResolver returns the per-identity materialization sources (the identity's skills /
// Agent.md / pyscripts host dirs mapped to their in-box roots). It is injected via
// WithMaterializeSources so the backend stays decoupled from config path resolution and the
// docker_integration tests can seed their own fixtures. A nil resolver (or empty result)
// means nothing is materialized — Resolve still succeeds.
type SourceResolver func(identityID string) []MaterializeSource

// DockerBackend implements Backend over the moby/moby/client Docker Engine API. It is the
// per-identity box runtime every routed tool ultimately execs into.
type DockerBackend struct {
	cli     *client.Client
	image   string
	limits  Resources
	sources SourceResolver
}

// Option configures a DockerBackend at construction (functional-options, mirroring the moby
// client's own NewClientWithOpts) so the documented 3-arg NewDockerBackend call stays valid.
type Option func(*DockerBackend)

// WithMaterializeSources wires the per-identity source resolver Resolve consults to
// materialize skills / Agent.md / pyscripts into the box at create and resume (D-10).
func WithMaterializeSources(r SourceResolver) Option {
	return func(b *DockerBackend) { b.sources = r }
}

// NewDockerBackend builds a DockerBackend over an existing moby client, the fat box image
// ref (AURA_SANDBOX_IMAGE), and the cgroup caps (D-14). Materialization sources are opt-in
// via WithMaterializeSources.
func NewDockerBackend(cli *client.Client, imageRef string, limits Resources, opts ...Option) *DockerBackend {
	b := &DockerBackend{cli: cli, image: imageRef, limits: limits}
	for _, o := range opts {
		o(b)
	}
	return b
}

// boxName is the deterministic container + workspace-volume name for an identity.
func boxName(identityID string) string { return boxNamePrefix + identityID }
