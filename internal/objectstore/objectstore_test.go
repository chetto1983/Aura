package objectstore

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestAssetKeyContainsNoFilename(t *testing.T) {
	key := AssetKey("identity-1", "asset-2")
	if key != "identity/identity-1/asset/asset-2/original" {
		t.Fatalf("AssetKey() = %q, want identity/identity-1/asset/asset-2/original", key)
	}
	for _, forbidden := range []string{".pdf", ".jpg", ".png", "invoice", "\\"} {
		if strings.Contains(key, forbidden) {
			t.Fatalf("AssetKey() contains filename-like fragment %q in %q", forbidden, key)
		}
	}
}

func TestFakeRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewFake()
	ref := ObjectRef{Bucket: "bucket", Key: AssetKey("id", "asset")}
	original := []byte("hello asset")
	if err := store.Put(ctx, ref, strings.NewReader(string(original)), PutOptions{MIMEType: "text/plain", Size: int64(len(original))}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	original[0] = 'H'

	attrs, err := store.Head(ctx, ref)
	if err != nil {
		t.Fatalf("Head() error = %v", err)
	}
	if attrs.SizeBytes != int64(len("hello asset")) {
		t.Fatalf("Head().SizeBytes = %d, want %d", attrs.SizeBytes, len("hello asset"))
	}
	if attrs.MIMEType != "text/plain" {
		t.Fatalf("Head().MIMEType = %q, want text/plain", attrs.MIMEType)
	}
	if attrs.ETag == "" {
		t.Fatal("Head().ETag is empty")
	}

	body, attrs, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read Get body: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close Get body: %v", err)
	}
	if string(got) != "hello asset" {
		t.Fatalf("Get() body = %q, want hello asset", got)
	}
	got[0] = 'H'
	body, _, err = store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get() after caller mutation error = %v", err)
	}
	again, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read second Get body: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close second Get body: %v", err)
	}
	if string(again) != "hello asset" {
		t.Fatalf("store did not isolate returned bytes, got %q", again)
	}
	if attrs.MIMEType != "text/plain" {
		t.Fatalf("Get() attrs MIMEType = %q, want text/plain", attrs.MIMEType)
	}

	if err := store.Delete(ctx, ref); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Head(ctx, ref); err == nil {
		t.Fatal("Head() after Delete succeeded, want error")
	}
}

func TestFakePresignPut(t *testing.T) {
	store := NewFake()
	expiresIn := 10 * time.Minute
	presigned, err := store.PresignPut(context.Background(), PresignPutRequest{
		Ref:       ObjectRef{Bucket: "bucket", Key: "identity/id/asset/asset/original"},
		MIMEType:  "image/png",
		Size:      42,
		ExpiresIn: expiresIn,
	})
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	if presigned.Method != "PUT" {
		t.Fatalf("Method = %q, want PUT", presigned.Method)
	}
	if presigned.RequiredHeaders["Content-Type"] != "image/png" {
		t.Fatalf("Content-Type header = %q, want image/png", presigned.RequiredHeaders["Content-Type"])
	}
	if presigned.RequiredHeaders["Content-Length"] != "42" {
		t.Fatalf("Content-Length header = %q, want 42", presigned.RequiredHeaders["Content-Length"])
	}
	if time.Until(presigned.ExpiresAt) <= 0 || time.Until(presigned.ExpiresAt) > expiresIn {
		t.Fatalf("ExpiresAt = %s, want within %s", presigned.ExpiresAt, expiresIn)
	}
}

func TestFilesystemPresignPutRequiresUploadHeaders(t *testing.T) {
	store := NewFilesystem(t.TempDir())
	presigned, err := store.PresignPut(context.Background(), PresignPutRequest{
		Ref:      ObjectRef{Bucket: "bucket", Key: "identity/id/asset/asset/original"},
		MIMEType: "image/png",
		Size:     42,
	})
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	if presigned.RequiredHeaders["Content-Type"] != "image/png" {
		t.Fatalf("Content-Type header = %q, want image/png", presigned.RequiredHeaders["Content-Type"])
	}
	if presigned.RequiredHeaders["Content-Length"] != "42" {
		t.Fatalf("Content-Length header = %q, want 42", presigned.RequiredHeaders["Content-Length"])
	}
}

func TestS3PresignPutRequiresSignedUploadHeaders(t *testing.T) {
	store, err := NewS3(context.Background(), S3Config{
		Endpoint:  "http://127.0.0.1:3900",
		Region:    "garage",
		AccessKey: "test",
		SecretKey: "test",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	presigned, err := store.PresignPut(context.Background(), PresignPutRequest{
		Ref:      ObjectRef{Bucket: "bucket", Key: "identity/id/asset/asset/original"},
		MIMEType: "image/png",
		Size:     42,
	})
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	if presigned.RequiredHeaders["Content-Type"] != "image/png" {
		t.Fatalf("Content-Type header = %q, want image/png", presigned.RequiredHeaders["Content-Type"])
	}
	if presigned.RequiredHeaders["Content-Length"] != "42" {
		t.Fatalf("Content-Length header = %q, want 42", presigned.RequiredHeaders["Content-Length"])
	}
}

func TestFilesystemRoundTripAndRejectsUnsafeKeys(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystem(t.TempDir())
	ref := ObjectRef{Bucket: "bucket", Key: AssetKey("id", "asset")}

	if err := store.Put(ctx, ref, strings.NewReader("file asset"), PutOptions{MIMEType: "text/plain", Size: int64(len("file asset"))}); err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	body, attrs, err := store.Get(ctx, ref)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	got, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := body.Close(); err != nil {
		t.Fatalf("close body: %v", err)
	}
	if string(got) != "file asset" {
		t.Fatalf("Get() body = %q, want file asset", got)
	}
	if attrs.SizeBytes != int64(len("file asset")) || attrs.MIMEType != "text/plain" || attrs.ETag == "" {
		t.Fatalf("Get() attrs = %#v, want size/mime/etag", attrs)
	}

	unsafeKeys := []string{
		"../escape",
		"identity/id/../escape",
		"identity/id/asset/a..b/original",
		`identity\id\asset`,
		`C:\escape`,
		"/absolute/escape",
	}
	for _, key := range unsafeKeys {
		ref := ObjectRef{Bucket: "bucket", Key: key}
		if err := store.Put(ctx, ref, strings.NewReader("x"), PutOptions{MIMEType: "text/plain", Size: 1}); err == nil {
			t.Fatalf("Put(%q) succeeded, want unsafe key error", key)
		}
		if _, err := store.Head(ctx, ref); err == nil {
			t.Fatalf("Head(%q) succeeded, want unsafe key error", key)
		}
		if _, err := store.PresignPut(ctx, PresignPutRequest{Ref: ref, MIMEType: "text/plain", Size: 1}); err == nil {
			t.Fatalf("PresignPut(%q) succeeded, want unsafe key error", key)
		}
	}
}
