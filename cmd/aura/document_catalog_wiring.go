package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

func buildDocumentCatalogService(chat *chatEnv) agui.DocumentCatalogService {
	return &documents.DeletingCatalog{
		Catalog: &documents.CatalogService{Store: documents.NewPostgresCatalogStore(chat.pool)},
		Assets:  runtimeDocumentAssetDeleter{assets: chat.assets},
		Staged:  runtimeStagedDocumentPurger{router: chat.sandboxRouter},
	}
}

func buildDocumentEventService(chat *chatEnv) agui.DocumentEventService {
	return documents.NewPostgresIngestionEventStore(chat.pool)
}

func buildStorageOrphanService(chat *chatEnv, objects objectstore.Store) agui.StorageOrphanService {
	return &documents.StorageOrphanService{
		Objects: objects,
		Ledger:  documents.NewPostgresCatalogStore(chat.pool),
	}
}

type runtimeDocumentAssetDeleter struct {
	assets *assets.Service
}

func (d runtimeDocumentAssetDeleter) DeleteDocumentAsset(ctx context.Context, identityID, assetID string) error {
	if d.assets == nil {
		return nil
	}
	_, err := d.assets.Delete(ctx, identityID, assetID)
	return err
}

// runtimeStagedDocumentPurger removes the working copy document_open materialized
// inside the caller's box. It resolves the path through tools.StagedDocumentPath —
// the same function that wrote it — so the two can never point at different files.
//
// It routes on the request context, which carries the identity: the box is
// per-identity, so a purge that resolved any other way would either miss the file
// or reach into someone else's container.
type runtimeStagedDocumentPurger struct {
	router *usersandbox.SandboxRouter
}

func (p runtimeStagedDocumentPurger) PurgeStagedDocument(ctx context.Context, fileName string) error {
	if p.router == nil {
		return nil
	}
	boxPath, err := tools.StagedDocumentPath(fileName)
	if err != nil {
		// A name the staging rules reject was never written under that name, so
		// there is nothing to remove and nothing to report.
		return nil
	}
	handle, err := p.router.Route(ctx)
	if err != nil {
		return fmt.Errorf("route to the identity box: %w", err)
	}
	// rm -f, so a document the agent never opened — the common case — is not an
	// error. What must be an error is a box we could not reach or a remove that
	// failed, because then the bytes are still readable.
	result, err := p.router.Exec(ctx, handle, usersandbox.ExecRequest{
		Command: "rm -f -- " + shellQuoteSingle(boxPath),
	})
	if err != nil {
		return fmt.Errorf("remove %s: %w", boxPath, err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("remove %s: exit %d: %s", boxPath, result.ExitCode, strings.TrimSpace(string(result.Stderr)))
	}
	return nil
}

// shellQuoteSingle wraps a path for a POSIX shell. The path is built from a
// validated bare file name joined to a fixed directory, so this is belt to the
// validator's braces rather than the only guard.
func shellQuoteSingle(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
