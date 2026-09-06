// docker_backend.go is the FIRST moby/moby/client v0.4.1 caller in the repo: DockerBackend
// implements the Backend E2B seam (backend.go) over the Docker Engine API. It owns the
// moby client handle, the fat box image ref, the cgroup caps, and the egress-sidecar image
// ref + launch/teardown (SBX-04, D-07); the per-verb lifecycle (Resolve/Suspend/Resume/Stop)
// lives in docker_backend_lifecycle.go and Exec in docker_backend_exec.go. The dangerous
// HostConfig fields are pinned safe in translate.go (SBX-02) — this file never sets them
// directly, and the sidecar it launches is the ONLY container that carries CAP_NET_ADMIN.

package usersandbox

import (
	"context"
	"fmt"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

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
// For skills the Dest is skills.InSandboxSkillsRoot ("/skills") so the in-box path equals
// the one SnippetSandboxPath renders.
type MaterializeSource struct {
	HostDir string
	Dest    string
	// SkipOnFault makes THIS source individually skippable: a host tree that cannot be
	// staged (a symlink, a non-regular file, an unreadable entry) is dropped with a log line
	// and the rest of the box is materialized anyway.
	//
	// It is set for a SHARED source and for nothing else, and the asymmetry is the whole
	// point. A fault inside your OWN tree — or inside the deployment's — is a fault you must
	// see, and failing Resolve closed is how you see it. A fault inside SOMEBODY ELSE'S tree
	// must not cost you your box: a malformed skill one person shared would otherwise deny
	// every grantee the box through which they reach all of their own tools, which is the
	// opposite of the degrade-don't-deny policy the ACL already takes (amendment #217,
	// D-217-4). Skipping is also the safe direction — the mirror then removes a body it can
	// no longer justify instead of leaving it behind.
	SkipOnFault bool
}

// SourceResolver returns the materialization sources for one identity (host dirs mapped to
// their in-box roots; today the composition root supplies the skills trees). It is injected
// via WithMaterializeSources so the backend stays decoupled from config path resolution and
// the docker_integration tests can seed their own fixtures. A nil resolver (or empty result)
// means nothing is materialized — Resolve still succeeds.
//
// It takes a ctx and may fail because the answer is no longer a pure function of the
// identity: since amendment #214 criterion 5 what belongs in a box depends on which grants
// stand right now, which is a query. An error fails Resolve CLOSED — a box filled from a
// half-known answer is the one outcome this seam must not produce.
type SourceResolver func(ctx context.Context, identityID string) ([]MaterializeSource, error)

// DockerBackend implements Backend over the moby/moby/client Docker Engine API. It is the
// per-identity box runtime every routed tool ultimately execs into.
type DockerBackend struct {
	cli     *client.Client
	image   string
	limits  Resources
	sources SourceResolver
	// egressImage is the aura-egress sidecar image ref. Empty (the default) DISABLES the
	// sidecar entirely — a dev/local backend or the 37-04 lifecycle tests run box-only. The
	// composition root wires WithEgress under the strict profiles so every networked box
	// gets the always-on tenancy floor (D-07).
	egressImage string
	// spoolDir is where MaterializeIn builds its per-source tars. Empty means os.TempDir().
	spoolDir string
}

// Option configures a DockerBackend at construction (functional-options, mirroring the moby
// client's own New) so the documented 3-arg NewDockerBackend call stays valid.
type Option func(*DockerBackend)

// WithMaterializeSources wires the per-identity source resolver Resolve consults to
// materialize the resolver's sources into the box at create and resume (D-10).
func WithMaterializeSources(r SourceResolver) Option {
	return func(b *DockerBackend) { b.sources = r }
}

// WithEgress enables the always-on per-box egress sidecar (SBX-04, D-07) using image as the
// aura-egress ref (docker/aura-egress/Dockerfile; a production deployment passes a
// digest-pinned registry ref). Without this option the backend runs box-only (no sidecar),
// which is the dev/local posture and what the 37-04 lifecycle tests rely on.
func WithEgress(image string) Option {
	return func(b *DockerBackend) { b.egressImage = image }
}

// WithSpoolDir names the directory the per-source tars are built in before they are streamed
// to the daemon. Empty (the default) means os.TempDir().
//
// The composition root passes the run dir's tmp subdir, and that is not a filing preference:
// this deployment mounts /tmp as a 64 MiB tmpfs (compose.yaml), so spooling there would be
// both a hard size ceiling and no memory relief at all — a tmpfs IS memory, which is the exact
// cost the spooling exists to remove. The run dir is the transient-artifact volume the skills
// installer's work tree already uses for the same reason, and its tmp subdir is the half of it
// that already has a 24h janitor for whatever a SIGKILL leaves behind.
func WithSpoolDir(dir string) Option {
	return func(b *DockerBackend) { b.spoolDir = dir }
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

// EgressImage returns the wired aura-egress sidecar image ref (empty == sidecar disabled). It
// exposes the applied WithEgress option so the composition root's wiring is regression-testable
// without a daemon — a docker-free cmd/aura test asserts newSandboxBackend echoes
// cfg.Sandbox.EgressImage here — without leaking any other backend state.
func (b *DockerBackend) EgressImage() string { return b.egressImage }

// SpoolDir returns the wired materialize spool dir (empty == os.TempDir()). Like EgressImage
// it exists so the composition root's wiring is regression-testable without a daemon: WHERE
// the tars are built is a deployment fact — /tmp here is a 64 MiB tmpfs, and the run dir's tmp
// subdir is the one with a janitor — and a wiring that silently reverted to the default would
// look identical from the outside until a tree outgrew the tmpfs.
func (b *DockerBackend) SpoolDir() string { return b.spoolDir }

// boxName is the deterministic container + workspace-volume name for an identity.
func boxName(identityID string) string { return boxNamePrefix + identityID }

// egressNamePrefix is the per-identity egress-sidecar container name prefix. The sidecar
// name is derived from the identity id alone (egressName) so Suspend/Resume/Stop can find
// and manage it from a BoxHandle without the full spec.
const egressNamePrefix = "aura-egress-"

// egressName is the deterministic egress-sidecar container name for an identity.
func egressName(identityID string) string { return egressNamePrefix + identityID }

// suspendGraceSec is how long a Suspend waits for SIGTERM before the daemon SIGKILLs. Docker's
// default is 10s and BOTH stops in a Suspend pay it, so a suspend cost ~20s of pure waiting.
//
// Nothing in a box answers SIGTERM: the sandbox image's PID 1 is a plain `sleep`-style command
// with no handler, and PID 1 in a namespace ignores signals it has not installed one for — so
// the full grace elapses every time and the daemon SIGKILLs anyway. The wait buys nothing and is
// measurable: 10.26s at the default vs 2.22s at 2s, on a native-Linux daemon.
//
// Short is also SAFE here, not merely fast: a box is only suspended after IdleTTLSec of no
// activity, so by construction there is no in-flight work to flush, and the workspace is a
// volume whose writes already landed.
const suspendGraceSec = 2

// stopOptions is the shared ContainerStop option set for the suspend path (box + sidecar), so
// the two halves of a Suspend cannot drift to different grace periods.
func stopOptions() client.ContainerStopOptions {
	grace := suspendGraceSec
	return client.ContainerStopOptions{Timeout: &grace}
}

// findEgress returns the sidecar container id for an identity, or "" when egress is disabled
// (no image wired) or no sidecar exists. It is the single find-by-name used by every egress
// lifecycle hook.
func (b *DockerBackend) findEgress(ctx context.Context, identityID string) (string, error) {
	if b.egressImage == "" {
		return "", nil
	}
	id, err := b.findBox(ctx, egressName(identityID))
	if err != nil {
		return "", fmt.Errorf("find egress sidecar for %q: %w", identityID, err)
	}
	return id, nil
}

// launchEgress get-or-creates and (re)starts the per-box egress sidecar (D-07), sharing the
// box netns via NetworkMode "container:<boxID>" with CAP_NET_ADMIN on the SIDECAR ONLY. It is
// a no-op when egress is disabled. An already-existing sidecar (across a suspend/resume) is
// simply (re)started against the retained box netns; a fresh box gets a fresh sidecar built
// from the box's CONFIGURED EgressPolicy. Called by Resolve AFTER the box is live so the
// netns target exists.
func (b *DockerBackend) launchEgress(ctx context.Context, spec SandboxSpec, h BoxHandle) error {
	if b.egressImage == "" {
		return nil
	}
	name := egressName(spec.IdentityID)
	if existing, err := b.findEgress(ctx, spec.IdentityID); err != nil {
		return err
	} else if existing != "" {
		return b.ensureRunning(ctx, existing)
	}

	sc, err := buildEgressSidecar(spec.Egress, h.ContainerID, spec.Runtime)
	if err != nil {
		return fmt.Errorf("build egress sidecar: %w", err)
	}
	sc.Image = b.egressImage
	if err := b.ensureImage(ctx, sc.Image); err != nil {
		return fmt.Errorf("ensure egress image %q: %w", sc.Image, err)
	}
	res, err := b.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:   name,
		Config: &container.Config{Image: sc.Image, Env: sc.Env},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode(sc.NetworkMode),
			CapAdd:      sc.CapAdd,
			AutoRemove:  false,
			Privileged:  false,
		},
	})
	if err != nil {
		return fmt.Errorf("create egress sidecar %q: %w", name, err)
	}
	if _, err := b.cli.ContainerStart(ctx, res.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start egress sidecar %q: %w", name, err)
	}
	return nil
}

