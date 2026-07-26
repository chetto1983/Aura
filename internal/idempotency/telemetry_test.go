package idempotency

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestStoreEmitsBoundedIdempotencyMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	otel.SetMeterProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	now := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	request := testBeginRequest(t, "telemetry-contract")
	emitBeginMetricFixtures(t, now, request)
	emitTransitionMetricFixtures(t, now, request)

	got := collectIdempotencyPoints(t, reader)
	want := map[idempotencyMetricPoint]int64{
		{operation: "idempotency_reserve", state: "in_progress", outcome: "success"}:          1,
		{operation: "idempotency_replay", state: "completed", outcome: "replayed"}:            1,
		{operation: "idempotency_reserve", state: "failed", outcome: "conflict"}:              1,
		{operation: "idempotency_reserve", state: "in_progress", outcome: "in_progress"}:      1,
		{operation: "idempotency_replay", state: "expired", outcome: "skipped"}:               1,
		{operation: "idempotency_reserve", state: "indeterminate", outcome: "indeterminate"}:  1,
		{operation: "idempotency_reserve", state: "failed", outcome: "error"}:                 1,
		{operation: "idempotency_complete", state: "completed", outcome: "success"}:           1,
		{operation: "idempotency_complete", state: "indeterminate", outcome: "indeterminate"}: 1,
		{operation: "idempotency_complete", state: "failed", outcome: "error"}:                2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("idempotency points = %#v, want %#v", got, want)
	}
}

func TestStoreMetricReachesPrivatePrometheusHandler(t *testing.T) {
	runtime, err := obs.InitRuntime(context.Background(), obs.Config{
		Service: "aura-idempotency-test", OtelExporter: "none", EnablePrometheus: true, LogWriter: io.Discard,
	})
	if err != nil {
		t.Fatalf("InitRuntime: %v", err)
	}
	t.Cleanup(func() { _ = runtime.Shutdown(context.Background()) })

	request := testBeginRequest(t, "prometheus-scrape")
	store := newStore(&fakeOperationQueries{tryStart: func(context.Context, sqlc.TryStartOperationParams) (int64, error) {
		return 1, nil
	}}, Config{})
	if _, err := store.Begin(context.Background(), request); err != nil {
		t.Fatalf("Begin: %v", err)
	}

	recorder := httptest.NewRecorder()
	runtime.MetricsHandler().ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
	body := recorder.Body.String()
	if !strings.Contains(body, `aura_idempotency_operation_total{operation="idempotency_reserve",outcome="success",state="in_progress"} 1`) {
		t.Fatalf("private scrape omitted real Store event:\n%s", body)
	}
	for _, forbidden := range []string{request.Operation.IdentityID, request.Operation.Key} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("private scrape leaked operation identity %q", forbidden)
		}
	}
}

