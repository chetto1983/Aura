package runner

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
)

const (
	defaultConversationProjectionPageSize = 100
	conversationProjectionQueueSize       = 64
	conversationProjectionAttempts        = 3
	conversationProjectionWorkTimeout     = 30 * time.Second
)

// ConversationProjectionSource is the PostgreSQL-authoritative replay feed.
type ConversationProjectionSource interface {
	ListProjectionTurns(context.Context, string, conversations.ProjectionCursor, int) ([]conversations.ProjectionTurn, conversations.ProjectionCursor, error)
}

type conversationProjectionTombstoneSource interface {
	ListProjectionTombstones(context.Context, string, string, int) ([]conversations.ProjectionTombstone, string, error)
}

// ConversationProjectionSink is the rebuildable derived graph boundary.
//
// ProjectedThroughSeq is a READ on that boundary, and it is here rather than in its own
// interface because the same tenant resolution answers it: the context ladder needs to
// know how far a conversation reached the graph before it tells the model a dropped span
// is still readable there.
type ConversationProjectionSink interface {
	ApplyConversationProjection(context.Context, arcadedb.ConversationProjection) error
	ProjectedThroughSeq(ctx context.Context, identityID, conversationID string) (int, error)
	DeleteConversationProjection(context.Context, string, string) error
	DeleteIdentityConversationProjections(context.Context, string) error
	PruneConversationProjections(context.Context, string, []string) error
}

// ProjectedThroughSeq asks the sink how far this conversation reached the graph, for the
// context ladder's offload pointer. It is fail-soft on purpose and answers 0 for a nil
// projector, a missing identity, or any error: 0 means "claim nothing", so a projection
// this call could not read costs a silent drop rather than a pointer at turns the graph
// may not hold.
func (p *ConversationProjector) ProjectedThroughSeq(
	ctx context.Context,
	identityID, conversationID string,
) int {
	if p == nil || p.sink == nil || strings.TrimSpace(identityID) == "" ||
		strings.TrimSpace(conversationID) == "" {
		return 0
	}
	through, err := p.sink.ProjectedThroughSeq(ctx, identityID, conversationID)
	if err != nil {
		return 0
	}
	return max(through, 0)
}

type conversationProjectionOffer struct {
	identityID string
}

// ConversationProjector moves ordered authoritative pages into the derived graph.
type ConversationProjector struct {
	source   ConversationProjectionSource
	sink     ConversationProjectionSink
	pageSize int

	queue      chan conversationProjectionOffer
	workerDone chan struct{}

	mu      sync.Mutex
	closed  bool
	pending int
	drained chan struct{}
	lastErr error
	cursors map[string]conversations.ProjectionCursor
	once    sync.Once
}

// NewConversationProjector binds an authoritative source to one derived sink.
func NewConversationProjector(source ConversationProjectionSource, sink ConversationProjectionSink, pageSize int) *ConversationProjector {
	if pageSize <= 0 {
		pageSize = defaultConversationProjectionPageSize
	}
	drained := make(chan struct{})
	close(drained)
	p := &ConversationProjector{
		source: source, sink: sink, pageSize: pageSize,
		queue:      make(chan conversationProjectionOffer, conversationProjectionQueueSize),
		workerDone: make(chan struct{}), drained: drained,
		cursors: make(map[string]conversations.ProjectionCursor),
	}
	go p.run()
	return p
}

// ProjectPage applies one ordered page and returns its authoritative cursor.
// The cursor advances only after every graph write succeeds, so a retry replays
// the whole page instead of skipping a partially projected tail.
func (p *ConversationProjector) ProjectPage(
	ctx context.Context,
	identityID string,
	after conversations.ProjectionCursor,
) (conversations.ProjectionCursor, error) {
	if err := p.validate(identityID); err != nil {
		return after, err
	}
	turns, next, err := p.source.ListProjectionTurns(ctx, identityID, after, p.pageSize)
	if err != nil {
		return after, fmt.Errorf("runner: list conversation projection page: %w", err)
	}
	if len(turns) > 0 && next == after {
		return after, fmt.Errorf("runner: conversation projection source did not advance its cursor")
	}
	if err := p.applyTurns(ctx, identityID, turns); err != nil {
		return after, err
	}
	return next, nil
}

func (p *ConversationProjector) applyTurns(
	ctx context.Context,
	identityID string,
	turns []conversations.ProjectionTurn,
) error {
	projections := make([]arcadedb.ConversationProjection, 0)
	byConversation := make(map[string]int)
	for _, turn := range turns {
		if turn.IdentityID != identityID {
			return fmt.Errorf("runner: projection source returned foreign identity %q", turn.IdentityID)
		}
		index, ok := byConversation[turn.ConversationID]
		if !ok {
			index = len(projections)
			byConversation[turn.ConversationID] = index
			projections = append(projections, arcadedb.ConversationProjection{
				IdentityID: identityID, ConversationID: turn.ConversationID,
			})
		}
		projections[index].Turns = append(projections[index].Turns, arcadedb.ConversationTurnProjection{
			IdentityID: turn.IdentityID, ConversationID: turn.ConversationID,
			Seq: turn.Seq, Role: turn.Role, Content: turn.Content,
			ContentHash: turn.ContentHash, OccurredAt: turn.OccurredAt, SourceRef: turn.SourceRef,
		})
	}
	for _, projection := range projections {
		if err := p.sink.ApplyConversationProjection(ctx, projection); err != nil {
			return fmt.Errorf("runner: apply conversation projection %s: %w", projection.ConversationID, err)
		}
	}
	return nil
}

