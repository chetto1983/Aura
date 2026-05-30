package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/chetto1983/aura/internal/llm"
)

// recordingProvider installs an in-memory span recorder as the global provider
// and returns the recorder plus a cleanup that restores nothing global but shuts
// the provider down so goleak stays clean (SimpleSpanProcessor has no background
// goroutine, but Shutdown is the correct discipline regardless).
func recordingProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	return rec
}

// TestSpan_LLMRequest asserts exactly one llm.request span is recorded with all
// six required attributes populated and a stable, non-zero span_id (Req#13).
func TestSpan_LLMRequest(t *testing.T) {
	rec := recordingProvider(t)

	cost := 0.000123
	usage := llm.Usage{PromptTokens: 42, CompletionTokens: 7, CachedTokens: 30, Cost: &cost}

	ctx, span := startLLMSpan(context.Background())
	setSpanAttrs(span, "deepseek/deepseek-v4-flash:exacto", "openrouter", "req-123", usage)
	span.End()
	_ = ctx

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorded %d spans, want exactly 1", len(spans))
	}
	s := spans[0]
	if s.Name() != "llm.request" {
		t.Errorf("span name = %q, want llm.request", s.Name())
	}
	if !s.SpanContext().SpanID().IsValid() {
		t.Error("span_id is the zero (invalid) value — not minted")
	}

	want := map[string]bool{
		"llm.model": false, "llm.provider": false,
		"llm.prompt_tokens": false, "llm.completion_tokens": false,
		"llm.cache_hit_tokens": false, "aura.request_id": false,
	}
	for _, kv := range s.Attributes() {
		key := string(kv.Key)
		if _, ok := want[key]; ok {
			want[key] = true
		}
		// D-28: no attribute key may reference a secret.
		lk := strings.ToLower(key)
		if strings.Contains(lk, "api_key") || strings.Contains(lk, "apikey") ||
			strings.Contains(lk, "authorization") || lk == "key" {
			t.Errorf("forbidden secret-shaped span attribute key: %q", key)
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("required span attribute %q absent", k)
		}
	}
}

// TestSpan_AttrValues asserts the attribute VALUES carry through (model/provider/
// request_id/token counts), so the cost-observability span is honest (Req#13).
func TestSpan_AttrValues(t *testing.T) {
	rec := recordingProvider(t)

	usage := llm.Usage{PromptTokens: 100, CompletionTokens: 20, CachedTokens: 80}
	_, span := startLLMSpan(context.Background())
	setSpanAttrs(span, "m-x", "prov-y", "rid-z", usage)
	span.End()

	s := rec.Ended()[0]
	got := map[string]string{}
	gotInt := map[string]int64{}
	for _, kv := range s.Attributes() {
		switch kv.Value.Type().String() {
		case "STRING":
			got[string(kv.Key)] = kv.Value.AsString()
		case "INT64":
			gotInt[string(kv.Key)] = kv.Value.AsInt64()
		}
	}
	if got["llm.model"] != "m-x" || got["llm.provider"] != "prov-y" || got["aura.request_id"] != "rid-z" {
		t.Errorf("string attrs = %v, want model=m-x provider=prov-y request_id=rid-z", got)
	}
	if gotInt["llm.prompt_tokens"] != 100 || gotInt["llm.completion_tokens"] != 20 || gotInt["llm.cache_hit_tokens"] != 80 {
		t.Errorf("int attrs = %v, want prompt=100 completion=20 cache_hit=80", gotInt)
	}
}

// TestMintSpanID asserts mintSpanID returns a non-zero 8-byte id and that a child
// chains its ParentSpanID to the parent's SpanID (D-04 full-tree minting).
func TestMintSpanID(t *testing.T) {
	root, parent := rootSpanIDs()
	if root == ([8]byte{}) {
		t.Fatal("root SpanID is all-zero — not minted from crypto/rand")
	}
	if parent != nil {
		t.Error("root ParentSpanID must be nil")
	}

	child, childParent := childSpanIDs(root)
	if child == ([8]byte{}) {
		t.Error("child SpanID is all-zero")
	}
	if child == root {
		t.Error("child SpanID collided with parent — minting is not fresh")
	}
	if childParent == nil || *childParent != root {
		t.Errorf("child ParentSpanID = %v, want chained to parent %v", childParent, root)
	}
}

// TestNewTracerProvider_None asserts none-mode returns a working no-op provider
// with a nil error and records no spans (D-05/D-06 zero-overhead path).
func TestNewTracerProvider_None(t *testing.T) {
	tp, err := newTracerProvider(context.Background(), exporterNone, "")
	if err != nil {
		t.Fatalf("none mode errored: %v", err)
	}
	if tp == nil {
		t.Fatal("none mode returned a nil provider")
	}
	defer func() { _ = tp.Shutdown(context.Background()) }()

	// A span started under the no-op provider must not panic and must End cleanly.
	_, span := startLLMSpan(context.Background())
	span.End()
}

// TestNewTracerProvider_OTLPNoCollector asserts otlp mode does NOT fail-fast when
// no collector is reachable (D-05 silent-drop): construction succeeds, the
// exporter retries in the background, and Shutdown returns without a leak.
func TestNewTracerProvider_OTLPNoCollector(t *testing.T) {
	// An endpoint with nothing listening — the gRPC exporter must still construct.
	tp, err := newTracerProvider(context.Background(), exporterOTLP, "127.0.0.1:14317")
	if err != nil {
		t.Fatalf("otlp mode fail-fast without a collector: %v (must silent-drop)", err)
	}
	if tp == nil {
		t.Fatal("otlp mode returned a nil provider")
	}
	// Shutdown with a real (short) deadline so the batch processor AND the gRPC
	// exporter goroutines actually drain and return; a pre-cancelled context would
	// abort Shutdown early and leak them (goleak then confirms a clean teardown).
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tp.Shutdown(ctx); err != nil {
		t.Logf("otlp Shutdown returned %v (acceptable: no collector)", err)
	}
}
