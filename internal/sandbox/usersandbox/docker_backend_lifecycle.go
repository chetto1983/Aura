// docker_backend_lifecycle.go implements the Backend E2B lifecycle verbs over moby: Resolve
// (idempotent VolumeCreate + get-or-create container + transparent Resume + materialize),
// Suspend (ContainerStop, volume retained), Resume (ContainerStart, same container/volume),
// and Stop (ContainerRemove + per-identity workspace/cache VolumeRemove). The
// box is created with AutoRemove:false (via toHostConfig) so a Suspend does not destroy it
// (Pitfall 5).

package usersandbox

import (
	"context"
	"errors"
	"fmt"
	"slices"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// Resolve is idempotent get-or-create for the identity's box: it ensures the per-identity
// workspace and uv / npm / pip warm-cache volumes exist (VolumeCreate is
// idempotent by name), then gets-or-creates the container named aura-box-<identityID>. An already-existing
// Suspended box is transparently started (Resume, D-08) rather than re-created. After the box
// is live it materializes the resolver's sources for the identity into the workspace
// volume (D-10) at BOTH create and resume; a materialize failure fails Resolve closed.
func (b *DockerBackend) Resolve(ctx context.Context, spec SandboxSpec) (BoxHandle, error) {
	if spec.Kind != Shell && spec.Kind != Browser {
		return BoxHandle{}, fmt.Errorf("invalid box kind")
	}
	defer b.identities.Lock(spec.IdentityID)()
	if spec.Image == "" {
		spec.Image = b.image
	}
	if spec.Kind == Browser {
		if spec.Limits == (Resources{}) {
			spec.Limits = b.browserLimits
		}
		spec.Limits = browserResources(spec.Limits)
	}
	h, started, err := b.startBox(ctx, spec)
	if err != nil {
		return BoxHandle{}, err
	}

	// A failed step stops the box only when it may have left the box less secured than this
	// call found it: when this call started it, or when the egress check failed on its own. A
	// box found running was secured by the Resolve that started it, and a caller that gives up,
	// as download collection does on a spent tool deadline, must not stop a working browser
	// (final review, 2026-10-15).
	secured := false
	var egressErr error
	defer func() {
		if secured || (!started && (egressErr == nil || ctx.Err() != nil)) {
			return
		}
		_ = b.suspendEgress(context.WithoutCancel(ctx), sidecarIdentity(h.IdentityID, h.Kind))
		_, _ = b.cli.ContainerStop(context.WithoutCancel(ctx), h.ContainerID, stopOptions())
	}()
	// Materialize the resolver's sources into the box (create + resume, D-10). This is
	// the MaterializeIn call site; it fails Resolve closed so the box never runs a tool
	// against a stale/missing skill set.
	if err := b.materializeInputs(ctx, h); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: materialize inputs: %w", err)
	}
	if err := b.writeBoxFiles(ctx, h); err != nil {
		return BoxHandle{}, fmt.Errorf("resolve: write box files: %w", err)
	}

	// Launch the always-on egress sidecar sharing the box netns (SBX-04, D-07), AFTER the box
	// is live so the "container:<box>" netns target exists. It is a no-op when egress is
	// disabled (no WithEgress). A create/resume that cannot enforce the tenancy floor fails
	// Resolve CLOSED — a networked box must never run without its egress boundary.
	if egressErr = b.launchEgress(ctx, spec, h); egressErr != nil {
		return BoxHandle{}, fmt.Errorf("resolve: launch egress sidecar: %w", egressErr)
	}
	secured = true
	return h, nil
}

