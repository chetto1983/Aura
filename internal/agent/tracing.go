// Package agent's OTel wiring (D-03..D-06): the real TracerProvider bootstrap,
// full-tree crypto/rand SpanID minting (resolving the Phase-2 deferral at
// agent.go:51-52), and the per-call llm.request span helper. This file REPLACES
// the otel_deps.go blank-import anchor — the v1.44.0 trace train is now used for
// real, so the anchor is deleted in the same commit (truth: go.mod keeps the four
// modules pinned because tracing.go imports them).
package agent

import (
	"context"
	"crypto/rand"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/chetto1983/aura/internal/llm"
)

// OTel exporter modes (D-06). Default is otlp; none is a true zero-overhead no-op.
const (
	exporterNone   = "none"
	exporterStdout = "stdout"
	exporterOTLP   = "otlp"
)

// tracerName is the instrumentation scope for every span this package starts.
const tracerName = "github.com/chetto1983/aura/internal/agent"

// newTracerProvider builds the real exporter per AURA_OTEL_EXPORTER ∈
// {otlp,stdout,none} (D-05/D-06). "none" returns a no-exporter provider (spans
// are minted but dropped — zero overhead, never an error). "otlp" targets the
// endpoint over insecure gRPC and SILENT-DROPS without a collector (the exporter
// retries in the background and logs only at debug; it never fails-fast and never
// auto-falls-back to stdout, which would pollute the REPL). The caller defers
// tp.Shutdown(ctx) to flush the batch on exit. It also installs tp as the global
// provider so otel.Tracer(...) resolves to it.
func newTracerProvider(ctx context.Context, mode, endpoint string) (*sdktrace.TracerProvider, error) {
	if mode == exporterNone {
		tp := sdktrace.NewTracerProvider() // no exporter wired = no-op spans
		otel.SetTracerProvider(tp)
		return tp, nil
	}

	var (
		exp sdktrace.SpanExporter
		err error
	)
	switch mode {
	case exporterStdout:
		exp, err = stdouttrace.New()
	default: // otlp (the D-06 default)
		// Without a collector the OTel SDK reports export failures (e.g. "export
		// timeout: connection refused" on the default localhost:4317) through the
		// GLOBAL error handler. The process-global handler is installed by
		// obs.Init (rate-limited slog, O-03) so those errors surface but a broken
		// exporter cannot flood the logs — this branch no longer installs a no-op
		// that would silently swallow every future export error process-wide.
		// Spans still batch in the background and ship once a collector appears.
		exp, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure())
	}
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp))
	otel.SetTracerProvider(tp)
	return tp, nil
}

// TracerProvider is the package-edge handle the REPL (cmd/aura chat) and Phase-9
// swarm hold to flush the span batch on exit (Req#13). It hides the otel SDK type
// so callers at the binary edge never import go.opentelemetry.io directly.
type TracerProvider interface {
	// Shutdown flushes any batched spans and releases the exporter. The caller
	// defers it on REPL exit; a bounded ctx keeps a missing collector from hanging.
	Shutdown(ctx context.Context) error
}

// NewTracerProvider is the exported wiring the binary edge uses to install the
// real exporter from AURA_OTEL_EXPORTER (D-05/D-06) and obtain a Shutdown handle.
// It delegates to newTracerProvider (the package-internal builder the agent loop's
// spans resolve against via the global provider) and returns the SDK provider as
// the narrow TracerProvider interface.
func NewTracerProvider(ctx context.Context, mode, endpoint string) (TracerProvider, error) {
	return newTracerProvider(ctx, mode, endpoint)
}

// mintSpanID returns a fresh 8-byte OTel/W3C SpanID from crypto/rand (D-04). It
// panics only if the OS entropy source fails, which is unrecoverable.
func mintSpanID() [8]byte {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic("agent: crypto/rand failed minting SpanID: " + err.Error())
	}
	return id
}

// rootSpanIDs mints the SpanID for the root InvocationContext and leaves the
// parent nil (root has no parent — D-04).
func rootSpanIDs() (span [8]byte, parent *[8]byte) {
	return mintSpanID(), nil
}

// startLLMSpan starts the per-call "llm.request" span from ic.Ctx (Req#13). The
// returned context carries the span; the caller MUST call span.End() (deferred or
// explicit) exactly once per LLM call. When ctx already carries an "agent.turn"
// span (the loop threads it through, O-08), this span nests under it.
func startLLMSpan(ctx context.Context) (context.Context, oteltrace.Span) {
	return otel.Tracer(tracerName).Start(ctx, "llm.request")
}

// startTurnSpan starts the "agent.turn" span wrapping the whole Run loop (O-08).
// The returned context becomes the parent for every per-call llm.request and
// per-tool tool.execute span this turn, so a trace shows per-turn latency. The
// caller MUST End() it on every Run return path (deferred over the iter.Seq2
// closure). request_id and thread_id are stamped now; the terminal outcome is
// stamped on End via endTurnSpan.
func startTurnSpan(ctx context.Context, requestID, threadID string) (context.Context, oteltrace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "agent.turn")
	span.SetAttributes(
		attribute.String("aura.request_id", requestID),
		attribute.String("aura.thread_id", threadID),
	)
	return ctx, span
}

// endTurnSpan stamps the terminal outcome on the agent.turn span and ends it. A
// nil/no-op span tolerates this (the SDK guards a zero span). reason is the
// terminal path label ("text_response"/"content_stop"/"max_steps"/... or "" when
// none applies); it is stamped only when non-empty so the deferred zero-value call
// site stays cheap.
func endTurnSpan(span oteltrace.Span, reason string) {
	if reason != "" {
		span.SetAttributes(attribute.String("aura.turn_outcome", reason))
	}
	span.End()
}

// startToolSpan starts the per-dispatch "tool.execute" span (O-08), nested under
// the agent.turn span carried by ctx. The caller MUST End() it when the tool
// returns. name + mutating are stamped now; success/error is stamped on End via
// endToolSpan. Span creation is cheap and never fatal — a no-op tracer mints a
// dropped span without allocating an exporter record.
func startToolSpan(ctx context.Context, name string, mutating bool) (context.Context, oteltrace.Span) {
	ctx, span := otel.Tracer(tracerName).Start(ctx, "tool.execute")
	span.SetAttributes(
		attribute.String("tool.name", name),
		attribute.Bool("tool.mutating", mutating),
	)
	return ctx, span
}

// endToolSpan stamps the tool outcome (success flag + the error string when one
// occurred) and ends the tool.execute span. errMsg is "" on success.
func endToolSpan(span oteltrace.Span, errMsg string) {
	span.SetAttributes(attribute.Bool("tool.success", errMsg == ""))
	if errMsg != "" {
		span.SetAttributes(attribute.String("tool.error", errMsg))
	}
	span.End()
}

// setSpanAttrs stamps the llm.request span with the cost/identity attributes
// (Req#13/AI-SPEC §4). It sets llm.model, llm.provider, llm.prompt_tokens,
// llm.completion_tokens, llm.cache_hit_tokens (= usage.CachedTokens, the cache
// READ count), and aura.request_id. It NEVER sets an api_key attribute (D-28).
func setSpanAttrs(span oteltrace.Span, model, provider, requestID string, usage llm.Usage) {
	span.SetAttributes(
		attribute.String("llm.model", model),
		attribute.String("llm.provider", provider),
		attribute.Int("llm.prompt_tokens", usage.PromptTokens),
		attribute.Int("llm.completion_tokens", usage.CompletionTokens),
		attribute.Int("llm.cache_hit_tokens", usage.CachedTokens),
		attribute.String("aura.request_id", requestID),
	)
}
