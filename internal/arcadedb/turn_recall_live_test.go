//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/embeddings"
)

const (
	recallLiveSpace  = "es1-recall-live"
	recallLiveQuery  = "che tempo fa domani a Torino?"
	recallLiveKey    = "ctx1:live"
	recallLiveRoute  = "route1:live"
	recallLivePolicy = "policy1:live"
	recallLiveSelf   = "postgres://aura/conversations/current/turns/1"
)

// recallEmbedder puts each fixture text on a chosen unit vector, so a test sets cosine
// distances exactly: blend(c) sits at distance 1-c from the query, on axis 0.
type recallEmbedder struct {
	space   string
	vectors map[string][]float64
}

func (e recallEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: e.space}, nil
}

func (e recallEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for index, text := range texts {
		if vector, ok := e.vectors[strings.TrimPrefix(text, taskDocumentPrefix)]; ok {
			out[index] = vector
			continue
		}
		out[index] = recallAxis(2)
	}
	return out, nil
}

func recallAxis(index int) []float64 {
	vector := make([]float64, vectorDimensions)
	vector[index] = 1
	return vector
}

func recallBlend(cosine float64) []float64 {
	vector := make([]float64, vectorDimensions)
	vector[0], vector[1] = cosine, math.Sqrt(1-cosine*cosine)
	return vector
}

type recallFixture struct {
	t        *testing.T
	client   *Client
	embedder recallEmbedder
}

func newRecallFixture(t *testing.T) *recallFixture {
	embedder := recallEmbedder{space: recallLiveSpace, vectors: map[string][]float64{recallLiveQuery: recallAxis(0)}}
	return &recallFixture{t: t, client: disposableMemoryClient(t).WithEmbedder(embedder), embedder: embedder}
}

func recallLiveLabel(source, effort string) TurnDecision {
	return TurnDecision{
		ContextKey: recallLiveKey, Effort: effort, EffortRequested: effort, EffortSource: source,
		RouteKey: recallLiveRoute, PolicyVersion: recallLivePolicy,
	}
}

func (f *recallFixture) project(conversation string, turn ConversationTurnProjection) {
	f.t.Helper()
	turn.IdentityID, turn.ConversationID = "identity-a", conversation
	turn.ContentHash, turn.OccurredAt = conversationContentHash(turn.Content), time.Now().UTC()
	if turn.SourceRef == "" {
		turn.SourceRef = fmt.Sprintf("postgres://aura/conversations/%s/turns/%d", conversation, turn.Seq)
	}
	if err := f.client.ApplyConversationProjection(context.Background(), ConversationProjection{
		IdentityID: "identity-a", ConversationID: conversation, Turns: []ConversationTurnProjection{turn},
	}); err != nil {
		f.t.Fatalf("project %s/%d: %v", conversation, turn.Seq, err)
	}
}

// user projects one user turn at cosine c from the query and returns its source ref.
func (f *recallFixture) user(conversation, text string, cosine float64, decision TurnDecision) string {
	f.embedder.vectors[text] = recallBlend(cosine)
	ref := fmt.Sprintf("postgres://aura/conversations/%s/turns/1", conversation)
	f.project(conversation, ConversationTurnProjection{Seq: 1, Role: "user", Content: text, SourceRef: ref, Decision: decision})
	return ref
}

// answer projects the answer after a user turn and the trace on it, whose one step ran tools
// with the given statuses.
func (f *recallFixture) answer(conversation string, tools map[string]string) {
	f.t.Helper()
	ref := fmt.Sprintf("postgres://aura/conversations/%s/turns/2", conversation)
	f.project(conversation, ConversationTurnProjection{Seq: 2, Role: "assistant", Content: "Fatto: " + conversation, SourceRef: ref})
	if len(tools) == 0 {
		return
	}
	trace := freshReasoningTrace()
	trace.IdentityID, trace.TraceID, trace.ConversationID, trace.TurnSeq, trace.SourceRef =
		"identity-a", conversation+"-trace", conversation, 2, ref
	trace.Steps[0].ToolCalls = nil
	for _, name := range slices.Sorted(maps.Keys(tools)) {
		trace.Steps[0].ToolCalls = append(trace.Steps[0].ToolCalls, ReasoningToolCall{
			CallID: "call-" + name, ToolName: name, Status: tools[name], DurationMillis: 10,
			ArgumentDigest: strings.Repeat("a", reasoningDigestRunes), Observation: "observed", SourceRef: ref,
		})
	}
	if err := f.client.UpsertReasoningTrace(context.Background(), trace); err != nil {
		f.t.Fatalf("UpsertReasoningTrace %s: %v", conversation, err)
	}
}

