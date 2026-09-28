package assets

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// KeysHeld mirrors the real store: the keys among keys some row of the identity holds, in any
// status, deleting rows and tombstones included.
func (s *fakeAssetStore) KeysHeld(_ context.Context, identityID string, keys []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var held []string
	for _, asset := range s.assets {
		if asset.IdentityID == identityID && slices.Contains(keys, asset.ObjectKey) {
			held = append(held, asset.ObjectKey)
		}
	}
	return held, nil
}

// Relocate mirrors the real store: every row of the identity in bucket that holds a moved key
// now holds its new one, and takes the new name when the move carries one.
func (s *fakeAssetStore) Relocate(_ context.Context, identityID, bucket string, moves []KeyMove) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, asset := range s.assets {
		if asset.IdentityID != identityID || asset.ObjectBucket != bucket {
			continue
		}
		for _, move := range moves {
			if asset.ObjectKey == move.From {
				asset.ObjectKey = move.To
				if move.Name != "" {
					asset.FileName = move.Name
				}
				s.assets[id] = asset
				break
			}
		}
	}
	return nil
}

func keyedRow(id, identityID, bucket, key, name string) Asset {
	return Asset{ID: id, IdentityID: identityID, Status: StatusAccepted, ObjectBucket: bucket, ObjectKey: key, FileName: name}
}

func requireRowAt(t *testing.T, rows *fakeAssetStore, id, key, name string) {
	t.Helper()
	row, ok := rows.row(id)
	if !ok || row.ObjectKey != key || row.FileName != name {
		t.Fatalf("row %s = %+v (present %v), want it at %q named %q", id, row, ok, key, name)
	}
}

// A moved file used to leave its asset row on the old key, over bytes that were gone: the
// asset no longer downloaded and the listing lost its name. The row now follows the bytes.
func TestMoveTakesTheAssetRowWithTheFile(t *testing.T) {
	browser := opsFixture(t, "chat/row.pdf", "archivio/.keep")
	rows := withAssetRow(browser,
		keyedRow("asset-row", "owner-1", "aura-assets", "chat/row.pdf", "report.pdf"),
		keyedRow("asset-theirs", "owner-2", "aura-other", "chat/row.pdf", "theirs.pdf"),
	)

	if _, err := browser.Move(t.Context(), "owner-1", []string{"/chat/row.pdf"}, "/archivio"); err != nil {
		t.Fatal(err)
	}
	requireRowAt(t, rows, "asset-row", "archivio/row.pdf", "report.pdf")
	requireRowAt(t, rows, "asset-theirs", "chat/row.pdf", "theirs.pdf")
	if keys := allKeys(t, browser); !slices.Contains(keys, "archivio/row.pdf") || slices.Contains(keys, "chat/row.pdf") {
		t.Fatalf("keys after move: %v", keys)
	}
}

// The listing shows a row's file name, so a rename that moved only the key would show the old
// name on the new key.
func TestRenameGivesTheAssetRowTheNewName(t *testing.T) {
	browser := opsFixture(t, "chat/row.pdf")
	rows := withAssetRow(browser, keyedRow("asset-row", "owner-1", "aura-assets", "chat/row.pdf", "report.pdf"))

	if _, err := browser.Rename(t.Context(), "owner-1", "/chat/row.pdf", "q3.pdf"); err != nil {
		t.Fatal(err)
	}
	requireRowAt(t, rows, "asset-row", "chat/q3.pdf", "q3.pdf")
}

// A folder carries every key beneath it, and every row on those keys goes along -- names kept,
// since renaming a folder renames none of the files inside it.
func TestFolderMoveAndRenameTakeEveryRowBeneathThem(t *testing.T) {
	browser := opsFixture(t, "clienti/rossi/a.pdf", "clienti/rossi/sotto/b.png", "altro/c.txt")
	rows := withAssetRow(browser,
		keyedRow("asset-a", "owner-1", "aura-assets", "clienti/rossi/a.pdf", "contratto.pdf"),
		keyedRow("asset-b", "owner-1", "aura-assets", "clienti/rossi/sotto/b.png", "logo.png"),
		keyedRow("asset-c", "owner-1", "aura-assets", "altro/c.txt", "note.txt"),
	)

	if _, err := browser.Rename(t.Context(), "owner-1", "/clienti/rossi", "rossi-srl"); err != nil {
		t.Fatal(err)
	}
	requireRowAt(t, rows, "asset-a", "clienti/rossi-srl/a.pdf", "contratto.pdf")
	requireRowAt(t, rows, "asset-b", "clienti/rossi-srl/sotto/b.png", "logo.png")
	if _, err := browser.Move(t.Context(), "owner-1", []string{"/clienti"}, "/archivio"); err != nil {
		t.Fatal(err)
	}
	requireRowAt(t, rows, "asset-a", "archivio/clienti/rossi-srl/a.pdf", "contratto.pdf")
	requireRowAt(t, rows, "asset-b", "archivio/clienti/rossi-srl/sotto/b.png", "logo.png")
	requireRowAt(t, rows, "asset-c", "altro/c.txt", "note.txt")
}

