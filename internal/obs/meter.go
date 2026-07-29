package obs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type readerComponent struct {
	reader  sdkmetric.Reader
	handler http.Handler
	cleanup ShutdownFunc
}

type meterFactories struct {
	prometheus func(*prometheus.Registry) (readerComponent, error)
	otlp       func(context.Context, string) (readerComponent, error)
}

type meterOptions struct {
	resource         *resource.Resource
	enablePrometheus bool
	enableOTLP       bool
	endpoint         string
	registry         *prometheus.Registry
	factories        meterFactories
}

type meterRuntime struct {
	provider *sdkmetric.MeterProvider
	handler  http.Handler
	shutdown ShutdownFunc
}

func buildMeterRuntime(ctx context.Context, opts meterOptions) (*meterRuntime, error) {
	if opts.resource == nil {
		opts.resource = resource.Empty()
	}
	if opts.registry == nil {
		opts.registry = prometheus.NewRegistry()
	}
	if opts.factories.prometheus == nil {
		opts.factories.prometheus = newPrometheusComponent
	}
	if opts.factories.otlp == nil {
		opts.factories.otlp = newOTLPMetricComponent
	}

	components := make([]readerComponent, 0, 2)
	cleanupPartial := func(buildErr error) error {
		shutdowns := make([]ShutdownFunc, 0, len(components))
		for _, component := range components {
			if component.cleanup != nil {
				shutdowns = append(shutdowns, component.cleanup)
			}
		}
		if cleanupErr := newShutdownStack(shutdowns...)(ctx); cleanupErr != nil {
			return errors.Join(buildErr, fmt.Errorf("clean partial meter runtime: %w", cleanupErr))
		}
		return buildErr
	}

	if opts.enablePrometheus {
		component, err := opts.factories.prometheus(opts.registry)
		if err != nil {
			return nil, fmt.Errorf("create prometheus metric reader: %w", err)
		}
		components = append(components, component)
	}
	if opts.enableOTLP {
		component, err := opts.factories.otlp(ctx, opts.endpoint)
		if err != nil {
			return nil, cleanupPartial(fmt.Errorf("create OTLP metric reader: %w", err))
		}
		components = append(components, component)
	}

	providerOptions := []sdkmetric.Option{sdkmetric.WithResource(opts.resource)}
	for _, view := range catalogViews() {
		providerOptions = append(providerOptions, sdkmetric.WithView(view))
	}
	// The SDK shuts readers down in registration order. Register the readers in
	// reverse construction order so OTLP is drained before Prometheus.
	for _, v := range slices.Backward(components) {
		providerOptions = append(providerOptions, sdkmetric.WithReader(v.reader))
	}
	provider := sdkmetric.NewMeterProvider(providerOptions...)
	runtime := &meterRuntime{
		provider: provider,
		shutdown: newShutdownStack(provider.Shutdown),
	}
	for _, component := range components {
		if component.handler != nil {
			runtime.handler = component.handler
			break
		}
	}
	return runtime, nil
}

func newPrometheusComponent(registry *prometheus.Registry) (readerComponent, error) {
	if registry == nil {
		return readerComponent{}, errors.New("prometheus registry is nil")
	}
	exporter, err := otelprometheus.New(
		otelprometheus.WithRegisterer(registry),
		otelprometheus.WithoutScopeInfo(),
	)
	if err != nil {
		return readerComponent{}, err
	}
	return readerComponent{
		reader:  exporter,
		handler: promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
	}, nil
}

func newOTLPMetricComponent(ctx context.Context, endpoint string) (readerComponent, error) {
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return readerComponent{}, err
	}
	return readerComponent{
		reader:  sdkmetric.NewPeriodicReader(exporter),
		cleanup: exporter.Shutdown,
	}, nil
}

// newShutdownStack composes construction-ordered components into a repeat-safe
// reverse-order shutdown. Every component receives the caller's bounded context,
// and all components are attempted even if an earlier one returns an error.
func newShutdownStack(shutdowns ...ShutdownFunc) ShutdownFunc {
	var (
		once sync.Once
		err  error
	)
	return func(ctx context.Context) error {
		once.Do(func() {
			for _, v := range slices.Backward(shutdowns) {
				if v == nil {
					continue
				}
				err = errors.Join(err, v(ctx))
			}
		})
		return err
	}
}
