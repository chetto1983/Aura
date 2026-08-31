package arcadedb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
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

// ConversationTurnHit is one identity-scoped hybrid-search result.
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

// ConversationSearchResult reports the retrieval path and explicit abstention.
type ConversationSearchResult struct {
	Turns         []ConversationTurnHit
	RetrievalPath string
	Abstained     bool
	Reason        string
}

// conversationSchemaStatements owns the replay-safe short-term memory schema.
// Plan 49-07 is the single aggregation owner that adds this fragment to
// EnsureMemorySchema after the Wave-2 memory.go owner has landed.
func conversationSchemaStatements() []string {
	return []string{
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
	}
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
	for _, turn := range projection.Turns {
		turnParams := map[string]any{
			"identity_id": turn.IdentityID, "conversation_id": turn.ConversationID,
			"turn_seq": turn.Seq, "role": turn.Role, "content": turn.Content,
			"content_hash": turn.ContentHash,
			"occurred_at":  turn.OccurredAt.UTC().Format(time.RFC3339Nano),
			"source_ref":   turn.SourceRef,
		}
		statement := upsertConversationTurnStatement
		if vector := c.embedStatement(ctx, turn.Content); vector != nil {
			statement += ", embedding = :embedding"
			turnParams["embedding"] = vector
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
	}
	return nil
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

const searchConversationTurnsStatement = "SELECT identity_id, conversation_id, turn_seq, role," +
	" content, content_hash, occurred_at, source_ref FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" +
	" AND SEARCH_INDEX('" + conversationTurnType + "[content]', :query) = true" +
	" AND $score >= :min_lexical_score"

const fuseConversationTurnRIDsStatement = "SELECT @rid AS rid FROM (SELECT expand(`vector.fuse`(" +
	"`vector.neighbors`('" + conversationTurnType + "[embedding]', :vector, :candidates," +
	" { filter: (SELECT @rid FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL).@rid, maxDistance: :max_distance })," +
	" (SELECT @rid, $score FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" +
	" AND SEARCH_INDEX('" + conversationTurnType + "[content]', :query) = true" +
	" AND $score >= :min_lexical_score LIMIT :candidates), { \"fusion\": \"RRF\" }" +
	"))) LIMIT :candidates"

const hydrateConversationTurnsStatement = "SELECT @rid, identity_id, conversation_id, turn_seq, role," +
	" content, content_hash, occurred_at, source_ref FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL AND @rid IN :rids"

// SearchConversationTurnsHybrid searches only turns owned by identityID.
func (c *Client) SearchConversationTurnsHybrid(
	ctx context.Context,
	identityID, query string,
	limit int,
) (ConversationSearchResult, error) {
	if strings.TrimSpace(identityID) == "" {
		return ConversationSearchResult{}, fmt.Errorf("arcadedb: conversation search identity must be non-empty")
	}
	if strings.TrimSpace(query) == "" {
		return ConversationSearchResult{}, fmt.Errorf("arcadedb: conversation search query must be non-empty")
	}
	limits := c.memoryLimits()
	if err := validateRuneLimit("conversation search query", query, limits.QueryRunes); err != nil {
		return ConversationSearchResult{}, err
	}
	limit = boundedLimit(limit, 5, limits.Results)
	if c.embedder == nil {
		return c.searchConversationTurnsLexical(ctx, identityID, query, limit, reasonEmbedderNotConfigured)
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskQueryPrefix, []string{query}))
	if err != nil {
		return c.searchConversationTurnsLexical(ctx, identityID, query, limit, reasonEmbeddingFailed)
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return c.searchConversationTurnsLexical(ctx, identityID, query, limit, reasonEmbeddingInvalid)
	}
	candidates := min(max(limit*4, 20), limits.HybridCandidates)
	params := map[string]any{
		"identity_id": identityID, "query": escapeLucene(query), "vector": vectors[0],
		"candidates": candidates, "max_distance": limits.DenseMaxDistance,
		"min_lexical_score": lexicalScoreFloor(query, limits.LexicalMinScore),
	}
	ranked, err := c.Query(ctx, fuseConversationTurnRIDsStatement, params)
	if err != nil {
		return c.searchConversationTurnsLexical(ctx, identityID, query, limit, reasonFusionFailed)
	}
	rids := make([]string, 0, len(ranked))
	for _, row := range ranked {
		if rid := strings.TrimSpace(fmt.Sprintf("%v", row["rid"])); rid != "" && rid != "<nil>" {
			rids = append(rids, rid)
		}
	}
	if len(rids) == 0 {
		return ConversationSearchResult{RetrievalPath: retrievalPathHybrid, Abstained: true, Reason: reasonNoQualifiedCandidates}, nil
	}
	rows, err := c.Query(ctx, hydrateConversationTurnsStatement, map[string]any{
		"identity_id": identityID, "rids": rids,
	})
	if err != nil {
		return c.searchConversationTurnsLexical(ctx, identityID, query, limit, reasonHydrationFailed)
	}
	order := make(map[string]int, len(rids))
	for i, rid := range rids {
		order[rid] = i
	}
	type rankedTurn struct {
		hit  ConversationTurnHit
		rank int
	}
	ordered := make([]rankedTurn, 0, len(rows))
	for _, row := range rows {
		hit, ok := conversationTurnHitFromRow(row, identityID)
		if !ok {
			continue
		}
		rank, found := order[fmt.Sprintf("%v", row["@rid"])]
		if !found {
			rank = len(order)
		}
		ordered = append(ordered, rankedTurn{hit: hit, rank: rank})
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].rank < ordered[j].rank })
	hits := make([]ConversationTurnHit, 0, min(len(ordered), limit))
	for _, item := range ordered {
		if len(hits) == limit {
			break
		}
		hits = append(hits, item.hit)
	}
	return ConversationSearchResult{
		Turns: hits, RetrievalPath: retrievalPathHybrid,
		Abstained: len(hits) == 0, Reason: reasonIfEmpty(hits, reasonNoQualifiedCandidates),
	}, nil
}

