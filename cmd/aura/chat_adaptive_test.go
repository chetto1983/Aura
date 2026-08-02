package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/config"
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

// adaptiveProjectorWriterSpy stands in for the ArcadeDB writer. It has no Close:
// the projector's graph client is a stateless HTTP client now, so the runtime has
// nothing to shut down but the worker. The three close-ordering assertions this
// file used to carry were asserting a resource that no longer exists — they are
// replaced below by the lifecycle contract that survives.
type adaptiveProjectorWriterSpy struct {
	order *[]string
}

func (*adaptiveProjectorWriterSpy) Write(
	context.Context,
	string,
	map[string]any,
) ([]map[string]any, error) {
	return nil, nil
}

func TestAdaptiveProjectorRuntimeStartsOnceAndStopsItsWorker(t *testing.T) {
	var order []string
	worker := &adaptiveProjectorLifecycleSpy{order: &order}

	runtime, err := startAdaptiveProjectorRuntime(context.Background(), worker)
	if err != nil {
		t.Fatalf("startAdaptiveProjectorRuntime: %v", err)
	}
	if worker.starts != 1 {
		t.Fatalf("worker starts = %d, want 1", worker.starts)
	}
	if err := runtime.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if worker.stops != 1 {
		t.Fatalf("worker stops = %d, want 1", worker.stops)
	}
	if want := []string{"start", "stop"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
}

// A worker that refuses to start must yield NO runtime: a non-nil one would be
// stopped at shutdown, calling Stop on a worker that never ran.
func TestAdaptiveProjectorRuntimeRefusesWhenWorkerStartFails(t *testing.T) {
	var order []string
	startErr := errors.New("worker rejected start")
	worker := &adaptiveProjectorLifecycleSpy{order: &order, startErr: startErr}

	runtime, err := startAdaptiveProjectorRuntime(context.Background(), worker)
	if runtime != nil || !errors.Is(err, startErr) {
		t.Fatalf("start result = runtime:%v err:%v, want nil/%v", runtime, err, startErr)
	}
	if worker.stops != 0 {
		t.Fatalf("failed start called Stop %d times, want 0", worker.stops)
	}
	if want := []string{"start"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("failed start order = %v, want %v", order, want)
	}
}

func TestBuildAdaptiveProjectorRuntimeOpensExactlyOneGraphClient(t *testing.T) {
	pool := unreachablePool(t)
	defer pool.Close()
	cfg := &config.ArcadeDBConfig{BaseURL: "http://arcadedb:2480", Database: "aura_memory", User: "aura_memory"}
	var order []string
	writer := &adaptiveProjectorWriterSpy{order: &order}
	opens := 0
	open := func(
		_ context.Context,
		got *config.ArcadeDBConfig,
	) (adaptive.GraphWriter, error) {
		opens++
		if got != cfg {
			t.Fatalf("graph config = %p, want %p", got, cfg)
		}
		return writer, nil
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
}

// The projector must be stopped BEFORE the MCP clients it may still be using.
func TestChatEnvCloseStopsAdaptiveProjectorBeforeOtherMCP(t *testing.T) {
	var order []string
	worker := &adaptiveProjectorLifecycleSpy{order: &order}
	runtime, err := startAdaptiveProjectorRuntime(context.Background(), worker)
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

	want := []string{"start", "stop", "mcp-close"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("chat close order = %v, want %v", order, want)
	}
}
