package arcadedb

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RecallMode selects one bounded operation on the unified memory read surface.
type RecallMode string

const (
	// RecallModeSemantic searches fact and conversation evidence together.
	RecallModeSemantic RecallMode = "semantic"
	// RecallModeRecent browses bounded recent conversation windows.
	RecallModeRecent RecallMode = "recent"
	// RecallModeOpen opens one conversation at a stable turn anchor.
	RecallModeOpen RecallMode = "open"
	// RecallModeScroll continues a bounded cursor-bound conversation read.
	RecallModeScroll RecallMode = "scroll"
	// RecallModeReasoning reserves the explicit reasoning-only read contract.
	RecallModeReasoning RecallMode = "reasoning"
)

// RecallDirection selects which side of a stable conversation anchor to read.
type RecallDirection string

const (
	// RecallDirectionBefore pages toward lower stable turn sequences.
	RecallDirectionBefore RecallDirection = "before"
	// RecallDirectionAfter pages toward higher stable turn sequences.
	RecallDirectionAfter RecallDirection = "after"
)

// RecallRequest is the identity-scoped internal form of memory_recall.
type RecallRequest struct {
	IdentityID     string
	Mode           RecallMode
	Query          string
	Entity         string
	Predicate      string
	AsOf           time.Time
	ConversationID string
	AnchorSeq      int
	Cursor         string
	Direction      RecallDirection
	Limit          int
	// ExcludeConversationIDs is host-derived negative scope. It can suppress
	// conversation evidence but never choose IdentityID or add candidates.
	ExcludeConversationIDs []string
}

// RecallEvidenceKind discriminates the typed evidence union.
type RecallEvidenceKind string

const (
	// RecallEvidenceFact carries one atomic long-term fact.
	RecallEvidenceFact RecallEvidenceKind = "fact"
	// RecallEvidenceConversation carries one bounded historical span.
	RecallEvidenceConversation RecallEvidenceKind = "conversation"
)

// RecallConversationWindow is a bounded chronological view around one hit.
type RecallConversationWindow struct {
	ConversationID string
	AnchorSeq      int
	Turns          []ConversationTurnHit
}

// RecallEvidence carries exactly one fact or conversation window.
type RecallEvidence struct {
	Kind         RecallEvidenceKind
	Rank         int
	Score        float64
	Fact         *FactHit
	Conversation *RecallConversationWindow
}

// RecallRetrieval separates contributing evidence tiers from the backend used.
type RecallRetrieval struct {
	EffectivePath          string
	Path                   string
	FactCandidateCount     int
	ConversationCandidates int
	FactCount              int
	ConversationCount      int
	ReasoningCount         int
	EntityCount            int
	BackendLatency         time.Duration
}

// RecallCursor is unsigned transport state that is revalidated before every query.
type RecallCursor struct {
	Version        int             `json:"version"`
	IdentityID     string          `json:"identity_id"`
	ConversationID string          `json:"conversation_id"`
	AnchorSeq      int             `json:"anchor_seq"`
	Direction      RecallDirection `json:"direction"`
	PageSize       int             `json:"page_size"`
}

// RecallResult is the storage-layer result consumed by the MCP adapter.
type RecallResult struct {
	Evidence []RecallEvidence
	// Entities are the graph nodes the question reached through its evidence,
	// each with its own facts (memory_recall_expand.go). Additive: never a
	// substitute for Evidence, never counted against its budget.
	Entities   []RecallEntityNode
	Abstained  bool
	Reason     string
	NextCursor string
	Retrieval  RecallRetrieval
}

const (
	recallDefaultLimit = 5
	recallWindowRadius = 2
	retrievalPathGraph = "graph"
	effectivePathFacts = "facts"
	effectivePathTurns = "conversations"
	effectivePathMixed = "mixed"
)

// The two rankings are separate STATEMENTS, not two legs of one fusion, because the
// candidate window is the first place a fact was being lost: `candidates` bounds the
// fused pool, and a fact the mixing had pushed to rank 46 never entered a pool of 20 at
// all. Ranked against its own kind it is rank 1. See memory_recall_quota.go for the
// measurement that produced this shape.
const recallFactFuseStatement = "SELECT @rid AS rid, score FROM (SELECT expand(" + rerankOpen + "`vector.fuse`(" +
	"`vector.neighbors`('" + factEdgeType + "[embedding]', :vector, :candidates, " +
	"{ filter: (SELECT @rid FROM " + factEdgeType + " WHERE " + asOfCondition +
	").@rid, maxDistance: :max_distance }), " +
	"(SELECT @rid, $score FROM " + factEdgeType +
	" WHERE SEARCH_INDEX('" + factEdgeType + "[statement]', :query) = true AND " +
	"$score >= :min_lexical_score AND " + asOfCondition + " LIMIT :candidates), " +
	"{ fusion: 'RRF' }" +
	")" + rerankClose + "))" + relevanceFloor + " ORDER BY score DESC, rid ASC LIMIT :candidates"

