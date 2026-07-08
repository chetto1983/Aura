// docker_backend_lifecycle.go implements the Backend E2B lifecycle verbs over moby: Resolve
// (idempotent VolumeCreate + get-or-create container + transparent Resume + materialize),
// Suspend (ContainerStop, volume retained), Resume (ContainerStart, same container/volume),
// and Stop (ContainerRemove + per-identity VolumeRemove — never the shared uv/npm/pip caches). The
// box is created with AutoRemove:false (via toHostConfig) so a Suspend does not destroy it
// (Pitfall 5).

package usersandbox

import (
	"context"
	"fmt"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Resolve is idempotent get-or-create for the identity's box: it ensures the per-identity
// workspace volume and the shared uv / npm / pip warm-cache volumes exist (VolumeCreate is
// idempotent by name), then gets-or-creates the container named aura-box-<identityID>. An already-existing
// Suspended box is transparently started (Resume, D-08) rather than re-created. After the box
// is live it materializes the identity's skills / Agent.md / pyscripts into the workspace
// volume (D-10) at BOTH create and resume; a materialize failure fails Resolve closed.
func (b *DockerBackend) Resolve(ctx context.Context, spec SandboxSpec) (BoxHandle, error) {
	name := boxName(spec.IdentityID)
	if spec.WorkspaceVol == "" {
		spec.WorkspaceVol = name
	}
	if spec.Image == "" {
		spec.Image = b.image
	}
	if err := b.ensureVolume(ctx, name); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: ensure workspace volume %q: %w", name, err)
	}
	if err := b.ensureVolume(ctx, uvCacheVolume); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: ensure uv-cache volume: %w", err)
	}
	if err := b.ensureVolume(ctx, npmCacheVolume); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: ensure npm-cache volume: %w", err)
	}
	if err := b.ensureVolume(ctx, pipCacheVolume); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: ensure pip-cache volume: %w", err)
	}

	existing, err := b.findBox(ctx, name)
	if err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: find box %q: %w", name, err)
	}
	var h BoxHandle
	if existing != "" {
		// Transparent Resume of a Suspended (or otherwise stopped) box — D-08.
		if err := b.ensureRunning(ctx, existing); err != nil {
			return BoxHandle{}, fmt.Errorf("resolve: resume box %q: %w", name, err)
		}
		h = BoxHandle{ContainerID: existing, IdentityID: spec.IdentityID}
	} else {
		id, cerr := b.createBox(ctx, name, spec)
		if cerr != nil {
			return BoxHandle{}, fmt.Errorf("resolve: create box %q: %w", name, cerr)
		}
		h = BoxHandle{ContainerID: id, IdentityID: spec.IdentityID}
	}

	// Materialize skills / Agent.md / pyscripts into the box (create + resume, D-10). This is
	// the MaterializeIn call site; it fails Resolve closed so the box never runs a tool
	// against a stale/missing skill set.
	if err := b.materializeInputs(ctx, h); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: materialize inputs: %w", err)
	}

	// Launch the always-on egress sidecar sharing the box netns (SBX-04, D-07), AFTER the box
	// is live so the "container:<box>" netns target exists. It is a no-op when egress is
	// disabled (no WithEgress). A create/resume that cannot enforce the tenancy floor fails
	// Resolve CLOSED — a networked box must never run without its egress boundary.
	if err := b.launchEgress(ctx, spec, h); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: launch egress sidecar: %w", err)
	}
	return h, nil
}

// Suspend stops the box but RETAINS the container and its volume (OperatingMode:Suspended,
// D-08) so a later Resolve/Resume is fast and lossless. The egress sidecar is stopped in
// lockstep (retained, not removed) so it never outlives the box netns as an orphan.
func (b *DockerBackend) Suspend(ctx context.Context, h BoxHandle) error {
	if err := b.suspendEgress(ctx, h.IdentityID); err != nil {
		return fmt.Errorf("suspend box: %w", err)
	}
	if _, err := b.cli.ContainerStop(ctx, h.ContainerID, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("suspend box: %w", err)
	}
	return nil
}

// Resume restarts a Suspended box against its retained volume (same container), then restarts
// the retained egress sidecar so the box comes back WITH its tenancy floor re-applied.
func (b *DockerBackend) Resume(ctx context.Context, h BoxHandle) error {
	if _, err := b.cli.ContainerStart(ctx, h.ContainerID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("resume box: %w", err)
	}
	if err := b.resumeEgress(ctx, h.IdentityID); err != nil {
		return fmt.Errorf("resume box: %w", err)
	}
	return nil
}

