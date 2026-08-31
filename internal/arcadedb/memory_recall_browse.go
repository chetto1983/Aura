package arcadedb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	recallCursorVersion        = 1
	recallCursorMaxEncodedSize = 1024
	recallCursorMaxDecodedSize = 768
	recallBrowseDefaultPage    = 5
	recallBrowseMaxPage        = 20
	recallBrowseIDMaxRunes     = 256
)

const recentConversationAnchorsStatement = "SELECT identity_id, conversation_id, turn_seq, role, " +
	"content, content_hash, occurred_at, source_ref FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" +
	" ORDER BY occurred_at DESC, conversation_id ASC, turn_seq DESC LIMIT :recent_limit"

const recallConversationAfterStatement = "SELECT identity_id, conversation_id, turn_seq, role, " +
	"content, content_hash, occurred_at, source_ref FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id" +
	" AND deleted_at IS NULL AND turn_seq >= :anchor_seq" +
	" ORDER BY turn_seq ASC LIMIT :page_size"

const recallConversationBeforeStatement = "SELECT identity_id, conversation_id, turn_seq, role, " +
	"content, content_hash, occurred_at, source_ref FROM " + conversationTurnType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id" +
	" AND deleted_at IS NULL AND turn_seq <= :anchor_seq" +
	" ORDER BY turn_seq DESC LIMIT :page_size"

func (c *Client) recallRecent(ctx context.Context, request RecallRequest) (RecallResult, error) {
	pageSize := boundedLimit(request.Limit, recallBrowseDefaultPage, recallBrowseMaxPage)
	rows, err := c.Query(ctx, recentConversationAnchorsStatement, map[string]any{
		"identity_id": request.IdentityID, "recent_limit": pageSize,
	})
	if err != nil {
		return RecallResult{}, fmt.Errorf("arcadedb: recall recent conversations: %w", err)
	}
	evidence := make([]RecallEvidence, 0, pageSize)
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		anchor, ok := conversationTurnHitFromRow(row, request.IdentityID)
		if !ok || anchor.ConversationID == "" {
			continue
		}
		if _, duplicate := seen[anchor.ConversationID]; duplicate {
			continue
		}
		window, err := c.recallConversationWindow(ctx, anchor, recallWindowRadius)
		if err != nil {
			continue
		}
		seen[anchor.ConversationID] = struct{}{}
		windowCopy := window
		evidence = append(evidence, RecallEvidence{
			Kind: RecallEvidenceConversation, Rank: len(evidence) + 1,
			Conversation: &windowCopy,
		})
	}
	return recallConversationResult(evidence, len(rows), "no_recent_conversations"), nil
}

func (c *Client) recallOpen(ctx context.Context, request RecallRequest) (RecallResult, error) {
	if strings.TrimSpace(request.Cursor) != "" {
		return RecallResult{}, fmt.Errorf("arcadedb: open mode does not accept a cursor")
	}
	pageSize := boundedLimit(request.Limit, recallBrowseDefaultPage, recallBrowseMaxPage)
	direction, err := normalizeRecallDirection(request.Direction)
	if err != nil {
		return RecallResult{}, err
	}
	cursor := RecallCursor{
		Version: recallCursorVersion, IdentityID: request.IdentityID,
		ConversationID: strings.TrimSpace(request.ConversationID), AnchorSeq: request.AnchorSeq,
		Direction: direction, PageSize: pageSize,
	}
	if err := cursor.validate(); err != nil {
		return RecallResult{}, err
	}
	return c.recallCursorPage(ctx, cursor)
}

func (c *Client) recallScroll(ctx context.Context, request RecallRequest) (RecallResult, error) {
	cursor, err := decodeRecallCursor(request.Cursor)
	if err != nil {
		return RecallResult{}, err
	}
	if strings.TrimSpace(request.IdentityID) != cursor.IdentityID {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor identity mismatch")
	}
	if strings.TrimSpace(request.ConversationID) != cursor.ConversationID {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor conversation mismatch")
	}
	if request.AnchorSeq != cursor.AnchorSeq {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor anchor mismatch")
	}
	direction, err := normalizeRecallDirection(request.Direction)
	if err != nil {
		return RecallResult{}, err
	}
	if direction != cursor.Direction {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor direction mismatch")
	}
	if request.Limit > 0 && boundedLimit(request.Limit, recallBrowseDefaultPage, recallBrowseMaxPage) != cursor.PageSize {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor page size mismatch")
	}
	return c.recallCursorPage(ctx, cursor)
}

