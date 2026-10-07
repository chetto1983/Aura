package arcadedb

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/embeddings"
)

func recallRequest() TurnRecallRequest {
	return TurnRecallRequest{
		IdentityID: "identity-a", Text: "che tempo fa domani a Torino?", ContextKey: "ctx1:k",
		RouteKey: "route1:r", PolicyVersion: "policy1:p", SourceRef: "postgres://aura/conversations/c9/turns/3",
		DeferredTools: []string{"calendar_add", "web_search"}, IncludeLabels: true,
	}
}

func labelRow(ref, source, effort string) string {
	return `{"distance":0.03,"source_ref":"` + ref + `","effort":"` + effort + `","effort_requested":"` + effort +
		`","effort_source":"` + source + `","recall_context_key":"ctx1:k","effort_route_key":"route1:r",` +
		`"effort_policy_version":"policy1:p"}`
}

// recallResponder answers each pool with rows, and records which statements were asked.
func recallResponder(user, teacher, tools []string) func(recordedRequest) testResponse {
	return func(request recordedRequest) testResponse {
		statement, _ := request.Payload["command"].(string)
		params, _ := request.Payload["params"].(map[string]any)
		rows := []string{}
		switch {
		case strings.Contains(statement, "CONTAINS (status = 'succeeded'"):
			rows = tools
		case params["effort_source"] == "user":
			rows = user
		case params["effort_source"] == "teacher":
			rows = teacher
		}
		return testResponse{Body: `{"result":[` + strings.Join(rows, ",") + `]}`}
	}
}

func recallStatements(requests []recordedRequest) []recordedRequest {
	var out []recordedRequest
	for _, request := range requests {
		if statement, _ := request.Payload["command"].(string); strings.Contains(statement, "vector.neighbors") {
			out = append(out, request)
		}
	}
	return out
}

func TestRecallTurnsReadsThreePoolsWithOneEmbedding(t *testing.T) {
	embedder := &stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}
	client, requests := routedClient(t, recallResponder(
		[]string{labelRow("postgres://aura/conversations/c2/turns/1", "user", "high")},
		[]string{labelRow("postgres://aura/conversations/c3/turns/1", "teacher", "low")},
		[]string{`{"distance":0.04,"source_ref":"postgres://aura/conversations/c1/turns/1","recall_context_key":"ctx1:k",` +
			`"tools":["web_search","web_search","calendar_add"]}`},
	))
	request := recallRequest()
	recall, err := client.WithEmbedder(embedder).RecallTurns(context.Background(), request)
	if err != nil {
		t.Fatalf("RecallTurns: %v", err)
	}
	if want := withTask(taskDocumentPrefix, []string{request.Text}); len(embedder.calls) != 1 || !slices.Equal(embedder.calls[0], want) {
		t.Fatalf("embedder calls = %q, want one document-template embedding %q", embedder.calls, want)
	}
	asked := recallStatements(*requests)
	if len(asked) != 3 {
		t.Fatalf("pool queries = %d, want user labels, teacher labels and tool turns", len(asked))
	}
	for _, query := range asked {
		statement, _ := query.Payload["command"].(string)
		params, _ := query.Payload["params"].(map[string]any)
		for _, fragment := range []string{
			"maxDistance: :radius", "identity_id = :identity_id", "role = 'user'", "deleted_at IS NULL",
			"embed_space = :space", "source_ref <> :source_ref", "recall_context_key = :context_key",
			"@rid.out('NEXT_TURN').in('INITIATED_BY').out('HAS_STEP').out('INVOKED')",
		} {
			if !strings.Contains(statement, fragment) {
				t.Errorf("pool query lacks %q:\n%s", fragment, statement)
			}
		}
		if params["radius"] != 0.1 || params["neighbours"] != float64(5) || params["space"] != stubSpace ||
			params["context_key"] != "ctx1:k" || params["source_ref"] != request.SourceRef || params["identity_id"] != "identity-a" {
			t.Errorf("pool query params = %v", params)
		}
	}
	if len(recall.UserLabels) != 1 || recall.UserLabels[0].SourceRef != "postgres://aura/conversations/c2/turns/1" ||
		recall.UserLabels[0].RequestedEffort != "high" {
		t.Errorf("user labels = %+v", recall.UserLabels)
	}
	if len(recall.TeacherLabels) != 1 || recall.TeacherLabels[0].Effort != "low" {
		t.Errorf("teacher labels = %+v", recall.TeacherLabels)
	}
	if len(recall.ToolTurns) != 1 || !slices.Equal(recall.ToolTurns[0].Tools, []string{"calendar_add", "web_search"}) ||
		recall.ToolTurns[0].Distance != 0.04 {
		t.Errorf("tool turns = %+v, want one turn with its tools deduplicated and sorted", recall.ToolTurns)
	}
}

