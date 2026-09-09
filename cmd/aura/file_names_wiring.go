package main

import (
	"github.com/chetto1983/aura/internal/assets"
	"github.com/jackc/pgx/v5/pgxpool"
)

// buildFileNamer backs the real names in the file manager's listing.
//
// The name lives on the SAME Postgres row as the object key (aura.assets carries both), so
// the listing reads it there. It used to derive a search id from each key and ask the
// document index (ArcadeDB) instead, which put a plain bucket listing behind the search
// index — and behind that index's per-query filter cap. Measured on the live stack
// 2026-09-09: a library of 117 objects exceeded the cap of 100, so the whole lookup failed
// at once and every row in the cockpit showed a uuid, while Postgres could name all 129.
//
// A key with no assets row still gets no name, and the file manager keeps labelling it with
// the tail of the key — right for a file dropped straight into the bucket, where the key IS
// the name.
func buildFileNamer(pool *pgxpool.Pool) *assets.Store {
	if pool == nil {
		return nil
	}
	return assets.NewStore(pool)
}
