package arcadedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/redact"
)

const (
	conversationVertexType = "Conversation"
	conversationTurnType   = "ConversationTurn"
	hasTurnEdgeType        = "HAS_TURN"
	nextTurnEdgeType       = "NEXT_TURN"
)

// ConversationTurnProjection is one PostgreSQL-authoritative searchable turn.
type ConversationTurnProjection struct {
	IdentityID     string
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ContentHash    string
	OccurredAt     time.Time
	SourceRef      string
}

// ConversationProjection batches authoritative turns under their graph parent.
type ConversationProjection struct {
	IdentityID     string
	ConversationID string
	Turns          []ConversationTurnProjection
}

// ConversationTurnHit is one identity-scoped turn read back by recall.
type ConversationTurnHit struct {
	IdentityID     string
	ConversationID string
	Seq            int
	Role           string
	Content        string
	ContentHash    string
	OccurredAt     string
	SourceRef      string
}

// conversationSchemaStatements owns the replay-safe short-term memory schema.
// Plan 49-07 is the single aggregation owner that adds this fragment to
// EnsureMemorySchema after the Wave-2 memory.go owner has landed.
func conversationSchemaStatements() []string {
	return append([]string{
		"CREATE VERTEX TYPE " + conversationVertexType + " IF NOT EXISTS",
		"CREATE PROPERTY " + conversationVertexType + ".identity_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationVertexType + ".conversation_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationVertexType + ".source_ref IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationVertexType + ".projected_through_seq IF NOT EXISTS INTEGER",
		"CREATE PROPERTY " + conversationVertexType + ".projection_updated_at IF NOT EXISTS DATETIME",
		"CREATE INDEX IF NOT EXISTS ON " + conversationVertexType + " (identity_id, conversation_id) UNIQUE",

		"CREATE VERTEX TYPE " + conversationTurnType + " IF NOT EXISTS",
		"CREATE PROPERTY " + conversationTurnType + ".identity_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".conversation_id IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".turn_seq IF NOT EXISTS INTEGER",
		"CREATE PROPERTY " + conversationTurnType + ".role IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".content IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".content_hash IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".occurred_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + conversationTurnType + ".source_ref IF NOT EXISTS STRING",
		"CREATE PROPERTY " + conversationTurnType + ".deleted_at IF NOT EXISTS DATETIME",
		"CREATE PROPERTY " + conversationTurnType + ".embedding IF NOT EXISTS ARRAY_OF_FLOATS",
		"CREATE INDEX IF NOT EXISTS ON " + conversationTurnType + " (identity_id, conversation_id, turn_seq) UNIQUE",
		"CREATE INDEX IF NOT EXISTS ON " + conversationTurnType + " (content) FULL_TEXT " +
			"METADATA {analyzer:'org.apache.lucene.analysis.en.EnglishAnalyzer'}",
		"CREATE INDEX IF NOT EXISTS ON " + conversationTurnType + " (embedding) LSM_VECTOR METADATA " +
			"{ \"dimensions\": " + strconv.Itoa(vectorDimensions) +
			", \"similarity\": \"COSINE\", \"quantization\": \"" + vectorQuantization + "\" }",

		"CREATE EDGE TYPE " + hasTurnEdgeType + " IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON " + hasTurnEdgeType + " (`@out`, `@in`) UNIQUE",
		"CREATE EDGE TYPE " + nextTurnEdgeType + " IF NOT EXISTS",
		"CREATE INDEX IF NOT EXISTS ON " + nextTurnEdgeType + " (`@out`, `@in`) UNIQUE",
	}, spaceStampStatements(conversationTurnType)...)
}

const upsertConversationProjectionStatement = "UPDATE " + conversationVertexType +
	" SET identity_id = :identity_id, conversation_id = :conversation_id," +
	" source_ref = :source_ref, projected_through_seq = :projected_through_seq," +
	" projection_updated_at = :projection_updated_at UPSERT RETURN AFTER" +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id"

