package assets

import (
	"context"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/multimodal"
	"github.com/chetto1983/aura/internal/objectstore"
)

func TestAudioProcessorReadsObjectAndCallsSTTSidecar(t *testing.T) {
	objects := objectstore.NewFake()
	ref := objectstore.ObjectRef{Bucket: "b", Key: "voice/original"}
	ogg := []byte("OggS\x00fake-opus")
	if _, err := objects.Put(context.Background(), ref, strings.NewReader(string(ogg)), objectstore.PutOptions{MIMEType: "audio/ogg", Size: int64(len(ogg))}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	var gotFile, gotModel, gotLanguage string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("STT method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("STT path = %s, want .../audio/transcriptions", r.URL.Path)
		}
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
			t.Fatalf("STT content type = %q, want multipart/*", r.Header.Get("Content-Type"))
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("STT multipart read: %v", err)
			}
			body, _ := io.ReadAll(part)
			switch part.FormName() {
			case "file":
				gotFile = string(body)
			case "model":
				gotModel = string(body)
			case "language":
				gotLanguage = string(body)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ciao Aura"}`)
	}))
	defer srv.Close()

	result, err := NewAudioProcessor(objects, multimodal.STTConfig{
		LocalBaseURL: srv.URL,
		LocalModel:   "large-v3-turbo",
		Language:     "it",
	}).ProcessAsset(context.Background(), Asset{
		ID:           "asset-1",
		Modality:     ModalityAudio,
		ObjectBucket: ref.Bucket,
		ObjectKey:    ref.Key,
		FileName:     "voice.ogg",
		MIMEType:     "audio/ogg",
	})
	if err != nil {
		t.Fatalf("ProcessAsset: %v", err)
	}

	if result.Status != StatusComplete || result.Summary != "ciao Aura" {
		t.Fatalf("result = %+v, want complete transcript summary", result)
	}
	if result.Metadata["transcript"] != "ciao Aura" {
		t.Fatalf("metadata = %#v, want transcript", result.Metadata)
	}
	if gotFile != string(ogg) {
		t.Fatalf("STT file field = %q, want object bytes", gotFile)
	}
	if gotModel != "large-v3-turbo" || gotLanguage != "it" {
		t.Fatalf("STT fields model/language = %q/%q, want configured values", gotModel, gotLanguage)
	}
}

// TestAudioFormat locks the exported container→STT-format map (37C-02): the
// ";codecs=" media-type parameter a browser MediaRecorder emits is stripped
// before the switch, so "audio/webm;codecs=opus" maps to "webm" (not the "ogg"
// default) and "audio/mp4;codecs=mp4a.40.2" to "m4a"; bare MIME types keep their
// prior mapping and anything unrecognised (incl. empty) stays "ogg".
func TestAudioFormat(t *testing.T) {
	tests := []struct {
		name     string
		mimeType string
		want     string
	}{
		{"webm with codecs param stripped", "audio/webm;codecs=opus", "webm"},
		{"mp4 with codecs param stripped", "audio/mp4;codecs=mp4a.40.2", "m4a"},
		{"whitespace around the semicolon", "audio/webm ; codecs=opus", "webm"},
		{"bare webm", "audio/webm", "webm"},
		{"mpeg maps to mp3", "audio/mpeg", "mp3"},
		{"legacy mp3 alias", "audio/mp3", "mp3"},
		{"wav", "audio/wav", "wav"},
		{"x-wav alias", "audio/x-wav", "wav"},
		{"flac", "audio/flac", "flac"},
		{"bare mp4", "audio/mp4", "m4a"},
		{"m4a alias", "audio/m4a", "m4a"},
		{"x-m4a alias", "audio/x-m4a", "m4a"},
		{"empty defaults to ogg", "", "ogg"},
		{"unknown container defaults to ogg", "application/octet-stream", "ogg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AudioFormat(tt.mimeType); got != tt.want {
				t.Errorf("AudioFormat(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}
