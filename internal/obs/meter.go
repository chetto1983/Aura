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
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

type readerComponent struct {
	reader  sdkmetric.Reader
	handler http.Handler
}

type meterFactories struct {
	prometheus func(*prometheus.Registry) (readerComponent, error)
}

type meterOptions struct {
	resource         *resource.Resource
	enablePrometheus bool
	registry         *prometheus.Registry
	factories        meterFactories
}

type meterRuntime struct {
	provider *sdkmetric.MeterProvider
	handler  http.Handler
	shutdown ShutdownFunc
}

func buildMeterRuntime(opts meterOptions) (*meterRuntime, error) {
	if opts.resource == nil {
		opts.resource = resource.Empty()
	}
	if opts.registry == nil {
		opts.registry = prometheus.NewRegistry()
	}
	if opts.factories.prometheus == nil {
		opts.factories.prometheus = newPrometheusComponent
	}

	var component readerComponent
	if opts.enablePrometheus {
		var err error
		component, err = opts.factories.prometheus(opts.registry)
		if err != nil {
			return nil, fmt.Errorf("create prometheus metric reader: %w", err)
		}
	}

	providerOptions := []sdkmetric.Option{sdkmetric.WithResource(opts.resource)}
	for _, view := range catalogViews() {
		providerOptions = append(providerOptions, sdkmetric.WithView(view))
	}
	if component.reader != nil {
		providerOptions = append(providerOptions, sdkmetric.WithReader(component.reader))
	}
	provider := sdkmetric.NewMeterProvider(providerOptions...)
	return &meterRuntime{
		provider: provider,
		handler:  component.handler,
		shutdown: newShutdownStack(provider.Shutdown),
	}, nil
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
