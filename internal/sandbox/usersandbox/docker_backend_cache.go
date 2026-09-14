package usersandbox

import (
	"context"
	"fmt"
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

// Docker cannot replace a container's mounts. Keep its workspace, discard the old
// container and egress netns, and let Resolve create a box with private empty caches.
func (b *DockerBackend) reconcileCacheMounts(ctx context.Context, containerID, identityID string) (string, error) {
	ins, err := b.cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", err
	}
	if hasIdentityCacheMounts(ins.Container.Mounts, identityID) {
		return containerID, nil
	}
	if err := b.teardownEgress(ctx, identityID); err != nil {
		return "", fmt.Errorf("remove old egress sidecar: %w", err)
	}
	if _, err := b.cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{Force: true}); ignoreNotFound(err) != nil {
		return "", fmt.Errorf("remove box with non-private caches: %w", err)
	}
	return "", nil
}
