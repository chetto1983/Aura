package usersandbox

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func hasIdentityCacheMounts(actual []container.MountPoint, identityID string) bool {
	for _, want := range identityCacheMounts(identityID) {
		if !slices.ContainsFunc(actual, func(got container.MountPoint) bool {
			return got.Type == want.Type && got.Name == want.Source && got.Destination == want.Target && got.RW
		}) {
			return false
		}
	}
	return true
}

// staleBoxReason says why an existing box must be recreated, or "" when it is current. Docker
// cannot change a container's mounts, host config, hostname or image in place, so a box built
// before any of them changed keeps the old ones forever unless Resolve replaces it. Measured
// 2026-09-26: a box created before the browser relay existed kept serving that image, and boxes
// created before Init was pinned kept piling up zombies. wantImageID "" means the configured
// image is not present locally; that is never a reason to recreate, because Resolve must not pull.
func staleBoxReason(ins container.InspectResponse, identityID, wantImageID string) string {
	switch {
	case !hasIdentityCacheMounts(ins.Mounts, identityID):
		return "non-private cache mounts"
	case ins.HostConfig == nil || ins.HostConfig.Init == nil || !*ins.HostConfig.Init:
		return "no init to reap orphans"
	case ins.Config == nil || ins.Config.Hostname != boxHostname:
		return "a hostname that changes on recreate"
	case wantImageID != "" && ins.Image != wantImageID:
		return "an older image"
	}
	return ""
}

// reconcileBox keeps a current box and removes a stale one, returning "" so Resolve creates it
// again. The workspace and cache volumes survive; the container layer, its running processes
// and the egress netns do not, which is why this happens only when something really changed.
func (b *DockerBackend) reconcileBox(ctx context.Context, containerID string, spec SandboxSpec) (string, error) {
	ins, err := b.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	wantImageID := ""
	if img, err := b.cli.ImageInspect(ctx, spec.Image); err == nil {
		wantImageID = img.ID
	}
	reason := staleBoxReason(ins.Container, spec.IdentityID, wantImageID)
	if reason == "" {
		return containerID, nil
	}
	// Named by the container Docker reports (aura-box-<identity>), not by spec.IdentityID, which
	// CodeQL traces from a credential-bearing value into this log (code-scanning alert 188).
	slog.Info("sandbox: recreating a stale box, volumes kept", "container", ins.Container.Name, "reason", reason)
	if err := b.teardownEgress(ctx, spec.IdentityID); err != nil {
		return "", fmt.Errorf("remove old egress sidecar: %w", err)
	}
	if _, err := b.cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true}); ignoreNotFound(err) != nil {
		return "", fmt.Errorf("remove box with %s: %w", reason, err)
	}
	return "", nil
}