const upsertConversationTurnStatement = "UPDATE " + conversationTurnType +
	" SET identity_id = :identity_id, conversation_id = :conversation_id," +
	" turn_seq = :turn_seq, role = :role, content = :content," +
	" content_hash = :content_hash, occurred_at = :occurred_at," +
	" source_ref = :source_ref, deleted_at = NULL"

const upsertConversationTurnWhere = " UPSERT RETURN AFTER WHERE identity_id = :identity_id" +
	" AND conversation_id = :conversation_id AND turn_seq = :turn_seq"

// storedTurnVectorsStatement names the turns this projection must not embed again: those
// with a vector, and those a space refused. Turns with neither are the reconciler's to
// fill; turns answered in another space are the pass's (spec §5), so no turn is embedded
// twice.
const storedTurnVectorsStatement = "SELECT turn_seq, content_hash FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id" +
	" AND (embedding IS NOT NULL OR embed_space IS NOT NULL)"

const createHasTurnStatement = "CREATE EDGE " + hasTurnEdgeType +
	" FROM (SELECT FROM " + conversationVertexType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id)" +
	" TO (SELECT FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id AND turn_seq = :turn_seq)" +
	" IF NOT EXISTS"

const createNextTurnStatement = "CREATE EDGE " + nextTurnEdgeType +
	" FROM (SELECT FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id" +
	" AND turn_seq < :turn_seq ORDER BY turn_seq DESC LIMIT 1)" +
	" TO (SELECT FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id AND turn_seq = :turn_seq)" +
	" IF NOT EXISTS"

// ApplyConversationProjection idempotently writes eligible turns and their order.
func (c *Client) ApplyConversationProjection(ctx context.Context, projection ConversationProjection) error {
	if err := validateConversationProjection(projection); err != nil {
		return err
	}
	highWater := 0
	for _, turn := range projection.Turns {
		highWater = max(highWater, turn.Seq)
	}
	params := map[string]any{
		"identity_id":           projection.IdentityID,
		"conversation_id":       projection.ConversationID,
		"source_ref":            conversationSourceRef(projection.ConversationID),
		"projected_through_seq": highWater,
		"projection_updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if _, err := c.Command(ctx, upsertConversationProjectionStatement, params); err != nil {
		return fmt.Errorf("arcadedb: upsert conversation projection: %w", err)
	}
	answered, err := c.storedTurnVectorHashes(ctx, projection)
	if err != nil {
		return err
	}
	// The reconciler replays every turn once a minute: a turn whose content is already
	// answered keeps its answer, so the embedder sees only what changed, all in one request.
	vectors := c.embedChangedTurns(ctx, projection.Turns, answered)
	for index, turn := range projection.Turns {
		turnParams := map[string]any{
			"identity_id": turn.IdentityID, "conversation_id": turn.ConversationID,
			"turn_seq": turn.Seq, "role": turn.Role, "content": turn.Content,
			"content_hash": turn.ContentHash,
			"occurred_at":  turn.OccurredAt.UTC().Format(time.RFC3339Nano),
			"source_ref":   turn.SourceRef,
		}
		statement := upsertConversationTurnStatement
		if vector, changed := vectors[index]; changed {
			statement += vector.replaceClause(turnParams)
		}
		statement += upsertConversationTurnWhere
		if _, err := c.Command(ctx, statement, turnParams); err != nil {
			return fmt.Errorf("arcadedb: upsert conversation turn %d: %w", turn.Seq, err)
		}
		if _, err := c.Command(ctx, createHasTurnStatement, turnParams); err != nil {
			return fmt.Errorf("arcadedb: link conversation turn %d: %w", turn.Seq, err)
		}
		if _, err := c.Command(ctx, createNextTurnStatement, turnParams); err != nil {
			return fmt.Errorf("arcadedb: order conversation turn %d: %w", turn.Seq, err)
		}
		// Close the reasoning link from this side. The trace for this turn was written
		// before the vertex existed, so its own attempt created nothing; this is the half
		// that makes the edge appear, and it is a no-op for the turns no trace names,
		// which is most of them.
		//
		// It does NOT share the failure policy of the two writes above, and the first
		// version of this line did: "if ArcadeDB cannot serve this write it did not serve
		// theirs either" was wrong, because those two write types this schema creates
		// while this one READS ReasoningTrace, which belongs to a schema the projection
		// neither owns nor may assume. On a database where reasoning was never
		// provisioned the statement raises, and aborting here would lose the whole
		// projection -- every turn, for that identity, forever -- to decorate it. Caught
		// by the live tier the same day: three conversation-projection tests failed at
		// their first Apply against a disposable database with only the conversation
		// schema on it.
		//
		// Logged, never swallowed: a genuine backend failure has already surfaced through
		// the two writes above, so what reaches here is the absence of something optional.
		if _, err := c.Command(ctx, linkReasoningInitiatorFromTurnStatement, turnParams); err != nil {
			slog.Warn("conversation projection could not link its reasoning trace",
				"conversation_id", redact.Line(projection.ConversationID),
				"turn_seq", turn.Seq, "err", redact.Line(err.Error()))
		}
	}
	return nil
}