// suspendEgress stops the sidecar in lockstep with the box (Suspend). The sidecar container is
// RETAINED (not removed) so Resume can restart it against the same box netns; Stop removes it.
func (b *DockerBackend) suspendEgress(ctx context.Context, identityID string) error {
	existing, err := b.findEgress(ctx, identityID)
	if err != nil || existing == "" {
		return err
	}
	if _, err := b.cli.ContainerStop(ctx, existing, stopOptions()); err != nil {
		return fmt.Errorf("suspend egress sidecar for %q: %w", identityID, err)
	}
	return nil
}

// resumeEgress restarts the retained sidecar after the box is back up (Resume). The sidecar
// re-applies the filter-table floor to the box's new netns on start.
func (b *DockerBackend) resumeEgress(ctx context.Context, identityID string) error {
	existing, err := b.findEgress(ctx, identityID)
	if err != nil || existing == "" {
		return err
	}
	return b.ensureRunning(ctx, existing)
}

// teardownEgress removes the sidecar with the box (Stop) — no orphan sidecar survives a box
// delete.
func (b *DockerBackend) teardownEgress(ctx context.Context, identityID string) error {
	existing, err := b.findEgress(ctx, identityID)
	if err != nil || existing == "" {
		return err
	}
	if _, err := b.cli.ContainerRemove(ctx, existing, client.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("remove egress sidecar for %q: %w", identityID, err)
	}
	return nil
}
