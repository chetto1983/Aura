package main

import (
	"context"
	"os"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/assets"
)

// sendFileAssetAdapter bridges send_file's primitive-typed tools.AssetDeliverer seam to the
// concrete *assets.Service (WEBART-01). cmd/aura is the ONLY package importing both tools and
// assets, so the adapter lives here — the substrate (internal/agent/tools) never learns the
// concrete service exists. It is wired onto the retained SendFile handle at serve boot
// (serve.go, after buildAssetService).
type sendFileAssetAdapter struct{ svc *assets.Service }

// Compile-time proof the adapter satisfies the tool's seam.
var _ tools.AssetDeliverer = sendFileAssetAdapter{}

// IngestAgentDelivery opens the (already send_file-workspace-fenced + 50-MiB-gated) host file and
// ingests its bytes as an owned, thread-scoped asset via IngestAgentFile, returning the created
// asset id. It is best-effort at the call site: any error makes send_file degrade to a path-only
// descriptor (D-02), so the returned error never wedges the turn.
func (a sendFileAssetAdapter) IngestAgentDelivery(ctx context.Context, identityID, threadID, hostPath, filename, mimeType string, size int64) (string, error) {
	f, err := os.Open(hostPath) //nolint:gosec // hostPath is already workspace-fenced + size-gated by send_file before this call.
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	asset, err := a.svc.IngestAgentFile(ctx, assets.AgentIngestRequest{
		IdentityID: identityID,
		ThreadID:   threadID,
		FileName:   filename,
		MIMEType:   mimeType,
		SizeBytes:  size,
		Reader:     f,
	})
	if err != nil {
		return "", err
	}
	return asset.ID, nil
}
