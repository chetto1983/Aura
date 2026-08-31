package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
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

	cursor, err := projector.ProjectPage(context.Background(), "identity-a", conversations.ProjectionCursor{})
	if err != nil {
		t.Fatalf("ProjectPage: %v", err)
	}
	if cursor.ConversationID != "conversation-1" || cursor.Seq != 1 {
		t.Fatalf("cursor = %+v", cursor)
	}
	if commandCount != 4 {
		t.Fatalf("ArcadeDB commands = %d, want conversation upsert, turn upsert, HAS_TURN, NEXT_TURN", commandCount)
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
	for range 2 {
		if err := projector.DeleteConversation(context.Background(), "identity-a", "conversation-1"); err != nil {
			t.Fatalf("DeleteConversation: %v", err)
		}
	}
	if len(sink.turns) != 0 {
		t.Fatalf("deleted projection remains: %+v", sink.turns)
	}
}
