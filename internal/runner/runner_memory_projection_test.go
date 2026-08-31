package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
)

type tracerProjectionSource struct {
	turns []conversations.ProjectionTurn
}

func (s tracerProjectionSource) ListProjectionTurns(
	context.Context,
	string,
	conversations.ProjectionCursor,
	int,
) ([]conversations.ProjectionTurn, conversations.ProjectionCursor, error) {
	return append([]conversations.ProjectionTurn(nil), s.turns...),
		conversations.ProjectionCursor{ConversationID: "conversation-1", Seq: 1}, nil
}

func TestConversationProjectionTracer(t *testing.T) {
	const content = "Remember the blue notebook"
	sum := sha256.Sum256([]byte(content))
	contentHash := hex.EncodeToString(sum[:])
	source := tracerProjectionSource{turns: []conversations.ProjectionTurn{{
		IdentityID: "identity-a", ConversationID: "conversation-1", Seq: 1,
		Role: "user", Content: content, ContentHash: contentHash,
		OccurredAt: time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		SourceRef:  "postgres://conversation/conversation-1/turn/1",
	}}}
	var commandCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var payload struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal(raw, &payload)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/query/aura" {
			_, _ = io.WriteString(w, `{"result":[{"identity_id":"identity-a","conversation_id":"conversation-1","turn_seq":1,"role":"user","content":"Remember the blue notebook","content_hash":"`+contentHash+`","occurred_at":"2026-08-31T12:00:00Z","source_ref":"postgres://conversation/conversation-1/turn/1"}]}`)
			return
		}
		if payload.Command != "" {
			commandCount++
		}
		_, _ = io.WriteString(w, `{"result":[]}`)
	}))
	t.Cleanup(server.Close)
	sink, err := arcadedb.New(arcadedb.Config{BaseURL: server.URL, Database: "aura", User: "root"})
	if err != nil {
		t.Fatalf("arcadedb.New: %v", err)
	}
	projector := NewConversationProjector(source, sink, 16)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })

	cursor, err := projector.ProjectPage(context.Background(), "identity-a", conversations.ProjectionCursor{})
	if err != nil {
		t.Fatalf("ProjectPage: %v", err)
	}
	if cursor.ConversationID != "conversation-1" || cursor.Seq != 1 {
		t.Fatalf("cursor = %+v", cursor)
	}
	if commandCount != 5 {
		t.Fatalf("ArcadeDB commands = %d, want conversation upsert, turn upsert, stale-vector clear, HAS_TURN, NEXT_TURN", commandCount)
	}
	result, err := sink.SearchConversationTurnsHybrid(context.Background(), "identity-a", "blue notebook", 5)
	if err != nil {
		t.Fatalf("SearchConversationTurnsHybrid: %v", err)
	}
	if len(result.Turns) != 1 || result.Turns[0].Content != content || result.Turns[0].SourceRef == "" {
		t.Fatalf("search result lost content/provenance: %+v", result)
	}
}

type reconciliationProjectionSource struct {
	mu    sync.Mutex
	turns []conversations.ProjectionTurn
}

func (s *reconciliationProjectionSource) ListProjectionTurns(
	_ context.Context,
	identityID string,
	after conversations.ProjectionCursor,
	limit int,
) ([]conversations.ProjectionTurn, conversations.ProjectionCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	turns := append([]conversations.ProjectionTurn(nil), s.turns...)
	sort.Slice(turns, func(i, j int) bool {
		if turns[i].ConversationID == turns[j].ConversationID {
			return turns[i].Seq < turns[j].Seq
		}
		return turns[i].ConversationID < turns[j].ConversationID
	})
	out := make([]conversations.ProjectionTurn, 0, min(limit, len(turns)))
	next := after
	for _, turn := range turns {
		if turn.IdentityID != identityID || turn.ConversationID < after.ConversationID ||
			(turn.ConversationID == after.ConversationID && turn.Seq <= after.Seq) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, turn)
		next = conversations.ProjectionCursor{ConversationID: turn.ConversationID, Seq: turn.Seq}
	}
	return out, next, nil
}

func (s *reconciliationProjectionSource) replaceContent(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := sha256.Sum256([]byte(content))
	s.turns[0].Content = content
	s.turns[0].ContentHash = hex.EncodeToString(sum[:])
}

