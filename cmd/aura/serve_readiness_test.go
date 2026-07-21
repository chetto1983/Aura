package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/readiness"
)

type schedulerLifecycleFunc func(context.Context) error

func (f schedulerLifecycleFunc) Start(ctx context.Context) error { return f(ctx) }

type failingServeListener struct {
	once   sync.Once
	closed chan struct{}
}

func newFailingServeListener() *failingServeListener {
	return &failingServeListener{closed: make(chan struct{})}
}

func (l *failingServeListener) Accept() (net.Conn, error) {
	return nil, errors.New("listener accept sentinel")
}

func (l *failingServeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (*failingServeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

func TestServeListenerBindFailureIsSynchronous(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy listener: %v", err)
	}
	t.Cleanup(func() { _ = occupied.Close() })

	listener, err := bindServeListener(occupied.Addr().String(), net.Listen)
	if err == nil {
		if listener != nil {
			_ = listener.Close()
		}
		t.Fatal("address-in-use bind returned nil error")
	}
	if listener != nil {
		t.Fatal("bind failure returned a live listener")
	}
}

func TestServeListenerRuntimeFailureCancelsScheduler(t *testing.T) {
	snapshot := readiness.NewSnapshot(readiness.Config{MigrationCompatible: true, SchedulerEnabled: true})
	snapshot.MarkListenerBound()
	listener := newFailingServeListener()
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	schedulerCancelled := make(chan struct{})
	scheduler := schedulerLifecycleFunc(func(ctx context.Context) error {
		<-ctx.Done()
		close(schedulerCancelled)
		return nil
	})

	err := runServeComponents(context.Background(), snapshot, listener, srv, scheduler, func() {
		_ = srv.Shutdown(context.Background())
	})
	if err == nil || !strings.Contains(err.Error(), "listener accept sentinel") {
		t.Fatalf("runtime listener error = %v, want propagated sentinel", err)
	}
	select {
	case <-schedulerCancelled:
	case <-time.After(time.Second):
		t.Fatal("runtime listener failure did not cancel/join scheduler")
	}
	if !containsReadinessCode(snapshot.Reasons(), readiness.CodeListenerUnavailable) {
		t.Fatalf("runtime listener failure reasons = %v, want listener_unavailable", snapshot.Reasons())
	}
}

func TestServeListenerErrServerClosedIsCleanDuringDrain(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	snapshot := readiness.NewSnapshot(readiness.Config{MigrationCompatible: true})
	snapshot.MarkListenerBound()
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler := schedulerLifecycleFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	err = runServeComponents(ctx, snapshot, listener, srv, scheduler, func() {
		_ = srv.Shutdown(context.Background())
	})
	if err != nil {
		t.Fatalf("intentional shutdown returned %v, want nil", err)
	}
}

func containsReadinessCode(codes []readiness.Code, want readiness.Code) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}
