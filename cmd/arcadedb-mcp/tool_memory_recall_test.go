package main

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/chetto1983/aura/internal/arcadedb"
)

const recallRankedMixed = `{"result":[{"rid":"#10:1","score":0.04},{"rid":"#20:1","score":0.03}]}`

const recallFactHydration = `{"result":[{"@rid":"#10:1","statement":"Davide keeps the blue notebook.","predicate":"keeps","subject":"Davide","object":"blue notebook","valid_from":"2026-01-01T00:00:00Z","fact_key":"fact-blue","sources":[{"run_id":"run-1"}]}]}`

const recallTurnHydration = `{"result":[{"@rid":"#20:1","identity_id":"00000000-0000-0000-0000-000000000001","conversation_id":"conversation-1","turn_seq":7,"role":"user","content":"We discussed the notebook in Turin.","content_hash":"hash-7","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/7"}]}`

const recallTurnWindow = `{"result":[{"identity_id":"00000000-0000-0000-0000-000000000001","conversation_id":"conversation-1","turn_seq":6,"role":"assistant","content":"I found the earlier itinerary.","content_hash":"hash-6","occurred_at":"2026-08-31T11:59:00Z","source_ref":"postgres://conversation/conversation-1/turn/6"},{"identity_id":"00000000-0000-0000-0000-000000000001","conversation_id":"conversation-1","turn_seq":7,"role":"user","content":"We discussed the notebook in Turin.","content_hash":"hash-7","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/7"}]}`

type recallStubEmbedder struct {
	err error
}

func (s recallStubEmbedder) Embed(context.Context, []string) ([][]float64, error) {
	if s.err != nil {
		return nil, s.err
	}
	return [][]float64{make([]float64, 768)}, nil
}

func TestMemoryRecallMixedTierTracer(t *testing.T) {
	client, _ := newRecordingDB(t,
		recallRankedMixed, recallFactHydration, recallTurnHydration, recallTurnWindow,
	)
	_, output, err := memoryRecallHandler(singleTenant(t, client))(
		context.Background(), reqWithIdentity(testIdentity), MemoryRecallInput{
			Query: "where did we discuss the blue notebook", Limit: 5,
		},
	)
	if err != nil {
		t.Fatalf("memory_recall: %v", err)
	}
	if len(output.Evidence) != 2 || output.Evidence[0].Kind != "fact" ||
		output.Evidence[1].Kind != "conversation" {
		t.Fatalf("evidence = %+v", output.Evidence)
	}
	if output.Retrieval.EffectivePath != "mixed" || output.Retrieval.FactCount != 1 ||
		output.Retrieval.ConversationCount != 1 {
		t.Fatalf("retrieval = %+v", output.Retrieval)
	}
	if len(output.Facts) != 1 || len(output.Evidence[1].Conversation.Turns) != 2 {
		t.Fatalf("legacy facts / conversation window = %+v / %+v", output.Facts, output.Evidence[1])
	}
}

func TestMemoryRecallBackendPath(t *testing.T) {
	tests := []struct {
		name      string
		input     MemoryRecallInput
		responses []string
		embedder  arcadedb.Embedder
		wantPath  string
	}{
		{
			name: "hybrid query", input: MemoryRecallInput{Query: "blue notebook"},
			responses: []string{`{"result":[{"rid":"#10:1","score":0.03}]}`, recallFactHydration, `{"result":[]}`},
			embedder:  recallStubEmbedder{}, wantPath: "hybrid",
		},
		{
			name: "entity graph", input: MemoryRecallInput{Entity: "Davide"},
			responses: []string{oneFactRow}, wantPath: "graph",
		},
		{
			name: "embedding fallback", input: MemoryRecallInput{Query: "blue notebook"},
			responses: []string{`{"result":[{"rid":"#10:1","score":0.03}]}`, recallFactHydration, `{"result":[]}`},
			embedder:  recallStubEmbedder{err: errors.New("embedder unavailable")}, wantPath: "lexical",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newRecordingDB(t, tt.responses...)
			client.WithEmbedder(tt.embedder)
			output, attrs, err := callMemoryRecallWithSpan(t, client, tt.input)
			if err != nil {
				t.Fatalf("memory_recall: %v", err)
			}
			if output.Retrieval.Path != tt.wantPath {
				t.Fatalf("path = %q, want %q", output.Retrieval.Path, tt.wantPath)
			}
			if got := recallAttribute(attrs, "memory.recall.path"); got != output.Retrieval.Path {
				t.Fatalf("span path = %q, response path = %q", got, output.Retrieval.Path)
			}
			if got := recallAttribute(attrs, "memory.recall.effective_path"); got != output.Retrieval.EffectivePath {
				t.Fatalf("span effective path = %q, response = %q", got, output.Retrieval.EffectivePath)
			}
		})
	}
}

func callMemoryRecallWithSpan(
	t *testing.T,
	client *arcadedb.Client,
	input MemoryRecallInput,
) (MemoryRecallOutput, []attribute.KeyValue, error) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	ctx, span := provider.Tracer("memory-recall-test").Start(context.Background(), "tool.call")
	_, output, err := memoryRecallHandler(singleTenant(t, client))(ctx, reqWithIdentity(testIdentity), input)
	span.End()
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	return output, spans[0].Attributes(), err
}

func recallAttribute(attrs []attribute.KeyValue, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}
