package main

import (
	"context"
	"io"
	"net/url"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/objectstore"
)

// The file manager's direct route and document_open read through the same accessor; with no
// resolver (the pre-provisioning deployment) the shared store and bucket are the only ones.

func seededOpener(t *testing.T) identityObjectOpener {
	t.Helper()
	store := objectstore.NewFake()
	ref := objectstore.ObjectRef{Bucket: "shared", Key: "chat/report.pdf"}
	_, err := store.Put(context.Background(), ref, strings.NewReader("%PDF-1.4 body"), objectstore.PutOptions{
		MIMEType: "application/pdf",
		Metadata: map[string]string{objectstore.MetadataFileName: url.PathEscape("Relazione annuale.pdf")},
	})
	if err != nil {
		t.Fatalf("seed Put: %v", err)
	}
	return identityObjectOpener{objects: store, bucket: "shared"}
}

func TestOpenSeekableCarriesTheStoredAttrsAndServesFromAnOffset(t *testing.T) {
	opener := seededOpener(t)
	object, attrs, err := opener.OpenSeekable(context.Background(), "identity-1", "chat/report.pdf")
	if err != nil {
		t.Fatalf("OpenSeekable: %v", err)
	}
	defer func() { _ = object.Close() }()
	if attrs.MIMEType != "application/pdf" || attrs.SizeBytes != int64(len("%PDF-1.4 body")) {
		t.Fatalf("attrs = %+v", attrs)
	}
	// The name the object carries, decoded: what the route puts in Content-Disposition.
	if attrs.FileName != "Relazione annuale.pdf" {
		t.Fatalf("FileName = %q", attrs.FileName)
	}
	if _, err := object.Seek(9, io.SeekStart); err != nil {
		t.Fatalf("Seek: %v", err)
	}
	tail, err := io.ReadAll(object)
	if err != nil || string(tail) != "body" {
		t.Fatalf("read from offset 9 = %q, %v", tail, err)
	}
}

func TestOpenObjectAndOpenSeekableRefuseWhatTheyCannotResolve(t *testing.T) {
	ctx := context.Background()
	opener := seededOpener(t)
	if _, _, err := opener.OpenSeekable(ctx, "identity-1", "chat/missing.pdf"); err == nil {
		t.Fatal("OpenSeekable of a missing key succeeded")
	}
	if _, _, err := opener.OpenSeekable(ctx, "identity-1", "  "); err == nil {
		t.Fatal("OpenSeekable of an empty key succeeded")
	}
	if _, _, err := (identityObjectOpener{}).OpenSeekable(ctx, "identity-1", "chat/report.pdf"); err == nil {
		t.Fatal("OpenSeekable without a store succeeded")
	}
	body, mimeType, err := opener.OpenObject(ctx, "identity-1", "chat/report.pdf")
	if err != nil || mimeType != "application/pdf" {
		t.Fatalf("OpenObject = %q, %v", mimeType, err)
	}
	_ = body.Close()
	if _, _, err := opener.OpenObject(ctx, "identity-1", "chat/missing.pdf"); err == nil {
		t.Fatal("OpenObject of a missing key succeeded")
	}
}