func (p *ConversationProjector) validate(identityID string) error {
	if p == nil || p.source == nil || p.sink == nil {
		return fmt.Errorf("runner: conversation projector is not initialized")
	}
	if strings.TrimSpace(identityID) == "" {
		return fmt.Errorf("runner: conversation projection identity must be non-empty")
	}
	return nil
}

// OfferConversation schedules fail-soft projection work for an identity. A
// full queue returns false immediately; reconciliation remains the recovery path.
func (p *ConversationProjector) OfferConversation(identityID string) bool {
	if p == nil || strings.TrimSpace(identityID) == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	select {
	case p.queue <- conversationProjectionOffer{identityID: identityID}:
		if p.pending == 0 {
			p.drained = make(chan struct{})
			p.lastErr = nil
		}
		p.pending++
		return true
	default:
		return false
	}
}

func (p *ConversationProjector) run() {
	defer close(p.workerDone)
	for offer := range p.queue {
		err := p.projectOffer(offer.identityID)
		p.completeOffer(err)
	}
}

func (p *ConversationProjector) projectOffer(identityID string) error {
	p.mu.Lock()
	cursor := p.cursors[identityID]
	p.mu.Unlock()
	var err error
	for attempt := range conversationProjectionAttempts {
		ctx, cancel := context.WithTimeout(context.Background(), conversationProjectionWorkTimeout)
		var next conversations.ProjectionCursor
		next, err = p.ProjectPage(ctx, identityID, cursor)
		cancel()
		if err == nil {
			p.mu.Lock()
			p.cursors[identityID] = next
			p.mu.Unlock()
			return nil
		}
		if attempt+1 < conversationProjectionAttempts {
			timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
			<-timer.C
		}
	}
	return err
}

func (p *ConversationProjector) completeOffer(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.lastErr = errors.Join(p.lastErr, err)
	}
	p.pending--
	if p.pending == 0 {
		close(p.drained)
	}
}

// Flush waits for every accepted offer and reports projection failures.
func (p *ConversationProjector) Flush(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	drained := p.drained
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return fmt.Errorf("runner: flush conversation projection: %w", ctx.Err())
	case <-drained:
		p.mu.Lock()
		err := p.lastErr
		p.mu.Unlock()
		return err
	}
}

// Reconcile replays PostgreSQL from the beginning and prunes absent graph records.
func (p *ConversationProjector) Reconcile(ctx context.Context, identityID string) error {
	if err := p.validate(identityID); err != nil {
		return err
	}
	cursor := conversations.ProjectionCursor{}
	live := make(map[string]struct{})
	for {
		turns, next, err := p.source.ListProjectionTurns(ctx, identityID, cursor, p.pageSize)
		if err != nil {
			return fmt.Errorf("runner: reconcile conversation projection: %w", err)
		}
		if len(turns) > 0 && next == cursor {
			return fmt.Errorf("runner: reconcile conversation projection: source cursor did not advance")
		}
		for _, turn := range turns {
			live[turn.ConversationID] = struct{}{}
		}
		if err := p.applyTurns(ctx, identityID, turns); err != nil {
			return err
		}
		if len(turns) == 0 {
			break
		}
		cursor = next
	}
	if tombstones, ok := p.source.(conversationProjectionTombstoneSource); ok {
		if err := p.applyTombstones(ctx, identityID, tombstones); err != nil {
			return err
		}
	}
	liveIDs := make([]string, 0, len(live))
	for conversationID := range live {
		liveIDs = append(liveIDs, conversationID)
	}
	slices.Sort(liveIDs)
	if err := p.sink.PruneConversationProjections(ctx, identityID, liveIDs); err != nil {
		return fmt.Errorf("runner: prune conversation projections: %w", err)
	}
	p.mu.Lock()
	p.cursors[identityID] = cursor
	p.mu.Unlock()
	return nil
}

func (p *ConversationProjector) applyTombstones(
	ctx context.Context,
	identityID string,
	source conversationProjectionTombstoneSource,
) error {
	after := ""
	for {
		tombstones, next, err := source.ListProjectionTombstones(ctx, identityID, after, p.pageSize)
		if err != nil {
			return fmt.Errorf("runner: list conversation projection tombstones: %w", err)
		}
		for _, tombstone := range tombstones {
			if tombstone.IdentityID != identityID {
				return fmt.Errorf("runner: projection tombstone returned foreign identity %q", tombstone.IdentityID)
			}
			if err := p.DeleteConversation(ctx, identityID, tombstone.ConversationID); err != nil {
				return err
			}
		}
		if len(tombstones) == 0 {
			return nil
		}
		if next == after {
			return fmt.Errorf("runner: conversation projection tombstone cursor did not advance")
		}
		after = next
	}
}

// DeleteConversation removes one derived projection idempotently.
func (p *ConversationProjector) DeleteConversation(ctx context.Context, identityID, conversationID string) error {
	if err := p.validate(identityID); err != nil {
		return err
	}
	if strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("runner: delete conversation projection requires conversation id")
	}
	return p.sink.DeleteConversationProjection(ctx, identityID, conversationID)
}

// DeleteIdentity removes every derived conversation for an identity.
func (p *ConversationProjector) DeleteIdentity(ctx context.Context, identityID string) error {
	if err := p.validate(identityID); err != nil {
		return err
	}
	return p.sink.DeleteIdentityConversationProjections(ctx, identityID)
}

// Close drains accepted work and joins the ordered worker.
func (p *ConversationProjector) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	p.once.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.queue)
		p.mu.Unlock()
	})
	select {
	case <-ctx.Done():
		return fmt.Errorf("runner: close conversation projector: %w", ctx.Err())
	case <-p.workerDone:
		p.mu.Lock()
		err := p.lastErr
		p.mu.Unlock()
		return err
	}
}
