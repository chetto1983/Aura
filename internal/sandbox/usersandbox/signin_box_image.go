package usersandbox

import (
	"context"
	"errors"
	"fmt"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/client"
)

// SignInImageState checks availability without starting the first-use download.
func (b *DockerBackend) SignInImageState(ctx context.Context, image string) (bool, error) {
	if image == "" {
		return false, errors.New("sandbox: sign-in image is not configured")
	}
	_, err := b.cli.ImageInspect(ctx, image)
	if cerrdefs.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

// PullSignInImage uses the SDK's stream and counts compressed download layers once.
func (b *DockerBackend) PullSignInImage(ctx context.Context, image string, progress func(int64, int64)) error {
	response, err := b.cli.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return fmt.Errorf("pull sign-in image: %w", err)
	}
	type layerProgress struct{ done, total int64 }
	layers := map[string]layerProgress{}
	for message, err := range response.JSONMessages(ctx) {
		if err != nil {
			return fmt.Errorf("pull sign-in image: %w", err)
		}
		if message.Error != nil {
			return fmt.Errorf("pull sign-in image: %w", message.Error)
		}
		if message.Status != "Downloading" && message.Status != "Download complete" {
			continue
		}
		layer := layers[message.ID]
		if message.Progress != nil {
			layer.total = max(layer.total, message.Progress.Total)
			layer.done = max(layer.done, message.Progress.Current)
		}
		if message.Status == "Download complete" {
			layer.done = layer.total
		}
		layers[message.ID] = layer
		var done, total int64
		for _, value := range layers {
			done += value.done
			total += value.total
		}
		if progress != nil {
			progress(done, total)
		}
	}
	return ctx.Err()
}
