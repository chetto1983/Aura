package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ReasoningDeleteSelector names one identity-scoped reasoning source to remove.
type ReasoningDeleteSelector struct {
	IdentityID     string
	TraceID        string
	ConversationID string
	SourceRef      string
}

// DeleteExpiredReasoning removes at most limit terminal traces eligible at now.
func (c *Client) DeleteExpiredReasoning(
	ctx context.Context,
	identityID string,
	now time.Time,
	limit int,
) (int, error) {
	return c.deleteExpiredReasoning(ctx, identityID, now, limit)
}

// DeleteReasoningBySource removes one identity-scoped trace source immediately.
func (c *Client) DeleteReasoningBySource(
	ctx context.Context,
	selector ReasoningDeleteSelector,
) (int, error) {
	return c.deleteReasoningBySource(ctx, selector)
}

// DeleteIdentityReasoning removes every reasoning trace for one identity.
func (c *Client) DeleteIdentityReasoning(context.Context, string) (int, error) {
	return 0, errors.New("arcadedb: identity reasoning deletion not implemented")
}

const selectExpiredReasoningStatement = "SELECT trace_id FROM " + reasoningTraceType +
	" WHERE identity_id = :identity_id AND expires_at IS NOT NULL AND expires_at <= :now" +
	" ORDER BY expires_at, trace_id LIMIT :limit"

const selectReasoningByConversationStatement = "SELECT trace_id FROM " + reasoningTraceType +
	" WHERE identity_id = :identity_id AND conversation_id = :conversation_id ORDER BY trace_id"

const selectReasoningBySourceStatement = "SELECT trace_id FROM " + reasoningTraceType +
	" WHERE identity_id = :identity_id AND source_ref = :source_ref ORDER BY trace_id"

var deleteReasoningGraphStatements = []string{
	"DELETE FROM TOUCHED WHERE outV().identity_id = :identity_id AND outV().trace_id = :trace_id",
	"DELETE FROM INVOKED WHERE outV().identity_id = :identity_id AND outV().trace_id = :trace_id",
	"DELETE FROM NEXT WHERE (outV().identity_id = :identity_id AND outV().trace_id = :trace_id)" +
		" OR (inV().identity_id = :identity_id AND inV().trace_id = :trace_id)",
	"DELETE FROM HAS_STEP WHERE outV().identity_id = :identity_id AND outV().trace_id = :trace_id",
	"DELETE FROM INITIATED_BY WHERE outV().identity_id = :identity_id AND outV().trace_id = :trace_id",
	"DELETE FROM " + reasoningToolCallType + " WHERE identity_id = :identity_id AND trace_id = :trace_id",
	"DELETE FROM " + reasoningStepType + " WHERE identity_id = :identity_id AND trace_id = :trace_id",
	"DELETE FROM " + reasoningTraceType + " WHERE identity_id = :identity_id AND trace_id = :trace_id",
}

// DeleteExpiredReasoning removes at most limit terminal traces eligible at now.
func (c *Client) deleteExpiredReasoning(
	ctx context.Context,
	identityID string,
	now time.Time,
	limit int,
) (int, error) {
	identityID = strings.TrimSpace(identityID)
	if identityID == "" {
		return 0, fmt.Errorf("arcadedb: reasoning expiry identity must be non-empty")
	}
	if now.IsZero() {
		return 0, fmt.Errorf("arcadedb: reasoning expiry clock must be set")
	}
	limit = min(boundedLimit(limit, c.memoryLimits().MaintenanceBatch, c.memoryLimits().MaintenanceBatch),
		c.memoryLimits().MaintenanceBatch)
	return c.deleteReasoningSelected(ctx, identityID, selectExpiredReasoningStatement, map[string]any{
		"identity_id": identityID,
		"now":         now.UTC().Format(time.RFC3339Nano),
		"limit":       limit,
	})
}

// DeleteReasoningBySource removes one identity-scoped trace source immediately.
func (c *Client) deleteReasoningBySource(
	ctx context.Context,
	selector ReasoningDeleteSelector,
) (int, error) {
	selector.IdentityID = strings.TrimSpace(selector.IdentityID)
	selector.TraceID = strings.TrimSpace(selector.TraceID)
	selector.ConversationID = strings.TrimSpace(selector.ConversationID)
	selector.SourceRef = strings.TrimSpace(selector.SourceRef)
	if selector.IdentityID == "" {
		return 0, fmt.Errorf("arcadedb: reasoning deletion identity must be non-empty")
	}
	selected := 0
	for _, value := range []string{selector.TraceID, selector.ConversationID, selector.SourceRef} {
		if value != "" {
			selected++
		}
	}
	if selected != 1 {
		return 0, fmt.Errorf("arcadedb: reasoning deletion requires exactly one trace, conversation, or source")
	}
	if selector.TraceID != "" {
		return c.deleteReasoningTraceIDs(ctx, selector.IdentityID, []string{selector.TraceID})
	}
	if selector.ConversationID != "" {
		return c.deleteReasoningSelected(ctx, selector.IdentityID, selectReasoningByConversationStatement,
			map[string]any{"identity_id": selector.IdentityID, "conversation_id": selector.ConversationID})
	}
	return c.deleteReasoningSelected(ctx, selector.IdentityID, selectReasoningBySourceStatement,
		map[string]any{"identity_id": selector.IdentityID, "source_ref": selector.SourceRef})
}

func (c *Client) deleteReasoningSelected(
	ctx context.Context,
	identityID, statement string,
	params map[string]any,
) (int, error) {
	session, err := c.beginTx(ctx)
	if err != nil {
		return 0, err
	}
	defer c.rollbackTx(context.WithoutCancel(ctx), session)
	rows, err := c.queryInTx(ctx, session, statement, params)
	if err != nil {
		return 0, fmt.Errorf("arcadedb: select reasoning deletion roots: %w", err)
	}
	traceIDs := uniqueReasoningTraceIDs(rows)
	if err := c.deleteReasoningTraceIDsInTx(ctx, session, identityID, traceIDs); err != nil {
		return 0, err
	}
	if err := c.commitTx(ctx, session); err != nil {
		return 0, fmt.Errorf("arcadedb: commit reasoning deletion: %w", err)
	}
	return len(traceIDs), nil
}

func (c *Client) deleteReasoningTraceIDs(ctx context.Context, identityID string, traceIDs []string) (int, error) {
	session, err := c.beginTx(ctx)
	if err != nil {
		return 0, err
	}
	defer c.rollbackTx(context.WithoutCancel(ctx), session)
	traceIDs = uniqueStrings(traceIDs)
	if err := c.deleteReasoningTraceIDsInTx(ctx, session, identityID, traceIDs); err != nil {
		return 0, err
	}
	if err := c.commitTx(ctx, session); err != nil {
		return 0, fmt.Errorf("arcadedb: commit reasoning deletion: %w", err)
	}
	return len(traceIDs), nil
}

func (c *Client) deleteReasoningTraceIDsInTx(
	ctx context.Context,
	session, identityID string,
	traceIDs []string,
) error {
	for _, traceID := range traceIDs {
		params := map[string]any{"identity_id": identityID, "trace_id": traceID}
		for _, statement := range deleteReasoningGraphStatements {
			if _, err := c.commandInTx(ctx, session, statement, params); err != nil {
				return fmt.Errorf("arcadedb: delete reasoning trace %q: %w", traceID, err)
			}
		}
	}
	return nil
}

func uniqueReasoningTraceIDs(rows []map[string]any) []string {
	traceIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		traceIDs = append(traceIDs, rowString(row, "trace_id"))
	}
	return uniqueStrings(traceIDs)
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