func (f *recallFixture) recall(edit func(*TurnRecallRequest)) TurnRecall {
	f.t.Helper()
	request := TurnRecallRequest{
		IdentityID: "identity-a", Text: recallLiveQuery, ContextKey: recallLiveKey, RouteKey: recallLiveRoute,
		PolicyVersion: recallLivePolicy, SourceRef: recallLiveSelf,
		DeferredTools: []string{"calendar_add", "web_search", "weather_lookup"}, IncludeLabels: true,
	}
	if edit != nil {
		edit(&request)
	}
	recall, err := f.client.RecallTurns(context.Background(), request)
	if err != nil {
		f.t.Fatalf("RecallTurns: %v", err)
	}
	return recall
}

func refs(turns []RecalledTurn) []string {
	out := make([]string, len(turns))
	for index, turn := range turns {
		out[index] = turn.SourceRef
	}
	return out
}

func TestRecallTurnsLiveReturnsTheEffortAndToolsOfAParaphrase(t *testing.T) {
	f := newRecallFixture(t)
	paraphrase := f.user("c-a", "previsioni meteo per domani a Torino", 0.97, recallLiveLabel("teacher", "low"))
	f.answer("c-a", map[string]string{"web_search": "succeeded", "weather_lookup": "failed"})

	recall := f.recall(nil)
	if len(recall.TeacherLabels) != 1 || recall.TeacherLabels[0].SourceRef != paraphrase ||
		recall.TeacherLabels[0].RequestedEffort != "low" || math.Abs(recall.TeacherLabels[0].Distance-0.03) > 0.005 {
		t.Fatalf("teacher labels = %+v, want the paraphrase at distance 0.03 with effort low", recall.TeacherLabels)
	}
	if len(recall.ToolTurns) != 1 || !slices.Equal(recall.ToolTurns[0].Tools, []string{"web_search"}) {
		t.Fatalf("tool turns = %+v, want the paraphrase with only its successful call", recall.ToolTurns)
	}
	if len(recall.UserLabels) != 0 {
		t.Fatalf("user labels = %+v, want none", recall.UserLabels)
	}
}

// The floor this plan depends on: before 26.10.1 a filter matching nothing was no filter,
// so an identity with no row in the current space would have ranked another model's rows.
func TestRecallTurnsLiveIgnoresAnotherSpaceEvenWhenTheCurrentOneIsEmpty(t *testing.T) {
	f := newRecallFixture(t)
	other := recallEmbedder{space: "es1-another-model", vectors: map[string][]float64{"previsioni meteo per domani a Torino": recallBlend(0.99)}}
	f.client = f.client.WithEmbedder(other)
	f.user("c-other", "previsioni meteo per domani a Torino", 0.99, recallLiveLabel("teacher", "low"))
	f.answer("c-other", map[string]string{"web_search": "succeeded"})
	f.client = f.client.WithEmbedder(f.embedder)

	recall := f.recall(nil)
	if n := len(recall.UserLabels) + len(recall.TeacherLabels) + len(recall.ToolTurns); n != 0 {
		t.Fatalf("recall returned %d rows from another embedding space: %+v", n, recall)
	}
}

