// Package agent pins the OpenTelemetry v1.44.0 unified trace train ahead of its
// first real use in tracing.go (Plan 04 of this phase). The four modules are a
// single verified release train (D-07; RESEARCH Standard Stack): the trace API,
// the SDK TracerProvider/batcher, and the two exporters Aura selects between via
// AURA_OTEL_EXPORTER. They are blank-imported here so `go mod tidy` keeps them
// pinned at v1.44.0 — Plan 04 swaps these anchors for the live TracerProvider
// bootstrap and the per-call llm.request span. Do not delete without landing
// tracing.go first, or the pinned train would be pruned (truth: go.mod must carry
// the four otel modules at v1.44.0 with a clean go.sum).
package agent

import (
	_ "go.opentelemetry.io/otel"
	_ "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	_ "go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	_ "go.opentelemetry.io/otel/sdk"
)
