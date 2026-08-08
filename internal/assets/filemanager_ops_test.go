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
