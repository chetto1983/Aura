package mediagen

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type fakeAsset struct {
	data     []byte
	meta     ReferenceMeta
	err      error
	closed   bool
	streamed []byte // overrides data for the reader if set, to simulate a lying declared size
}

type fakeReferenceReader struct {
	assets map[string]*fakeAsset
	opened []string
}

func (f *fakeReferenceReader) Open(ctx context.Context, identityID, assetID string) (io.ReadCloser, ReferenceMeta, error) {
	f.opened = append(f.opened, identityID+"/"+assetID)
	asset, found := f.assets[identityID+"/"+assetID]
	if !found {
		return nil, ReferenceMeta{}, errors.New("not found")
	}
	if asset.err != nil {
		return nil, ReferenceMeta{}, asset.err
	}
	data := asset.data
	if asset.streamed != nil {
		data = asset.streamed
	}
	return &closeTrackingReader{Reader: strings.NewReader(string(data)), asset: asset}, asset.meta, nil
}

type closeTrackingReader struct {
	io.Reader
	asset *fakeAsset
}

func (c *closeTrackingReader) Close() error {
	c.asset.closed = true
	return nil
}

func TestLoadReferencesProducesDataURLs(t *testing.T) {
	png := tinyPNG(t)
	last := tinyJPEG(t)
	reader := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/a1": {data: png, meta: ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: int64(len(png))}},
		"owner/a2": {data: last, meta: ReferenceMeta{MIMEType: "image/jpeg", Modality: "image", SizeBytes: int64(len(last))}},
	}}
	refs, err := LoadReferences(context.Background(), reader, "owner", []string{"a1", "a2"}, 1<<20)
	if err != nil {
		t.Fatalf("LoadReferences: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("refs = %d, want 2", len(refs))
	}
	want0 := "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	if refs[0].Type != "image_url" || refs[0].ImageURL.URL != want0 {
		t.Fatalf("refs[0] = %#v", refs[0])
	}
	want1 := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(last)
	if refs[1].ImageURL.URL != want1 {
		t.Fatalf("refs[1] = %#v", refs[1])
	}
	if !reader.assets["owner/a1"].closed || !reader.assets["owner/a2"].closed {
		t.Fatal("every reference reader must be closed")
	}
	if !reflectOrderPreserved(reader.opened, []string{"owner/a1", "owner/a2"}) {
		t.Fatalf("opened order = %v", reader.opened)
	}
}

func reflectOrderPreserved(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLoadReferencesEmptyIDsReturnsNil(t *testing.T) {
	refs, err := LoadReferences(context.Background(), &fakeReferenceReader{}, "owner", nil, 1<<20)
	if err != nil || refs != nil {
		t.Fatalf("LoadReferences(nil ids) = %v, %v", refs, err)
	}
}

func TestLoadReferencesNilReaderIsNotAPanic(t *testing.T) {
	_, err := LoadReferences(context.Background(), nil, "owner", []string{"a1"}, 1<<20)
	if err == nil {
		t.Fatal("want an error, not a panic, for a nil reader")
	}
	if ErrorCode(err) == "asset_not_found" {
		t.Fatal("a nil reader is a wiring bug, not a user-facing asset_not_found")
	}
}

func TestLoadReferencesForeignDeletedAndMissingAllMapToAssetNotFound(t *testing.T) {
	reader := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/deleted": {err: errors.New("asset was deleted")},
		"owner/foreign": {err: errors.New("asset belongs to another identity")},
	}}
	for _, id := range []string{"missing", "deleted", "foreign"} {
		_, err := LoadReferences(context.Background(), reader, "owner", []string{id}, 1<<20)
		if ErrorCode(err) != "asset_not_found" {
			t.Fatalf("id %q: ErrorCode = %q, want asset_not_found", id, ErrorCode(err))
		}
	}
}

func TestLoadReferencesVideoModalityIsUnsupported(t *testing.T) {
	reader := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/vid": {data: []byte("fake mp4"), meta: ReferenceMeta{MIMEType: "video/mp4", Modality: "video", SizeBytes: 8}},
	}}
	_, err := LoadReferences(context.Background(), reader, "owner", []string{"vid"}, 1<<20)
	if ErrorCode(err) != "unsupported" {
		t.Fatalf("ErrorCode = %q, want unsupported for a video passed as an image reference", ErrorCode(err))
	}
}

func TestLoadReferencesNonImageMIMEIsUnsupported(t *testing.T) {
	reader := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/doc": {data: []byte("pdf bytes"), meta: ReferenceMeta{MIMEType: "application/pdf", Modality: "image", SizeBytes: 9}},
	}}
	_, err := LoadReferences(context.Background(), reader, "owner", []string{"doc"}, 1<<20)
	if ErrorCode(err) != "unsupported" {
		t.Fatalf("ErrorCode = %q, want unsupported for a non-image MIME type", ErrorCode(err))
	}
}

func TestLoadReferencesRejectsDeclaredSizeOverLimit(t *testing.T) {
	reader := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/big": {data: []byte("0123456789"), meta: ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: 1 << 30}},
	}}
	_, err := LoadReferences(context.Background(), reader, "owner", []string{"big"}, 1<<20)
	if ErrorCode(err) != "too_large" {
		t.Fatalf("ErrorCode = %q, want too_large from the declared size alone", ErrorCode(err))
	}
}

func TestLoadReferencesRejectsAStreamThatLiesAboutItsSize(t *testing.T) {
	reader := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/lying": {
			meta:     ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: 4},
			streamed: []byte("far more bytes than the declared size promised"),
		},
	}}
	_, err := LoadReferences(context.Background(), reader, "owner", []string{"lying"}, 10)
	if ErrorCode(err) != "too_large" {
		t.Fatalf("ErrorCode = %q, want too_large when the stream exceeds the limit despite a small declared size", ErrorCode(err))
	}
}

func TestLoadReferencesStopsAtTheFirstFailureAndClosesWhatItOpened(t *testing.T) {
	png := tinyPNG(t)
	reader := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/ok": {data: png, meta: ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: int64(len(png))}},
	}}
	_, err := LoadReferences(context.Background(), reader, "owner", []string{"ok", "missing"}, 1<<20)
	if ErrorCode(err) != "asset_not_found" {
		t.Fatalf("ErrorCode = %q, want asset_not_found", ErrorCode(err))
	}
	if !reader.assets["owner/ok"].closed {
		t.Fatal("a reference opened before the failure must still be closed")
	}
}

type cancelledOnOpenReader struct{}

func (cancelledOnOpenReader) Open(ctx context.Context, identityID, assetID string) (io.ReadCloser, ReferenceMeta, error) {
	<-ctx.Done()
	return nil, ReferenceMeta{}, ctx.Err()
}

func TestLoadReferencesPropagatesContextCancellationDistinctFromNotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	_, err := LoadReferences(ctx, cancelledOnOpenReader{}, "owner", []string{"a1"}, 1<<20)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded surfaced as-is, not asset_not_found", err)
	}
	if ErrorCode(err) == "asset_not_found" {
		t.Fatal("cancellation must not be mislabeled as asset_not_found")
	}
}
