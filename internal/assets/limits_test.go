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