// startBox ensures the box's volumes and gets the box running, and reports whether it started
// it. Starting a browser box takes no admission slot: a slot is a Chrome, and AdmitBrowser asks
// for one before the browser call that can open it.
func (b *DockerBackend) startBox(ctx context.Context, spec SandboxSpec) (BoxHandle, bool, error) {
	name := workspaceName(spec.IdentityID, spec.Kind)
	if err := b.ensureVolume(ctx, name); err != nil {
		return BoxHandle{}, false, fmt.Errorf("resolve: ensure workspace volume %q: %w", name, err)
	}
	caches := identityCacheMounts(spec.IdentityID)
	if spec.Kind == Browser {
		caches = nil
	}
	for _, cache := range caches {
		if err := b.ensureVolume(ctx, cache.Source); err != nil {
			return BoxHandle{}, false, fmt.Errorf("resolve: ensure cache volume %q: %w", cache.Source, err)
		}
	}
	if spec.Kind == Shell {
		if err := b.sweepLegacyBrowserHome(ctx, spec.IdentityID); err != nil {
			return BoxHandle{}, false, fmt.Errorf("resolve: remove the legacy browser home: %w", err)
		}
	}

	existing, err := b.findBox(ctx, name)
	if err != nil {
		return BoxHandle{}, false, fmt.Errorf("resolve: find box %q: %w", name, err)
	}
	if existing != "" {
		existing, err = b.reconcileBox(ctx, existing, spec)
		if err != nil {
			return BoxHandle{}, false, fmt.Errorf("resolve: reconcile box %q: %w", name, err)
		}
	}
	h := BoxHandle{ContainerID: existing, IdentityID: spec.IdentityID, Kind: spec.Kind}
	if existing != "" {
		// Transparent Resume of a Suspended (or otherwise stopped) box — D-08.
		started, err := b.ensureRunning(ctx, existing)
		if err != nil {
			return BoxHandle{}, false, fmt.Errorf("resolve: resume box %q: %w", name, err)
		}
		return h, started, nil
	}
	if h.ContainerID, err = b.createBox(ctx, name, spec); err != nil {
		return BoxHandle{}, false, fmt.Errorf("resolve: create box %q: %w", name, err)
	}
	return h, true, nil
}

// Suspend stops the box but RETAINS the container and its volume (OperatingMode:Suspended,
// D-08) so a later Resolve/Resume is fast and lossless. The egress sidecar is stopped in
// lockstep (retained, not removed) so it never outlives the box netns as an orphan.
func (b *DockerBackend) Suspend(ctx context.Context, h BoxHandle) error {
	defer b.identities.Lock(h.IdentityID)()
	if err := b.suspendEgress(ctx, sidecarIdentity(h.IdentityID, h.Kind)); err != nil {
		return fmt.Errorf("suspend box: %w", err)
	}
	if _, err := b.cli.ContainerStop(ctx, h.ContainerID, stopOptions()); err != nil {
		return fmt.Errorf("suspend box: %w", err)
	}
	return nil
}

// Resume restarts a Suspended box against its retained volume (same container), then restarts
// the retained egress sidecar so the box comes back WITH its tenancy floor re-applied.
func (b *DockerBackend) Resume(ctx context.Context, h BoxHandle) error {
	defer b.identities.Lock(h.IdentityID)()
	if _, err := b.cli.ContainerStart(ctx, h.ContainerID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("resume box: %w", err)
	}
	if err := b.resumeEgress(ctx, sidecarIdentity(h.IdentityID, h.Kind)); err != nil {
		_ = b.suspendEgress(context.WithoutCancel(ctx), sidecarIdentity(h.IdentityID, h.Kind))
		_, _ = b.cli.ContainerStop(context.WithoutCancel(ctx), h.ContainerID, stopOptions())
		return fmt.Errorf("resume box: %w", err)
	}
	return nil
}

// Stop destroys the box AND its per-identity volume (ShutdownPolicy:Delete, D-08) — only on
// explicit deprovision. The egress sidecar is removed first (no orphan), then the box, then
// the per-identity workspace and cache volumes.
func (b *DockerBackend) Stop(ctx context.Context, h BoxHandle) error {
	defer b.identities.Lock(h.IdentityID)()
	return b.stop(ctx, h)
}

func (b *DockerBackend) stop(ctx context.Context, h BoxHandle) error {
	if err := b.teardownEgress(ctx, sidecarIdentity(h.IdentityID, h.Kind)); err != nil {
		return fmt.Errorf("stop: remove egress sidecar: %w", err)
	}
	if _, err := b.cli.ContainerRemove(ctx, h.ContainerID, client.ContainerRemoveOptions{Force: true}); ignoreNotFound(err) != nil {
		return fmt.Errorf("stop: remove box: %w", err)
	}
	if _, err := b.cli.VolumeRemove(ctx, workspaceName(h.IdentityID, h.Kind), client.VolumeRemoveOptions{Force: true}); ignoreNotFound(err) != nil {
		return fmt.Errorf("stop: remove workspace volume: %w", err)
	}
	if h.Kind == Browser {
		return nil
	}
	for _, cache := range identityCacheMounts(h.IdentityID) {
		if _, err := b.cli.VolumeRemove(ctx, cache.Source, client.VolumeRemoveOptions{}); ignoreNotFound(err) != nil {
			return fmt.Errorf("stop: remove cache volume %q: %w", cache.Source, err)
		}
	}
	return nil
}