func (c *Client) searchConversationTurnsLexical(
	ctx context.Context,
	identityID, query string,
	limit int,
	reason string,
) (ConversationSearchResult, error) {
	rows, err := c.Query(ctx, searchConversationTurnsStatement+" LIMIT "+strconv.Itoa(limit), map[string]any{
		"identity_id": identityID, "query": escapeLucene(query),
		"min_lexical_score": lexicalScoreFloor(query, c.memoryLimits().LexicalMinScore),
	})
	if err != nil {
		return ConversationSearchResult{}, fmt.Errorf("arcadedb: search conversation turns: %w", err)
	}
	hits := make([]ConversationTurnHit, 0, len(rows))
	for _, row := range rows {
		if hit, ok := conversationTurnHitFromRow(row, identityID); ok {
			hits = append(hits, hit)
		}
	}
	return ConversationSearchResult{
		Turns: hits, RetrievalPath: retrievalPathLexical,
		Abstained: len(hits) == 0, Reason: reasonIfEmpty(hits, reason),
	}, nil
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

func reasonIfEmpty[T any](items []T, reason string) string {
	if len(items) == 0 {
		return reason
	}
	return ""
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
	return fmt.Errorf("arcadedb: delete conversation projection: not implemented")
}

// DeleteIdentityConversationProjections removes the complete derived conversation tier.
func (c *Client) DeleteIdentityConversationProjections(ctx context.Context, identityID string) error {
	return fmt.Errorf("arcadedb: delete identity conversation projections: not implemented")
}

// PruneConversationProjections removes graph conversations absent from the source replay.
func (c *Client) PruneConversationProjections(ctx context.Context, identityID string, liveConversationIDs []string) error {
	return fmt.Errorf("arcadedb: prune conversation projections: not implemented")
}
