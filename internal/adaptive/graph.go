package adaptive

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// GraphWriter executes the private adaptive Cypher projection.
type GraphWriter interface {
	Write(context.Context, string, map[string]any) ([]map[string]any, error)
}

// GraphStore projects the authoritative Postgres outbox into ArcadeDB's shared
// database. It anchors on :User and deliberately uses private labels that the
// memory tools do not query, so the projection stays invisible to recall.
type GraphStore struct {
	writer GraphWriter
}

// NewGraphStore binds an adaptive projection to its graph writer.
func NewGraphStore(writer GraphWriter) *GraphStore {
	return &GraphStore{writer: writer}
}

// Project idempotently writes one immutable outbox record to the graph.
func (g *GraphStore) Project(ctx context.Context, record OutboxRecord) error {
	if g == nil || g.writer == nil {
		return errors.New("adaptive graph store requires a writer")
	}
	if record.ID == uuid.Nil || record.OwnerID == uuid.Nil || record.AggregateID == "" || record.Sequence <= 0 {
		return errors.New("adaptive graph projection record is incomplete")
	}
	rows, err := g.writer.Write(ctx, `
MERGE (u:User {identifier: $owner_id})
ON CREATE SET
  u.id = $owner_id,
  u.created_at = datetime($created_at),
  u.attributes_json = '{}'
ON MATCH SET
  u.id = coalesce(u.id, $owner_id),
  u.created_at = coalesce(u.created_at, datetime($created_at)),
  u.attributes_json = coalesce(u.attributes_json, '{}')
MERGE (episode:AdaptiveEpisode {id: $aggregate_id})
ON CREATE SET
  episode.owner_id = $owner_id,
  episode.created_at = $created_at
WITH u, episode
WHERE episode.owner_id = $owner_id
MERGE (u)-[:HAS_ADAPTIVE_EPISODE]->(episode)
MERGE (event:AdaptiveEvent {id: $event_id})
ON CREATE SET
  event.owner_id = $owner_id,
  event.aggregate_id = $aggregate_id,
  event.sequence = $sequence,
  event.decision_id = $decision_id,
  event.event_kind = $event_kind,
  event.payload_json = $payload_json,
  event.payload_hash = $payload_hash,
  event.created_at = $created_at
WITH episode, event
WHERE event.owner_id = $owner_id
  AND event.aggregate_id = $aggregate_id
  AND event.sequence = $sequence
  AND event.payload_hash = $payload_hash
MERGE (episode)-[rel:HAS_ADAPTIVE_EVENT]->(event)
ON CREATE SET rel.sequence = $sequence
RETURN event.id AS id
`, map[string]any{
		"owner_id":     record.OwnerID.String(),
		"aggregate_id": record.AggregateID,
		"event_id":     record.ID.String(),
		"sequence":     record.Sequence,
		"decision_id":  record.DecisionID.String(),
		"event_kind":   string(record.Kind),
		"payload_json": string(record.Payload),
		"payload_hash": hex.EncodeToString(record.PayloadHash),
		"created_at":   record.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"),
	})
	if err != nil {
		return fmt.Errorf("project adaptive event %s: %w", record.ID, err)
	}
	if len(rows) != 1 {
		return fmt.Errorf("project adaptive event %s: immutable graph conflict", record.ID)
	}
	return nil
}

// PurgeOwner removes only the private adaptive subgraph. The shared :User node
// and all agent-memory messages/entities/preferences remain owned by their
// existing deprovision lifecycle.
func (g *GraphStore) PurgeOwner(ctx context.Context, ownerID uuid.UUID) error {
	if g == nil || g.writer == nil {
		return errors.New("adaptive graph store requires a writer")
	}
	_, err := g.writer.Write(ctx, `
MATCH (episode:AdaptiveEpisode {owner_id: $owner_id})
OPTIONAL MATCH (episode)-[:HAS_ADAPTIVE_EVENT]->(event:AdaptiveEvent)
DETACH DELETE event, episode
`, map[string]any{"owner_id": ownerID.String()})
	if err != nil {
		return fmt.Errorf("purge adaptive owner %s: %w", ownerID, err)
	}
	return nil
}

// PurgeAdaptiveGraph adapts GraphStore to agui's deprovisioning port without coupling
// that package to UUID parsing or graph-store implementation details.
func (g *GraphStore) PurgeAdaptiveGraph(ctx context.Context, identityID string) error {
	ownerID, err := uuid.Parse(identityID)
	if err != nil {
		return fmt.Errorf("parse adaptive owner identity %q: %w", identityID, err)
	}
	return g.PurgeOwner(ctx, ownerID)
}