// A copy is a new file, not a new asset: the row stays with the original, which keeps the
// provenance it records, and the copy is listed by its key like any file dropped in the bucket.
func TestCopyLeavesTheAssetRowWithTheOriginal(t *testing.T) {
	browser := opsFixture(t, "chat/row.pdf", "archivio/.keep")
	rows := withAssetRow(browser, keyedRow("asset-row", "owner-1", "aura-assets", "chat/row.pdf", "report.pdf"))

	if _, err := browser.Copy(t.Context(), "owner-1", []string{"/chat/row.pdf"}, "/archivio"); err != nil {
		t.Fatal(err)
	}
	requireRowAt(t, rows, "asset-row", "chat/row.pdf", "report.pdf")
	if held, _ := rows.KeysHeld(t.Context(), "owner-1", []string{"archivio/row.pdf"}); len(held) != 0 {
		t.Fatalf("the copy got a row: %v", held)
	}
}

// Writing over a key an asset row holds would replace that asset's bytes under it -- or, for a
// row being deleted, hand the new bytes to the sweep that removes its object. Refused before
// anything is copied, for a move and for a copy alike.
func TestMoveAndCopyRefuseADestinationAnAssetHolds(t *testing.T) {
	for _, status := range []Status{StatusAccepted, StatusDeleting} {
		for name, write := range map[string]func(*Browser) error{
			"move": func(b *Browser) error {
				_, err := b.Move(t.Context(), "owner-1", []string{"/inbox/row.pdf"}, "/archivio")
				return err
			},
			"copy": func(b *Browser) error {
				_, err := b.Copy(t.Context(), "owner-1", []string{"/inbox/row.pdf"}, "/archivio")
				return err
			},
		} {
			browser := opsFixture(t, "inbox/row.pdf", "archivio/row.pdf")
			held := keyedRow("asset-held", "owner-1", "aura-assets", "archivio/row.pdf", "held.pdf")
			held.Status = status
			rows := withAssetRow(browser, held, keyedRow("asset-moving", "owner-1", "aura-assets", "inbox/row.pdf", "moving.pdf"))

			if err := write(browser); !errors.Is(err, ErrDestinationHeld) {
				t.Fatalf("%s onto a %s row's key: err = %v, want ErrDestinationHeld", name, status, err)
			}
			requireRowAt(t, rows, "asset-held", "archivio/row.pdf", "held.pdf")
			requireRowAt(t, rows, "asset-moving", "inbox/row.pdf", "moving.pdf")
			if keys := allKeys(t, browser); !slices.Contains(keys, "inbox/row.pdf") {
				t.Fatalf("%s removed the source it refused to write: %v", name, keys)
			}
		}
	}
}

// rowsUnreachable lets the file manager reach an asset store whose key check or relocation
// fails, as a lost connection would.
type rowsUnreachable struct {
	*fakeAssetStore
	held, relocate error
}

func (r rowsUnreachable) KeysHeld(ctx context.Context, identityID string, keys []string) ([]string, error) {
	if r.held != nil {
		return nil, r.held
	}
	return r.fakeAssetStore.KeysHeld(ctx, identityID, keys)
}

func (r rowsUnreachable) Relocate(ctx context.Context, identityID, bucket string, moves []KeyMove) error {
	if r.relocate != nil {
		return r.relocate
	}
	return r.fakeAssetStore.Relocate(ctx, identityID, bucket, moves)
}

// Without knowing whether an asset holds the destination, writing there could replace its
// bytes, so nothing is copied.
func TestMoveWritesNothingWhenTheDestinationCannotBeChecked(t *testing.T) {
	browser := opsFixture(t, "inbox/row.pdf")
	rows := withAssetRow(browser, keyedRow("asset-row", "owner-1", "aura-assets", "inbox/row.pdf", "report.pdf"))
	browser.Rows = rowsUnreachable{fakeAssetStore: rows, held: errors.New("database unreachable")}

	if _, err := browser.Move(t.Context(), "owner-1", []string{"/inbox/row.pdf"}, "/archivio"); err == nil {
		t.Fatal("the move went ahead without checking its destination")
	}
	if keys := allKeys(t, browser); !slices.Equal(keys, []string{"inbox/row.pdf"}) {
		t.Fatalf("keys = %v, want only the untouched source", keys)
	}
}

// A row that cannot follow keeps its source: the bytes it names stay where it names them.
func TestMoveKeepsTheSourceWhenItsRowCannotFollow(t *testing.T) {
	browser := opsFixture(t, "inbox/row.pdf")
	rows := withAssetRow(browser, keyedRow("asset-row", "owner-1", "aura-assets", "inbox/row.pdf", "report.pdf"))
	browser.Rows = rowsUnreachable{fakeAssetStore: rows, relocate: errors.New("database unreachable")}

	if _, err := browser.Move(t.Context(), "owner-1", []string{"/inbox/row.pdf"}, "/archivio"); err == nil {
		t.Fatal("the move reported success although its row stayed behind")
	}
	requireRowAt(t, rows, "asset-row", "inbox/row.pdf", "report.pdf")
	if keys := allKeys(t, browser); !slices.Contains(keys, "inbox/row.pdf") {
		t.Fatalf("keys = %v, want the source kept for its row", keys)
	}
}
