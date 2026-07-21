package objectstore

import (
	"context"
	"io"
	"slices"
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
	putAttrs, err := store.Put(ctx, ref, strings.NewReader(string(original)), PutOptions{MIMEType: "text/plain", Size: int64(len(original))})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if putAttrs.SizeBytes != int64(len("hello asset")) || putAttrs.MIMEType != "text/plain" || putAttrs.ETag == "" {
		t.Fatalf("Put() attrs = %#v, want size/mime/etag", putAttrs)
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

func TestFakeListsObjectsByBucketAndPrefix(t *testing.T) {
	ctx := context.Background()
	store := NewFake()
	refs := []ObjectRef{
		{Bucket: "bucket", Key: "identity/a/asset/1/original"},
		{Bucket: "bucket", Key: "identity/a/asset/2/original"},
		{Bucket: "bucket", Key: "identity/b/asset/3/original"},
		{Bucket: "other", Key: "identity/a/asset/4/original"},
	}
	for _, ref := range refs {
		if _, err := store.Put(ctx, ref, strings.NewReader(ref.Key), PutOptions{MIMEType: "text/plain", Size: int64(len(ref.Key))}); err != nil {
			t.Fatalf("Put(%#v): %v", ref, err)
		}
	}

	items, err := store.List(ctx, ListRequest{Bucket: "bucket", Prefix: "identity/a/"})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Ref.Key)
		if item.Attrs.SizeBytes == 0 || item.Attrs.ETag == "" {
			t.Fatalf("listed item missing attrs: %#v", item)
		}
	}
	want := []string{"identity/a/asset/1/original", "identity/a/asset/2/original"}
	if !slices.Equal(got, want) {
		t.Fatalf("List keys = %#v, want %#v", got, want)
	}
}