// ignoreNotFound collapses "that object is already gone" into success and leaves every other
// failure exactly as it was.
//
// It is what makes Stop match the contract agui.SandboxPurger states it relies on
// ("destroying an absent box converges"). It did not, and the gap was not theoretical:
// measured 2026-09-08, purging five identities whose boxes had already been removed failed
// on `stop: remove box: No such container`. The sandbox leg runs FIRST in the reverse saga
// and a failed step is never journalled, so one absent container made every later plane —
// memory, object store, dirs — permanently unreachable for that identity.
//
// The distinction is the same one sandboxPurgerFor's comment draws for a backendless router:
// "we could not check" and "there is nothing there" are different facts, and only one of them
// is safe to treat as done. A 404 from the daemon is the second; an unreachable daemon is the
// first and still fails. cerrdefs.IsNotFound covers both shapes moby produces — the sentinel
// an HTTP 404 maps to via errhttp.ToNative, and the NotFound() marker its unexported
// objectNotFoundError carries.
func ignoreNotFound(err error) error {
	if err == nil || cerrdefs.IsNotFound(err) {
		return nil
	}
	return err
}

// materializeInputs copies the resolver's sources for the identity into the box volume
// at create and resume (D-10) by delegating to the MaterializeIn tar-stream helper. With no
// SourceResolver wired (or no sources for the identity) it is a no-op and Resolve still
// succeeds; a resolver error and a materialize error both propagate so Resolve fails closed.
// writeBoxFiles writes the identity's BoxFileSource files, at create and resume alike, so a
// recreated box gets them back without anything having been stored in it.
func (b *DockerBackend) writeBoxFiles(ctx context.Context, h BoxHandle) error {
	source := b.boxFiles
	if h.Kind == Browser {
		source = b.browserFiles
	}
	if source == nil {
		return nil
	}
	files, err := source(h.IdentityID)
	if err != nil {
		return fmt.Errorf("box files for %q: %w", h.IdentityID, err)
	}
	for _, f := range files {
		if err := CopyFileIn(ctx, b.cli, h, f.Path, f.Content, f.Mode); err != nil {
			return err
		}
	}
	return nil
}

func (b *DockerBackend) materializeInputs(ctx context.Context, h BoxHandle) error {
	if h.Kind == Browser {
		return nil
	}
	if b.sources != nil {
		srcs, err := b.sources(ctx, h.IdentityID)
		if err != nil {
			return fmt.Errorf("materialize sources for %q: %w", h.IdentityID, err)
		}
		if len(srcs) > 0 {
			if err := MaterializeIn(ctx, b.cli, h, srcs, b.spoolDir); err != nil {
				return err
			}
		}
	}
	return b.syncPlugins(ctx, h)
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
		if slices.Contains(c.Names, "/"+name) {
			return c.ID, nil
		}
	}
	return "", nil
}

// ensureRunning starts the container unless it is already running (the transparent-resume
// leg of Resolve) and reports whether it started it. A running box is a no-op, so a second
// Resolve is idempotent.
func (b *DockerBackend) ensureRunning(ctx context.Context, containerID string) (bool, error) {
	ins, err := b.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return false, err
	}
	if ins.Container.State != nil && ins.Container.State.Running {
		return false, nil
	}
	if _, err := b.cli.ContainerStart(ctx, containerID, client.ContainerStartOptions{}); err != nil {
		return false, err
	}
	return true, nil
}

