package main

import (
	"context"
	"io"
)

// Temporary RED shim. GREEN removes this file.
func prepareCLIIdempotency(ctx context.Context, args []string, _ io.Writer) (context.Context, []string, error) {
	return ctx, args, nil
}

func cliOperationFromContext(context.Context) (string, [32]byte, bool) {
	return "", [32]byte{}, false
}