// embedChangedTurns answers every turn whose content has no stored answer, in one request.
// Each changed turn gets an entry -- its vector, its refusal, or nothing when no route
// answered -- and "nothing" clears a vector computed for older content. A route outage
// never clears a turn whose content still matches: that turn is not changed.
func (c *Client) embedChangedTurns(
	ctx context.Context,
	turns []ConversationTurnProjection,
	answered map[int]string,
) map[int]storedVector {
	changed := make(map[int]storedVector)
	texts := make([]string, 0, len(turns))
	indexes := make([]int, 0, len(turns))
	for index, turn := range turns {
		if answered[turn.Seq] == turn.ContentHash {
			continue
		}
		changed[index] = storedVector{}
		texts = append(texts, turn.Content)
		indexes = append(indexes, index)
	}
	if len(texts) == 0 || c.embedder == nil {
		return changed
	}
	vectors, err := c.embedStored(ctx, texts)
	if err != nil {
		return changed
	}
	for position, index := range indexes {
		changed[index] = vectors[position]
	}
	return changed
}

// storedTurnVectorHashes maps each turn of the conversation that already carries an answer
// -- a vector, or a space's refusal -- to the content_hash that answer was computed from.
func (c *Client) storedTurnVectorHashes(ctx context.Context, projection ConversationProjection) (map[int]string, error) {
	rows, err := c.Query(ctx, storedTurnVectorsStatement, map[string]any{
		"identity_id": projection.IdentityID, "conversation_id": projection.ConversationID,
	})
	if err != nil {
		return nil, fmt.Errorf("arcadedb: read stored conversation vectors: %w", err)
	}
	hashes := make(map[int]string, len(rows))
	for _, row := range rows {
		hashes[int(rowInt(row, "turn_seq"))] = rowString(row, "content_hash")
	}
	return hashes, nil
}

func validateConversationProjection(projection ConversationProjection) error {
	if strings.TrimSpace(projection.IdentityID) == "" {
		return fmt.Errorf("arcadedb: conversation projection identity must be non-empty")
	}
	if strings.TrimSpace(projection.ConversationID) == "" {
		return fmt.Errorf("arcadedb: conversation projection id must be non-empty")
	}
	for _, turn := range projection.Turns {
		switch {
		case turn.IdentityID != projection.IdentityID:
			return fmt.Errorf("arcadedb: conversation turn %d has foreign identity", turn.Seq)
		case turn.ConversationID != projection.ConversationID:
			return fmt.Errorf("arcadedb: conversation turn %d has foreign conversation", turn.Seq)
		case turn.Seq <= 0:
			return fmt.Errorf("arcadedb: conversation turn sequence must be positive")
		case turn.Role != "user" && turn.Role != "assistant":
			return fmt.Errorf("arcadedb: conversation turn %d has ineligible role %q", turn.Seq, turn.Role)
		case strings.TrimSpace(turn.Content) == "":
			return fmt.Errorf("arcadedb: conversation turn %d content must be non-empty", turn.Seq)
		case strings.TrimSpace(turn.SourceRef) == "":
			return fmt.Errorf("arcadedb: conversation turn %d source_ref must be non-empty", turn.Seq)
		case turn.OccurredAt.IsZero():
			return fmt.Errorf("arcadedb: conversation turn %d occurred_at must be set", turn.Seq)
		case turn.ContentHash != conversationContentHash(turn.Content):
			return fmt.Errorf("arcadedb: conversation turn %d content_hash does not match content", turn.Seq)
		}
	}
	return nil
}

