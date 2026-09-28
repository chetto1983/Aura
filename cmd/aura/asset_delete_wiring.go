package main

import (
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/cron/handlers"
)

// buildAssetDeleteSweep gives the nightly retention job the pass that finishes asset deletes:
// every identity's rows left deleting by a failed object or row removal, and the backlog that
// piled up before a delete went past marking them (258 rows on the lab VM, 2026-09-28).
// Refused and failed rows are records of an outcome, not content, so they live as long as the
// deployment keeps its other metadata traces (AURA_RETENTION_METADATA_TRACE_HOURS).
//
// The return type is the INTERFACE and a missing dependency returns a bare nil, which the
// retention handler skips; a nil-pointer sweep inside a non-nil interface would be called.
func buildAssetDeleteSweep(chat *chatEnv) handlers.Sweeper {
	if chat.assets == nil || chat.identity == nil {
		return nil
	}
	return assets.DeleteSweep{
		Assets:         chat.assets,
		Identities:     identityRoster{store: chat.identity},
		FailedLifetime: chat.cfg.Retention.MetadataTraceTTL,
	}
}
