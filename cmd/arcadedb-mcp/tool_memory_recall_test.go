package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

func TestMemoryRecallModeContract(t *testing.T) {
	session := inMemoryIdentityServer(t, newServer(nil, testClock, ""))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	var recallToolFound bool
	for _, tool := range listed.Tools {
		if tool.Name != "memory_recall" {
			continue
		}
		recallToolFound = true
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("input schema = %T", tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("properties = %T", schema["properties"])
		}
		mode, ok := properties["mode"].(map[string]any)
		if !ok {
			t.Fatalf("mode schema = %T", properties["mode"])
		}
		rawEnum, ok := mode["enum"].([]any)
		if !ok {
			t.Fatalf("mode enum = %T (%v)", mode["enum"], mode["enum"])
		}
		got := make([]string, 0, len(rawEnum))
		for _, value := range rawEnum {
			text, ok := value.(string)
			if !ok {
				t.Fatalf("enum value = %T", value)
			}
			got = append(got, text)
		}
		want := []string{"semantic", "recent", "open", "scroll", "reasoning"}
		if !slices.Equal(got, want) {
			t.Fatalf("mode enum = %v, want %v", got, want)
		}
		if required, ok := schema["required"].([]any); ok {
			for _, name := range required {
				if name == "mode" {
					t.Fatal("mode is required; omission must preserve semantic recall")
				}
			}
		}
	}
	if !recallToolFound {
		t.Fatal("memory_recall is not advertised")
	}
}

func TestMemoryRecallActiveSourceHeader(t *testing.T) {
	t.Run("identity is required before header decode", func(t *testing.T) {
		client, rec := newRecordingDB(t)
		req := reqWithIdentity("")
		req.Extra.Header = http.Header{memoryRecallActiveSourcesHeader: {"%%%"}}
		_, _, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), req, MemoryRecallInput{Query: "blue notebook"})
		if !errors.Is(err, errMissingOAuthSubject) {
			t.Fatalf("error = %v, want missing OAuth subject", err)
		}
		if len(rec.statements) != 0 {
			t.Fatalf("unauthenticated header reached database: %v", rec.statements)
		}
	})

	for _, tc := range []struct {
		name   string
		header http.Header
	}{
		{name: "malformed", header: http.Header{memoryRecallActiveSourcesHeader: {"%%%"}}},
		{name: "over encoded cap", header: http.Header{memoryRecallActiveSourcesHeader: {strings.Repeat("a", 2049)}}},
		{name: "ambiguous duplicate headers", header: http.Header{memoryRecallActiveSourcesHeader: {"one", "two"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, rec := newRecordingDB(t)
			req := reqWithIdentity(testIdentity)
			req.Extra.Header = tc.header
			_, _, err := memoryRecallHandler(singleTenant(t, client))(
				context.Background(), req, MemoryRecallInput{Query: "blue notebook"})
			if err == nil {
				t.Fatal("invalid active-source header was accepted")
			}
			if len(rec.statements) != 0 {
				t.Fatalf("invalid header reached database: %v", rec.statements)
			}
		})
	}

	t.Run("turn id must match the host actor", func(t *testing.T) {
		client, rec := newRecordingDB(t)
		req := recallRequestWithActiveSource(t, testIdentity, "actor-turn", memoryRecallActiveSource{
			ConversationID: "conversation-active", TurnID: "different-turn",
		})
		_, _, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), req, MemoryRecallInput{Query: "blue notebook"})
		if err == nil {
			t.Fatal("actor/source turn mismatch was accepted")
		}
		if len(rec.statements) != 0 {
			t.Fatalf("actor mismatch reached database: %v", rec.statements)
		}
	})

	t.Run("source count is bounded before database access", func(t *testing.T) {
		sources := make([]memoryRecallActiveSource, 0, memoryRecallMaxActiveSources+1)
		for index := range memoryRecallMaxActiveSources + 1 {
			sources = append(sources, memoryRecallActiveSource{
				ConversationID: fmt.Sprintf("conversation-%02d", index), TurnID: "turn-current",
			})
		}
		client, rec := newRecordingDB(t)
		req := recallRequestWithActiveSource(t, testIdentity, "turn-current", sources...)
		_, _, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), req, MemoryRecallInput{Query: "blue notebook"})
		if err == nil {
			t.Fatal("over-count active-source header was accepted")
		}
		if len(rec.statements) != 0 {
			t.Fatalf("over-count header reached database: %v", rec.statements)
		}
	})

	t.Run("foreign conversation fails after ownership lookup and before recall", func(t *testing.T) {
		foreign := `{"result":[{"identity_id":"foreign-identity","conversation_id":"conversation-active"}]}`
		client, rec := newRecordingDB(t, foreign)
		req := recallRequestWithActiveSource(t, testIdentity, "turn-current", memoryRecallActiveSource{
			ConversationID: "conversation-active", TurnID: "turn-current",
		})
		_, _, err := memoryRecallHandler(singleTenant(t, client))(
			context.Background(), req, MemoryRecallInput{Query: "blue notebook"})
		if err == nil || !strings.Contains(err.Error(), "not owned") {
			t.Fatalf("foreign conversation error = %v", err)
		}
		if len(rec.statements) != 1 || !strings.Contains(rec.statements[0], "FROM Conversation") {
			t.Fatalf("foreign exclusion reached recall query: %v", rec.statements)
		}
	})
}