type reconciliationProjectionSink struct {
	mu        sync.Mutex
	turns     map[string]arcadedb.ConversationTurnProjection
	order     []int
	failApply int
}

func newReconciliationProjectionSink() *reconciliationProjectionSink {
	return &reconciliationProjectionSink{turns: make(map[string]arcadedb.ConversationTurnProjection)}
}

func (s *reconciliationProjectionSink) ApplyConversationProjection(_ context.Context, p arcadedb.ConversationProjection) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failApply > 0 {
		s.failApply--
		return context.DeadlineExceeded
	}
	for _, turn := range p.Turns {
		key := turn.IdentityID + "/" + turn.ConversationID + "/" + strconv.Itoa(turn.Seq)
		s.turns[key] = turn
		s.order = append(s.order, turn.Seq)
	}
	return nil
}

func (s *reconciliationProjectionSink) DeleteConversationProjection(_ context.Context, identityID, conversationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, turn := range s.turns {
		if turn.IdentityID == identityID && turn.ConversationID == conversationID {
			delete(s.turns, key)
		}
	}
	return nil
}

func (s *reconciliationProjectionSink) DeleteIdentityConversationProjections(_ context.Context, identityID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, turn := range s.turns {
		if turn.IdentityID == identityID {
			delete(s.turns, key)
		}
	}
	return nil
}

func (s *reconciliationProjectionSink) PruneConversationProjections(_ context.Context, identityID string, live []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	wanted := make(map[string]struct{}, len(live))
	for _, conversationID := range live {
		wanted[conversationID] = struct{}{}
	}
	for key, turn := range s.turns {
		if turn.IdentityID != identityID {
			continue
		}
		if _, ok := wanted[turn.ConversationID]; !ok {
			delete(s.turns, key)
		}
	}
	return nil
}

func testProjectionTurn(seq int, content string) conversations.ProjectionTurn {
	sum := sha256.Sum256([]byte(content))
	return conversations.ProjectionTurn{
		IdentityID: "identity-a", ConversationID: "conversation-1", Seq: seq,
		Role: "user", Content: content, ContentHash: hex.EncodeToString(sum[:]),
		OccurredAt: time.Date(2026, 8, 31, 12, seq, 0, 0, time.UTC),
		SourceRef:  "postgres://conversation/conversation-1/turn/" + strconv.Itoa(seq),
	}
}

func TestConversationProjectionReconcileRepairsRestartGapAndEdit(t *testing.T) {
	source := &reconciliationProjectionSource{turns: []conversations.ProjectionTurn{
		testProjectionTurn(1, "restart gap original"),
	}}
	sink := newReconciliationProjectionSink()
	projector := NewConversationProjector(source, sink, 1)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })

	if err := projector.Reconcile(context.Background(), "identity-a"); err != nil {
		t.Fatalf("Reconcile(restart): %v", err)
	}
	if len(sink.turns) != 1 {
		t.Fatalf("restart reconciliation projected %d turns, want 1", len(sink.turns))
	}
	source.replaceContent("restart gap edited")
	if err := projector.Reconcile(context.Background(), "identity-a"); err != nil {
		t.Fatalf("Reconcile(edit): %v", err)
	}
	if len(sink.turns) != 1 {
		t.Fatalf("edit reconciliation duplicated turn: %+v", sink.turns)
	}
	for _, turn := range sink.turns {
		if turn.Content != "restart gap edited" {
			t.Fatalf("edit did not replace derived content: %+v", turn)
		}
	}
}