func conversationTurnHitFromRow(row map[string]any, identityID string) (ConversationTurnHit, bool) {
	if rowString(row, "identity_id") != identityID {
		return ConversationTurnHit{}, false
	}
	return ConversationTurnHit{
		IdentityID: identityID, ConversationID: rowString(row, "conversation_id"),
		Seq: int(rowInt(row, "turn_seq")), Role: rowString(row, "role"),
		Content: rowString(row, "content"), ContentHash: rowString(row, "content_hash"),
		OccurredAt: rowString(row, "occurred_at"), SourceRef: rowString(row, "source_ref"),
	}, true
}

func conversationContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func conversationSourceRef(conversationID string) string {
	return "postgres://aura/conversations/" + conversationID
}

// DeleteConversationProjection removes all derived records for one source conversation.
func (c *Client) DeleteConversationProjection(ctx context.Context, identityID, conversationID string) error {
	if strings.TrimSpace(identityID) == "" || strings.TrimSpace(conversationID) == "" {
		return fmt.Errorf("arcadedb: delete conversation projection requires identity and conversation")
	}
	params := map[string]any{"identity_id": identityID, "conversation_id": conversationID}
	if _, err := c.Command(ctx,
		"DELETE FROM "+conversationTurnType+
			" WHERE identity_id = :identity_id AND conversation_id = :conversation_id", params); err != nil {
		return fmt.Errorf("arcadedb: delete conversation turns: %w", err)
	}
	if _, err := c.Command(ctx,
		"DELETE FROM "+conversationVertexType+
			" WHERE identity_id = :identity_id AND conversation_id = :conversation_id", params); err != nil {
		return fmt.Errorf("arcadedb: delete conversation: %w", err)
	}
	return nil
}

// DeleteIdentityConversationProjections removes the complete derived conversation tier.
func (c *Client) DeleteIdentityConversationProjections(ctx context.Context, identityID string) error {
	if strings.TrimSpace(identityID) == "" {
		return fmt.Errorf("arcadedb: delete identity conversation projections requires identity")
	}
	params := map[string]any{"identity_id": identityID}
	if _, err := c.Command(ctx,
		"DELETE FROM "+conversationTurnType+" WHERE identity_id = :identity_id", params); err != nil {
		return fmt.Errorf("arcadedb: delete identity conversation turns: %w", err)
	}
	if _, err := c.Command(ctx,
		"DELETE FROM "+conversationVertexType+" WHERE identity_id = :identity_id", params); err != nil {
		return fmt.Errorf("arcadedb: delete identity conversations: %w", err)
	}
	return nil
}

// PruneConversationProjections removes graph conversations absent from the source replay.
func (c *Client) PruneConversationProjections(ctx context.Context, identityID string, liveConversationIDs []string) error {
	if strings.TrimSpace(identityID) == "" {
		return fmt.Errorf("arcadedb: prune conversation projections requires identity")
	}
	rows, err := c.Query(ctx,
		"SELECT identity_id, conversation_id FROM "+conversationVertexType+
			" WHERE identity_id = :identity_id",
		map[string]any{"identity_id": identityID})
	if err != nil {
		return fmt.Errorf("arcadedb: list conversation projections for pruning: %w", err)
	}
	live := make(map[string]struct{}, len(liveConversationIDs))
	for _, conversationID := range liveConversationIDs {
		if conversationID = strings.TrimSpace(conversationID); conversationID != "" {
			live[conversationID] = struct{}{}
		}
	}
	for _, row := range rows {
		if rowString(row, "identity_id") != identityID {
			continue
		}
		conversationID := rowString(row, "conversation_id")
		if _, ok := live[conversationID]; ok {
			continue
		}
		if err := c.DeleteConversationProjection(ctx, identityID, conversationID); err != nil {
			return err
		}
	}
	return nil
}
