//nolint:revive // Internal objectstore contracts are exported across Aura packages.
package objectstore

import (
	"context"
	"io"
	"path"
	"strings"
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
}

type PresignPutRequest struct {
	Ref        ObjectRef
	MIMEType   string
	Size       int64
	ExpiresIn  time.Duration
	PublicBase string
}

type PresignedPut struct {
	URL             string            `json:"upload_url"`
	Method          string            `json:"method"`
	RequiredHeaders map[string]string `json:"required_headers"`
	ExpiresAt       time.Time         `json:"expires_at"`
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

// AssetKey places an uploaded file where its owner can see it and the reconciler can read
// it: "chat/<assetID>-<name>" in the identity's own bucket.
//
// It used to be "identity/<id>/asset/<id>/original", and every part of that has stopped
// earning its place:
//
//   - The identity segment was the isolation. It is not any more — each identity has its
//     own bucket, so a key cannot address another's store at all, and repeating the id
//     inside a bucket that belongs to that id says nothing.
//   - "identity/" is excluded from the ingest sweep and hidden from the file manager, so an
//     attachment landing there was invisible AND unindexed: uploading a document in chat
//     and then asking about it found nothing.
//   - "original" has no extension. The extractor routes on it, so a .docx arriving as
//     "original" was fed to Tika as an unknown type and threw — the errors that made a
//     working pipeline look broken.
//
// The EXTENSION is carried and the name is NOT, and that split is the whole design. A key
// travels into presigned URLs, S3 access logs and error strings, so "Quarterly Secrets.pdf"
// in a key leaks the document's subject to everyone who sees a link — the reason
// TestServicePresignNeverPutsFilenameInObjectKey exists and the reason this function used
// to carry no name at all. ".pdf" leaks nothing and is exactly what the extractor routes
// on, so the extension comes along and the name stays behind on the asset row.
func AssetKey(assetID, fileName string) string {
	return "chat/" + assetID + assetExtension(fileName)
}

// assetExtension returns a lowercase ".ext", or "" when the name has none.
//
// Bounded and character-checked because it is caller-supplied: a chat client or a Telegram
// message can send any name, and this fragment becomes part of an object key.
func assetExtension(name string) string {
	ext := strings.ToLower(path.Ext(path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))))
	if len(ext) < 2 || len(ext) > 12 {
		return ""
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
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
