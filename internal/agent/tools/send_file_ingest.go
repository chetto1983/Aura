// send_file_ingest.go holds the delivery-ingest seam send_file's emitDelivery tail consumes
// (WEBART-01/D-01): a narrow, primitive-typed AssetDeliverer interface so the tools package never
// imports internal/assets (the substrate names no concrete service — the *assets.Service adapter
// lives in cmd/aura, the only package importing both). The ingest is best-effort (D-02): any
// miss — a nil deliverer, an unscoped identity, an empty thread id, or an ingest error — degrades
// to today's path-only descriptor and NEVER errors the turn.

package tools

import (
	"context"
	"mime"
	"path/filepath"

	"github.com/chetto1983/aura/internal/identityctx"
)

// AssetDeliverer stores a host-file's bytes under the identity's object store and returns the
// created thread-scoped asset id. It is primitive-typed on purpose: the tools package must not
// import internal/assets (the substrate is delivery-mechanism unaware). Best-effort — any error
// makes the caller degrade to a path-only descriptor (D-02).
type AssetDeliverer interface {
	IngestAgentDelivery(ctx context.Context, identityID, threadID, hostPath, filename, mimeType string, size int64) (assetID string, err error)
}

// ingestForDelivery best-effort ingests a workspace-fenced host file as an owned, thread-scoped
// asset and returns its id + the guessed mime on success. It DEGRADES (ok=false) — the caller
// keeps the path-only descriptor, no turn error — whenever the deliverer is nil (path-only
// registry paths, D-02), the identity is unscoped (CLI / no principal), the ambient thread id
// (sessionID == ConvID) is empty (D-05), or the underlying IngestAgentDelivery errors (D-02
// Put-failure). The size is the caller's already-stat'd size the maxSendFileBytes gate computed,
// so the ingest and the descriptor's size_bytes agree and the file is stat'd exactly once.
func ingestForDelivery(ctx context.Context, deliverer AssetDeliverer, hostPath, filename string, size int64) (assetID, mimeType string, ok bool) {
	if deliverer == nil {
		return "", "", false
	}
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		return "", "", false
	}
	tc, tcOK := toolCallCtx(ctx)
	if !tcOK || tc.sessionID == "" {
		return "", "", false
	}
	mimeType = guessDeliveryMIME(filename)
	id, err := deliverer.IngestAgentDelivery(ctx, identityID, tc.sessionID, hostPath, filename, mimeType, size)
	if err != nil || id == "" {
		return "", "", false
	}
	return id, mimeType, true
}

// guessDeliveryMIME maps a filename extension to a content-type hint, defaulting to
// application/octet-stream. It is only a hint the ingest hands to the store (which re-sniffs the
// bytes on Accept); the send_file tool never trusts it as an authoritative type.
func guessDeliveryMIME(filename string) string {
	if ct := mime.TypeByExtension(filepath.Ext(filename)); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
