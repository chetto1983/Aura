package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

type openerStub struct {
	asset Asset
	body  []byte
}

func (o openerStub) OpenForIdentity(context.Context, string, string) (io.ReadCloser, Asset, error) {
	return io.NopCloser(bytes.NewReader(o.body)), o.asset, nil
}

func TestNativeMedia(t *testing.T) {
	t.Parallel()
	for modality, want := range map[Modality]bool{
		ModalityImage: true, ModalityVideo: true,
		ModalityAudio: false, ModalityDocument: false, ModalityUnknown: false,
	} {
		if got := NativeMedia(modality); got != want {
			t.Errorf("NativeMedia(%q) = %v, want %v", modality, got, want)
		}
	}
}

func TestTurnMediaLoaderLoadsVideo(t *testing.T) {
	t.Parallel()
	body := []byte("mp4-bytes")
	digest := sha256.Sum256(body)
	loader := TurnMediaLoader{
		Opener: openerStub{body: body, asset: Asset{
			ID: "v1", ThreadID: "t1", MIMEType: "video/mp4", Modality: ModalityVideo,
			SizeBytes: int64(len(body)), ContentHash: hex.EncodeToString(digest[:]),
		}},
		ThreadID: "t1",
		Allowed:  map[string]bool{"v1": true},
	}
	part, err := loader.LoadContentPart(context.Background(), "", "owner", "v1")
	if err != nil {
		t.Fatalf("LoadContentPart: %v", err)
	}
	if part.MIMEType != "video/mp4" || !bytes.Equal(part.Bytes, body) {
		t.Fatalf("part = %+v", part)
	}
}

func TestTurnMediaLoaderStillRefusesAudio(t *testing.T) {
	t.Parallel()
	body := []byte("ogg")
	digest := sha256.Sum256(body)
	loader := TurnMediaLoader{
		Opener: openerStub{body: body, asset: Asset{
			ID: "a1", ThreadID: "t1", MIMEType: "audio/ogg", Modality: ModalityAudio,
			SizeBytes: int64(len(body)), ContentHash: hex.EncodeToString(digest[:]),
		}},
		ThreadID: "t1",
		Allowed:  map[string]bool{"a1": true},
	}
	_, err := loader.LoadContentPart(context.Background(), "", "owner", "a1")
	if err == nil || !strings.Contains(err.Error(), "not native media") {
		t.Fatalf("err = %v, want the modality refusal", err)
	}
}