func TestMemoryRecallSuppressesActiveConversation(t *testing.T) {
	ownership := `{"result":[{"identity_id":"` + testIdentity + `","conversation_id":"conversation-active"}]}`
	ranked := `{"result":[{"rid":"#20:1","score":0.04},{"rid":"#20:2","score":0.03}]}`
	turns := `{"result":[` +
		`{"@rid":"#20:1","identity_id":"` + testIdentity + `","conversation_id":"conversation-active","turn_seq":9,"role":"user","content":"active notebook","content_hash":"active","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://active/9"},` +
		`{"@rid":"#20:2","identity_id":"` + testIdentity + `","conversation_id":"conversation-history","turn_seq":7,"role":"user","content":"historical notebook","content_hash":"history","occurred_at":"2026-08-30T12:00:00Z","source_ref":"postgres://history/7"}]}`
	window := `{"result":[{"identity_id":"` + testIdentity + `","conversation_id":"conversation-history","turn_seq":7,"role":"user","content":"historical notebook","content_hash":"history","occurred_at":"2026-08-30T12:00:00Z","source_ref":"postgres://history/7"}]}`
	client, rec := newRecordingDB(t, ownership, ranked, `{"result":[]}`, turns, window)
	client.WithEmbedder(recallStubEmbedder{})
	req := recallRequestWithActiveSource(t, testIdentity, "turn-current", memoryRecallActiveSource{
		ConversationID: "conversation-active", TurnID: "turn-current",
	})
	_, output, err := memoryRecallHandler(singleTenant(t, client))(
		context.Background(), req, MemoryRecallInput{Query: "blue notebook", Limit: 5})
	if err != nil {
		t.Fatalf("memory_recall: %v", err)
	}
	if len(output.Evidence) != 1 || output.Evidence[0].Conversation == nil ||
		output.Evidence[0].Conversation.ConversationID != "conversation-history" {
		t.Fatalf("evidence = %+v, want only bound historical conversation", output.Evidence)
	}
	for _, evidence := range output.Evidence {
		if evidence.Conversation != nil && evidence.Conversation.ConversationID == "conversation-active" {
			t.Fatalf("active conversation leaked: %+v", output.Evidence)
		}
	}
	statement, params, ok := rec.find("vector.fuse")
	if !ok || !strings.Contains(statement, "conversation_id NOT IN :excluded_conversation_ids") {
		t.Fatalf("recall query lacks negative filter: %q", statement)
	}
	if got := params["excluded_conversation_ids"]; !slices.Equal(anyStringSlice(got), []string{"conversation-active"}) {
		t.Fatalf("negative filter params = %#v", got)
	}
}

func TestMemoryRecallActiveSourceIsNotModelInput(t *testing.T) {
	raw, err := json.Marshal(MemoryRecallInput{Query: "blue notebook"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "active") || strings.Contains(text, "source") || strings.Contains(text, "turn_id") {
		t.Fatalf("model input carries active-source state: %s", text)
	}
	if _, ok := any(MemoryRecallInput{}).(interface{ GetMeta() map[string]any }); ok {
		t.Fatal("memory recall input unexpectedly exposes MCP metadata")
	}

	client, rec := newRecordingDB(t, `{"result":[]}`)
	req := reqWithIdentity(testIdentity)
	req.Params.SetMeta(map[string]any{
		memoryRecallActiveSourcesHeader: "model-supplied-exclusion",
	})
	_, _, err = memoryRecallHandler(singleTenant(t, client))(
		context.Background(), req, MemoryRecallInput{Query: "blue notebook"})
	if err != nil {
		t.Fatalf("model metadata changed recall behavior: %v", err)
	}
	if len(rec.statements) == 0 || strings.Contains(rec.statements[0], "FROM Conversation WHERE") {
		t.Fatalf("model metadata reached exclusion ownership lookup: %v", rec.statements)
	}
}

func recallRequestWithActiveSource(
	t *testing.T,
	identity string,
	actorTurn string,
	sources ...memoryRecallActiveSource,
) *mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(struct {
		Version int                        `json:"version"`
		Sources []memoryRecallActiveSource `json:"sources"`
	}{Version: 1, Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	req := reqWithParentActor(identity, actorTurn)
	req.Extra.Header.Set(memoryRecallActiveSourcesHeader, base64.RawURLEncoding.EncodeToString(raw))
	return req
}

func anyStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		if stringsValue, ok := value.([]string); ok {
			return stringsValue
		}
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			values = append(values, text)
		}
	}
	return values
}
