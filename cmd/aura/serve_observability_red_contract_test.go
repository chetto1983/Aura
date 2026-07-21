package main

// Test-only RED contract. Task 1 GREEN replaces these deliberately failing
// lifecycle shims with production implementations in serve_lifecycle.go.

import (
	"context"
	"errors"
	"net"
	"net/http"

	"github.com/chetto1983/aura/internal/readiness"
)

var errMetricsLifecycleNotImplemented = errors.New("private metrics lifecycle not implemented")

type privateMetricsComponent struct {
	listener net.Listener
	server   *http.Server
}

func bindPrivateMetricsListener(string, listenFunc) (net.Listener, error) {
	return nil, errMetricsLifecycleNotImplemented
}

func newPrivateMetricsServer(string, http.Handler) *http.Server {
	return &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
}

func runServeComponentsWithMetrics(
	context.Context,
	*readiness.Snapshot,
	net.Listener,
	*http.Server,
	schedulerLifecycle,
	*privateMetricsComponent,
	func(),
) error {
	return errMetricsLifecycleNotImplemented
}
