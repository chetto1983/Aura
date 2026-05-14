package tools

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aura/aura/internal/storage/sources/store"
)

// sourcePurger is the memoryindex side of "forget this source" — declared
// here as a local interface to avoid importing the memoryindex package into
// every tool consumer.
type sourcePurger interface {
	PurgeSource(ctx context.Context, sourceID string) error
}

// DeleteSourceTool removes an ingested source from Aura's memory: the raw
// directory on disk, the SQLite memoryindex rows, and the Qdrant vectors
// mirroring those rows. Wiki pages that linked to the source are left in
// place because they may now describe a deleted source — operator decides
// what to do with them (memory_hygiene can flag dangling references in a
// later pass).
type DeleteSourceTool struct {
	store  *source.Store
	purger sourcePurger
}

func NewDeleteSourceTool(store *source.Store, purger sourcePurger) *DeleteSourceTool {
	if store == nil {
		return nil
	}
	return &DeleteSourceTool{store: store, purger: purger}
}

func (t *DeleteSourceTool) Name() string { return "delete_source" }

func (t *DeleteSourceTool) Description() string {
	return "Permanently delete a source: removes the raw directory under wiki/raw/<id>/ from disk and purges its rows from the memoryindex (SQLite + Qdrant mirror). Wiki pages that referenced the source are left untouched. Confirm intent before calling; this is irreversible."
}

func (t *DeleteSourceTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"source_id": map[string]any{
				"type":        "string",
				"description": "Source ID (e.g. src_<16hex>). Must match the regex enforced by source.Store.",
			},
		},
		"required": []string{"source_id"},
	}
}

func (t *DeleteSourceTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	if t == nil || t.store == nil {
		return "", errors.New("delete_source: source store unavailable")
	}
	id, err := requiredString(args, "source_id")
	if err != nil {
		return "", err
	}
	// Purge the memoryindex first: it is idempotent on missing IDs and it is
	// the LLM-visible state ("search_memory shouldn't return the deleted
	// source"). If the file removal then fails, the index is consistent and
	// we can retry. The previous order (files first, index second) left
	// "ghost source" rows in the index when PurgeSource errored (F-026).
	if t.purger != nil {
		if err := t.purger.PurgeSource(ctx, id); err != nil {
			return "", fmt.Errorf("delete_source: purge memoryindex: %w", err)
		}
	}
	if err := t.store.Delete(ctx, id); err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("delete_source: memoryindex purged but source %s not found on disk", id)
		}
		return "", fmt.Errorf("delete_source: remove files (memoryindex already purged): %w", err)
	}
	return fmt.Sprintf("Deleted source %s (files + memoryindex). Wiki pages referencing this source are unchanged.", id), nil
}