func (c *Client) recallCursorPage(ctx context.Context, cursor RecallCursor) (RecallResult, error) {
	statement := recallConversationAfterStatement
	if cursor.Direction == RecallDirectionBefore {
		statement = recallConversationBeforeStatement
	}
	rows, err := c.Query(ctx, statement, map[string]any{
		"identity_id": cursor.IdentityID, "conversation_id": cursor.ConversationID,
		"anchor_seq": cursor.AnchorSeq, "page_size": cursor.PageSize,
	})
	if err != nil {
		return RecallResult{}, fmt.Errorf("arcadedb: recall conversation page: %w", err)
	}
	turns := make([]ConversationTurnHit, 0, min(len(rows), cursor.PageSize))
	for _, row := range rows {
		turn, ok := conversationTurnHitFromRow(row, cursor.IdentityID)
		if !ok || turn.ConversationID != cursor.ConversationID {
			continue
		}
		turns = append(turns, turn)
		if len(turns) == cursor.PageSize {
			break
		}
	}
	if cursor.Direction == RecallDirectionBefore {
		slices.Reverse(turns)
	}
	if len(turns) == 0 {
		return RecallResult{
			Evidence: make([]RecallEvidence, 0), Abstained: true,
			Reason:    "conversation_anchor_not_found",
			Retrieval: RecallRetrieval{Path: retrievalPathGraph},
		}, nil
	}
	next := cursor
	if cursor.Direction == RecallDirectionBefore {
		next.AnchorSeq = turns[0].Seq
	} else {
		next.AnchorSeq = turns[len(turns)-1].Seq
	}
	nextCursor, err := encodeRecallCursor(next)
	if err != nil {
		return RecallResult{}, err
	}
	window := RecallConversationWindow{
		ConversationID: cursor.ConversationID, AnchorSeq: cursor.AnchorSeq, Turns: turns,
	}
	return RecallResult{
		Evidence: []RecallEvidence{{
			Kind: RecallEvidenceConversation, Rank: 1, Conversation: &window,
		}},
		NextCursor: nextCursor,
		Retrieval: RecallRetrieval{
			EffectivePath: effectivePathTurns, Path: retrievalPathGraph,
			ConversationCandidates: len(turns), ConversationCount: 1,
		},
	}, nil
}

func recallConversationResult(evidence []RecallEvidence, candidates int, emptyReason string) RecallResult {
	result := RecallResult{
		Evidence: evidence,
		Retrieval: RecallRetrieval{
			Path: retrievalPathGraph, ConversationCandidates: candidates,
			ConversationCount: len(evidence),
		},
	}
	if len(evidence) == 0 {
		result.Abstained = true
		result.Reason = emptyReason
		return result
	}
	result.Retrieval.EffectivePath = effectivePathTurns
	return result
}

func normalizeRecallDirection(direction RecallDirection) (RecallDirection, error) {
	if direction == "" {
		return RecallDirectionAfter, nil
	}
	switch direction {
	case RecallDirectionBefore, RecallDirectionAfter:
		return direction, nil
	default:
		return "", fmt.Errorf("arcadedb: recall direction must be %q or %q", RecallDirectionBefore, RecallDirectionAfter)
	}
}

func encodeRecallCursor(cursor RecallCursor) (string, error) {
	if err := cursor.validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("arcadedb: encode recall cursor: %w", err)
	}
	if len(raw) > recallCursorMaxDecodedSize {
		return "", fmt.Errorf("arcadedb: recall cursor payload exceeds %d bytes", recallCursorMaxDecodedSize)
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) > recallCursorMaxEncodedSize {
		return "", fmt.Errorf("arcadedb: recall cursor exceeds %d bytes", recallCursorMaxEncodedSize)
	}
	return encoded, nil
}

func decodeRecallCursor(encoded string) (RecallCursor, error) {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return RecallCursor{}, fmt.Errorf("arcadedb: scroll mode requires a cursor")
	}
	if len(encoded) > recallCursorMaxEncodedSize {
		return RecallCursor{}, fmt.Errorf("arcadedb: recall cursor exceeds %d bytes", recallCursorMaxEncodedSize)
	}
	raw, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil {
		return RecallCursor{}, fmt.Errorf("arcadedb: invalid recall cursor encoding: %w", err)
	}
	if len(raw) > recallCursorMaxDecodedSize {
		return RecallCursor{}, fmt.Errorf("arcadedb: recall cursor payload exceeds %d bytes", recallCursorMaxDecodedSize)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var cursor RecallCursor
	if err := decoder.Decode(&cursor); err != nil {
		return RecallCursor{}, fmt.Errorf("arcadedb: invalid recall cursor: %w", err)
	}
	if err := ensureRecallCursorEOF(decoder); err != nil {
		return RecallCursor{}, err
	}
	if err := cursor.validate(); err != nil {
		return RecallCursor{}, err
	}
	return cursor, nil
}

func ensureRecallCursorEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("arcadedb: invalid recall cursor: trailing JSON value")
		}
		return fmt.Errorf("arcadedb: invalid recall cursor: %w", err)
	}
	return nil
}

func (cursor RecallCursor) validate() error {
	switch {
	case cursor.Version != recallCursorVersion:
		return fmt.Errorf("arcadedb: recall cursor version %d is unsupported", cursor.Version)
	case !validRecallCursorID(cursor.IdentityID):
		return fmt.Errorf("arcadedb: recall cursor identity is invalid")
	case !validRecallCursorID(cursor.ConversationID):
		return fmt.Errorf("arcadedb: recall cursor conversation is invalid")
	case cursor.AnchorSeq <= 0:
		return fmt.Errorf("arcadedb: recall cursor anchor must be positive")
	case cursor.Direction != RecallDirectionBefore && cursor.Direction != RecallDirectionAfter:
		return fmt.Errorf("arcadedb: recall cursor direction is invalid")
	case cursor.PageSize < 1 || cursor.PageSize > recallBrowseMaxPage:
		return fmt.Errorf("arcadedb: recall cursor page size must be between 1 and %d", recallBrowseMaxPage)
	}
	return nil
}

func validRecallCursorID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > recallBrowseIDMaxRunes || strings.ContainsRune(value, '\x00') {
		return false
	}
	return !looksLikeArcadeRID(value)
}

func looksLikeArcadeRID(value string) bool {
	if !strings.HasPrefix(value, "#") {
		return false
	}
	cluster, position, ok := strings.Cut(strings.TrimPrefix(value, "#"), ":")
	if !ok {
		return false
	}
	_, clusterErr := strconv.ParseUint(cluster, 10, 64)
	_, positionErr := strconv.ParseUint(position, 10, 64)
	return clusterErr == nil && positionErr == nil
}