// The engine filters before top-k; these rows check the second line of defence, which keeps
// a row the engine should never have returned from becoming a label or a preload.
func TestRecallTurnsDropsIncompatibleRows(t *testing.T) {
	good := labelRow("postgres://aura/conversations/good/turns/1", "user", "high")
	user := []string{
		strings.Replace(good, `"route1:r"`, `"route1:other"`, 1),
		strings.Replace(good, `"policy1:p"`, `"policy1:other"`, 1),
		strings.Replace(good, `"recall_context_key":"ctx1:k"`, `"recall_context_key":"ctx1:other"`, 1),
		strings.Replace(good, "conversations/good/turns/1", "conversations/c9/turns/3", 1),
		strings.Replace(good, `"effort":"high"`, `"effort":""`, 1),
		strings.Replace(good, `"effort_requested":"high"`, `"effort_requested":""`, 1),
		strings.Replace(good, `"distance":0.03`, `"distance":0.2`, 1),
		strings.Replace(good, `"effort_source":"user"`, `"effort_source":"memory"`, 1),
		strings.Replace(good, `"source_ref":"postgres://aura/conversations/good/turns/1"`, `"source_ref":""`, 1),
		good,
	}
	tools := []string{
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/t1/turns/1","recall_context_key":"ctx1:other","tools":["web_search"]}`,
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/c9/turns/3","recall_context_key":"ctx1:k","tools":["web_search"]}`,
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/t3/turns/1","recall_context_key":"ctx1:k","tools":[]}`,
		`{"distance":0.04,"source_ref":"postgres://aura/conversations/t4/turns/1","recall_context_key":"ctx1:k","tools":["web_search"]}`,
	}
	client, _ := routedClient(t, recallResponder(user, nil, tools))
	recall, err := client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}).
		RecallTurns(context.Background(), recallRequest())
	if err != nil {
		t.Fatalf("RecallTurns: %v", err)
	}
	if len(recall.UserLabels) != 1 || recall.UserLabels[0].SourceRef != "postgres://aura/conversations/good/turns/1" {
		t.Fatalf("user labels = %+v, want only the compatible row", recall.UserLabels)
	}
	if len(recall.ToolTurns) != 1 || recall.ToolTurns[0].SourceRef != "postgres://aura/conversations/t4/turns/1" {
		t.Fatalf("tool turns = %+v, want only the compatible row with tools", recall.ToolTurns)
	}
}

func TestRecallTurnsAsksOnlyThePoolsItCanUse(t *testing.T) {
	for _, test := range []struct {
		name  string
		edit  func(*TurnRecallRequest)
		pools int
	}{
		{name: "no labels requested", edit: func(r *TurnRecallRequest) { r.IncludeLabels = false }, pools: 1},
		{name: "no route", edit: func(r *TurnRecallRequest) { r.RouteKey = "" }, pools: 1},
		{name: "no policy", edit: func(r *TurnRecallRequest) { r.PolicyVersion = "" }, pools: 1},
		{name: "no deferred tools", edit: func(r *TurnRecallRequest) { r.DeferredTools = nil }, pools: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := routedClient(t, recallResponder(nil, nil, nil))
			request := recallRequest()
			test.edit(&request)
			if _, err := client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}}).
				RecallTurns(context.Background(), request); err != nil {
				t.Fatalf("RecallTurns: %v", err)
			}
			if got := len(recallStatements(*requests)); got != test.pools {
				t.Fatalf("pool queries = %d, want %d", got, test.pools)
			}
		})
	}
}