const recallTurnFuseStatement = "SELECT @rid AS rid, score FROM (SELECT expand(" + rerankOpen + "`vector.fuse`(" +
	"`vector.neighbors`('" + conversationTurnType + "[embedding]', :vector, :candidates, " +
	"{ filter: (SELECT @rid FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" + recallExclusionMarker + ").@rid, maxDistance: :max_distance }), " +
	"(SELECT @rid, $score FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" + recallExclusionMarker + " AND SEARCH_INDEX('" +
	conversationTurnType + "[content]', :query) = true AND $score >= :min_lexical_score " +
	"LIMIT :candidates), { fusion: 'RRF' }" +
	")" + rerankClose + "))" + relevanceFloor + " ORDER BY score DESC, rid ASC LIMIT :candidates"

// The lexical fallback splits the same way. Leaving it fused would keep the defect alive
// on the degraded path, where it is harder to see and no less wrong.
const recallFactLexicalStatement = "SELECT @rid AS rid, $score AS score FROM " + factEdgeType +
	" WHERE SEARCH_INDEX('" + factEdgeType + "[statement]', :query) = true AND " +
	"$score >= :min_lexical_score AND " + asOfCondition +
	" ORDER BY score DESC, rid ASC LIMIT :candidates"

const recallTurnLexicalStatement = "SELECT @rid AS rid, $score AS score FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" + recallExclusionMarker +
	" AND SEARCH_INDEX('" + conversationTurnType + "[content]', :query) = true AND " +
	"$score >= :min_lexical_score ORDER BY score DESC, rid ASC LIMIT :candidates"

const hydrateRecallFactsStatement = "SELECT @rid, statement, predicate, valid_from, valid_to, " +
	"sources, fact_key, outV().name AS subject, outV().kind AS subject_kind, " +
	"inV().name AS object, inV().kind AS object_kind FROM " + factEdgeType +
	" WHERE @rid IN :rids AND " + asOfCondition

const hydrateRecallTurnsStatement = "SELECT @rid, identity_id, conversation_id, turn_seq, role, " +
	"content, content_hash, occurred_at, source_ref FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" + recallExclusionMarker + " AND @rid IN :rids"

const recallConversationWindowStatement = "SELECT identity_id, conversation_id, turn_seq, role, " +
	"content, content_hash, occurred_at, source_ref FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id" +
	" AND deleted_at IS NULL AND turn_seq BETWEEN :from_seq AND :to_seq ORDER BY turn_seq"

// RecallMemory executes one identity-scoped unified recall operation.
func (c *Client) RecallMemory(ctx context.Context, request RecallRequest) (RecallResult, error) {
	started := time.Now()
	if strings.TrimSpace(request.IdentityID) == "" {
		return RecallResult{}, fmt.Errorf("arcadedb: memory recall identity must be non-empty")
	}
	if request.Mode == "" {
		request.Mode = RecallModeSemantic
	}
	exclusions, err := canonicalRecallExclusions(request.ExcludeConversationIDs)
	if err != nil {
		return RecallResult{}, err
	}
	request.ExcludeConversationIDs = exclusions
	var (
		result    RecallResult
		recallErr error
	)
	switch request.Mode {
	case RecallModeSemantic:
		result, recallErr = c.recallSemantic(ctx, request)
	case RecallModeRecent:
		result, recallErr = c.recallRecent(ctx, request)
	case RecallModeOpen:
		result, recallErr = c.recallOpen(ctx, request)
	case RecallModeScroll:
		result, recallErr = c.recallScroll(ctx, request)
	case RecallModeReasoning:
		result = RecallResult{
			Evidence: make([]RecallEvidence, 0), Abstained: true,
			Reason: "reasoning_not_available",
		}
	default:
		recallErr = fmt.Errorf("arcadedb: unsupported memory recall mode %q", request.Mode)
	}
	if recallErr != nil {
		return RecallResult{}, recallErr
	}
	result.Retrieval.BackendLatency = time.Since(started)
	if result.Evidence == nil {
		result.Evidence = make([]RecallEvidence, 0)
	}
	return result, nil
}

