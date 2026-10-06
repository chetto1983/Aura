package arcadedb

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const recallPeriodMaxPage = 100

// DATETIME has millisecond precision; date(long) avoids the database display format.
// https://docs.arcadedb.com/arcadedb/reference/managing-dates
const recallPeriodStatement = "SELECT identity_id, conversation_id, turn_seq, role, " +
	"content, content_hash, occurred_at, occurred_at.asLong() AS occurred_millis, source_ref FROM " +
	conversationTurnType + " WHERE identity_id = :identity_id AND deleted_at IS NULL" +
	recallExclusionMarker + " AND occurred_at >= date(:period_from) AND occurred_at < date(:period_to)"

const recallPeriodAfter = " AND (occurred_at > date(:after_millis) OR " +
	"(occurred_at = date(:after_millis) AND conversation_id > :after_conversation) OR " +
	"(occurred_at = date(:after_millis) AND conversation_id = :after_conversation AND turn_seq > :after_seq))"

const recallPeriodOrder = " ORDER BY occurred_at ASC, conversation_id ASC, turn_seq ASC LIMIT :page_size"

func (c *Client) recallPeriod(ctx context.Context, request RecallRequest) (RecallResult, error) {
	if request.Cursor != "" {
		return RecallResult{}, fmt.Errorf("arcadedb: period starts with from/to; continue its cursor with scroll")
	}
	if err := validatePeriodSelectors(request); err != nil {
		return RecallResult{}, err
	}
	if request.From.IsZero() || request.To.IsZero() {
		return RecallResult{}, fmt.Errorf("arcadedb: period requires both from and to")
	}
	cursor := RecallCursor{
		Version: recallCursorVersion, Mode: RecallModePeriod, IdentityID: request.IdentityID,
		From: request.From.UTC().Format(time.RFC3339Nano), To: request.To.UTC().Format(time.RFC3339Nano),
		PageSize: boundedLimit(request.Limit, 20, recallPeriodMaxPage),
	}
	if err := cursor.validate(); err != nil {
		return RecallResult{}, err
	}
	return c.recallPeriodPage(ctx, cursor, request.ExcludeConversationIDs)
}

func validatePeriodSelectors(request RecallRequest) error {
	if request.Query != "" || request.Entity != "" || request.Predicate != "" ||
		request.ConversationID != "" || request.AnchorSeq != 0 || request.Direction != "" || !request.AsOf.IsZero() {
		return fmt.Errorf("arcadedb: period rejects topic, fact, and conversation selectors")
	}
	return nil
}

func (cursor RecallCursor) periodBounds() (time.Time, time.Time, error) {
	from, fromErr := time.Parse(time.RFC3339Nano, cursor.From)
	to, toErr := time.Parse(time.RFC3339Nano, cursor.To)
	if fromErr != nil || toErr != nil || !from.Before(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("arcadedb: period requires RFC3339 from strictly before to")
	}
	return from, to, nil
}

func (cursor RecallCursor) validatePeriod() error {
	if cursor.Version != recallCursorVersion || !validRecallCursorID(cursor.IdentityID) {
		return fmt.Errorf("arcadedb: period cursor version or identity is invalid")
	}
	if cursor.Direction != "" || cursor.PageSize < 1 || cursor.PageSize > recallPeriodMaxPage {
		return fmt.Errorf("arcadedb: period cursor direction or page size is invalid")
	}
	from, to, err := cursor.periodBounds()
	if err != nil {
		return err
	}
	if cursor.AnchorSeq == 0 && cursor.ConversationID == "" && cursor.AfterMillis == 0 {
		return nil
	}
	if cursor.AnchorSeq <= 0 || !validRecallCursorID(cursor.ConversationID) ||
		cursor.AfterMillis < periodBoundaryMillis(from) || cursor.AfterMillis >= periodBoundaryMillis(to) {
		return fmt.Errorf("arcadedb: period cursor checkpoint is outside its interval or invalid")
	}
	return nil
}