func TestRecallTurnsReadsNothingWithoutAnEmbedderTextOrContext(t *testing.T) {
	for _, test := range []struct {
		name     string
		embedder DenseEmbedder
		edit     func(*TurnRecallRequest)
	}{
		{name: "no embedder"},
		{name: "no text", embedder: &stubEmbedder{}, edit: func(r *TurnRecallRequest) { r.Text = "  " }},
		{name: "no context key", embedder: &stubEmbedder{}, edit: func(r *TurnRecallRequest) { r.ContextKey = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := routedClient(t, recallResponder(nil, nil, nil))
			if test.embedder != nil {
				client = client.WithEmbedder(test.embedder)
			}
			request := recallRequest()
			if test.edit != nil {
				test.edit(&request)
			}
			recall, err := client.RecallTurns(context.Background(), request)
			if err != nil {
				t.Fatalf("RecallTurns: %v", err)
			}
			if len(*requests) != 0 || len(recall.UserLabels)+len(recall.TeacherLabels)+len(recall.ToolTurns) != 0 {
				t.Fatalf("recall = %+v after %d requests, want nothing asked and nothing returned", recall, len(*requests))
			}
		})
	}
}

type flippingSpaceEmbedder struct {
	spaces []string
	reads  int
}

func (e *flippingSpaceEmbedder) Space(context.Context) (embeddings.Space, error) {
	id := e.spaces[min(e.reads, len(e.spaces)-1)]
	e.reads++
	return embeddings.Space{ID: id}, nil
}

func (e *flippingSpaceEmbedder) Embed(context.Context, []string) ([][]float64, error) {
	return [][]float64{vectorOf(1)}, nil
}

// A model swapped in between the two space reads would rank a new model's vector against
// rows embedded by the old one; recall refuses rather than guessing.
func TestRecallTurnsFailsClosedOnASpaceChangeOrABadVector(t *testing.T) {
	for _, test := range []struct {
		name     string
		embedder DenseEmbedder
	}{
		{name: "space changed", embedder: &flippingSpaceEmbedder{spaces: []string{"es1-a", "es1-b"}}},
		{name: "wrong width", embedder: &stubEmbedder{vectors: [][][]float64{{{1, 0}}}}},
		{name: "embed error", embedder: &stubEmbedder{err: context.DeadlineExceeded}},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := routedClient(t, recallResponder(nil, nil, nil))
			if _, err := client.WithEmbedder(test.embedder).RecallTurns(context.Background(), recallRequest()); err == nil {
				t.Fatal("RecallTurns succeeded; want an error the agent logs once")
			}
			if len(recallStatements(*requests)) != 0 {
				t.Fatal("a pool was queried with a vector recall refused")
			}
		})
	}
}

func TestRecallTurnsNeedsAnIdentityAndItsOwnSourceRef(t *testing.T) {
	client, _ := routedClient(t, recallResponder(nil, nil, nil))
	client = client.WithEmbedder(&stubEmbedder{vectors: [][][]float64{{vectorOf(1)}}})
	for _, edit := range []func(*TurnRecallRequest){
		func(r *TurnRecallRequest) { r.IdentityID = "" },
		func(r *TurnRecallRequest) { r.SourceRef = "" },
	} {
		request := recallRequest()
		edit(&request)
		if _, err := client.RecallTurns(context.Background(), request); err == nil {
			t.Errorf("RecallTurns(%+v) succeeded, want a validation error", request)
		}
	}
}