// createBox ensures the image is present, creates the container from toHostConfig (the single
// pinned-safe HostConfig — SBX-02) with the idle keep-alive command, and starts it. It never
// sets AutoRemove (toHostConfig pins it false) so the box is suspendable. The hostname is
// boxHostname, not Docker's per-container id: a browser killed with its box leaves a profile
// lock naming the hostname, and Chrome refuses a profile locked from another host (prd.md §12,
// a persistent profile per browser session).
func (b *DockerBackend) createBox(ctx context.Context, name string, spec SandboxSpec) (string, error) {
	id, err := b.createStoppedBox(ctx, name, spec)
	if err != nil {
		return "", err
	}
	if _, err := b.cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("start new box: %w", err)
	}
	return id, nil
}

func (b *DockerBackend) createStoppedBox(ctx context.Context, name string, spec SandboxSpec) (string, error) {
	if err := b.ensureImage(ctx, spec.Image); err != nil {
		return "", err
	}
	res, err := b.cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: name,
		Config: &container.Config{
			Hostname:   boxHostname,
			Image:      spec.Image,
			Cmd:        keepAliveCmd,
			WorkingDir: workspaceTarget,
			Labels:     map[string]string{"aura.browser-isolation": "1"},
		},
		HostConfig: toHostConfig(spec),
	})
	if err != nil {
		return "", err
	}
	return res.ID, nil
}

// CheckRuntime answers the readiness question without creating anything: one ImageInspect on the
// box image. It fails when the daemon socket is unreachable (the call itself errors) and when the
// image is absent — the two conditions that make every subsequent tool call answer
// sandbox_unavailable. It never pulls, never creates a container or a volume, and never touches
// the reaper's lastUsed, so polling it on a healthcheck interval costs nothing and leaks nothing.
func (b *DockerBackend) CheckRuntime(ctx context.Context) error {
	if b == nil || b.cli == nil {
		return fmt.Errorf("docker client is not configured")
	}
	if _, err := b.cli.ImageInspect(ctx, b.image); err != nil {
		return fmt.Errorf("box image %q unavailable: %w", b.image, err)
	}
	return nil
}

// EnsureImage is the exported, box-free seam the boot preflight (D-03) calls to eagerly pull
// the configured box image before any tool call reaches the lazy first-use path (createBox).
// It is a one-line wrapper over the existing unexported ensureImage — the create-box path
// already calls the identical inspect/pull body, so this method must not duplicate it
// (CLAUDE.md REUSABLE CODE).
func (b *DockerBackend) EnsureImage(ctx context.Context) error {
	return b.ensureImage(ctx, b.image)
}

// EnsureEgressImage is the DISCRETIONARY egress-sidecar counterpart to EnsureImage. specFor
// (router.go) sets Egress.Floor:true on every strict-profile box, so an unreachable egress
// image produces the identical silent tool-surface failure EnsureImage targets for the box
// image — but D-03 names only AURA_SANDBOX_IMAGE as Fatal, so the boot preflight treats this
// as advisory (WARN), never Fatal. An empty egressImage (WithEgress("") never called) is a
// no-op success: no sidecar is wired, so there is nothing to ensure.
func (b *DockerBackend) EnsureEgressImage(ctx context.Context) error {
	if b.egressImage == "" {
		return nil
	}
	return b.ensureImage(ctx, b.egressImage)
}

// ErrImageUnavailable is an image its registry does not have, or will not hand over: the daemon
// answers "repository does not exist" and "denied" alike as not found (moby
// daemon/internal/distribution/errors.go). Pulling again gets the same answer.
var ErrImageUnavailable = errors.New("image not in its registry")

// ensureImage pulls the box image only when it is not already present locally. This keeps a
// locally-built, registry-less image (e.g. aura-sandbox:latest) working — ImageInspect finds
// it and no pull is attempted — while a remote test image is pulled on first use.
func (b *DockerBackend) ensureImage(ctx context.Context, ref string) error {
	if _, err := b.cli.ImageInspect(ctx, ref); err == nil {
		return nil
	}
	resp, err := b.cli.ImagePull(ctx, ref, client.ImagePullOptions{})
	if cerrdefs.IsNotFound(err) {
		return fmt.Errorf("pull image %q: %w: %w", ref, ErrImageUnavailable, err)
	}
	if err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	defer func() { _ = resp.Close() }()
	if err := resp.Wait(ctx); err != nil {
		return fmt.Errorf("pull image %q: %w", ref, err)
	}
	return nil
}