func (c *Client) recallSemantic(ctx context.Context, request RecallRequest) (RecallResult, error) {
	if strings.TrimSpace(request.Entity) != "" {
		return c.recallEntity(ctx, request)
	}
	query := strings.TrimSpace(request.Query)
	if query == "" {
		return RecallResult{}, fmt.Errorf("arcadedb: memory recall needs query or entity")
	}
	limits := c.memoryLimits()
	if err := validateRuneLimit("memory recall query", query, limits.QueryRunes); err != nil {
		return RecallResult{}, err
	}
	limit := boundedLimit(request.Limit, recallDefaultLimit, limits.Results)
	if request.AsOf.IsZero() {
		request.AsOf = time.Now()
	}
	candidates := min(max(limit*4, 20), limits.HybridCandidates)
	params := map[string]any{
		"identity_id": request.IdentityID,
		"query":       escapeLucene(query), "candidates": candidates,
		"as_of":        request.AsOf.UTC().Format(time.RFC3339),
		"max_distance": limits.DenseMaxDistance, "min_relevance": limits.MinRelevance,
		"min_lexical_score": lexicalScoreFloor(query, limits.LexicalMinScore),
	}
	path, reason := retrievalPathHybrid, ""
	factStatement, turnStatement := recallFactFuseStatement, recallTurnFuseStatement
	vector, embeddingReason := c.recallQueryVector(ctx, query)
	if vector == nil {
		path, reason = retrievalPathLexical, embeddingReason
		factStatement, turnStatement = recallFactLexicalStatement, recallTurnLexicalStatement
	} else {
		params["vector"] = vector
	}
	facts, turns, err := c.rankRecallKinds(ctx, factStatement, turnStatement, params, request.ExcludeConversationIDs)
	if err != nil && path == retrievalPathHybrid {
		path, reason = retrievalPathLexical, reasonFusionFailed
		facts, turns, err = c.rankRecallKinds(
			ctx, recallFactLexicalStatement, recallTurnLexicalStatement, params, request.ExcludeConversationIDs,
		)
	}
	if err != nil {
		return RecallResult{}, fmt.Errorf("arcadedb: unified memory recall: %w", err)
	}
	result, err := c.hydrateRecallRanking(ctx, request, mergeRecallRankings(facts, turns), limit, path, reason)
	if err != nil {
		return RecallResult{}, err
	}
	result.Entities = c.expandRecallEntities(ctx, request, result.Evidence)
	result.Retrieval.EntityCount = len(result.Entities)
	return result, nil
}

// rankRecallKinds runs the two per-kind rankings. Both are attempted even when the first
// fails: a memory with no conversations yet, or a fact index still building, must still
// answer from the half that works rather than reporting an empty memory. The error is
// returned only when NEITHER side produced a ranking, which is the only case the caller
// can do nothing about.
func (c *Client) rankRecallKinds(
	ctx context.Context,
	factStatement, turnStatement string,
	params map[string]any,
	excluded []string,
) ([]recallRankedRID, []recallRankedRID, error) {
	factRows, factErr := c.Query(ctx, applyRecallExclusions(factStatement, params, excluded), params)
	turnRows, turnErr := c.Query(ctx, applyRecallExclusions(turnStatement, params, excluded), params)
	if factErr != nil && turnErr != nil {
		return nil, nil, fmt.Errorf("rank facts: %v; rank turns: %w", factErr, turnErr)
	}
	return decodeRecallRanking(factRows), decodeRecallRanking(turnRows), nil
}

func (c *Client) recallEntity(ctx context.Context, request RecallRequest) (RecallResult, error) {
	hits, err := c.FactsAbout(ctx, request.Entity, request.Predicate, request.Limit, request.AsOf,
		FactsAboutDirect)
	if err != nil {
		return RecallResult{}, err
	}
	evidence := make([]RecallEvidence, 0, len(hits))
	for index := range hits {
		hit := hits[index]
		evidence = append(evidence, RecallEvidence{
			Kind: RecallEvidenceFact, Rank: index + 1, Fact: &hit,
		})
	}
	result := RecallResult{
		Evidence: evidence,
		Retrieval: RecallRetrieval{
			Path: retrievalPathGraph, FactCandidateCount: len(hits), FactCount: len(hits),
		},
	}
	if len(hits) == 0 {
		result.Abstained = true
		result.Reason = "no_facts"
	} else {
		result.Retrieval.EffectivePath = effectivePathFacts
	}
	return result, nil
}

