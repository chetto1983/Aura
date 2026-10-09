package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/board"
	"github.com/chetto1983/aura/internal/identityctx"
)

// BoardTool is the agent's hand on the identity's work board (prd.md §16, "A work board for
// the identity and its agent", 2026-10-09): one deferred, action-multiplexed verb in the shape
// of `task`. It reads the board as a bounded snapshot, the rule StickyFlow's copilot measured
// (at most 32 cards of 160 characters), and acts only on cards it has seen in this
// conversation, so a card id the model invented is refused instead of guessed at.
type BoardTool struct {
	Store boardStore

	mu   sync.Mutex
	seen map[string]map[string]bool

	routerOnce sync.Once
	router     *ActionRouter
}

// boardStore is the seam over *board.Store, declared here at the consumer.
type boardStore interface {
	Board(ctx context.Context, identityID string) (board.Board, error)
	Cards(ctx context.Context, identityID string) ([]board.Card, error)
	Search(ctx context.Context, identityID, query string, limit int) ([]board.Card, error)
	AddCard(ctx context.Context, identityID string, in board.NewCard, source board.Source, actor board.Actor) (board.Card, error)
	UpdateCard(ctx context.Context, identityID, cardID string, patch board.CardPatch, actor board.Actor) (board.Card, error)
	MoveCard(ctx context.Context, identityID, cardID, column, beforeID string, actor board.Actor) (board.Card, error)
	DeleteCard(ctx context.Context, identityID, cardID string) (bool, error)
}

// The snapshot bound the model reads, from StickyFlow's copilot (snapshot.ts).
const (
	boardSnapshotCards = 32
	boardSnapshotRunes = 160
)

const boardParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["list", "search", "add", "update", "move", "delete"], "description": "list (the board: columns and up to 32 cards, optionally one column), search (cards whose label or description contains query, or tagged with it), add (a new card), update (change a card's fields), move (to another column or position), delete (remove a card; asks the operator first)."},
    "column": {"type": "string", "description": "list: show only this column. add: the column to add to (default: the first). move: the target column."},
    "query": {"type": "string", "description": "Required when action=search."},
    "id": {"type": "string", "description": "Required when action=update, move or delete: the id of a card that list, search or add returned in this conversation."},
    "label": {"type": "string", "description": "Required when action=add; optional for update. One line, at most 200 characters."},
    "description": {"type": "string", "description": "Optional for add and update. At most 4000 characters."},
    "priority": {"type": "integer", "enum": [1, 2, 3], "description": "Optional for add and update: 1 low, 2 medium (default), 3 high."},
    "tags": {"type": "array", "items": {"type": "string"}, "description": "Optional for add and update: short tags; on update they replace the card's tags."},
    "due": {"type": "string", "description": "Optional for add and update: an RFC-3339 due date, or an empty string on update to clear it."},
    "before": {"type": "string", "description": "Optional for move: put the card just before this card id; omitted means the bottom of the column."}
  },
  "required": ["action"]
}`

// Spec returns the deferred manifest entry. The Summary carries the words an operator uses for
// a board, because tool_search's BM25 over it is how the model finds the tool.
func (t *BoardTool) Spec() Spec {
	return Spec{
		Name:    "board",
		Summary: "The operator's work board (kanban, backlog, to-do list, cards in columns such as To do, Doing, Done): list, search, add, update, move or delete cards for work that outlives this turn.",
		Description: "The identity's work board, shared with the operator who sees it in the cockpit. Use it for work that outlives this turn: something to do later, a follow-up, a task the operator mentions in passing. " +
			"Use todo, not board, for the steps of the turn you are in, and task, not board, when something must happen at a set time; a card can name that task. " +
			"action=list shows the columns and up to 32 cards; action=search finds cards; add, update and move change the board; delete removes a card and stops for the operator's approval. " +
			"Only act on card ids that list, search or add returned in this conversation, and never delete a card the operator did not ask you to delete.",
		Parameters:     json.RawMessage(boardParamsSchema),
		Deferred:       true,
		Mutating:       true,
		Multiplexed:    true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
	}
}

// Execute dispatches the action through the router.
func (t *BoardTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return dispatchStoreAction(ctx, "board", raw, t.Store != nil, "no board is configured", t.actionRouter)
}

func (t *BoardTool) actionRouter() *ActionRouter {
	t.routerOnce.Do(func() {
		t.router = NewActionRouter(map[string]ActionFunc{
			"list":   t.actionList,
			"search": t.actionSearch,
			"add":    t.actionAdd,
			"update": t.actionUpdate,
			"move":   t.actionMove,
			"delete": t.actionDelete,
		})
	})
	return t.router
}

// boardIdentity is the identity whose board a call works: the same fallback every other
// no-principal path uses.
func boardIdentity(ctx context.Context) string {
	if id := identityctx.IdentityID(ctx); id != "" {
		return id
	}
	return identityctx.LocalOperatorIdentity
}

// boardOrigin reads where a call comes from off its session. A conversation's own session is
// its UUID; a scheduled job (agent_job:<run>) or a swarm worker (<conv>-swarm-w<i>) is not,
// and adds its cards as background work with no conversation link.
func boardOrigin(ctx context.Context) (conversationID string, source board.Source) {
	session := SessionIDFromContext(ctx)
	if _, err := uuid.Parse(session); err == nil {
		return session, board.SourceChat
	}
	return "", board.SourceBackground
}

func (t *BoardTool) remember(ctx context.Context, cards ...board.Card) {
	key := SessionIDFromContext(ctx)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seen == nil {
		t.seen = map[string]map[string]bool{}
	}
	if t.seen[key] == nil {
		t.seen[key] = map[string]bool{}
	}
	for _, c := range cards {
		t.seen[key][c.ID] = true
	}
}

// requireSeen refuses an id this conversation has not been shown.
func (t *BoardTool) requireSeen(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("board: id is required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.seen[SessionIDFromContext(ctx)][id] {
		return fmt.Errorf("card %s is not on the board you listed: run board list or board search first", id)
	}
	return nil
}

// Evict forgets the ids a finished session was shown (SessionEvictor, R-41), so a
// long-running daemon does not keep them for good.
func (t *BoardTool) Evict(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.seen, sessionID)
}

func parseDue(raw *string) (**time.Time, error) {
	if raw == nil {
		return nil, nil
	}
	var due *time.Time
	if *raw != "" {
		parsed, err := time.Parse(time.RFC3339, *raw)
		if err != nil {
			return nil, fmt.Errorf("board: due must be an RFC-3339 date, e.g. 2026-10-10T09:00:00Z")
		}
		due = &parsed
	}
	return &due, nil
}
