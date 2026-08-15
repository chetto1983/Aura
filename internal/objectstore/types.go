//nolint:revive // Internal objectstore contracts are exported across Aura packages.
package objectstore

import (
	"context"
	"io"
	"time"

	"github.com/google/uuid"
)

type ObjectRef struct {
	Bucket string
	Key    string
}

type Attrs struct {
	SizeBytes int64
	ETag      string
	MIMEType  string
	// ModifiedAt is what the store reports, not when Aura noticed. It was discarded from
	// every S3 listing until a file browser needed a date column, which is the only thing
	// that can order "recent" for a human.
	ModifiedAt time.Time
	// Metadata is the object's user metadata, keyed without the "x-amz-meta-" prefix every
	// S3 client strips. Nil for a store that has none and for an object written before
	// anything set any — see MetadataFileName, which is the one entry Aura writes.
	Metadata map[string]string
}

type ObjectInfo struct {
	Ref   ObjectRef
	Attrs Attrs
}

type ListRequest struct {
	Bucket string
	Prefix string
	// Limit bounds returned objects. Zero means no application-level bound.
	Limit int
	// Delimiter groups keys server-side, so listing one folder of a large bucket costs one
	// page instead of every key under it. Empty means a flat listing, which is what every
	// caller before the file browser wanted.
	//
	// A group comes back as an ObjectInfo whose Key ENDS WITH the delimiter and whose size
	// is zero -- the same shape S3 calls a CommonPrefix. That keeps the interface a single
	// []ObjectInfo instead of a second return value every existing caller would ignore.
	Delimiter string
}

type PutOptions struct {
	MIMEType string
	Size     int64
	// Metadata rides with the object. Callers uploading an asset should take it from
	// PlaceAsset rather than assembling one, so the key and the name it omits are decided
	// in the same place.
	Metadata map[string]string
}

type PresignPutRequest struct {
	Ref       ObjectRef
	MIMEType  string
	Size      int64
	ExpiresIn time.Duration
	// Metadata is SIGNED into the presigned URL, so the uploading client must send the
	// matching x-amz-meta-* headers or the store rejects the PUT. They come back in
	// PresignedPut.RequiredHeaders, which every client already forwards verbatim.
	Metadata   map[string]string
	PublicBase string
}

type PresignedPut struct {
	URL             string            `json:"upload_url"`
	Method          string            `json:"method"`
	RequiredHeaders map[string]string `json:"required_headers"`
	ExpiresAt       time.Time         `json:"expires_at"`
}

// presignRequiredHeaders is the header set a client MUST send with a presigned PUT.
//
// Shared by all three stores because it is one contract, not three: the S3 signature covers
// the metadata headers, so a store that forgot to declare one would hand out a URL that
// fails at upload time with a signature mismatch — the least diagnosable failure in the set.
func presignRequiredHeaders(mimeType string, metadata map[string]string) map[string]string {
	headers := map[string]string{"Content-Type": mimeType}
	for key, value := range metadata {
		headers["x-amz-meta-"+key] = value
	}
	return headers
}

type Store interface {
	PresignPut(context.Context, PresignPutRequest) (PresignedPut, error)
	Put(context.Context, ObjectRef, io.Reader, PutOptions) (Attrs, error)
	Head(context.Context, ObjectRef) (Attrs, error)
	Get(context.Context, ObjectRef) (io.ReadCloser, Attrs, error)
	List(context.Context, ListRequest) ([]ObjectInfo, error)
	Delete(context.Context, ObjectRef) error
	// Copy duplicates one object inside the store. It exists so a file manager's move and
	// rename -- which S3 has no primitive for, both being copy-then-delete -- do not have to
	// stream every byte out through the daemon and back.
	Copy(ctx context.Context, src, dst ObjectRef) error
}

// ShareSnapshotKey returns the object-store key for a share's redacted
// conversation snapshot: share/<shareID>/snapshot/<snapshotID>/canonical.json.
// Takes uuid.UUID rather than string — a deliberate deviation from AssetKey
// above — so a hostile "../identity/<victim>/asset/x" string is
// unrepresentable in the type, not merely unlikely (T-37F-25). The key
// derives from shareID+snapshotID, never token_hash: token_hash is NULL for
// the internal tier (migration 0040's CHECK), and D-10 requires internal
// shares to resolve artifacts via the same snapshot as public ones.
// Deriving a key from an authenticator would also couple key rotation to
// data movement — the token authenticates, the snapshot id locates.
func ShareSnapshotKey(shareID, snapshotID uuid.UUID) string {
	return "share/" + shareID.String() + "/snapshot/" + snapshotID.String() + "/canonical.json"
}

// ShareArtifactKey returns the object-store key for one artifact delivered
// within a share's snapshot:
// share/<shareID>/snapshot/<snapshotID>/asset/<assetID>. Every key this
// returns sits under ShareKeyPrefix(shareID) — the invariant revoke's
// List(prefix)+Delete depends on to reclaim every byte (T-37F-07).
func ShareArtifactKey(shareID, snapshotID, assetID uuid.UUID) string {
	return "share/" + shareID.String() + "/snapshot/" + snapshotID.String() + "/asset/" + assetID.String()
}

// ShareKeyPrefix returns the revoke-scope prefix for one share:
// share/<shareID>/. The "share/" root is lexically disjoint from AssetKey's
// "identity/" root in both directions — a share key can never address an
// identity object, and an identity key can never address a share object —
// which is what makes a future dedicated-bucket split a one-line change
// (T-37F-05).
func ShareKeyPrefix(shareID uuid.UUID) string {
	return "share/" + shareID.String() + "/"
}
