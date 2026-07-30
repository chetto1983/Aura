package rerank

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/obs"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestRerankClientRecordsDegradationAndDuration(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	telemetry := newRerankTelemetry(provider.Meter("aura.rerank.test"))

	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &RerankClient{
		BaseURL:   server.URL,
		Client:    server.Client(),
		telemetry: &telemetry,
	}
	scored, err := client.Rerank(t.Context(), "query", []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(scored) != 2 || !scored[0].Degraded {
		t.Fatalf("degraded result = %#v", scored)
	}

	var resources metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &resources); err != nil {
		t.Fatal(err)
	}
	var calls int64
	var durations uint64
	for _, scope := range resources.ScopeMetrics {
		for _, metric := range scope.Metrics {
			switch metric.Name {
			case obs.MustDescriptor(obs.RerankCallsID).Name:
				sum, ok := metric.Data.(metricdata.Sum[int64])
				if !ok {
					t.Fatalf("rerank calls metric type = %T", metric.Data)
				}
				for _, point := range sum.DataPoints {
					attrs := point.Attributes.ToSlice()
					if metricAttribute(attrs, "outcome") == "error" &&
						metricAttribute(attrs, "error_class") == "unavailable" {
						calls += point.Value
					}
				}
			case obs.MustDescriptor(obs.RerankDurationID).Name:
				histogram, ok := metric.Data.(metricdata.Histogram[float64])
				if !ok {
					t.Fatalf("rerank duration metric type = %T", metric.Data)
				}
				for _, point := range histogram.DataPoints {
					durations += point.Count
				}
			}
		}
	}
	if calls != 1 || durations != 1 {
		t.Fatalf("rerank metrics calls=%d durations=%d", calls, durations)
	}
}

func metricAttribute(attributes []attribute.KeyValue, key string) string {
	for _, item := range attributes {
		if string(item.Key) == key {
			return item.Value.AsString()
		}
	}
	return ""
}
