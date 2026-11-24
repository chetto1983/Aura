// delegation_artifact.go holds the ReportArchiver seam the terminal delivery uses
// to persist a finished delegation's full report as an owned text/markdown
// asset (51-11, SWARM-12 leg 1, UI-SPEC §2). Declared HERE, the consuming
// package, mirroring ConversationRecorder/ChannelDeliverer
// (delegation_delivery.go) and AssetDeliverer
// (internal/agent/tools/send_file_ingest.go): internal/swarm gains no import
// edge into internal/assets. *delegationReportArchiver
// (cmd/aura/serve_delegation.go) satisfies it via the SAME
// assets.Service.IngestAgentFile seam send_file already uses -- no new
// artifact store, no new frame type (D-02's closed shape).
package swarm

import (
	"context"
	"fmt"
)

// ReportArchiver persists a delegation's full report markdown, scoped to the
// origin conversation, and returns the created asset's id. Its method
// signature is primitive-typed end to end -- the same discipline
// AssetDeliverer follows -- so nothing about the concrete asset service
// leaks into this package.
type ReportArchiver interface {
	ArchiveReport(ctx context.Context, identityID, conversationID, deliveryKey, filename, markdown string) (assetID string, err error)
}

// archivePreparedReport is the durable terminal path: a missing or failed archiver
// keeps the queue row retryable. deliveryKey is persisted before this call and makes
// asset creation idempotent across every failure window after it returns. filename is
// derived from the report's child id, which transcript_api.go's validatePathSegment
// already guarantees is separator-free, with a ".md" suffix -- the same name the card's
// own artifact-pointer line names, so a human reading the card and a human reading the
// Artifacts panel see the same string.
func archivePreparedReport(ctx context.Context, archiver ReportArchiver, identityID, conversationID, deliveryKey, childID, markdown string) (string, error) {
	if archiver == nil {
		return "", fmt.Errorf("delegation report archiver is not configured")
	}
	filename := childID + ".md"
	if _, err := archiver.ArchiveReport(ctx, identityID, conversationID, deliveryKey, filename, markdown); err != nil {
		return "", fmt.Errorf("archive delegation report: %w", err)
	}
	return filename, nil
}