func TestFakeListLimitIsOrderedAndBounded(t *testing.T) {
	ctx := context.Background()
	store := NewFake()
	for _, key := range []string{"records/c", "records/a", "records/b"} {
		ref := ObjectRef{Bucket: "bucket", Key: key}
		if _, err := store.Put(ctx, ref, strings.NewReader(key), PutOptions{Size: int64(len(key))}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := store.List(ctx, ListRequest{Bucket: "bucket", Prefix: "records/", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{items[0].Ref.Key, items[1].Ref.Key}
	if want := []string{"records/a", "records/b"}; !slices.Equal(got, want) {
		t.Fatalf("bounded list = %v, want %v", got, want)
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
	if _, ok := presigned.RequiredHeaders["Content-Length"]; ok {
		t.Fatalf("Content-Length header must not be application-settable: %#v", presigned.RequiredHeaders)
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
	if _, ok := presigned.RequiredHeaders["Content-Length"]; ok {
		t.Fatalf("Content-Length header must not be application-settable: %#v", presigned.RequiredHeaders)
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
	if _, ok := presigned.RequiredHeaders["Content-Length"]; ok {
		t.Fatalf("Content-Length header must not be application-settable: %#v", presigned.RequiredHeaders)
	}
	if strings.Contains(strings.ToLower(presigned.URL), "content-length") {
		t.Fatalf("presigned URL must not sign browser-forbidden Content-Length: %s", presigned.URL)
	}
}

func TestS3PresignPutSignsPublicEndpointHost(t *testing.T) {
	store, err := NewS3(context.Background(), S3Config{
		Endpoint:       "http://garage:3900",
		PublicEndpoint: "http://127.0.0.1:3900",
		Region:         "garage",
		AccessKey:      "test",
		SecretKey:      "test",
		PathStyle:      true,
	})
	if err != nil {
		t.Fatalf("NewS3() error = %v", err)
	}
	if store.presignEndpoint != "http://127.0.0.1:3900" {
		t.Fatalf("presignEndpoint = %q, want public endpoint", store.presignEndpoint)
	}
	presigned, err := store.PresignPut(context.Background(), PresignPutRequest{
		Ref:      ObjectRef{Bucket: "bucket", Key: "identity/id/asset/asset/original"},
		MIMEType: "image/png",
		Size:     42,
	})
	if err != nil {
		t.Fatalf("PresignPut() error = %v", err)
	}
	if !strings.HasPrefix(presigned.URL, "http://127.0.0.1:3900/") {
		t.Fatalf("presigned URL = %s, want public endpoint host", presigned.URL)
	}
}

func TestBrowserUploadCORSConfigurationAllowsPresignedBrowserPut(t *testing.T) {
	cfg := browserUploadCORSConfiguration()
	if cfg == nil || len(cfg.CORSRules) != 1 {
		t.Fatalf("CORSRules = %#v, want exactly one rule", cfg)
	}
	rule := cfg.CORSRules[0]
	for _, method := range []string{"PUT", "GET", "HEAD"} {
		if !slices.Contains(rule.AllowedMethods, method) {
			t.Fatalf("AllowedMethods = %#v, missing %s", rule.AllowedMethods, method)
		}
	}
	if !slices.Contains(rule.AllowedOrigins, "*") {
		t.Fatalf("AllowedOrigins = %#v, want wildcard presigned-url origin", rule.AllowedOrigins)
	}
	if !slices.Contains(rule.AllowedHeaders, "*") {
		t.Fatalf("AllowedHeaders = %#v, want wildcard request headers", rule.AllowedHeaders)
	}
	if !slices.Contains(rule.ExposeHeaders, "ETag") {
		t.Fatalf("ExposeHeaders = %#v, want ETag", rule.ExposeHeaders)
	}
	if rule.MaxAgeSeconds == nil || *rule.MaxAgeSeconds <= 0 {
		t.Fatalf("MaxAgeSeconds = %#v, want positive preflight cache", rule.MaxAgeSeconds)
	}
}

func TestFilesystemRoundTripAndRejectsUnsafeKeys(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystem(t.TempDir())
	ref := ObjectRef{Bucket: "bucket", Key: AssetKey("id", "asset")}

	putAttrs, err := store.Put(ctx, ref, strings.NewReader("file asset"), PutOptions{MIMEType: "text/plain", Size: int64(len("file asset"))})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if putAttrs.SizeBytes != int64(len("file asset")) || putAttrs.MIMEType != "text/plain" || putAttrs.ETag == "" {
		t.Fatalf("Put() attrs = %#v, want size/mime/etag", putAttrs)
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
		if _, err := store.Put(ctx, ref, strings.NewReader("x"), PutOptions{MIMEType: "text/plain", Size: 1}); err == nil {
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

func TestFilesystemListsObjectsByBucketAndPrefix(t *testing.T) {
	ctx := context.Background()
	store := NewFilesystem(t.TempDir())
	refs := []ObjectRef{
		{Bucket: "bucket", Key: "identity/a/asset/1/original"},
		{Bucket: "bucket", Key: "identity/a/asset/2/original"},
		{Bucket: "bucket", Key: "identity/b/asset/3/original"},
	}
	for _, ref := range refs {
		if _, err := store.Put(ctx, ref, strings.NewReader(ref.Key), PutOptions{MIMEType: "text/plain", Size: int64(len(ref.Key))}); err != nil {
			t.Fatalf("Put(%#v): %v", ref, err)
		}
	}

	items, err := store.List(ctx, ListRequest{Bucket: "bucket", Prefix: "identity/a/"})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(items))
	for _, item := range items {
		got = append(got, item.Ref.Key)
		if strings.HasSuffix(item.Ref.Key, ".meta.json") {
			t.Fatalf("List returned metadata sidecar object: %#v", item)
		}
	}
	want := []string{"identity/a/asset/1/original", "identity/a/asset/2/original"}
	if !slices.Equal(got, want) {
		t.Fatalf("List keys = %#v, want %#v", got, want)
	}
}
