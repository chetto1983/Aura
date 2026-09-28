package assets

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// KeyedAsset is what the file manager needs to know about the asset behind an object key.
type KeyedAsset struct {
	ID       string
	FileName string
}

// KeyMove is one object key a file-manager move or rename relocated. Name, when not empty, is
// the new file name of the row that held From: a rename changes what a file is called, a move
// only where it lives.
type KeyMove struct {
	From, To, Name string
}

// AssetsByKey maps each object key this identity owns to its asset: the id, and the name a
// person gave the file.
//
// The file manager lists bucket KEYS, and a key deliberately carries no name — a chat
// attachment is `chat/<assetID>.<ext>` precisely so the name cannot leak through a
// presigned URL or an access log. The name it needs is on the SAME ROW as the key, so this
// is one indexed lookup rather than a trip through another datastore. The id rides along so
// an editor opens a library file AS its asset: without it every open uploaded a copy.
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
func (s *Store) AssetsByKey(ctx context.Context, identityID string, keys []string) (map[string]KeyedAsset, error) {
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
	found := make(map[string]KeyedAsset, len(rows))
	for _, row := range rows {
		if row.FileName != "" {
			found[row.ObjectKey] = KeyedAsset{ID: uuidString(row.ID), FileName: row.FileName}
		}
	}
	return found, nil
}

// KeysHeld returns the keys among keys some row of the identity holds, in any status.
func (s *Store) KeysHeld(ctx context.Context, identityID string, keys []string) ([]string, error) {
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return nil, err
	}
	var held []string
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var qErr error
		held, qErr = q.AssetKeysHeld(ctx, sqlc.AssetKeysHeldParams{IdentityID: pgIdentityID, ObjectKeys: keys})
		return qErr
	})
	return held, err
}

// Relocate moves the identity's rows in bucket along with the objects a move or rename
// relocated, in one transaction: either every row follows or none does.
func (s *Store) Relocate(ctx context.Context, identityID, bucket string, moves []KeyMove) error {
	pgIdentityID, err := pgUUID("identity_id", identityID)
	if err != nil {
		return err
	}
	return s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		for _, move := range moves {
			if err := q.RelocateAsset(ctx, sqlc.RelocateAssetParams{
				IdentityID: pgIdentityID, ObjectBucket: bucket,
				FromKey: move.From, ToKey: move.To, NewName: move.Name,
			}); err != nil {
				return fmt.Errorf("relocate %s: %w", move.From, err)
			}
		}
		return nil
	})
}
