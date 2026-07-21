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

type serveComponentResult struct {
	name string
	err  error
}

func runServeComponents(
	parent context.Context,
	state *readiness.Snapshot,
	listener net.Listener,
	server *http.Server,
	scheduler schedulerLifecycle,
	drain func(),
) error {
	if state == nil || listener == nil || server == nil || scheduler == nil {
		return errors.New("serve lifecycle: required component is nil")
	}

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan serveComponentResult, 2)
	go func() {
		state.MarkListenerRunning()
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) && state.IsDraining() {
			err = nil
		} else if err == nil {
			err = errors.New("http serve loop stopped without an error")
		}
		if err != nil {
			state.MarkListenerFailure(err)
			err = fmt.Errorf("agui listener stopped: %w", err)
		}
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

	consumed := 0
	var lifecycleErr error
	select {
	case <-parent.Done():
	case result := <-results:
		consumed++
		lifecycleErr = result.err
		if lifecycleErr == nil && parent.Err() == nil {
			lifecycleErr = fmt.Errorf("%s stopped unexpectedly", result.name)
			if result.name == "scheduler" {
				state.MarkSchedulerFailure(lifecycleErr)
			} else {
				state.MarkListenerFailure(lifecycleErr)
			}
		}
	}

	cancel()
	state.MarkDraining()
	if drain != nil {
		drain()
	}
	for consumed < 2 {
		result := <-results
		consumed++
		if lifecycleErr == nil && result.err != nil {
			lifecycleErr = result.err
		}
	}
	return lifecycleErr
}