func (c *Client) recallQueryVector(ctx context.Context, query string) ([]float64, string) {
	if c == nil || c.embedder == nil {
		return nil, reasonEmbedderNotConfigured
	}
	vectors, err := c.embedder.Embed(ctx, withTask(taskQueryPrefix, []string{query}))
	if err != nil {
		return nil, reasonEmbeddingFailed
	}
	if len(vectors) != 1 || len(vectors[0]) != vectorDimensions {
		return nil, reasonEmbeddingInvalid
	}
	return vectors[0], ""
}

type recallRankedRID struct {
	rid   string
	score float64
}

func decodeRecallRanking(rows []map[string]any) []recallRankedRID {
	seen := make(map[string]struct{}, len(rows))
	ranked := make([]recallRankedRID, 0, len(rows))
	for _, row := range rows {
		rid := strings.TrimSpace(fmt.Sprintf("%v", row["rid"]))
		if rid == "" || rid == "<nil>" {
			continue
		}
		if _, duplicate := seen[rid]; duplicate {
			continue
		}
		seen[rid] = struct{}{}
		score, _ := finiteFloat(row["score"])
		ranked = append(ranked, recallRankedRID{rid: rid, score: score})
	}
	return ranked
}

func (c *Client) hydrateRecallRanking(
	ctx context.Context,
	request RecallRequest,
	ranked []recallRankedRID,
	limit int,
	path string,
	reason string,
) (RecallResult, error) {
	if len(ranked) == 0 {
		return RecallResult{
			Evidence: make([]RecallEvidence, 0), Abstained: true,
			Reason:    reasonNoQualifiedCandidates,
			Retrieval: RecallRetrieval{Path: path},
		}, nil
	}
	rids := make([]string, len(ranked))
	for index := range ranked {
		rids[index] = ranked[index].rid
	}
	facts, factErr := c.hydrateRecallFacts(ctx, rids, request.AsOf)
	turns, turnErr := c.hydrateRecallTurns(ctx, request.IdentityID, rids, request.ExcludeConversationIDs)
	if factErr != nil && turnErr != nil {
		return RecallResult{}, fmt.Errorf("arcadedb: hydrate recall facts: %v; turns: %w", factErr, turnErr)
	}
	factProse := make(map[string]struct{}, len(facts))
	for _, fact := range facts {
		factProse[normalizedRecallProse(fact.Statement)] = struct{}{}
	}
	quota := recallQuotaFor(limit)
	var admitted recallAdmission
	evidence := make([]RecallEvidence, 0, min(len(ranked), limit))
	seenProse := make(map[string]struct{}, len(ranked))
	seenConversations := make(map[string]struct{})
	excludedConversations := recallExcludedConversationSet(request.ExcludeConversationIDs)
	var drops recallDrops
	defer func() { drops.report(path) }()
	for index, item := range ranked {
		if admitted.exhausted(quota) {
			break
		}
		if fact, ok := facts[item.rid]; ok {
			key := normalizedRecallProse(fact.Statement)
			if _, duplicate := seenProse[key]; duplicate {
				continue
			}
			if !admitted.admitFact(quota) {
				continue
			}
			seenProse[key] = struct{}{}
			factCopy := fact
			evidence = append(evidence, RecallEvidence{
				Kind: RecallEvidenceFact, Rank: index + 1, Score: item.score, Fact: &factCopy,
			})
			continue
		}
		turn, ok := turns[item.rid]
		if !ok {
			continue
		}
		if _, excluded := excludedConversations[turn.ConversationID]; excluded {
			continue
		}
		if _, duplicate := factProse[normalizedRecallProse(turn.Content)]; duplicate {
			continue
		}
		if _, duplicate := seenConversations[turn.ConversationID]; duplicate {
			continue
		}
		if !admitted.canAdmitTurn(quota) {
			continue
		}
		window, err := c.recallConversationWindow(ctx, turn, recallWindowRadius)
		if err != nil {
			drops.record(err)
			continue
		}
		window.Turns = omitRecallFactProse(window.Turns, factProse)
		if len(window.Turns) == 0 {
			continue
		}
		admitted.bookTurn()
		seenConversations[turn.ConversationID] = struct{}{}
		windowCopy := window
		evidence = append(evidence, RecallEvidence{
			Kind: RecallEvidenceConversation, Rank: index + 1, Score: item.score,
			Conversation: &windowCopy,
		})
	}
	result := RecallResult{
		Evidence: evidence, Reason: reason,
		Retrieval: RecallRetrieval{
			Path: path, FactCandidateCount: len(facts), ConversationCandidates: len(turns),
		},
	}
	for _, item := range evidence {
		switch item.Kind {
		case RecallEvidenceFact:
			result.Retrieval.FactCount++
		case RecallEvidenceConversation:
			result.Retrieval.ConversationCount++
		}
	}
	result.Retrieval.EffectivePath = effectiveRecallPath(
		result.Retrieval.FactCount, result.Retrieval.ConversationCount,
	)
	if len(evidence) == 0 {
		result.Abstained = true
		result.Reason = reasonNoQualifiedCandidates
	}
	return result, nil
}