func TestConversationProjectionQueueRetriesAndFlushesInOrder(t *testing.T) {
	source := &reconciliationProjectionSource{turns: []conversations.ProjectionTurn{
		testProjectionTurn(1, "first committed turn"),
		testProjectionTurn(2, "second committed turn"),
	}}
	sink := newReconciliationProjectionSink()
	sink.failApply = 1
	projector := NewConversationProjector(source, sink, 1)
	// Named results rather than `!offer() || !offer()`, which staticcheck reads as a
	// duplicated expression (SA4000). Semantics are unchanged -- `||` evaluates the second
	// call whenever the first offer succeeded -- and the point is that the bounded queue
	// accepts BOTH offers, not that either one did.
	firstOffer := projector.OfferConversation("identity-a")
	secondOffer := projector.OfferConversation("identity-a")
	if !firstOffer || !secondOffer {
		t.Fatal("bounded queue rejected available capacity")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := projector.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if err := projector.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if projector.OfferConversation("identity-a") {
		t.Fatal("closed projector accepted work")
	}
	if len(sink.order) != 2 || sink.order[0] != 1 || sink.order[1] != 2 {
		t.Fatalf("projection order = %v, want [1 2]", sink.order)
	}
}

func TestConversationProjectionDeleteConverges(t *testing.T) {
	source := &reconciliationProjectionSource{turns: []conversations.ProjectionTurn{
		testProjectionTurn(1, "delete me"),
	}}
	sink := newReconciliationProjectionSink()
	projector := NewConversationProjector(source, sink, 1)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })
	if err := projector.Reconcile(context.Background(), "identity-a"); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	source.mu.Lock()
	source.turns = nil
	source.mu.Unlock()
	for range 2 {
		if err := projector.Reconcile(context.Background(), "identity-a"); err != nil {
			t.Fatalf("Reconcile(delete): %v", err)
		}
	}
	if len(sink.turns) != 0 {
		t.Fatalf("deleted projection remains: %+v", sink.turns)
	}
}

type postCommitProjectionSource struct {
	mu    sync.Mutex
	turns []conversations.ProjectionTurn
	calls int
	order []string
}

func (s *postCommitProjectionSource) record(ctx context.Context, p conversations.AppendTurnParams) {
	if (p.Role != llm.RoleUser && p.Role != llm.RoleAssistant) ||
		strings.TrimSpace(p.Content) == "" || len(p.ToolCalls) != 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.order = append(s.order, "append")
	sum := sha256.Sum256([]byte(p.Content))
	seq := len(s.turns) + 1
	s.turns = append(s.turns, conversations.ProjectionTurn{
		IdentityID: identityctx.IdentityID(ctx), ConversationID: p.ConversationID,
		Seq: seq, Role: string(p.Role), Content: p.Content,
		ContentHash: hex.EncodeToString(sum[:]),
		OccurredAt:  time.Date(2026, 8, 31, 12, 0, seq, 0, time.UTC),
		SourceRef:   "postgres://conversation/" + p.ConversationID + "/turn/" + strconv.Itoa(seq),
	})
}

func (s *postCommitProjectionSource) ListProjectionTurns(
	_ context.Context,
	identityID string,
	after conversations.ProjectionCursor,
	limit int,
) ([]conversations.ProjectionTurn, conversations.ProjectionCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.order = append(s.order, "offer")
	out := make([]conversations.ProjectionTurn, 0, limit)
	next := after
	for _, turn := range s.turns {
		if turn.IdentityID != identityID || turn.ConversationID < after.ConversationID ||
			(turn.ConversationID == after.ConversationID && turn.Seq <= after.Seq) {
			continue
		}
		if len(out) == limit {
			break
		}
		out = append(out, turn)
		next = conversations.ProjectionCursor{ConversationID: turn.ConversationID, Seq: turn.Seq}
	}
	return out, next, nil
}

type postCommitConversationStore struct {
	*fakeConvStore
	source *postCommitProjectionSource
}

func (s *postCommitConversationStore) AppendTurn(ctx context.Context, p conversations.AppendTurnParams) error {
	if err := s.fakeConvStore.AppendTurn(ctx, p); err != nil {
		return err
	}
	s.source.record(ctx, p)
	return nil
}

func (s *postCommitConversationStore) AppendAssistantTurnWithCacheMetric(
	ctx context.Context,
	p conversations.AppendTurnParams,
	metric sqlc.InsertCacheMetricParams,
) error {
	if err := s.fakeConvStore.AppendAssistantTurnWithCacheMetric(ctx, p, metric); err != nil {
		return err
	}
	s.source.record(ctx, p)
	return nil
}

