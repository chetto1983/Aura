//nolint:revive // Internal asset processors expose small cross-package wiring APIs.
package assets

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/documents"
)

// DocumentProcessor settles an uploaded document by naming it the way the index names it.
//
// It writes nothing and reads nothing. Indexing belongs to the ingest sidecar: CocoIndex
// reconciles the identity's bucket and writes IndexedDocument + Passage into ArcadeDB,
// keyed by SearchDocumentID(identity, "s3", <object key>). Measured on the live stack
// 2026-08-17: an object that appeared in the bucket at 12:42:47 was answerable at 12:43:24
// -- 37 seconds, 22 passages -- with no Postgres row involved at any point.
//
// What this used to do instead was download the bytes it had just accepted, hash them a
// second time, register an ingestion job, and mint a SECOND document identity from
// ("web", "sha256:"+sha256(assetID)). That id cannot appear in the index, because the
// index derives its own from the object key -- so ResolveDocumentScope could never match
// it. Measured on three real uploads: Postgres held doc_78824b89…, the index holds
// doc_403e3fe4… for the same file. The consequence was not subtle. BuildKnowledgeCatalog
// narrows by exactly that lookup, so it returned "" for every uploaded document and the
// agent was never told about a single one; and the document_open the attachment block
// advertises (context.go) pointed at an id nothing could resolve.
//
// The status stays "processing" rather than "searchable" because nothing here has produced
// a passage. Nothing reads it to decide searchability either -- that is ArcadeDB's answer,
// asked where it belongs (BuildKnowledgeCatalog's isIndexed).
type DocumentProcessor struct{}

// ProcessAsset returns the document identity the index will file this object under.
//
// The fenced claim is still required. The work it guards is gone, but the guarantee is
// not: it is what proves the queue worker owns this asset, and dropping it would let any
// caller settle an asset out from under the worker holding its lease.
func (p *DocumentProcessor) ProcessAsset(ctx context.Context, asset Asset) (Result, error) {
	claim, ok := documents.ClaimedIngestionJob(ctx)
	if !ok || claim.ID == "" || claim.LockedBy == "" || claim.LeaseGeneration <= 0 {
		return Result{}, fmt.Errorf("document processor requires a fenced ingestion claim")
	}
	if claim.IdentityID != asset.IdentityID || (claim.AssetID != "" && claim.AssetID != asset.ID) {
		return Result{}, fmt.Errorf("document processor ingestion claim does not own the asset")
	}
	// "s3", not asset.SourceKind: the source kind here names the CHANNEL the upload arrived
	// on (web, telegram, cli), and the channels are wrappers over one door. What the index
	// records is where the bytes are -- the bucket -- so the id has to be derived from that
	// or the two sides are naming different things.
	documentID, err := documents.SearchDocumentID(asset.IdentityID, "s3", asset.ObjectKey)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Status:     StatusProcessing,
		DocumentID: documentID,
		Summary: fmt.Sprintf(
			"%s is stored; the ingest sidecar indexes it from the bucket.", asset.FileName,
		),
		Metadata: map[string]any{
			"object_bucket": asset.ObjectBucket,
			"object_key":    asset.ObjectKey,
		},
	}, nil
}