func TestRecallTurnsLiveExcludesOtherContexts(t *testing.T) {
	f := newRecallFixture(t)
	far := f.user("c-far", "come si fa il risotto ai funghi", 0.5, recallLiveLabel("teacher", "none"))
	f.project("current", ConversationTurnProjection{Seq: 1, Role: "user", Content: recallLiveQuery, SourceRef: recallLiveSelf, Decision: recallLiveLabel("teacher", "low")})
	otherContext := recallLiveLabel("teacher", "high")
	otherContext.ContextKey = "ctx1:another-history"
	ctxRef := f.user("c-ctx", "meteo domani a Torino?", 0.98, otherContext)
	f.answer("c-ctx", map[string]string{"web_search": "succeeded"})
	otherRoute := recallLiveLabel("teacher", "high")
	otherRoute.RouteKey = "route1:another-model"
	routeRef := f.user("c-route", "domani a Torino piove?", 0.98, otherRoute)
	otherPolicy := recallLiveLabel("user", "high")
	otherPolicy.PolicyVersion = "policy1:older"
	policyRef := f.user("c-policy", "tempo previsto domani a Torino", 0.98, otherPolicy)

	recall := f.recall(nil)
	for _, excluded := range []string{far, recallLiveSelf, ctxRef, routeRef, policyRef} {
		if slices.Contains(refs(recall.UserLabels), excluded) || slices.Contains(refs(recall.TeacherLabels), excluded) {
			t.Errorf("%s was returned as a label", excluded)
		}
	}
	if slices.Contains(refs(recall.ToolTurns), ctxRef) {
		t.Errorf("a tool turn from another context was returned")
	}
}

// Five closer rows of the wrong kind must not hide the one the pool is for (spec,
// "Production retrieval must apply these predicates BEFORE each pool's top-k").
func TestRecallTurnsLiveKeepsTheRightRowBehindCloserCopies(t *testing.T) {
	f := newRecallFixture(t)
	unlabelled := TurnDecision{ContextKey: recallLiveKey}
	for index := range 6 {
		f.user(fmt.Sprintf("c-copy-%d", index), fmt.Sprintf("che tempo farà domani a Torino %d", index), 0.995, unlabelled)
		f.user(fmt.Sprintf("c-teach-%d", index), fmt.Sprintf("meteo di domani su Torino %d", index), 0.995, recallLiveLabel("teacher", "low"))
		conversation := fmt.Sprintf("c-stale-%d", index)
		f.user(conversation, fmt.Sprintf("tempo domani Torino %d", index), 0.995, unlabelled)
		f.answer(conversation, map[string]string{"stale_tool": "succeeded"})
	}
	userRef := f.user("c-user", "previsioni per domani a Torino", 0.95, recallLiveLabel("user", "low"))
	f.answer("c-user", map[string]string{"weather_lookup": "succeeded"})

	started := time.Now()
	recall := f.recall(nil)
	t.Logf("recall wall time over this corpus: %s", time.Since(started))
	if len(recall.UserLabels) != 1 || recall.UserLabels[0].SourceRef != userRef {
		t.Fatalf("user labels = %v: teacher rows or unlabelled copies crowded out the user label", refs(recall.UserLabels))
	}
	if len(recall.TeacherLabels) != recallNeighbours {
		t.Fatalf("teacher labels = %d, want the pool full of teacher rows", len(recall.TeacherLabels))
	}
	if len(recall.ToolTurns) != 1 || recall.ToolTurns[0].SourceRef != userRef {
		t.Fatalf("tool turns = %v: turns that ran only an unusable tool crowded out a usable one", refs(recall.ToolTurns))
	}
}

// On a corpus small enough to rank by hand, bounded ANN must return exactly the exact top-k
// within the radius, in order. Any miss is reported with both lists.
func TestRecallTurnsLiveMatchesExactDistances(t *testing.T) {
	f := newRecallFixture(t)
	type labelled struct {
		ref    string
		cosine float64
	}
	var corpus []labelled
	for index, cosine := range []float64{0.999, 0.99, 0.98, 0.97, 0.96, 0.95, 0.94, 0.93, 0.85, 0.6} {
		ref := f.user(fmt.Sprintf("c-exact-%d", index), fmt.Sprintf("previsione del tempo %d", index), cosine, recallLiveLabel("teacher", "low"))
		corpus = append(corpus, labelled{ref: ref, cosine: cosine})
	}
	var exact []string
	for _, row := range corpus {
		if 1-row.cosine <= recallRadius && len(exact) < recallNeighbours {
			exact = append(exact, row.ref)
		}
	}
	recall := f.recall(nil)
	got := refs(recall.TeacherLabels)
	for _, turn := range recall.TeacherLabels {
		t.Logf("returned %s distance %.6f", turn.SourceRef, turn.Distance)
	}
	if !slices.Equal(got, exact) {
		t.Fatalf("bounded ANN = %v, exact = %v", got, exact)
	}
}
