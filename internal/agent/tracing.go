// Package agent's OTel span wiring (D-03..D-06): full-tree crypto/rand SpanID minting
// (resolving the Phase-2 deferral at agent.go:51-52) and the per-call span helpers.
//
// It builds no TracerProvider. It used to, and nothing installed it: the binary edge calls
// obs.Init (cmd/aura/chat_repl.go), which installs the global provider these spans resolve
// against, so the exporter built here — with its counting exporter and its export-failure
// metric — was minting nothing and counting nothing.
package agent

import (
	"context"
	"crypto/rand"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	oteltrace "go.opentelemetry.io/otel/trace"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/obs"
)

// tracerName is the instrumentation scope for every span this package starts.
const tracerName = "github.com/chetto1983/aura/internal/agent"

var spanIDReader io.Reader = rand.Reader

// TracerProvider is the package-edge handle the REPL (cmd/aura chat) and Phase-9
// swarm hold to flush the span batch on exit (Req#13). It hides the otel SDK type
// so callers at the binary edge never import go.opentelemetry.io directly.
type TracerProvider interface {
	// Shutdown flushes any batched spans and releases the exporter. The caller
	// defers it on REPL exit; a bounded ctx keeps a missing collector from hanging.
	Shutdown(ctx context.Context) error
}

// mintSpanID returns a fresh 8-byte OTel/W3C SpanID from crypto/rand (D-04). A
// transient entropy failure is telemetry degradation, not a reason to crash the
// agent process; return the zero-id fallback and count/log the fault.
func mintSpanID() [8]byte {
	var id [8]byte
	if _, err := io.ReadFull(spanIDReader, id[:]); err != nil {
		recordSpanIDEntropyFailure()
		slog.Error("agent span id entropy failed", "err", err)
		return [8]byte{}
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
		attribute.String("tool.class", obs.ClassifyTool(name)),
		attribute.Bool("tool.mutating", mutating),
	)
	return ctx, span
}

// endToolSpan stamps the tool outcome (success flag + the error string when one
// occurred) and ends the tool.execute span. errMsg is "" on success.
func endToolSpan(span oteltrace.Span, errMsg string) {
	span.SetAttributes(attribute.Bool("tool.success", errMsg == ""))
	if errMsg != "" {
		span.SetAttributes(attribute.String("tool.error_class", obs.ClassifyError(errMsg)))
	}
	span.End()
}

// stampReplayAttributes sets aura.tool.replayed and aura.tool.replay_layer on the
// tool.execute span carried by ctx (D-10/T-45-10) — the trace-facing half of the
// same fact reserve.go's replayedMarker puts in front of the model. It is a no-op
// when replayed is false: a fresh execution leaves both attributes ABSENT rather
// than stamping replayed=false, which is unambiguous on its own (no attribute
// means nothing was replayed) and costs nothing on the hot path every
// non-replayed call takes. The two literal attribute names live ONLY here.
func stampReplayAttributes(ctx context.Context, replayed bool, layer string) {
	if !replayed {
		return
	}
	oteltrace.SpanFromContext(ctx).SetAttributes(
		attribute.Bool("aura.tool.replayed", replayed),
		attribute.String("aura.tool.replay_layer", layer),
	)
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
