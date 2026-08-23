package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/readiness"
)

type listenFunc func(network, address string) (net.Listener, error)

type schedulerLifecycle interface {
	Start(context.Context) error
}

func shutdownBackgroundShells(env *serveEnv) {
	if env.toolHandles.BackgroundShells == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := env.toolHandles.BackgroundShells.Shutdown(ctx); err != nil {
		slog.Warn("aura serve: background shell shutdown", "err", err)
	}
}

func bindServeListener(address string, listen listenFunc) (net.Listener, error) {
	if listen == nil {
		return nil, errors.New("bind agui listener: listener factory is nil")
	}
	listener, err := listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("bind agui listener %s: %w", address, err)
	}
	return listener, nil
}

// bindMetricsListener binds the /metrics listener. It defaults to loopback (config.go)
// and WARNS on anything else, where it used to refuse outright.
//
// The refusal was checking the wrong thing. Inside a container the bind address says
// nothing about who can reach the socket: what decides exposure is whether the port is
// PUBLISHED, and that lives in compose, not here. Refusing 0.0.0.0 therefore blocked the
// ordinary, private arrangement — Prometheus in its own container on the project network
// — while doing nothing about the genuinely dangerous one, a published port, which this
// function cannot even see.
//
// The cost of that mistake was measured on 2026-08-13: to keep the listener on loopback
// the observability pack had to share aura's network namespace, and when the namespace
// owner was recreated the sharers stayed attached to a dead one. `up{job="aura"}` sat at
// 0 for five and a half hours with nobody informed (docker/compose#6626 — still open, no
// fix upstream). A guard that forces a fragile topology is not a safety feature.
//
// A non-loopback bind is still worth saying out loud, so it is logged at WARN with the
// one condition that makes it safe.
func bindMetricsListener(address string, listen listenFunc) (net.Listener, error) {
	if listen == nil {
		return nil, errors.New("bind metrics listener: listener factory is nil")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("bind metrics listener %s: %w", address, err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		slog.Warn("metrics listener is not loopback",
			"address", address,
			"safe_when", "the port is not published to the host and the network is private",
			"detail", "/metrics carries no credentials but does expose token counts, "+
				"tool names and error classes")
	}
	listener, err := listen("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("bind metrics listener %s: %w", address, err)
	}
	return listener, nil
}

func newMetricsServer(address string, handler http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", handler)
	return &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
}

type privateMetricsComponent struct {
	listener net.Listener
	server   *http.Server
}

type serveComponentResult struct {
	name string
	err  error
}

func runServeComponentsWithMetrics(
	parent context.Context,
	state *readiness.Snapshot,
	listener net.Listener,
	server *http.Server,
	scheduler schedulerLifecycle,
	metrics *privateMetricsComponent,
	drain func(),
) error {
	if state == nil || listener == nil || server == nil || scheduler == nil {
		return errors.New("serve lifecycle: required component is nil")
	}
	if metrics != nil && (metrics.listener == nil || metrics.server == nil) {
		return errors.New("serve lifecycle: private metrics component is incomplete")
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	componentCount := 2
	if metrics != nil {
		componentCount++
	}
	results := make(chan serveComponentResult, componentCount)
	go func() {
		var err error
		// Inner closure so the deferred boundary End records the terminal
		// transition BEFORE the result is delivered — otherwise the lifecycle
		// can return (and metric readers collect) ahead of the emission.
		func() {
			_, observeEnd := serveListenerBoundary.Start(ctx)
			defer observeEnd.PanicSafe(&err)
			state.MarkListenerRunning()
			err = server.Serve(listener)
			if errors.Is(err, http.ErrServerClosed) && state.IsDraining() {
				err = nil
			} else if err == nil {
				err = errors.New("http serve loop stopped without an error")
			}
			if err != nil {
				state.MarkListenerFailure(err)
				err = fmt.Errorf("agui listener stopped: %w", err)
			}
		}()
		results <- serveComponentResult{name: "listener", err: err}
	}()
	go func() {
		err := scheduler.Start(ctx)
		if err != nil {
			state.MarkSchedulerFailure(err)
			err = fmt.Errorf("scheduler stopped: %w", err)
		}
		results <- serveComponentResult{name: "scheduler", err: err}
	}()
	if metrics != nil {
		go func() {
			var err error
			// Same ordering guarantee as the listener goroutine above.
			func() {
				_, observeEnd := serveListenerBoundary.Start(ctx)
				defer observeEnd.PanicSafe(&err)
				err = metrics.server.Serve(metrics.listener)
				if errors.Is(err, http.ErrServerClosed) && state.IsDraining() {
					err = nil
				} else if err == nil {
					err = errors.New("private metrics serve loop stopped without an error")
				}
				if err != nil {
					err = fmt.Errorf("private metrics listener stopped: %w", err)
				}
			}()
			results <- serveComponentResult{name: "metrics", err: err}
		}()
	}

	consumed := 0
	var lifecycleErr error
	select {
	case <-parent.Done():
	case result := <-results:
		consumed++
		lifecycleErr = result.err
		if lifecycleErr == nil && parent.Err() == nil {
			lifecycleErr = fmt.Errorf("%s stopped unexpectedly", result.name)
			switch result.name {
			case "scheduler":
				state.MarkSchedulerFailure(lifecycleErr)
			case "listener":
				state.MarkListenerFailure(lifecycleErr)
			}
		}
	}

	cancel()
	state.MarkDraining()
	if drain != nil {
		drain()
	}
	for consumed < componentCount {
		result := <-results
		consumed++
		if lifecycleErr == nil && result.err != nil {
			lifecycleErr = result.err
		}
	}
	return lifecycleErr
}
