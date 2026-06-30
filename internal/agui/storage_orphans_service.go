package agui

import (
	"context"

	"github.com/chetto1983/aura/internal/documents"
)

// StorageOrphanService is the narrow storage cleanup surface consumed by AG-UI.
type StorageOrphanService interface {
	DryRun(context.Context, documents.StorageOrphanRequest) (documents.StorageOrphanReport, error)
	Cleanup(context.Context, documents.StorageOrphanCleanupRequest) (documents.StorageOrphanReport, error)
}
