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

	result, err := NewAudioProcessor(objects, STTConfig{
		BaseURL:  srv.URL,
		Model:    "large-v3-turbo",
		Language: "it",
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
