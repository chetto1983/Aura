package assets

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

func browserFixture(t *testing.T) *Browser {
	t.Helper()
	store := objectstore.NewFake()
	for _, key := range []string{
		"contabilita/2026/fattura-acme.pdf",
		"contabilita/2026/nota.txt",
		"contabilita/listino.xlsx",
		"manuali/pompa.txt",
		"radice.md",
	} {
		if _, err := store.Put(context.Background(),
			objectstore.ObjectRef{Bucket: "aura-assets", Key: key},
			strings.NewReader("x"), objectstore.PutOptions{Size: 1}); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	return &Browser{Objects: store, SharedBucket: "aura-assets"}
}

func keysOf(result BrowseResult) []string {
	out := make([]string, 0, len(result.Entries))
	for _, entry := range result.Entries {
		out = append(out, entry.Key)
	}
	return out
}

// The root of a bucket must show FOLDERS, not every key underneath it. A listing without a
// delimiter returns the whole corpus flattened, which is both the wrong shape for a browser
// and an unbounded read.
func TestBrowserGroupsTheRootIntoFolders(t *testing.T) {
	result, err := browserFixture(t).List(t.Context(), "owner-1", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(keysOf(result), ",")
	for _, want := range []string{"contabilita/", "manuali/", "radice.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("entries = %v, want %q", keysOf(result), want)
		}
	}
	if strings.Contains(got, "fattura-acme.pdf") {
		t.Fatalf("root listing leaked a nested object: %v", keysOf(result))
	}
	for _, entry := range result.Entries {
		if entry.Key == "contabilita/" && !entry.Folder {
			t.Fatal("a grouped prefix was not marked as a folder")
		}
	}
}

// Keys come back RELATIVE to the folder being viewed, so a client renders them directly
// instead of trimming a prefix it has to remember.
func TestBrowserReturnsKeysRelativeToTheFolder(t *testing.T) {
	result, err := browserFixture(t).List(t.Context(), "owner-1", "contabilita/", 100)
	if err != nil {
		t.Fatal(err)
	}
	got := keysOf(result)
	joined := strings.Join(got, ",")
	if !strings.Contains(joined, "listino.xlsx") || !strings.Contains(joined, "2026/") {
		t.Fatalf("entries = %v", got)
	}
	for _, key := range got {
		if strings.HasPrefix(key, "contabilita/") {
			t.Fatalf("entry %q still carries the viewed prefix", key)
		}
	}
}

// A prefix is caller-supplied, so it must not be able to climb out of the bucket.
func TestBrowserNormalisesHostilePrefixes(t *testing.T) {
	browser := browserFixture(t)
	for _, prefix := range []string{"../../etc/", "/contabilita/", "contabilita/../"} {
		result, err := browser.List(t.Context(), "owner-1", prefix, 100)
		if err != nil {
			t.Fatalf("%q: %v", prefix, err)
		}
		if strings.HasPrefix(result.Prefix, "/") || strings.Contains(result.Prefix, "..") {
			t.Fatalf("%q normalised to %q", prefix, result.Prefix)
		}
	}
}

// A folder is a folder whatever it is called. Deciding from the name -- "it has a dot, so
// it must be a file" -- left "v1.2" without its trailing slash, so the listing matched every
// sibling starting with those characters and every key came back relative to the wrong
// parent.
func TestBrowserTreatsADottedFolderAsAFolder(t *testing.T) {
	store := objectstore.NewFake()
	for _, key := range []string{"v1.2/nota.txt", "v1.20/altro.txt"} {
		if _, err := store.Put(context.Background(),
			objectstore.ObjectRef{Bucket: "aura-assets", Key: key},
			strings.NewReader("x"), objectstore.PutOptions{Size: 1}); err != nil {
			t.Fatalf("Put %s: %v", key, err)
		}
	}
	browser := &Browser{Objects: store, SharedBucket: "aura-assets"}

	result, err := browser.List(t.Context(), "owner-1", "v1.2", 100)
	if err != nil {
		t.Fatal(err)
	}
	if result.Prefix != "v1.2/" {
		t.Fatalf("prefix = %q, want v1.2/", result.Prefix)
	}
	if got := keysOf(result); len(got) != 1 || got[0] != "nota.txt" {
		t.Fatalf("entries = %v, want just nota.txt from the sibling-free folder", got)
	}
}

// An unconfigured browser is a DEPLOYMENT fault. It must not read as "this identity has no
// files", which is what an empty success would say.
func TestBrowserRefusesWhenUnconfigured(t *testing.T) {
	if _, err := (&Browser{}).List(t.Context(), "owner-1", "", 10); err == nil {
		t.Fatal("unconfigured browser returned a listing")
	}
	if _, err := browserFixture(t).List(t.Context(), "  ", "", 10); err == nil {
		t.Fatal("browser listed without an identity")
	}
}
