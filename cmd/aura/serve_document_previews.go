package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/boundedbuffer"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

type craftPreviewSandbox struct{ *usersandbox.SandboxRouter }

func (b craftPreviewSandbox) RunPreview(ctx context.Context, h usersandbox.BoxHandle, req usersandbox.ExecRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	output := boundedbuffer.New(64 << 10)
	process, err := b.ExecStream(ctx, h, req, nil, output)
	if err != nil {
		return nil, previewExitError(ctx, err)
	}
	stop := context.AfterFunc(ctx, process.Kill)
	defer stop()
	code, err := process.Wait()
	if err := previewExitError(ctx, err); err != nil {
		return nil, err
	}
	if code == 127 {
		return nil, documents.ErrPreviewUnavailable
	}
	if code != 0 {
		return nil, documents.ErrPreviewInvalid
	}
	return []byte(output.String()), nil
}

func previewExitError(ctx context.Context, err error) error {
	if errors.Is(err, usersandbox.ErrExecUnconfirmed) {
		return errors.Join(documents.ErrPreviewUnavailable, err)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("%w: native process", documents.ErrPreviewUnavailable)
	}
	return nil
}
