package assets

import (
	"context"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// NamesByKey maps each object key this identity owns to the name a person gave the file.
//
// The file manager lists bucket KEYS, and a key deliberately carries no name — a chat
// attachment is `chat/<assetID>.<ext>` precisely so the name cannot leak through a
// presigned URL or an access log. The name it needs is on the SAME ROW as the key, so this
// is one indexed lookup rather than a trip through another datastore.
//
// It replaces a lookup that derived a search id from the key and asked the document index
// (ArcadeDB) for the name. That path had three costs a join does not: it made a plain
// listing depend on the search index being up, it failed WHOLE-listing rather than
// per-row, and the index caps a query at 100 filters — so a library of 117 objects
// resolved zero names and the page showed uuids. Measured on the live stack 2026-09-09,
// with all 129 assets resolvable from Postgres.
//
// A key with no row is simply absent from the result: the caller keeps the key tail it
// already displays, which for a file dropped straight into the bucket is the right label
// because there the key IS the name.
func (s *Store) NamesByKey(ctx context.Context, identityID string, keys []string) (map[string]string, error) {
	if s == nil || len(keys) == 0 {
		return nil, nil
	}
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.AssetNamesByObjectKeyRow
	if err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var qErr error
		rows, qErr = q.AssetNamesByObjectKey(ctx, sqlc.AssetNamesByObjectKeyParams{
			IdentityID: pgIdentityID,
			ObjectKeys: keys,
		})
		return qErr
	}); err != nil {
		return nil, err
	}
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		if row.FileName != "" {
			names[row.ObjectKey] = row.FileName
		}
	}
	return names, nil
}
