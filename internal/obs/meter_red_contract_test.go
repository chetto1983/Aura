package obs

// This test-only contract keeps the RED suite executable before the production
// meter lifecycle exists. Task 1 GREEN replaces these deliberately failing
// implementations with internal/obs/meter.go.

import (
	"context"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

var errMeterLifecycleNotImplemented = errors.New("meter lifecycle not implemented")

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

func buildMeterRuntime(_ context.Context, opts meterOptions) (*meterRuntime, error) {
	_ = opts.endpoint
	return nil, errMeterLifecycleNotImplemented
}

func newPrometheusComponent(*prometheus.Registry) (readerComponent, error) {
	return readerComponent{}, errMeterLifecycleNotImplemented
}

func newShutdownStack(...ShutdownFunc) ShutdownFunc {
	return func(context.Context) error { return errMeterLifecycleNotImplemented }
}
