package arcadedb

// How far a conversation has been projected into the graph.
//
// `projected_through_seq` has been written by every projection since the vertex existed
// and read by nothing. It is the one fact that makes evicting a turn from the model's
// context safe to describe as an offload rather than a loss: a turn at or below the
// watermark is in this graph and `memory_recall` can page it back, while a turn above it
// exists only in Postgres and dropping it drops it for good.
//
// The read is deliberately narrow — one integer, by the unique (identity, conversation)
// index — because it sits on the turn's hot path. An identity with no memory database, a
// conversation never projected, or any error at all yields 0, which every caller must
// read as "claim nothing": the honest failure of a watermark is to under-report, never to
// promise a turn the graph does not hold.

import (
	"context"
	"fmt"
	"strings"
)

const conversationWatermarkStatement = "SELECT projected_through_seq FROM " +
	conversationVertexType + " WHERE identity_id = :identity_id" +
	" AND conversation_id = :conversation_id LIMIT 1"

// ProjectedThroughSeq reports the highest turn sequence this conversation has projected
// into the graph, or 0 when it has projected nothing.
func (c *Client) ProjectedThroughSeq(ctx context.Context, identityID, conversationID string) (int, error) {
	identityID = strings.TrimSpace(identityID)
	conversationID = strings.TrimSpace(conversationID)
	if identityID == "" || conversationID == "" {
		return 0, fmt.Errorf("arcadedb: projection watermark needs an identity and a conversation")
	}
	rows, err := c.Query(ctx, conversationWatermarkStatement, map[string]any{
		"identity_id": identityID, "conversation_id": conversationID,
	})
	if err != nil {
		return 0, fmt.Errorf("arcadedb: read projection watermark: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return int(rowInt(rows[0], "projected_through_seq")), nil
}
