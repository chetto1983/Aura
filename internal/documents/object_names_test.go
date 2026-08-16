package documents

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

const objectNamesIdentity = "00000000-0000-0000-0000-000000000001"

type fakeNameLookup struct {
	asked []string
	names map[string]string
	err   error
}

func (f *fakeNameLookup) DocumentNames(
	_ context.Context, _ string, documentIDs []string,
) (map[string]string, error) {
	f.asked = append([]string(nil), documentIDs...)
	return f.names, f.err
}

func mustSearchID(t *testing.T, key string) string {
	t.Helper()
	id, err := SearchDocumentID(objectNamesIdentity, "s3", key)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The whole point of the type: a listing speaks object keys, the index speaks search ids,
// and the digest that joins them is the same one services/ingest computes.
func TestNamesByKeyTranslatesKeysThroughTheSearchID(t *testing.T) {
	key := "chat/b4e391e0-6141-4807-b8e5-88ca58f21162.pdf"
	lookup := &fakeNameLookup{names: map[string]string{
		mustSearchID(t, key): "colm2025_conference.pdf",
	}}

	names, err := (&ObjectNames{Index: lookup}).NamesByKey(
		t.Context(), objectNamesIdentity, []string{key})
	if err != nil {
		t.Fatal(err)
	}
	if names[key] != "colm2025_conference.pdf" {
		t.Fatalf("names = %#v, want the key mapped back to its indexed name", names)
	}
	if !reflect.DeepEqual(lookup.asked, []string{mustSearchID(t, key)}) {
		t.Fatalf("asked for %#v, want the derived search id", lookup.asked)
	}
}

// A folder listing can repeat a key across pages, and an id asked for twice would widen the
// statement for no answer it does not already have.
func TestNamesByKeyAsksForEachIDOnce(t *testing.T) {
	key := "chat/one.pdf"
	lookup := &fakeNameLookup{}

	if _, err := (&ObjectNames{Index: lookup}).NamesByKey(
		t.Context(), objectNamesIdentity, []string{key, key, key}); err != nil {
		t.Fatal(err)
	}
	if len(lookup.asked) != 1 {
		t.Fatalf("asked %#v, want one id", lookup.asked)
	}
}

// A key that cannot be fingerprinted is a key no row was written for, so it has no name to
// find. In a listing that is an absence, never a failure.
func TestNamesByKeySkipsKeysThatCannotBeFingerprinted(t *testing.T) {
	lookup := &fakeNameLookup{}

	names, err := (&ObjectNames{Index: lookup}).NamesByKey(
		t.Context(), objectNamesIdentity, []string{"", "   "})
	if err != nil || names != nil {
		t.Fatalf("names = %#v, err = %v, want no lookup at all", names, err)
	}
	if lookup.asked != nil {
		t.Fatalf("asked %#v for unfingerprintable keys", lookup.asked)
	}
}

// Nil is a supported wiring: a deployment without ArcadeDB still browses its bucket.
func TestNamesByKeyIsInertWithoutAnIndex(t *testing.T) {
	names, err := (&ObjectNames{}).NamesByKey(
		t.Context(), objectNamesIdentity, []string{"chat/one.pdf"})
	if err != nil || names != nil {
		t.Fatalf("names = %#v, err = %v, want an inert lookup", names, err)
	}
}

func TestNamesByKeySurfacesLookupFailures(t *testing.T) {
	lookup := &fakeNameLookup{err: errors.New("arcadedb unreachable")}

	if _, err := (&ObjectNames{Index: lookup}).NamesByKey(
		t.Context(), objectNamesIdentity, []string{"chat/one.pdf"}); err == nil {
		t.Fatal("a failing lookup must be reported, so the caller decides whether to degrade")
	}
}

// An empty name is not a name: reporting one would replace a readable key with nothing.
func TestNamesByKeyDropsEmptyNames(t *testing.T) {
	key := "chat/one.pdf"
	lookup := &fakeNameLookup{names: map[string]string{mustSearchID(t, key): ""}}

	names, err := (&ObjectNames{Index: lookup}).NamesByKey(
		t.Context(), objectNamesIdentity, []string{key})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := names[key]; present {
		t.Fatalf("names = %#v, want the empty name dropped", names)
	}
}