func newPostCommitProjectionHarness(t *testing.T) (*Runner, *postCommitConversationStore, *postCommitProjectionSource, *reconciliationProjectionSink, context.Context) {
	t.Helper()
	source := &postCommitProjectionSource{}
	store := &postCommitConversationStore{fakeConvStore: newFakeConvStore(), source: source}
	cache := newFakeCacheMetricStore()
	store.cache = cache
	sink := newReconciliationProjectionSink()
	projector := NewConversationProjector(source, sink, 1)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := projector.Close(ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("close projector: %v", err)
		}
	})
	r := &Runner{Conv: store, cacheMetrics: cache, conversationProjector: projector}
	ctx := identityctx.WithIdentityID(context.Background(), "identity-a")
	return r, store, source, sink, ctx
}

func TestConversationProjectionPostCommit(t *testing.T) {
	r, _, source, sink, ctx := newPostCommitProjectionHarness(t)
	convID := newConvID(t)
	if err := r.appendUserTurn(ctx, convID, "first committed turn"); err != nil {
		t.Fatalf("append first user turn: %v", err)
	}
	if err := r.appendUserTurn(ctx, convID, "second committed turn"); err != nil {
		t.Fatalf("append second user turn: %v", err)
	}
	if err := r.persistAssistantAnswer(ctx, &turnTracker{convID: convID}, &agent.Event{
		LLMResponse: &agent.LLMResponse{Content: "final assistant answer", FinishReason: "stop"},
	}); err != nil {
		t.Fatalf("persist final assistant answer: %v", err)
	}
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.conversationProjector.Flush(flushCtx); err != nil {
		t.Fatalf("flush projection: %v", err)
	}
	source.mu.Lock()
	order := append([]string(nil), source.order...)
	source.mu.Unlock()
	for i := 0; i < len(order); i += 2 {
		if i+1 >= len(order) || order[i] != "append" || order[i+1] != "offer" {
			t.Fatalf("projection order = %v, want append before every offer", order)
		}
	}
	if got := sink.order; len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 3 {
		t.Fatalf("projected turn order = %v, want [1 2 3]", got)
	}

	tr := &turnTracker{convID: convID}
	if err := r.persistEvent(ctx, tr, &agent.Event{LLMResponse: &agent.LLMResponse{Reasoning: "private scratch"}}); err != nil {
		t.Fatalf("persist reasoning event: %v", err)
	}
	source.mu.Lock()
	callsAfterReasoning := source.calls
	source.mu.Unlock()
	if callsAfterReasoning != 3 {
		t.Fatalf("ineligible reasoning event offered projection work: calls=%d", callsAfterReasoning)
	}
}

func TestConversationProjectionFailSoft(t *testing.T) {
	r, store, source, sink, ctx := newPostCommitProjectionHarness(t)
	convID := newConvID(t)
	sink.failApply = conversationProjectionAttempts
	if err := r.appendUserTurn(ctx, convID, "durable despite graph failure"); err != nil {
		t.Fatalf("committed source turn failed because graph failed: %v", err)
	}
	flushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.conversationProjector.Flush(flushCtx); err == nil {
		t.Fatal("projection failure was not observable through Flush")
	}
	if got := len(store.turns[convID]); got != 1 {
		t.Fatalf("committed source turns = %d, want 1", got)
	}

	failedSource := &postCommitProjectionSource{}
	failedStore := &postCommitConversationStore{fakeConvStore: newFakeConvStore(), source: failedSource}
	failedStore.appendEr = errors.New("postgres append failed")
	failedProjector := NewConversationProjector(failedSource, newReconciliationProjectionSink(), 1)
	t.Cleanup(func() { _ = failedProjector.Close(context.Background()) })
	failedRunner := &Runner{Conv: failedStore, conversationProjector: failedProjector}
	if err := failedRunner.appendUserTurn(ctx, convID, "must not project"); err == nil {
		t.Fatal("failed PostgreSQL append unexpectedly succeeded")
	}
	if err := failedProjector.Flush(flushCtx); err != nil {
		t.Fatalf("flush after rejected append: %v", err)
	}
	failedSource.mu.Lock()
	failedCalls := failedSource.calls
	failedSource.mu.Unlock()
	if failedCalls != 0 {
		t.Fatalf("failed append offered projection work %d times", failedCalls)
	}
	if source.calls == 0 {
		t.Fatal("successful source append never reached the projector")
	}
}
