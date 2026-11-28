package gateway

import (
	"context"
	"encoding/json"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/chetto1983/aura/internal/obs"
)

// policyDecisionCount sums the policy-decision counter the reader collects.
func policyDecisionCount(t *testing.T, reader *sdkmetric.ManualReader) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	name := obs.MustDescriptor(obs.GatewayPolicyDecisionsID).Name
	var total int64
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
				for _, p := range sum.DataPoints {
					total += p.Value
				}
			}
		}
	}
	return total
}

// A policy routes one call once. Review and the host review's PolicyDenies read the same
// policy before Decide does; counting each read would report one call as two or three.
func TestPolicyDecisionCountsOncePerCall(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	g, _, ctx := policyGateway(map[string]Policy{policyKey("read_file", ""): PolicyAsk})
	g.policyDecisions = newPolicyDecisions(provider.Meter("gateway-policy-metric-test"))
	raw := json.RawMessage(`{"path":"notes.md"}`)
	if r, err := g.Review(ctx, readOnlySpec(), raw, testKey(), nil); err != nil || !r.Needed {
		t.Fatalf("Review = %+v, %v; want the ask policy to need the operator", r, err)
	}
	if g.PolicyDenies(ctx, readOnlySpec(), raw) {
		t.Fatal("an ask policy denied the call")
	}
	if got := policyDecisionCount(t, reader); got != 0 {
		t.Fatalf("Review and PolicyDenies counted %d policy decisions, want none", got)
	}
	if _, err := g.Decide(ctx, readOnlySpec(), raw, testKey()); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if got := policyDecisionCount(t, reader); got != 1 {
		t.Fatalf("Decide counted %d policy decisions, want 1", got)
	}
}
