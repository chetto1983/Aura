package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/multimodal"
)

func TestDescribeReadsWithTheHouseKey(t *testing.T) {
	server := newMediaServer(t, catalogJSON(true, false), textReply("A receipt for 12 euro"))
	text, err := server.client.Describe(t.Context(), pngBytes, "image/png", "Describe the image.")
	if err != nil || text != "A receipt for 12 euro" {
		t.Fatalf("Describe = %q, %v", text, err)
	}
	if got := server.auth.Load(); got != "Bearer "+houseKey {
		t.Fatalf("catalogue read with %q", got)
	}
	sent := <-server.sent
	if got := sent.header.Get("Authorization"); got != "Bearer "+houseKey {
		t.Fatalf("message sent with %q", got)
	}
	if sent.body["model"] != "claude-sonnet-5-5" {
		t.Fatalf("model %v", sent.body["model"])
	}
	blocks := blocksOf(t, messagesOf(t, sent.body)[0])
	if len(blocks) != 2 || blocks[0]["text"] != "Describe the image." || blocks[1]["type"] != "image" {
		t.Fatalf("describe message %#v", blocks)
	}
	if got := source(t, blocks[1]); got["media_type"] != "image/png" || got["data"] != base64Of(pngBytes) {
		t.Fatalf("image source %#v", got)
	}
}

func TestDescribeFallsBackWhenTheModelReadsNoImages(t *testing.T) {
	for name, tc := range map[string]struct {
		catalog, mimeType string
	}{
		"model reads no images": {catalogJSON(false, true), "image/png"},
		"type it cannot read":   {catalogJSON(true, true), "image/svg+xml"},
		"not an image":          {catalogJSON(true, true), "application/pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			server := newMediaServer(t, tc.catalog, textReply("never"))
			if _, err := server.client.Describe(t.Context(), pngBytes, tc.mimeType, "describe"); !errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
				t.Fatalf("Describe = %v, want ErrMandatoryModalityUnavailable", err)
			}
			if len(server.sent) != 0 {
				t.Fatal("an image the model cannot read was sent")
			}
		})
	}
	t.Run("no house key", func(t *testing.T) {
		server := newMediaServer(t, catalogJSON(true, true), textReply("never"))
		server.client.key = staticKey(" ")
		_, err := server.client.Describe(t.Context(), pngBytes, "image/png", "describe")
		if !errors.Is(err, llm.ErrMandatoryModalityUnavailable) || !errors.Is(err, ErrNoKey) {
			t.Fatalf("Describe = %v, want ErrNoKey as a mandatory modality failure", err)
		}
		if server.catalogs.Load() != 0 || len(server.sent) != 0 {
			t.Fatal("dialed without a key")
		}
	})
	t.Run("refusal", func(t *testing.T) {
		server := newMediaServer(t, catalogJSON(true, true), append(reply(textBlock(0, "I can't")), messageDelta("refusal", 3), messageStop()))
		if _, err := server.client.Describe(t.Context(), pngBytes, "image/png", "describe"); !errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
			t.Fatalf("Describe = %v, want the refusal as a mandatory modality failure", err)
		}
	})
}

// The asset pipeline sends the original bytes of a photo it could not downscale; Claude
// would answer it with a 400 on every retry, so the vision client must hand it to the
// sidecar instead, without dialing Claude.
func TestOversizeImageFallsBackToTheSidecar(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"read by the sidecar"}}]}`)
	}))
	defer sidecar.Close()
	server := newMediaServer(t, catalogJSON(true, true), textReply("never"))
	vision := multimodal.NewVisionClient(multimodal.VisionConfig{
		PrimaryReader: server.client, Model: "claude-sonnet-5-5",
		LocalBaseURL: sidecar.URL + "/v1", LocalModel: "glm-ocr", TimeoutSec: 5,
	})
	for name, image := range map[string][]byte{"over 10 MB": make([]byte, maxImageBase64Bytes/4*3+1), "over 8000 px": wideImage(t)} {
		text, model, err := vision.Describe(t.Context(), image, "image/png", "describe")
		if err != nil || text != "read by the sidecar" || model != "glm-ocr" {
			t.Fatalf("%s: Describe = %q, %q, %v; want the sidecar", name, text, model, err)
		}
	}
	if server.catalogs.Load() != 0 || len(server.sent) != 0 {
		t.Fatal("an image over Claude's limits dialed Claude")
	}
}

func TestDescribeFailuresAreRetryable(t *testing.T) {
	t.Run("key store down", func(t *testing.T) {
		server := newMediaServer(t, catalogJSON(true, true), textReply("never"))
		server.client.key = func(context.Context) (string, error) { return "", errors.New("store down") }
		if _, err := server.client.Describe(t.Context(), pngBytes, "image/png", "describe"); err == nil || errors.Is(err, llm.ErrMandatoryModalityUnavailable) {
			t.Fatalf("Describe = %v, want a retryable failure", err)
		}
	})
	t.Run("catalogue down", func(t *testing.T) {
		server := newMediaServer(t, `not json`, textReply("never"))
		if _, err := server.client.Describe(t.Context(), pngBytes, "image/png", "describe"); !errors.Is(err, llm.ErrModelCatalogUnavailable) {
			t.Fatalf("Describe = %v, want the catalogue failure", err)
		}
	})
	t.Run("cut description", func(t *testing.T) {
		server := newMediaServer(t, catalogJSON(true, true), append(reply(textBlock(0, "A recei")), messageDelta("max_tokens", 3), messageStop()))
		_, err := server.client.Describe(t.Context(), pngBytes, "image/png", "describe")
		if err == nil || errors.Is(err, llm.ErrMandatoryModalityUnavailable) || !strings.Contains(err.Error(), "length") {
			t.Fatalf("Describe = %v, want the cut named", err)
		}
	})
	t.Run("stream cut", func(t *testing.T) {
		server := newMediaServer(t, catalogJSON(true, true), reply(textBlock(0, "A recei")))
		if _, err := server.client.Describe(t.Context(), pngBytes, "image/png", "describe"); !errors.Is(err, ErrStreamIncomplete) {
			t.Fatalf("Describe = %v, want ErrStreamIncomplete", err)
		}
	})
}
