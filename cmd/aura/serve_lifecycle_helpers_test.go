package main

import (
	"context"
	"net"
	"net/http"

	"github.com/chetto1983/aura/internal/readiness"
)

// Helpers that used to live in production code with no production caller.
// They are test-only conveniences over the WithX form the runtime actually
// calls, so they belong here — not in the binary.

func runServeComponents(
	parent context.Context,
	state *readiness.Snapshot,
	listener net.Listener,
	server *http.Server,
	scheduler schedulerLifecycle,
	drain func(),
) error {
	return runServeComponentsWithMetrics(parent, state, listener, server, scheduler, nil, drain)
}
