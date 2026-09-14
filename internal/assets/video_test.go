package assets

import (
	"errors"
	"testing"
)

// TestVideoModalityAndLimits is the boundary test from the task brief, verbatim: MP4/WebM
// infer as ModalityVideo regardless of a missing or generic MIME type, the byte ceiling is
// exact (one byte over refuses), and an unsupported video extension is refused even though
// its size is trivially within the cap.
func TestVideoModalityAndLimits(t *testing.T) {
	limits := Limits{MaxVideoBytes: 50 << 20}
	for _, sample := range []struct{ name, mime string }{
		{"clip.mp4", "video/mp4"}, {"clip.webm", "video/webm"},
		{"clip.MP4", ""}, {"clip.webm", "application/octet-stream"},
	} {
		if got := InferModality(sample.name, sample.mime); got != ModalityVideo {
			t.Fatalf("%s: modality %q", sample.name, got)
		}
	}
	if err := limits.Validate(ModalityVideo, "clip.mp4", 50<<20); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(limits.Validate(ModalityVideo, "clip.mp4", (50<<20)+1), ErrAssetTooLarge) {
		t.Fatal("one excess byte must be refused")
	}
	if !errors.Is(limits.Validate(ModalityVideo, "clip.avi", 1), ErrAssetUnsupported) {
		t.Fatal("unsupported extension accepted")
	}
}

// TestInferModalityVideoIsExtensionGatedNotMIMEPrefixGated pins the deliberate asymmetry
// with image/audio: those two infer from ANY "image/"/"audio/" MIME prefix, but video does
// not infer from "video/*" at all -- only the two extensions the pipeline can actually play
// back. video/quicktime and video/x-matroska are real, common MIME types and neither is
// MP4/WebM, so a generic "video/" prefix branch would silently accept containers nothing
// downstream can serve.
func TestInferModalityVideoIsExtensionGatedNotMIMEPrefixGated(t *testing.T) {
	for _, tc := range []struct {
		name, fileName, mimeType string
		want                     Modality
	}{
		{"quicktime container is not MP4/WebM", "clip.mov", "video/quicktime", ModalityUnknown},
		{"matroska container is not MP4/WebM", "clip.mkv", "video/x-matroska", ModalityUnknown},
		{"generic video MIME with no matching extension", "clip.bin", "video/mp4", ModalityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InferModality(tc.fileName, tc.mimeType); got != tc.want {
				t.Fatalf("InferModality(%q, %q) = %v, want %v", tc.fileName, tc.mimeType, got, tc.want)
			}
		})
	}
}

// TestInferModalityMIMEExtensionMismatch documents which signal wins when the extension and
// the declared MIME type disagree: image/audio are MIME-prefix branches checked BEFORE the
// video extension branch, so a mislabeled image/audio MIME on a .mp4/.webm name still wins.
// Only when neither the document nor the image/audio MIME branches match does the video
// extension get to decide -- exactly the precedence InferModality's switch already encodes,
// pinned here so a later reordering trips a test instead of silently reclassifying uploads.
func TestInferModalityMIMEExtensionMismatch(t *testing.T) {
	for _, tc := range []struct {
		name, fileName, mimeType string
		want                     Modality
	}{
		{"image MIME beats a video extension", "clip.mp4", "image/png", ModalityImage},
		{"audio MIME beats a video extension", "clip.webm", "audio/mpeg", ModalityAudio},
		{"video extension wins once no other branch claims it", "clip.mp4", "application/octet-stream", ModalityVideo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InferModality(tc.fileName, tc.mimeType); got != tc.want {
				t.Fatalf("InferModality(%q, %q) = %v, want %v", tc.fileName, tc.mimeType, got, tc.want)
			}
		})
	}
}

// TestValidateRefusesANegativeVideoSize mirrors TestValidateRefusesANegativeSizeForEveryModality
// (limits_test.go) for the new modality: a negative size is refused before the per-modality
// rule runs, so it is caught regardless of the ceiling's value.
func TestValidateRefusesANegativeVideoSize(t *testing.T) {
	limits := Limits{MaxVideoBytes: 50 << 20}
	if err := limits.Validate(ModalityVideo, "clip.mp4", -1); err == nil {
		t.Fatal("Validate(video, -1) accepted a negative size")
	}
}