func (c *Client) recallPeriodScroll(ctx context.Context, request RecallRequest, cursor RecallCursor) (RecallResult, error) {
	if err := validatePeriodSelectors(request); err != nil {
		return RecallResult{}, err
	}
	from, to, err := cursor.periodBounds()
	if err != nil {
		return RecallResult{}, err
	}
	if (!request.From.IsZero() && !request.From.Equal(from)) ||
		(!request.To.IsZero() && !request.To.Equal(to)) ||
		(request.Limit > 0 && boundedLimit(request.Limit, 20, recallPeriodMaxPage) != cursor.PageSize) {
		return RecallResult{}, fmt.Errorf("arcadedb: period cursor interval or page size mismatch")
	}
	return c.recallPeriodPage(ctx, cursor, request.ExcludeConversationIDs)
}

func periodBoundaryMillis(instant time.Time) int64 {
	millis := instant.UnixMilli()
	if instant.Nanosecond()%int(time.Millisecond) != 0 {
		millis++
	}
	return millis
}

func (c *Client) recallPeriodPage(ctx context.Context, cursor RecallCursor, excluded []string) (RecallResult, error) {
	from, to, err := cursor.periodBounds()
	if err != nil {
		return RecallResult{}, err
	}
	params := map[string]any{
		"identity_id": cursor.IdentityID, "period_from": periodBoundaryMillis(from),
		"period_to": periodBoundaryMillis(to), "page_size": cursor.PageSize + 1,
	}
	statement := recallPeriodStatement
	if cursor.AnchorSeq > 0 {
		statement += recallPeriodAfter
		params["after_millis"] = cursor.AfterMillis
		params["after_conversation"] = cursor.ConversationID
		params["after_seq"] = cursor.AnchorSeq
	}
	rows, err := c.Query(ctx, applyRecallExclusions(statement+recallPeriodOrder, params, excluded), params)
	if err != nil {
		return RecallResult{}, fmt.Errorf("arcadedb: recall period: %w", err)
	}
	evidence := make([]RecallEvidence, 0)
	positions := make(map[string]int)
	var next RecallCursor
	for index, row := range rows {
		if index == cursor.PageSize {
			break
		}
		turn, ok := conversationTurnHitFromRow(row, cursor.IdentityID)
		if !ok || recallConversationExcluded(excluded, turn.ConversationID) ||
			!validRecallCursorID(turn.ConversationID) || turn.Seq <= 0 || strings.TrimSpace(turn.SourceRef) == "" {
			return RecallResult{}, fmt.Errorf("arcadedb: period returned an invalid or out-of-scope projected turn")
		}
		millis := rowInt(row, "occurred_millis")
		if millis < periodBoundaryMillis(from) || millis >= periodBoundaryMillis(to) {
			return RecallResult{}, fmt.Errorf("arcadedb: period returned a turn outside its interval")
		}
		turn.OccurredAt = time.UnixMilli(millis).UTC().Format(time.RFC3339Nano)
		position, exists := positions[turn.ConversationID]
		if !exists {
			position = len(evidence)
			positions[turn.ConversationID] = position
			evidence = append(evidence, RecallEvidence{
				Kind: RecallEvidenceConversation, Rank: position + 1,
				Conversation: &RecallConversationWindow{ConversationID: turn.ConversationID, AnchorSeq: turn.Seq},
			})
		}
		evidence[position].Conversation.Turns = append(evidence[position].Conversation.Turns, turn)
		next = cursor
		next.AfterMillis, next.ConversationID, next.AnchorSeq = millis, turn.ConversationID, turn.Seq
	}
	result := recallConversationResult(evidence, min(len(rows), cursor.PageSize), "no_conversations_in_period")
	if len(rows) > cursor.PageSize {
		result.NextCursor, err = encodeRecallCursor(next)
		if err != nil {
			return RecallResult{}, err
		}
	}
	return result, nil
}
