package assets

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"

	"github.com/chetto1983/aura/internal/objectstore"
	"github.com/jackc/pgx/v5"
)

// The file manager's write half. Every operation here is expressed in the vocabulary the
// UI uses -- create, rename, move, copy, delete -- over a store that has none of them.
//
// S3 has objects and prefixes, so:
//   - a FOLDER is a prefix, materialised by a zero-byte object whose key ends in "/" so an
//     empty one is still visible in a listing;
//   - rename and move are copy-then-delete, there being no primitive for either;
//   - anything done to a folder is done to every key beneath it.
//
// The recursion is why these live here rather than in the HTTP handler: getting it wrong
// means moving a folder and silently leaving its contents behind.

// folderMarkerSuffix makes an empty folder exist. Without it a folder with nothing in it has
// no keys, so no prefix, so the listing cannot show it and the user's new folder vanishes.
const folderMarkerSuffix = "/"

// maxFolderFanout bounds one recursive operation. A folder move walks every key underneath
// it, and an unbounded walk over somebody's whole corpus is a request that never returns.
const maxFolderFanout = 10000

// ErrTooManyObjects means a folder operation covers more keys than one request may touch.
var ErrTooManyObjects = errors.New("assets: folder contains too many objects for one operation")

// ErrDestinationHeld means a move, rename or copy would write over the object an asset row
// holds: its bytes would be replaced under the row, or removed with a row being deleted.
var ErrDestinationHeld = errors.New("assets: an asset already holds that name here")

// Create makes an empty file or folder under parent and returns its new id.
func (b *Browser) Create(ctx context.Context, identityID, parent, name, kind string) (string, error) {
	store, bucket, err := b.resolveStore(ctx, identityID)
	if err != nil {
		return "", err
	}
	safe := safeSegment(name)
	if safe == "" {
		return "", fmt.Errorf("assets: %q is not a usable name", name)
	}
	key := normalizeBrowsePrefix(parent) + safe
	if reserved(key) {
		return "", ErrReservedPrefix
	}
	if kind == "folder" {
		key += folderMarkerSuffix
	}
	if _, err := store.Put(ctx, objectstore.ObjectRef{Bucket: bucket, Key: key},
		strings.NewReader(""), objectstore.PutOptions{Size: 0}); err != nil {
		return "", err
	}
	return "/" + strings.TrimSuffix(key, folderMarkerSuffix), nil
}

// Rename gives one file or folder a new name in place, returning its new id.
func (b *Browser) Rename(ctx context.Context, identityID, id, name string) (string, error) {
	safe := safeSegment(name)
	if safe == "" {
		return "", fmt.Errorf("assets: %q is not a usable name", name)
	}
	source := cleanKey(id)
	if source == "" {
		return "", errors.New("assets: rename needs a file")
	}
	if reserved(source) {
		return "", ErrReservedPrefix
	}
	target := path.Dir(source)
	if target == "." || target == "/" {
		target = ""
	} else {
		target += "/"
	}
	return b.transfer(ctx, identityID, source, target+safe, true, safe)
}

// Move relocates ids into target, returning their new ids in the same order.
func (b *Browser) Move(ctx context.Context, identityID string, ids []string, target string) ([]string, error) {
	return b.transferAll(ctx, identityID, ids, target, true)
}

// Copy duplicates ids into target, returning the new ids in the same order.
func (b *Browser) Copy(ctx context.Context, identityID string, ids []string, target string) ([]string, error) {
	return b.transferAll(ctx, identityID, ids, target, false)
}

func (b *Browser) transferAll(
	ctx context.Context, identityID string, ids []string, target string, removeSource bool,
) ([]string, error) {
	folder := normalizeBrowsePrefix(target)
	moved := make([]string, 0, len(ids))
	for _, id := range ids {
		source := cleanKey(id)
		if source == "" {
			continue
		}
		// The destination keeps the source's own name; only its parent changes.
		id, err := b.transfer(ctx, identityID, source, folder+path.Base(source), removeSource, "")
		if err != nil {
			return nil, err
		}
		moved = append(moved, id)
	}
	return moved, nil
}

// transfer copies one file or one whole folder to destination, optionally removing the
// source afterwards. Copy-then-delete, in that order: a failed copy leaves the original
// where it was, which is the only recoverable way round. A move takes the asset rows of the
// keys it relocates between the two, so no row ever names a key without bytes; a failure
// there leaves the sources and their rows as they were, and the copies without a row. name,
// when not empty, is the renamed file's new name, which its row takes too. A copy gets no
// row: it is a new file, not a new upload, and is listed by its key like any other.
func (b *Browser) transfer(
	ctx context.Context, identityID, source, destination string, removeSource bool, name string,
) (string, error) {
	store, bucket, err := b.resolveStore(ctx, identityID)
	if err != nil {
		return "", err
	}
	if reserved(source) || reserved(destination) {
		return "", ErrReservedPrefix
	}
	if source == destination {
		return "/" + source, nil
	}
	// A folder cannot be moved into itself: the walk below would follow the copies it is
	// making and never terminate.
	if strings.HasPrefix(destination+"/", source+"/") {
		return "", fmt.Errorf("assets: cannot move %q into itself", source)
	}
	keys, err := b.descendants(ctx, store, bucket, source)
	if err != nil {
		return "", err
	}
	moves := make([]KeyMove, 0, len(keys))
	for _, key := range keys {
		move := KeyMove{From: key, To: destination + strings.TrimPrefix(key, source)}
		if key == source {
			move.Name = name
		}
		moves = append(moves, move)
	}
	if err := b.refuseHeldDestinations(ctx, identityID, moves); err != nil {
		return "", err
	}
	for _, move := range moves {
		if err := store.Copy(ctx,
			objectstore.ObjectRef{Bucket: bucket, Key: move.From},
			objectstore.ObjectRef{Bucket: bucket, Key: move.To}); err != nil {
			return "", fmt.Errorf("copy %s: %w", move.From, err)
		}
	}
	if !removeSource {
		return "/" + destination, nil
	}
	if b.Rows != nil {
		if err := b.Rows.Relocate(ctx, identityID, bucket, moves); err != nil {
			return "", fmt.Errorf("move the asset rows: %w", err)
		}
	}
	for _, move := range moves {
		if err := store.Delete(ctx, objectstore.ObjectRef{Bucket: bucket, Key: move.From}); err != nil {
			return "", fmt.Errorf("remove %s: %w", move.From, err)
		}
	}
	return "/" + destination, nil
}

