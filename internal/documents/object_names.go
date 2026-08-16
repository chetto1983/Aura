package documents

import "context"

// DocumentNameLookup resolves indexed documents' names from the ids they are keyed by.
// Narrow on purpose, like OpenService's own lookup: the listing needs one question answered
// and nothing else of the index's surface.
type DocumentNameLookup interface {
	DocumentNames(ctx context.Context, identityID string, documentIDs []string) (map[string]string, error)
}

// ObjectNames resolves stored objects' display names from their bucket keys.
//
// A chat attachment's key is `chat/<assetID>.pdf` on purpose -- the name is kept out of it
// so it cannot leak through a presigned URL or an access log -- which leaves every surface
// that lists the bucket showing a uuid. The name travels beside the key: the ingest sidecar
// reads it from the object's own metadata and writes it to IndexedDocument, and the id that
// row is keyed by is derived from (identity, source kind, key), so a key is enough to find
// it. That derivation is SearchDocumentID here and identity.search_document_id in
// services/ingest -- the same function on both sides of the pipeline.
//
// Objects the pipeline has not indexed simply have no entry, and the caller keeps whatever
// it showed before. For a file dropped straight into the bucket that is already right: its
// key IS its name.
type ObjectNames struct {
	Index DocumentNameLookup
}

// NamesByKey maps each object key that has an indexed name to that name.
func (o *ObjectNames) NamesByKey(
	ctx context.Context,
	identityID string,
	keys []string,
) (map[string]string, error) {
	if o == nil || o.Index == nil {
		return nil, nil
	}
	// Both directions are needed: ids to ask for, and the way back to the key the caller
	// speaks. Two keys cannot collide on one id -- the digest covers the key itself.
	keyByID := make(map[string]string, len(keys))
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		id, err := SearchDocumentID(identityID, "s3", key)
		if err != nil {
			// A key that cannot be fingerprinted is a key no row was ever written for, so it
			// has no name to find. That is not an error for a listing.
			continue
		}
		if _, seen := keyByID[id]; seen {
			continue
		}
		keyByID[id] = key
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	found, err := o.Index.DocumentNames(ctx, identityID, ids)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(found))
	for id, name := range found {
		if key, ok := keyByID[id]; ok && name != "" {
			names[key] = name
		}
	}
	return names, nil
}