func emitBeginMetricFixtures(t *testing.T, now time.Time, request BeginRequest) {
	t.Helper()
	tests := []struct {
		name     string
		tryStart tryStartQuery
		get      getOperationQuery
		wantErr  error
	}{
		{name: "acquired", tryStart: func(context.Context, sqlc.TryStartOperationParams) (int64, error) { return 1, nil }},
		{name: "replayed", tryStart: noAcquire, get: existingRow(request, StateCompleted, now, now.Add(time.Hour))},
		{name: "conflict", tryStart: noAcquire, get: func(context.Context, sqlc.GetOperationParams) (sqlc.GetOperationRow, error) {
			row := testRow(request, StateInProgress, now, time.Time{})
			row.PayloadHash[0] ^= 0xff
			return row, nil
		}, wantErr: ErrConflict},
		{name: "in progress", tryStart: noAcquire, get: existingRow(request, StateInProgress, now.Add(time.Second), time.Time{})},
		{name: "result expired", tryStart: noAcquire, get: existingRow(request, StateCompleted, now, now)},
		{name: "indeterminate", tryStart: noAcquire, get: existingRow(request, StateIndeterminate, now, time.Time{})},
		{name: "query error", tryStart: func(context.Context, sqlc.TryStartOperationParams) (int64, error) { return 0, errFakeQuery }, wantErr: errFakeQuery},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := newStore(&fakeOperationQueries{tryStart: test.tryStart, get: test.get}, Config{Now: func() time.Time { return now }}).Begin(context.Background(), request)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Begin error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func emitTransitionMetricFixtures(t *testing.T, now time.Time, request BeginRequest) {
	t.Helper()
	result := ReplayResult{Body: []byte(`{"ok":true}`), ExpiresAt: now.Add(time.Hour)}
	tests := []struct {
		name          string
		complete      completeOperationQuery
		indeterminate markIndeterminateQuery
		invoke        func(*Store) error
		wantErr       error
	}{
		{name: "complete", complete: func(context.Context, sqlc.CompleteOperationParams) (int64, error) { return 1, nil }, invoke: func(store *Store) error {
			return store.Complete(context.Background(), CompleteRequest{Operation: request.Operation, Fingerprint: request.Fingerprint, ClaimToken: 1, Result: result})
		}},
		{name: "complete error", complete: func(context.Context, sqlc.CompleteOperationParams) (int64, error) { return 0, errFakeQuery }, invoke: func(store *Store) error {
			return store.Complete(context.Background(), CompleteRequest{Operation: request.Operation, Fingerprint: request.Fingerprint, ClaimToken: 1, Result: result})
		}, wantErr: errFakeQuery},
		{name: "mark indeterminate", indeterminate: func(context.Context, sqlc.MarkOperationIndeterminateParams) (int64, error) { return 1, nil }, invoke: func(store *Store) error {
			return store.MarkIndeterminate(context.Background(), request.Operation, request.Fingerprint, 1)
		}},
		{name: "mark indeterminate error", indeterminate: func(context.Context, sqlc.MarkOperationIndeterminateParams) (int64, error) { return 0, errFakeQuery }, invoke: func(store *Store) error {
			return store.MarkIndeterminate(context.Background(), request.Operation, request.Fingerprint, 1)
		}, wantErr: errFakeQuery},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newStore(&fakeOperationQueries{complete: test.complete, indeterminate: test.indeterminate}, Config{Now: func() time.Time { return now }})
			if err := test.invoke(store); !errors.Is(err, test.wantErr) {
				t.Fatalf("transition error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func noAcquire(context.Context, sqlc.TryStartOperationParams) (int64, error) {
	return 0, pgx.ErrNoRows
}

func existingRow(request BeginRequest, state State, retry, expires time.Time) getOperationQuery {
	return func(context.Context, sqlc.GetOperationParams) (sqlc.GetOperationRow, error) {
		return testRow(request, state, retry, expires), nil
	}
}

type idempotencyMetricPoint struct {
	operation string
	state     string
	outcome   string
}

func collectIdempotencyPoints(t *testing.T, reader *sdkmetric.ManualReader) map[idempotencyMetricPoint]int64 {
	t.Helper()
	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	result := make(map[idempotencyMetricPoint]int64)
	wantName := obs.MustDescriptor(obs.IdempotencyOperationsID).Name
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, measurement := range scope.Metrics {
			if measurement.Name != wantName {
				continue
			}
			sum, ok := measurement.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Sum[int64]", wantName, measurement.Data)
			}
			for _, point := range sum.DataPoints {
				values := map[obs.AttributeKey]string{}
				for _, kv := range point.Attributes.ToSlice() {
					key := obs.AttributeKey(kv.Key)
					if !obs.IsBoundedAttribute(key) {
						t.Fatalf("metric contains unbounded attribute key %q", key)
					}
					value := kv.Value.AsString()
					if !obs.AllowedAttributeValue(key, value) {
						t.Fatalf("metric contains unbounded %s value %q", key, value)
					}
					values[key] = value
				}
				if len(values) != 3 {
					t.Fatalf("metric attributes = %v, want operation/state/outcome only", values)
				}
				key := idempotencyMetricPoint{
					operation: values[obs.AttributeOperation], state: values[obs.AttributeState], outcome: values[obs.AttributeOutcome],
				}
				result[key] += point.Value
			}
		}
	}
	return result
}
