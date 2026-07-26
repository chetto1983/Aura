package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/knowledge"
)

func TestAdaptiveProjectorWorkerIDIsUniquePerRuntime(t *testing.T) {
	first := newAdaptiveProjectorWorkerID()
	second := newAdaptiveProjectorWorkerID()

	if first == second {
		t.Fatalf("runtime worker IDs collide: %q", first)
	}
	for _, workerID := range []string{first, second} {
		if !strings.HasPrefix(workerID, adaptiveProjectorWorkerIDPrefix) {
			t.Fatalf("runtime worker ID %q lacks prefix %q", workerID, adaptiveProjectorWorkerIDPrefix)
		}
	}
}

type adaptiveProjectorLifecycleSpy struct {
	order    *[]string
	startErr error
	starts   int
	stops    int
}

func (s *adaptiveProjectorLifecycleSpy) Start(context.Context) error {
	s.starts++
	*s.order = append(*s.order, "start")
	return s.startErr
}

func (s *adaptiveProjectorLifecycleSpy) Stop(context.Context) error {
	s.stops++
	*s.order = append(*s.order, "stop")
	return nil
}

type adaptiveProjectorClientSpy struct {
	order  *[]string
	closes int
}

func (*adaptiveProjectorClientSpy) Write(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

func (s *adaptiveProjectorClientSpy) Close() error {
	s.closes++
	*s.order = append(*s.order, "close")
	return nil
}

func TestAdaptiveProjectorRuntimeStartsOnceAndStopsBeforeGraphClose(t *testing.T) {
	var order []string
	worker := &adaptiveProjectorLifecycleSpy{order: &order}
	client := &adaptiveProjectorClientSpy{order: &order}

	runtime, err := startAdaptiveProjectorRuntime(
		context.Background(),
		worker,
		client,
	)
	if err != nil {
		t.Fatalf("startAdaptiveProjectorRuntime: %v", err)
	}
	if worker.starts != 1 {
		t.Fatalf("worker starts = %d, want 1", worker.starts)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if worker.stops != 1 || client.closes != 1 {
		t.Fatalf("shutdown calls = stops:%d closes:%d", worker.stops, client.closes)
	}
	if want := []string{"start", "stop", "close"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
}

func TestAdaptiveProjectorRuntimeClosesGraphWhenWorkerStartFails(t *testing.T) {
	var order []string
	startErr := errors.New("worker rejected start")
	worker := &adaptiveProjectorLifecycleSpy{
		order: &order, startErr: startErr,
	}
	client := &adaptiveProjectorClientSpy{order: &order}

	runtime, err := startAdaptiveProjectorRuntime(
		context.Background(),
		worker,
		client,
	)
	if runtime != nil || !errors.Is(err, startErr) {
		t.Fatalf("start result = runtime:%v err:%v, want nil/%v", runtime, err, startErr)
	}
	if worker.stops != 0 || client.closes != 1 {
		t.Fatalf("failed start cleanup = stops:%d closes:%d", worker.stops, client.closes)
	}
	if want := []string{"start", "close"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("failed start order = %v, want %v", order, want)
	}
}

func TestBuildAdaptiveProjectorRuntimeOpensExactlyOneGraphClient(t *testing.T) {
	pool := unreachablePool(t)
	defer pool.Close()
	cfg := &knowledge.Config{BoltURL: "bolt://neo4j:7687"}
	var order []string
	client := &adaptiveProjectorClientSpy{order: &order}
	opens := 0
	open := func(
		_ context.Context,
		got *knowledge.Config,
	) (adaptiveGraphClient, error) {
		opens++
		if got != cfg {
			t.Fatalf("graph config = %p, want %p", got, cfg)
		}
		return client, nil
	}

	runtime, err := buildAdaptiveProjectorRuntime(
		context.Background(),
		cfg,
		pool,
		open,
	)
	if err != nil {
		t.Fatalf("buildAdaptiveProjectorRuntime: %v", err)
	}
	if opens != 1 {
		t.Fatalf("graph opens = %d, want 1", opens)
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := runtime.Stop(stopCtx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if client.closes != 1 {
		t.Fatalf("graph closes = %d, want 1", client.closes)
	}
}

func TestChatEnvCloseStopsAdaptiveProjectorBeforeOtherMCP(t *testing.T) {
	var order []string
	worker := &adaptiveProjectorLifecycleSpy{order: &order}
	client := &adaptiveProjectorClientSpy{order: &order}
	runtime, err := startAdaptiveProjectorRuntime(
		context.Background(),
		worker,
		client,
	)
	if err != nil {
		t.Fatalf("start runtime: %v", err)
	}
	env := &chatEnv{
		adaptiveProjector: runtime,
		mcpClosers: []func() error{
			func() error {
				order = append(order, "mcp-close")
				return nil
			},
		},
	}

	env.close()

	want := []string{"start", "stop", "close", "mcp-close"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("chat close order = %v, want %v", order, want)
	}
}
