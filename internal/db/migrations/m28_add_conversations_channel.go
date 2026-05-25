package migrations

import (
	"context"
	"database/sql"
)

func addConversationsChannel(ctx context.Context, tx *sql.Tx) error {
	return ensureConversationChannelColumn(ctx, tx)
}
