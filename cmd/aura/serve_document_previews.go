package main

import (
	"context"
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
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: start native process", documents.ErrPreviewUnavailable)
	}
	stop := context.AfterFunc(ctx, process.Kill)
	defer stop()
	code, err := process.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("%w: wait native process", documents.ErrPreviewUnavailable)
	}
	if code == 127 {
		return nil, documents.ErrPreviewUnavailable
	}
	if code != 0 {
		return nil, documents.ErrPreviewInvalid
	}
	return []byte(output.String()), nil
}
