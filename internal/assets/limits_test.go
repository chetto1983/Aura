package assets

import (
	"strings"
	"testing"
)

func TestLimitsInferAndValidate(t *testing.T) {
	limits := Limits{
		MaxDocumentBytes: 100,
		MaxImageBytes:    10,
		MaxAudioBytes:    20,
	}

	if got := InferModality("manual.pdf", ""); got != ModalityDocument {
		t.Fatalf("InferModality(pdf) = %q, want document", got)
	}
	if err := limits.Validate(ModalityDocument, "manual.pdf", 100); err != nil {
		t.Fatalf("Validate(pdf) error = %v", err)
	}

	if err := limits.Validate(InferModality("setup.exe", "application/octet-stream"), "setup.exe", 1); err == nil {
		t.Fatal("Validate(.exe) succeeded, want unsupported modality error")
	}

	if got := InferModality("photo.bin", "image/png"); got != ModalityImage {
		t.Fatalf("InferModality(image mime) = %q, want image", got)
	}
	if err := limits.Validate(ModalityImage, "photo.png", 11); err == nil || !strings.Contains(err.Error(), "image exceeds 10 bytes") {
		t.Fatalf("Validate(image over limit) error = %v, want image limit", err)
	}

	if got := InferModality("voice.mp3", ""); got != ModalityAudio {
		t.Fatalf("InferModality(audio ext) = %q, want audio", got)
	}
	if err := limits.Validate(ModalityAudio, "voice.mp3", 21); err == nil || !strings.Contains(err.Error(), "audio exceeds 20 bytes") {
		t.Fatalf("Validate(audio over limit) error = %v, want audio limit", err)
	}

	if err := limits.Validate(ModalityUnknown, "blob.bin", 1); err == nil {
		t.Fatal("Validate(unknown) succeeded, want unsupported modality error")
	}
}

// The modality fallback exists for the files that arrive with a useless MIME type --
// application/octet-stream from a browser upload, an empty string from a scheduled
// delivery -- where the extension is the only signal left. Without it those become
// ModalityUnknown and are refused, which is a rejection produced by the sender's
// Content-Type header rather than by anything about the file.
func TestInferModalityFallsBackToTheExtension(t *testing.T) {
	for _, tc := range []struct {
		name, fileName, mimeType string
		want                     Modality
	}{
		{"image without a usable mime type", "photo.png", "application/octet-stream", ModalityImage},
		{"audio without a usable mime type", "memo.mp3", "", ModalityAudio},
		{"neither the mime type nor the extension says anything", "payload.bin", "", ModalityUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := InferModality(tc.fileName, tc.mimeType); got != tc.want {
				t.Fatalf("InferModality(%q, %q) = %v, want %v", tc.fileName, tc.mimeType, got, tc.want)
			}
		})
	}
}

// A negative size is refused before any per-modality rule runs, because each of those
// compares against a maximum and a negative number passes all of them.
func TestValidateRefusesANegativeSizeForEveryModality(t *testing.T) {
	limits := Limits{MaxDocumentBytes: 100, MaxImageBytes: 10, MaxAudioBytes: 20}
	for _, modality := range []Modality{ModalityDocument, ModalityImage, ModalityAudio, ModalityUnknown} {
		if err := limits.Validate(modality, "note.pdf", -1); err == nil {
			t.Fatalf("Validate(%v, -1) accepted a negative size", modality)
		}
	}
}
