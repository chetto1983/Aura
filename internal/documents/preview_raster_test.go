package documents

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"strings"
	"testing"
)

func TestCraftPreviewScreensRasterDimensionsBeforeNativeOpen(t *testing.T) {
	valid := previewPixels(t, 1, 1)
	bomb := bytes.Clone(valid)
	binary.BigEndian.PutUint32(bomb[16:20], 100000)
	binary.BigEndian.PutUint32(bomb[20:24], 100000)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	b := newPreviewBox(t)
	_, err := NewCraftPreviews(b).Preview(t.Context(), "owner", "bomb.png", previewSource(string(bomb)))
	if !errors.Is(err, ErrPreviewTooLarge) {
		t.Fatalf("dimension bomb=%v", err)
	}
	for _, command := range b.commands {
		if strings.Contains(command, "-cli") {
			t.Fatalf("bomb reached native decoder: %s", command)
		}
	}
	if len(b.files) != 0 {
		t.Fatalf("bomb materialized: %v", b.files)
	}
}

func TestCraftPreviewReplaysRasterHeaderForNativeSource(t *testing.T) {
	var jpegBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	img.Set(2, 3, color.RGBA{R: 255, A: 255})
	if err := jpeg.Encode(&jpegBytes, img, nil); err != nil {
		t.Fatal(err)
	}
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	for _, source := range [][]byte{jpegBytes.Bytes(), pngBytes.Bytes()} {
		b := newPreviewBox(t)
		_, err := NewCraftPreviews(b).Preview(t.Context(), "owner", "photo.jpg", func(context.Context) (io.ReadCloser, int64, error) {
			return io.NopCloser(bytes.NewReader(source)), int64(len(source)), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for name, body := range b.files {
			if strings.Contains(name, "source.") && !bytes.Equal(body, source) {
				t.Fatal("header screening lost source bytes")
			}
		}
	}
}
