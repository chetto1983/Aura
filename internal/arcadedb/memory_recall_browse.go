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
	" WHERE identity_id = :identity_id AND deleted_at IS NULL" + recallExclusionMarker +
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
	params := map[string]any{
		"identity_id": request.IdentityID, "recent_limit": pageSize,
	}
	statement := applyRecallExclusions(recentConversationAnchorsStatement, params, request.ExcludeConversationIDs)
	rows, err := c.Query(ctx, statement, params)
	if err != nil {
		return RecallResult{}, fmt.Errorf("arcadedb: recall recent conversations: %w", err)
	}
	evidence := make([]RecallEvidence, 0, pageSize)
	seen := make(map[string]struct{}, len(rows))
	excluded := recallExcludedConversationSet(request.ExcludeConversationIDs)
	var drops recallDrops
	defer func() { drops.report("recent") }()
	for _, row := range rows {
		anchor, ok := conversationTurnHitFromRow(row, request.IdentityID)
		if !ok || anchor.ConversationID == "" {
			continue
		}
		if _, blocked := excluded[anchor.ConversationID]; blocked {
			continue
		}
		if _, duplicate := seen[anchor.ConversationID]; duplicate {
			continue
		}
		window, err := c.recallConversationWindow(ctx, anchor, recallWindowRadius)
		if err != nil {
			drops.record(err)
			continue
		}
		seen[anchor.ConversationID] = struct{}{}
		windowCopy := window
		evidence = append(evidence, RecallEvidence{
			Kind: RecallEvidenceConversation, Rank: len(evidence) + 1,
			Conversation: &windowCopy,
		})
	}
	result := recallConversationResult(evidence, len(rows), "no_recent_conversations")
	// recent orders by recency and nothing else, so a query it was handed changed
	// nothing: measured 2026-09-03, mode=recent with query="ArcadeDB" and with no
	// query at all returned byte-identical evidence. Say so in the metadata rather
	// than accepting a parameter and discarding it -- a caller who believed it was
	// filtering has no other way to learn that it was not.
	if strings.TrimSpace(request.Query) != "" && result.Reason == "" {
		result.Reason = reasonQueryIgnoredByRecent
	}
	return result, nil
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
	if recallConversationExcluded(request.ExcludeConversationIDs, cursor.ConversationID) {
		return activeConversationExcludedRecallResult(), nil
	}
	return c.recallCursorPage(ctx, cursor, recallPageOpened)
}

func (c *Client) recallScroll(ctx context.Context, request RecallRequest) (RecallResult, error) {
	cursor, err := decodeRecallCursor(request.Cursor)
	if err != nil {
		return RecallResult{}, err
	}
	if strings.TrimSpace(request.IdentityID) != cursor.IdentityID {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor identity mismatch")
	}
	// Omission is not a mismatch. The cursor already carries the conversation, the
	// anchor, the direction and the page size, and the tool calls it opaque -- so
	// `scroll` with nothing but the cursor is the natural call, and it used to fail
	// with "conversation mismatch", an error that reads like corruption rather than
	// like a missing argument. A caller that DOES supply one of these and contradicts
	// the cursor is still refused: that is real confusion about what is being paged,
	// not an omission.
	//
	// The identity check above stays unconditional and is the only one that guards
	// anything: identity is host-derived, never caller-supplied, so refusing a cursor
	// minted for another identity is a tenancy boundary rather than a restatement.
	if id := strings.TrimSpace(request.ConversationID); id != "" && id != cursor.ConversationID {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor conversation mismatch")
	}
	if request.AnchorSeq != 0 && request.AnchorSeq != cursor.AnchorSeq {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor anchor mismatch")
	}
	// Read the RAW direction: normalizeRecallDirection turns an omitted direction into
	// `after`, which would silently contradict a `before` cursor.
	if request.Direction != "" {
		direction, err := normalizeRecallDirection(request.Direction)
		if err != nil {
			return RecallResult{}, err
		}
		if direction != cursor.Direction {
			return RecallResult{}, fmt.Errorf("arcadedb: recall cursor direction mismatch")
		}
	}
	if request.Limit > 0 && boundedLimit(request.Limit, recallBrowseDefaultPage, recallBrowseMaxPage) != cursor.PageSize {
		return RecallResult{}, fmt.Errorf("arcadedb: recall cursor page size mismatch")
	}
	if recallConversationExcluded(request.ExcludeConversationIDs, cursor.ConversationID) {
		return activeConversationExcludedRecallResult(), nil
	}
	return c.recallCursorPage(ctx, cursor, recallPageFollowed)
}

// An empty page means two different things, and answering both with the same reason sends
// the caller looking for a bug that is not there. Opening a conversation at an anchor that
// holds no turn is a bad request; following a next_cursor to the end is the conversation
// simply running out, with nothing wrong about the anchor or the conversation.
type recallPageOrigin int

const (
	recallPageOpened recallPageOrigin = iota
	recallPageFollowed
)

func (c *Client) recallCursorPage(
	ctx context.Context,
	cursor RecallCursor,
	origin recallPageOrigin,
) (RecallResult, error) {
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
		reason := "conversation_anchor_not_found"
		if origin == recallPageFollowed {
			reason = "conversation_exhausted"
		}
		return RecallResult{
			Evidence: make([]RecallEvidence, 0), Abstained: true,
			Reason:    reason,
			Retrieval: RecallRetrieval{Path: retrievalPathGraph},
		}, nil
	}
	// The next page starts PAST what this one returned, and there is no next page when
	// this one was short.
	//
	// Both halves were missing and together they did not terminate. The page statements
	// are inclusive (`turn_seq >= :anchor_seq`), which is right for `open` -- the anchor
	// turn belongs in its own window -- but the next cursor was set to the last turn
	// RETURNED, so every following page repeated the previous page's boundary turn. At
	// the end of a conversation that made the cursor a fixed point: measured on a live
	// conversation whose last turn is 10, `scroll` after turn 10 returned turn 10 again
	// and a next_cursor byte-identical to the one passed in, so a client following
	// next_cursor to exhaustion never stops.
	nextCursor := ""
	if len(turns) == cursor.PageSize {
		next := cursor
		if cursor.Direction == RecallDirectionBefore {
			next.AnchorSeq = turns[0].Seq - 1
		} else {
			next.AnchorSeq = turns[len(turns)-1].Seq + 1
		}
		// Seq 0 is before the first turn, and the cursor's own validation rejects it.
		// Reaching it means the conversation is exhausted, which is what an absent
		// cursor says.
		if next.AnchorSeq > 0 {
			encoded, err := encodeRecallCursor(next)
			if err != nil {
				return RecallResult{}, err
			}
			nextCursor = encoded
		}
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
