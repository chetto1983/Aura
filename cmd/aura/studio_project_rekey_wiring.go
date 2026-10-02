package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/objectstore"
)

// studioProjectRekeyTimeout bounds the pass that runs before HTTP serves: the lab VM held two
// candidates, each at most 4 MiB, so a minute is ample and a hung object store cannot stall boot.
const studioProjectRekeyTimeout = time.Minute

// buildStudioProjectRekey gives the boot the pass that moves the Video Studio projects saved
// before aura-video-mcp Plan A out of the document index, through the file manager's own
// relocation. Without an asset service, an identity store or a pool there are no rows to read,
// and the boot gets nil.
func buildStudioProjectRekey(chat *chatEnv, objects objectstore.Store) *assets.StudioProjectRekey {
	if chat.assets == nil || chat.identity == nil || chat.pool == nil {
		return nil
	}
	return &assets.StudioProjectRekey{
		Assets:     chat.assets,
		Files:      buildFileBrowser(chat.cfg, chat.pool, objects),
		Identities: identityRoster{store: chat.identity},
	}
}

// rekeyStudioProjects runs that pass at every boot, within timeout, as reconcileArcadeMemoryTenants
// repairs what an older build left behind. A moved project no longer matches, so it is never a
// candidate again; a candidate the pass refused is read again, and logged, at each boot. A
// failure is a warning and never stops the boot -- a project left indexed costs search noise,
// not data -- and the next start tries it again.
func rekeyStudioProjects(ctx context.Context, rekey *assets.StudioProjectRekey, timeout time.Duration) {
	if rekey == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	moved, err := rekey.Run(ctx)
	if err != nil {
		slog.Warn("aura serve: studio project re-key left projects in the index", "moved", moved, "err", err)
		return
	}
	slog.Info("aura serve: studio project re-key done", "moved", moved)
}
