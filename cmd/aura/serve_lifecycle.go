package main

import (
	"context"
	"net"
	"net/http"

	"github.com/chetto1983/aura/internal/readiness"
)

type listenFunc func(network, address string) (net.Listener, error)

type schedulerLifecycle interface {
	Start(context.Context) error
}

func bindServeListener(string, listenFunc) (net.Listener, error) { return nil, nil }

func runServeComponents(
	context.Context,
	*readiness.Snapshot,
	net.Listener,
	*http.Server,
	schedulerLifecycle,
	func(),
) error {
	return nil
}