// Stop destroys the box AND its per-identity volume (ShutdownPolicy:Delete, D-08) — only on
// explicit deprovision. The egress sidecar is removed first (no orphan), then the box, then
// the per-identity volume. The shared aura-uv-cache / aura-npm-cache / aura-pip-cache volumes
// are deliberately left intact.
func (b *DockerBackend) Stop(ctx context.Context, h BoxHandle) error {
	if err := b.teardownEgress(ctx, h.IdentityID); err != nil {
		return fmt.Errorf("stop: remove egress sidecar: %w", err)
	}
	if _, err := b.cli.ContainerRemove(ctx, h.ContainerID, client.ContainerRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("stop: remove box: %w", err)
	}
	if _, err := b.cli.VolumeRemove(ctx, boxName(h.IdentityID), client.VolumeRemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("stop: remove workspace volume: %w", err)
	}
	return nil
}

// materializeInputs copies the identity's skills / Agent.md / pyscripts into the box volume
// at create and resume (D-10) by delegating to the MaterializeIn tar-stream helper. With no
// SourceResolver wired (or no sources for the identity) it is a no-op and Resolve still
// succeeds; otherwise a materialize error propagates so Resolve fails closed.
func (b *DockerBackend) materializeInputs(ctx context.Context, h BoxHandle) error {
	if b.sources == nil {
		return nil
	}
	srcs := b.sources(h.IdentityID)
	if len(srcs) == 0 {
		return nil
	}
	return MaterializeIn(ctx, b.cli, h, srcs)
}

// ensureVolume creates a named volume, idempotently: Docker's volume create returns the
// existing volume when the name already exists, so a second Resolve does not error.
func (b *DockerBackend) ensureVolume(ctx context.Context, name string) error {
	if _, err := b.cli.VolumeCreate(ctx, client.VolumeCreateOptions{Name: name}); err != nil {
		return err
	}
	return nil
}

// findBox returns the container ID of the box named name, or "" if none exists. It lists all
// containers (including stopped/Suspended ones) filtered by exact name.
func (b *DockerBackend) findBox(ctx context.Context, name string) (string, error) {
	res, err := b.cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: make(client.Filters).Add("name", "^/"+name+"$"),
	})
	if err != nil {
		return "", err
	}
	for _, c := range res.Items {
		for _, n := range c.Names {
			if n == "/"+name {
				return c.ID, nil
			}
		}
	}
	return "", nil
}

// ensureRunning starts the container unless it is already running (the transparent-resume
// leg of Resolve). A running box is a no-op, so a second Resolve is idempotent.
func (b *DockerBackend) ensureRunning(ctx context.Context, containerID string) error {
	ins, err := b.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	if ins.Container.State != nil && ins.Container.State.Running {
		return nil
	}
	if _, err := b.cli.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		return err
	}
	return nil
}

// createBox ensures the image is present, creates the container from toHostConfig (the single
// pinned-safe HostConfig — SBX-02) with the idle keep-alive command, and starts it. It never
// sets AutoRemove (toHostConfig pins it false) so the box is suspendable.
func (b *DockerBackend) createBox(ctx context.Context, name string, spec SandboxSpec) (string, error) {
	if err := b.ensureImage(ctx, spec.Image); err != nil {
		return "", err
	}
	res, err := b.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Image:      spec.Image,
			Cmd:        keepAliveCmd,
			WorkingDir: workspaceTarget,
		},
		HostConfig: toHostConfig(spec),
	})
	if err != nil {
		return "", err
	}
	if _, err := b.cli.ContainerStart(ctx, res.ID, client.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("start new box: %w", err)
	}
	return res.ID, nil
}

// ensureImage pulls the box image only when it is not already present locally. This keeps a
// locally-built, registry-less image (e.g. aura-sandbox:latest) working — ImageInspect finds
// it and no pull is attempted — while a remote test image is pulled on first use.
func (b *DockerBackend) ensureImage(ctx context.Context, ref string) error {
	if _, err := b.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	}
	resp, err := b.cli.ImagePull(ctx, ref, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer func() { _ = resp.Close() }()
	if err := resp.Wait(ctx); err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	return nil
}