// refuseHeldDestinations fails before anything is copied when an asset row holds a key the
// write would land on: the copy would replace that asset's bytes under its row, or hand them
// to the sweep removing a deleting row's object.
func (b *Browser) refuseHeldDestinations(ctx context.Context, identityID string, moves []KeyMove) error {
	if b.Rows == nil {
		return nil
	}
	targets := make([]string, 0, len(moves))
	for _, move := range moves {
		targets = append(targets, move.To)
	}
	held, err := b.Rows.KeysHeld(ctx, identityID, targets)
	if err != nil {
		return err
	}
	if len(held) > 0 {
		return fmt.Errorf("%w: %s", ErrDestinationHeld, held[0])
	}
	return nil
}

// Delete removes files and folders, a folder taking everything under it.
func (b *Browser) Delete(ctx context.Context, identityID string, ids []string) error {
	store, bucket, err := b.resolveStore(ctx, identityID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		source := cleanKey(id)
		if source == "" {
			continue
		}
		if reserved(source) {
			return ErrReservedPrefix
		}
		keys, err := b.descendants(ctx, store, bucket, source)
		if err != nil {
			return err
		}
		for _, key := range keys {
			if err := b.deleteKey(ctx, store, identityID, objectstore.ObjectRef{Bucket: bucket, Key: key}); err != nil {
				return fmt.Errorf("remove %s: %w", key, err)
			}
		}
	}
	return nil
}

// deleteKey removes one object and retires the asset row holding its key, in Service.Delete's
// order: the row is marked deleting before the bytes go, so a failure between the two leaves
// a row the retention sweep finishes, never a live row over a missing object.
func (b *Browser) deleteKey(ctx context.Context, store objectstore.Store, identityID string, ref objectstore.ObjectRef) error {
	holder, err := b.claimRow(ctx, identityID, ref)
	if err != nil {
		return err
	}
	if err := store.Delete(ctx, ref); err != nil {
		return err
	}
	if holder.ID == "" {
		return nil
	}
	if err := b.Rows.Finalize(ctx, holder.ID, identityID); err != nil {
		slog.Warn("aura assets: file manager delete left the row for the retention sweep", "asset_id", holder.ID, "err", err)
	}
	return nil
}

// claimRow marks deleting the identity's asset row that holds ref, if one does. A row on the
// same key in another bucket points at bytes this delete does not remove, so it stays.
func (b *Browser) claimRow(ctx context.Context, identityID string, ref objectstore.ObjectRef) (Asset, error) {
	if b.Rows == nil {
		return Asset{}, nil
	}
	holder, err := b.Rows.ByObjectKey(ctx, identityID, ref.Key)
	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, nil
	}
	if err != nil {
		return Asset{}, err
	}
	if holder.ObjectBucket != ref.Bucket {
		return Asset{}, nil
	}
	return b.Rows.Delete(ctx, holder.ID, identityID)
}

// descendants returns every key an operation on source touches: the object itself if it is
// one, plus everything under it if it is a folder. Both are listed, because a key can be
// both -- a folder marker is an object whose key ends in "/".
func (b *Browser) descendants(
	ctx context.Context, store objectstore.Store, bucket, source string,
) ([]string, error) {
	found := make([]string, 0, 1)
	seen := make(map[string]struct{}, 1)
	if _, err := store.Head(ctx, objectstore.ObjectRef{Bucket: bucket, Key: source}); err == nil {
		found, seen[source] = append(found, source), struct{}{}
	} else if !objectstore.IsNotFound(err) {
		return nil, err
	}
	// No delimiter: this is the one place a FLAT listing is wanted, because the whole subtree
	// has to move, not just its first level.
	nested, err := store.List(ctx, objectstore.ListRequest{
		Bucket: bucket, Prefix: source + "/", Limit: maxFolderFanout + 1,
	})
	if err != nil {
		return nil, err
	}
	if len(nested) > maxFolderFanout {
		return nil, ErrTooManyObjects
	}
	for _, object := range nested {
		if _, duplicate := seen[object.Ref.Key]; duplicate {
			continue
		}
		seen[object.Ref.Key] = struct{}{}
		found = append(found, object.Ref.Key)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("assets: %q not found", source)
	}
	return found, nil
}

// cleanKey turns a caller-supplied id into a bucket-relative key that cannot climb out.
func cleanKey(id string) string {
	cleaned := normalizeBrowsePrefix(id)
	return strings.TrimSuffix(cleaned, "/")
}

// safeSegment reduces a caller-supplied name to a single path component. A name carrying a
// directory would place the result somewhere other than the folder the user is looking at.
func safeSegment(name string) string {
	segment := path.Base(strings.ReplaceAll(strings.TrimSpace(name), `\`, "/"))
	if segment == "." || segment == ".." || segment == "/" || strings.HasPrefix(segment, ".") {
		return ""
	}
	return segment
}
