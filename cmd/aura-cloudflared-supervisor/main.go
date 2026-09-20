package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/cloudflaresupervisor"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(run(ctx, os.Args[1:]))
}

func run(ctx context.Context, args []string) int {
	if len(args) == 2 && args[0] == "healthcheck" {
		if cloudflaresupervisor.Healthcheck(ctx, args[1]) { // #nosec G602 -- enclosing condition establishes exactly two arguments.
			return 0
		}
		return 1
	}
	if len(args) != 0 {
		return 2
	}
	s := cloudflaresupervisor.NewSupervisor("/state", cloudflaresupervisor.ProcessLauncher{}, cloudflaresupervisor.Options{})
	return serve(ctx, "0.0.0.0:8085", s)
}

func serve(ctx context.Context, address string, s *cloudflaresupervisor.Supervisor) int {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx) }()
	server := &http.Server{Addr: address, Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.ListenAndServe() }()
	code := 0
	select {
	case <-ctx.Done():
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			code = 1
		}
	}
	cancel()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
	<-done
	return code
}
