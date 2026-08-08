package documents

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// These tests used to cover a five-hop resolution: a doc_<hex> to a catalog uuid, the uuid
// to a document, the document to its ACTIVE version, that to an asset id, and the asset to
// an object -- with cases for the uuid namespace, for picking the active version, for
// falling back to the newest, and for refusing a version with no stored original. Every one
// of those concepts is gone: a record carries source_key, so there is one hop and no
// version to choose between.
//
// Two guarantees outlived the model and are kept below, because they are about safety
// rather than shape: the caller's identity is passed to BOTH the lookup and the read, and a
// file name that becomes a path component is never trusted.

type fakeRecordLookup struct {
	identity string
	id       string
	record   arcadedb.DocumentCard
	err      error
}

func (f *fakeRecordLookup) DocumentByID(
	_ context.Context, identityID, searchDocumentID string,
) (arcadedb.DocumentCard, error) {
	f.identity, f.id = identityID, searchDocumentID
	return f.record, f.err
}

type fakeObjectOpener struct {
	identity string
	key      string
	body     string
	err      error
}

func (f *fakeObjectOpener) OpenObject(
	_ context.Context, identityID, key string,
) (io.ReadCloser, error) {
	f.identity, f.key = identityID, key
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(f.body)), nil
}

func openFixture() (*OpenService, *fakeRecordLookup, *fakeObjectOpener) {
	lookup := &fakeRecordLookup{record: arcadedb.DocumentCard{
		SearchDocumentID: "doc_" + strings.Repeat("a", 32),
		SourceKind:       "s3",
		SourceKey:        "contabilita/2026/fattura-acme.pdf",
		FileName:         "fattura-acme.pdf",
		RawSHA256:        strings.Repeat("b", 64),
		SizeBytes:        22083,
	}}
	opener := &fakeObjectOpener{body: "%PDF real bytes"}
	return &OpenService{Index: lookup, Objects: opener}, lookup, opener
}

func TestOpenDocumentResolvesTheSourceKeyInOneHop(t *testing.T) {
	service, lookup, opener := openFixture()
	body, meta, err := service.OpenDocument(t.Context(), retrievalIdentity, lookup.record.SearchDocumentID)
	if err != nil {
		t.Fatalf("OpenDocument: %v", err)
	}
	defer body.Close()
	read, _ := io.ReadAll(body)
	if string(read) != "%PDF real bytes" {
		t.Fatalf("body = %q", read)
	}
	// The key the record carries is the key that was read -- nothing in between resolves,
	// rewrites or guesses it.
	if opener.key != lookup.record.SourceKey {
		t.Fatalf("opened %q, want the record's source key %q", opener.key, lookup.record.SourceKey)
	}
	if meta.SourceKey != lookup.record.SourceKey || meta.SHA256 != lookup.record.RawSHA256 ||
		meta.SizeBytes != 22083 || meta.FileName != "fattura-acme.pdf" {
		t.Fatalf("meta = %+v", meta)
	}
}

// The identity must reach BOTH the lookup and the read. The lookup picks the tenant
// database and the read resolves the owner's own credential, so an identity that stopped
// at one of them would leave the other reading somebody else's store.
func TestOpenDocumentPassesTheIdentityToBothGates(t *testing.T) {
	service, lookup, opener := openFixture()
	body, _, err := service.OpenDocument(t.Context(), retrievalIdentity, lookup.record.SearchDocumentID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if lookup.identity != retrievalIdentity || opener.identity != retrievalIdentity {
		t.Fatalf("identity reached lookup=%q opener=%q, want %q both",
			lookup.identity, opener.identity, retrievalIdentity)
	}
}

func TestOpenDocumentSurfacesLookupAndReadFailures(t *testing.T) {
	service, lookup, _ := openFixture()
	lookup.err = errors.New("not found")
	if _, _, err := service.OpenDocument(t.Context(), retrievalIdentity, "doc_x"); err == nil {
		t.Fatal("a missing record opened successfully")
	}

	service, lookup, opener := openFixture()
	opener.err = errors.New("object store down")
	if _, _, err := service.OpenDocument(t.Context(), retrievalIdentity, lookup.record.SearchDocumentID); err == nil {
		t.Fatal("an unreadable object opened successfully")
	}
}

func TestOpenDocumentValidatesItsInputsAndWiring(t *testing.T) {
	service, lookup, _ := openFixture()
	for name, run := range map[string]func() error{
		"no document id": func() error {
			_, _, err := service.OpenDocument(t.Context(), retrievalIdentity, "  ")
			return err
		},
		"no identity": func() error {
			_, _, err := service.OpenDocument(t.Context(), " ", lookup.record.SearchDocumentID)
			return err
		},
		"no index": func() error {
			_, _, err := (&OpenService{Objects: &fakeObjectOpener{}}).
				OpenDocument(t.Context(), retrievalIdentity, "doc_x")
			return err
		},
		"no opener": func() error {
			_, _, err := (&OpenService{Index: lookup}).
				OpenDocument(t.Context(), retrievalIdentity, "doc_x")
			return err
		},
		"nil service": func() error {
			_, _, err := (*OpenService)(nil).OpenDocument(t.Context(), retrievalIdentity, "doc_x")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("invalid call succeeded")
			}
		})
	}
}

// The name becomes a path component inside the caller's box, so anything that could escape
// the documents directory or hide the file is replaced by the digest.
func TestDocumentFileName(t *testing.T) {
	sha := strings.Repeat("c", 64)
	for name, test := range map[string]struct {
		record arcadedb.DocumentCard
		want   string
	}{
		"plain name":       {arcadedb.DocumentCard{FileName: "report.pdf", RawSHA256: sha}, "report.pdf"},
		"strips directory": {arcadedb.DocumentCard{FileName: "a/b/report.pdf", RawSHA256: sha}, "report.pdf"},
		"strips backslash": {arcadedb.DocumentCard{FileName: `a\b\report.pdf`, RawSHA256: sha}, "report.pdf"},
		"traversal":        {arcadedb.DocumentCard{FileName: "../../etc/passwd", RawSHA256: sha}, "passwd"},
		"bare traversal":   {arcadedb.DocumentCard{FileName: "..", RawSHA256: sha}, "document-cccccccccccc"},
		"dotfile":          {arcadedb.DocumentCard{FileName: ".bashrc", RawSHA256: sha}, "document-cccccccccccc"},
		"empty falls back to the key": {
			arcadedb.DocumentCard{SourceKey: "contabilita/listino.xlsx", RawSHA256: sha}, "listino.xlsx",
		},
		"empty everything": {arcadedb.DocumentCard{RawSHA256: sha}, "document-cccccccccccc"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := documentFileName(test.record); got != test.want {
				t.Fatalf("documentFileName = %q, want %q", got, test.want)
			}
		})
	}
}

func TestShortHash(t *testing.T) {
	if got := shortHash(strings.Repeat("d", 64)); got != "dddddddddddd" {
		t.Fatalf("shortHash = %q", got)
	}
	if got := shortHash("  "); got != "unknown" {
		t.Fatalf("shortHash(empty) = %q", got)
	}
	if got := shortHash("abc"); got != "abc" {
		t.Fatalf("shortHash(short) = %q", got)
	}
}
