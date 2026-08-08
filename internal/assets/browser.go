package assets

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

// Browser lists one identity's objects as folders and files.
//
// Adapted from garage-webui's backend/router/browse.go (MIT, (c) 2024 Khairul Hidayat),
// which is the shape a Garage object browser actually needs: one delimiter-grouped page,
// prefixes separated from objects, keys returned relative to the prefix being viewed.
//
// The one deliberate departure is where it matters most. garage-webui builds an S3 client
// per bucket from admin credentials, because it IS the admin tool. Aura resolves the store
// and bucket through ObjectResolverBundle instead, so a listing is scoped by the owner's
// own credential rather than by a prefix this code remembered to apply -- an identity that
// resolves to its own bucket cannot read another's even if the prefix were wrong.
type Browser struct {
	Objects objectstore.Store
	// PerIdentity resolves the OWNER's store and bucket. Nil means the shared store, which
	// is the pre-provisioning deployment shape (see resolveObjects).
	PerIdentity  *ObjectResolverBundle
	SharedBucket string
}

// BrowseEntry is one row in a listing: a folder or an object.
type BrowseEntry struct {
	// Key is relative to the prefix being viewed, so a client renders it directly. Folders
	// keep their trailing "/" -- that is what distinguishes them, and what the caller
	// appends to the prefix to descend.
	Key        string    `json:"key"`
	Folder     bool      `json:"folder"`
	SizeBytes  int64     `json:"size_bytes"`
	ModifiedAt time.Time `json:"modified_at,omitzero"`
}

// BrowseResult is one page of a folder.
type BrowseResult struct {
	Bucket  string        `json:"bucket"`
	Prefix  string        `json:"prefix"`
	Entries []BrowseEntry `json:"entries"`
}

// ErrBrowserUnconfigured means no object store was wired, which is a deployment fault
// rather than a caller error and must not read as "this identity has no files".
var ErrBrowserUnconfigured = errors.New("assets: file browser is not configured")

// List returns one folder's contents for identityID.
//
// The delimiter is always "/": a listing without one returns every key in the bucket, which
// for a browser is both the wrong shape and an unbounded read of somebody's whole corpus.
func (b *Browser) List(
	ctx context.Context,
	identityID string,
	prefix string,
	limit int,
) (BrowseResult, error) {
	if b == nil || b.Objects == nil {
		return BrowseResult{}, ErrBrowserUnconfigured
	}
	if strings.TrimSpace(identityID) == "" {
		return BrowseResult{}, errors.New("assets: identity is required to browse files")
	}
	prefix = normalizeBrowsePrefix(prefix)
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

	store, bucket := b.Objects, b.SharedBucket
	if b.PerIdentity != nil {
		resolved, resolvedBucket, err := b.PerIdentity.ResolveForIdentity(ctx, b.Objects, identityID)
		if err != nil {
			return BrowseResult{}, err
		}
		store, bucket = resolved, resolvedBucket
	}

	objects, err := store.List(ctx, objectstore.ListRequest{
		Bucket: bucket, Prefix: prefix, Delimiter: "/", Limit: limit,
	})
	if err != nil {
		return BrowseResult{}, err
	}

	entries := make([]BrowseEntry, 0, len(objects))
	for _, object := range objects {
		relative := strings.TrimPrefix(object.Ref.Key, prefix)
		// The prefix itself comes back as a zero-length key when a folder marker object
		// exists. It is the folder being viewed, not an entry inside it.
		if relative == "" {
			continue
		}
		entries = append(entries, BrowseEntry{
			Key:        relative,
			Folder:     strings.HasSuffix(relative, "/"),
			SizeBytes:  object.Attrs.SizeBytes,
			ModifiedAt: object.Attrs.ModifiedAt,
		})
	}
	return BrowseResult{Bucket: bucket, Prefix: prefix, Entries: entries}, nil
}

// normalizeBrowsePrefix makes a caller-supplied path safe to concatenate.
//
// path.Clean collapses "..", so "a/../../etc" cannot climb out of the bucket, and a leading
// slash is dropped because an S3 key never starts with one -- a prefix of "/x/" silently
// matches nothing, which reads as an empty folder rather than as the mistake it is.
func normalizeBrowsePrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" || prefix == "/" {
		return ""
	}
	prefix = path.Clean("/" + prefix)
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix == "" || prefix == "." {
		return ""
	}
	// A prefix always addresses a FOLDER here, so it always ends in "/". Guessing from the
	// name instead -- "it has a dot, so it is a file" -- got a folder called "v1.2" wrong:
	// the listing would have matched every sibling starting with those characters and every
	// key would have come back relative to the wrong parent.
	return prefix + "/"
}
