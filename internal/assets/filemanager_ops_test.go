package assets

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

func opsFixture(t *testing.T, keys ...string) *Browser {
	t.Helper()
	store := objectstore.NewFake()
	for _, key := range keys {
		if _, err := store.Put(context.Background(),
			objectstore.ObjectRef{Bucket: "aura-assets", Key: key},
			strings.NewReader("x"), objectstore.PutOptions{Size: 1}); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	return &Browser{Objects: store, SharedBucket: "aura-assets"}
}

func allKeys(t *testing.T, browser *Browser) []string {
	t.Helper()
	objects, err := browser.Objects.List(context.Background(),
		objectstore.ListRequest{Bucket: "aura-assets"})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(objects))
	for _, object := range objects {
		keys = append(keys, object.Ref.Key)
	}
	return keys
}

// An empty folder has no keys, so no prefix, so a listing cannot show it. The marker object
// is what makes "new folder" survive a refresh instead of vanishing.
func TestCreateFolderLeavesAMarkerSoAnEmptyFolderExists(t *testing.T) {
	browser := opsFixture(t)
	id, err := browser.Create(t.Context(), "owner-1", "/contabilita", "2027", "folder")
	if err != nil {
		t.Fatal(err)
	}
	if id != "/contabilita/2027" {
		t.Fatalf("id = %q", id)
	}
	if !slices.Contains(allKeys(t, browser), "contabilita/2027/") {
		t.Fatalf("no folder marker: %v", allKeys(t, browser))
	}

	// A file gets no trailing slash, or it would read back as a folder.
	if _, err := browser.Create(t.Context(), "owner-1", "", "nota.txt", "file"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(allKeys(t, browser), "nota.txt") {
		t.Fatalf("file not created: %v", allKeys(t, browser))
	}
}

// Rename keeps the parent and changes only the last segment. Renaming a FOLDER has to carry
// everything under it: S3 has no rename, so the contents move key by key or not at all.
func TestRenameCarriesAFoldersWholeSubtree(t *testing.T) {
	browser := opsFixture(t,
		"contabilita/2026/fattura.pdf",
		"contabilita/2026/nota/allegato.txt",
		"contabilita/listino.xlsx",
	)
	id, err := browser.Rename(t.Context(), "owner-1", "/contabilita/2026", "2026-archivio")
	if err != nil {
		t.Fatal(err)
	}
	if id != "/contabilita/2026-archivio" {
		t.Fatalf("id = %q", id)
	}
	keys := allKeys(t, browser)
	for _, want := range []string{
		"contabilita/2026-archivio/fattura.pdf",
		"contabilita/2026-archivio/nota/allegato.txt",
		"contabilita/listino.xlsx",
	} {
		if !slices.Contains(keys, want) {
			t.Fatalf("missing %q after rename: %v", want, keys)
		}
	}
	for _, gone := range []string{"contabilita/2026/fattura.pdf", "contabilita/2026/nota/allegato.txt"} {
		if slices.Contains(keys, gone) {
			t.Fatalf("%q survived the rename: %v", gone, keys)
		}
	}
}

func TestMoveAndCopyDifferOnlyInWhetherTheSourceSurvives(t *testing.T) {
	browser := opsFixture(t, "inbox/relazione.docx", "archivio/.keep")

	if _, err := browser.Copy(t.Context(), "owner-1", []string{"/inbox/relazione.docx"}, "/archivio"); err != nil {
		t.Fatal(err)
	}
	keys := allKeys(t, browser)
	if !slices.Contains(keys, "archivio/relazione.docx") || !slices.Contains(keys, "inbox/relazione.docx") {
		t.Fatalf("copy did not duplicate: %v", keys)
	}

	if _, err := browser.Move(t.Context(), "owner-1", []string{"/inbox/relazione.docx"}, "/archivio"); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(allKeys(t, browser), "inbox/relazione.docx") {
		t.Fatalf("move left the source behind: %v", allKeys(t, browser))
	}
}

// Moving a folder into itself would make the walk follow the copies it is creating.
func TestMoveRefusesAFolderIntoItself(t *testing.T) {
	browser := opsFixture(t, "progetti/a.txt")
	if _, err := browser.Move(t.Context(), "owner-1", []string{"/progetti"}, "/progetti/sotto"); err == nil {
		t.Fatal("a folder was moved into itself")
	}
}

func TestDeleteRemovesAFoldersWholeSubtree(t *testing.T) {
	browser := opsFixture(t, "vecchio/a.txt", "vecchio/sotto/b.txt", "tenere/c.txt")
	if err := browser.Delete(t.Context(), "owner-1", []string{"/vecchio"}); err != nil {
		t.Fatal(err)
	}
	keys := allKeys(t, browser)
	if len(keys) != 1 || keys[0] != "tenere/c.txt" {
		t.Fatalf("delete took the wrong keys: %v", keys)
	}
}

// Names and ids are caller-supplied, so neither may place bytes outside the folder in view.
func TestOperationsRefuseNamesAndIDsThatEscape(t *testing.T) {
	browser := opsFixture(t, "contabilita/a.txt")
	for _, name := range []string{"../evil", "a/b", "..", ".", ".hidden", "   "} {
		if _, err := browser.Create(t.Context(), "owner-1", "/contabilita", name, "file"); err == nil {
			if slices.Contains(allKeys(t, browser), "evil") {
				t.Fatalf("%q escaped the folder", name)
			}
		}
	}
	// An id that climbs is normalised to a bucket-relative key, never a parent escape.
	if _, err := browser.Rename(t.Context(), "owner-1", "/../../etc/passwd", "x.txt"); err == nil {
		t.Fatal("renamed a path outside the bucket")
	}
}

// A folder walk is unbounded by nature; one request must not be able to start a walk over
// somebody's entire corpus.
func TestFolderOperationsAreBounded(t *testing.T) {
	store := objectstore.NewFake()
	for i := range maxFolderFanout + 2 {
		key := fmt.Sprintf("grande/%05d.txt", i)
		if _, err := store.Put(context.Background(),
			objectstore.ObjectRef{Bucket: "aura-assets", Key: key},
			strings.NewReader("x"), objectstore.PutOptions{Size: 1}); err != nil {
			t.Fatal(err)
		}
	}
	browser := &Browser{Objects: store, SharedBucket: "aura-assets"}
	if err := browser.Delete(t.Context(), "owner-1", []string{"/grande"}); !errors.Is(err, ErrTooManyObjects) {
		t.Fatalf("err = %v, want ErrTooManyObjects", err)
	}
}

// Every operation resolves the owner's bucket first, so an unconfigured browser is a
// deployment fault rather than a silent no-op on somebody's files.
func TestOperationsRefuseWhenUnconfigured(t *testing.T) {
	unconfigured := &Browser{}
	if _, err := unconfigured.Create(t.Context(), "owner-1", "", "a.txt", "file"); err == nil {
		t.Fatal("created without a store")
	}
	if err := unconfigured.Delete(t.Context(), "owner-1", []string{"/a.txt"}); err == nil {
		t.Fatal("deleted without a store")
	}
	if _, err := opsFixture(t).Create(t.Context(), "  ", "", "a.txt", "file"); err == nil {
		t.Fatal("created without an identity")
	}
}

// Aura's own storage lives in the same bucket as the user's files, because the bucket IS
// theirs. Hiding it from listings is the cosmetic half; refusing to write to it is the half
// that matters, since a delete reaching "identity/" takes every chat attachment with it.
func TestReservedPrefixesAreHiddenAndProtected(t *testing.T) {
	browser := opsFixture(t,
		"identity/1111/asset/2222/original",
		"share/3333/snapshot/4444/canonical.json",
		"contabilita/listino.xlsx",
	)

	result, err := browser.List(t.Context(), "owner-1", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range result.Entries {
		if strings.HasPrefix(entry.Key, "identity") || strings.HasPrefix(entry.Key, "share") {
			t.Fatalf("listing exposed Aura's own storage: %v", keysOf(result))
		}
	}
	if len(result.Entries) != 1 || result.Entries[0].Key != "contabilita/" {
		t.Fatalf("entries = %v, want just the user's folder", keysOf(result))
	}

	for name, run := range map[string]func() error{
		"delete assets": func() error {
			return browser.Delete(t.Context(), "owner-1", []string{"/identity"})
		},
		"delete one asset": func() error {
			return browser.Delete(t.Context(), "owner-1", []string{"/identity/1111/asset/2222/original"})
		},
		"delete shares": func() error {
			return browser.Delete(t.Context(), "owner-1", []string{"/share"})
		},
		"rename assets": func() error {
			_, err := browser.Rename(t.Context(), "owner-1", "/identity", "mio")
			return err
		},
		"move out of assets": func() error {
			_, err := browser.Move(t.Context(), "owner-1", []string{"/identity/1111"}, "/contabilita")
			return err
		},
		"copy into assets": func() error {
			_, err := browser.Copy(t.Context(), "owner-1", []string{"/contabilita/listino.xlsx"}, "/identity")
			return err
		},
		"create inside assets": func() error {
			_, err := browser.Create(t.Context(), "owner-1", "/identity", "x.txt", "file")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, ErrReservedPrefix) {
				t.Fatalf("err = %v, want ErrReservedPrefix", err)
			}
		})
	}

	// Nothing was touched by any of the refusals above.
	if !slices.Contains(allKeys(t, browser), "identity/1111/asset/2222/original") {
		t.Fatalf("an asset was lost: %v", allKeys(t, browser))
	}
}