func (c *Client) hydrateRecallFacts(
	ctx context.Context,
	rids []string,
	asOf time.Time,
) (map[string]FactHit, error) {
	rows, err := c.Query(ctx, hydrateRecallFactsStatement, map[string]any{
		"rids": rids, "as_of": asOf.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	hits := make(map[string]FactHit, len(rows))
	for _, row := range rows {
		hits[fmt.Sprintf("%v", row["@rid"])] = factHitFromRow(row)
	}
	return hits, nil
}

func (c *Client) hydrateRecallTurns(
	ctx context.Context,
	identityID string,
	rids []string,
	excludedConversationIDs []string,
) (map[string]ConversationTurnHit, error) {
	params := map[string]any{
		"identity_id": identityID, "rids": rids,
	}
	statement := applyRecallExclusions(hydrateRecallTurnsStatement, params, excludedConversationIDs)
	rows, err := c.Query(ctx, statement, params)
	if err != nil {
		return nil, err
	}
	hits := make(map[string]ConversationTurnHit, len(rows))
	excluded := recallExcludedConversationSet(excludedConversationIDs)
	for _, row := range rows {
		if hit, ok := conversationTurnHitFromRow(row, identityID); ok {
			if _, blocked := excluded[hit.ConversationID]; blocked {
				continue
			}
			hits[fmt.Sprintf("%v", row["@rid"])] = hit
		}
	}
	return hits, nil
}

func (c *Client) recallConversationWindow(
	ctx context.Context,
	anchor ConversationTurnHit,
	radius int,
) (RecallConversationWindow, error) {
	fromSeq := max(1, anchor.Seq-radius)
	toSeq := anchor.Seq + radius
	rows, err := c.Query(ctx, recallConversationWindowStatement, map[string]any{
		"identity_id": anchor.IdentityID, "conversation_id": anchor.ConversationID,
		"from_seq": fromSeq, "to_seq": toSeq,
	})
	if err != nil {
		return RecallConversationWindow{}, err
	}
	turns := make([]ConversationTurnHit, 0, len(rows))
	anchorFound := false
	for _, row := range rows {
		hit, ok := conversationTurnHitFromRow(row, anchor.IdentityID)
		if !ok || hit.ConversationID != anchor.ConversationID {
			continue
		}
		anchorFound = anchorFound || hit.Seq == anchor.Seq
		turns = append(turns, hit)
	}
	if !anchorFound {
		return RecallConversationWindow{}, fmt.Errorf("arcadedb: conversation anchor %d is stale", anchor.Seq)
	}
	sort.SliceStable(turns, func(i, j int) bool { return turns[i].Seq < turns[j].Seq })
	return RecallConversationWindow{
		ConversationID: anchor.ConversationID, AnchorSeq: anchor.Seq, Turns: turns,
	}, nil
}

func omitRecallFactProse(
	turns []ConversationTurnHit,
	factProse map[string]struct{},
) []ConversationTurnHit {
	out := make([]ConversationTurnHit, 0, len(turns))
	for _, turn := range turns {
		if _, duplicate := factProse[normalizedRecallProse(turn.Content)]; duplicate {
			continue
		}
		out = append(out, turn)
	}
	return out
}

func normalizedRecallProse(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func effectiveRecallPath(facts, conversations int) string {
	switch {
	case facts > 0 && conversations > 0:
		return effectivePathMixed
	case facts > 0:
		return effectivePathFacts
	case conversations > 0:
		return effectivePathTurns
	default:
		return ""
	}
}
