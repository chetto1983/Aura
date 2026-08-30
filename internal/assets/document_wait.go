package assets

import (
	"context"
	"fmt"
	"time"
)

// documentIndexPollInterval paces WaitDocumentIndexed against ArcadeDB. Indexing is
// the sidecar's job and takes tens of seconds (37s measured 2026-08-17), so a 2s poll
// answers promptly without hammering the index.
const documentIndexPollInterval = 2 * time.Second

// WaitDocumentIndexed blocks until the index actually holds documentID for
// identityID — the SAME question BuildKnowledgeCatalog asks (DocumentScope), so
// "indexed" here and "advertised as retrievable" can never disagree. The caller
// bounds the wait through ctx; a transient resolver error is retried until that
// deadline (resolveIndexed swallows it, which is the honest poll behaviour).
// Measured need (amendment #199, 2026-08-30): a Telegram turn that starts before
// indexing finishes gets a document_search miss and the model confabulates the
// document's content.
func (s *Service) WaitDocumentIndexed(ctx context.Context, identityID, documentID string) error {
	if s == nil || s.DocumentScope == nil {
		return fmt.Errorf("no document index to ask")
	}
	return waitDocumentIndexed(ctx, s.DocumentScope, identityID, documentID, documentIndexPollInterval)
}

func waitDocumentIndexed(ctx context.Context, scope DocumentScopeResolver, identityID, documentID string, interval time.Duration) error {
	if identityID == "" || documentID == "" {
		return fmt.Errorf("document index wait needs identity and document ids")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if resolveIndexed(ctx, scope, identityID, []string{documentID})[documentID] {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
