package mediagen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// tinyWebP is not a fully valid WebP file, only the byte prefix
// net/http.DetectContentType's masked RIFF/WEBP signature requires.
func tinyWebP() []byte {
	return []byte("RIFF\x00\x00\x00\x00WEBPVP8 more-bytes-than-the-signature")
}

func setAmbientOpenAIEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "ambient-services-key")
	t.Setenv("OPENAI_ADMIN_KEY", "ambient-admin-key")
	t.Setenv("OPENAI_CUSTOM_HEADERS", "X-Ambient: leaked")
}

type imageGenServer struct {
	requests []*http.Request
	bodies   []map[string]any
	status   int
	body     string
}

func (s *imageGenServer) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r.Clone(context.Background()))
		var body map[string]any
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if len(data) > 0 {
			if err := json.Unmarshal(data, &body); err != nil {
				t.Error(err)
			}
		}
		s.bodies = append(s.bodies, body)
		status := s.status
		if status == 0 {
			status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, s.body)
	}
}

func TestGenerateImageSendsAspectRatioAndReferencesAndIgnoresAmbientCredentials(t *testing.T) {
	setAmbientOpenAIEnv(t)
	png := tinyPNG(t)
	encoded := base64.StdEncoding.EncodeToString(png)
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"image/png"}],"usage":{"prompt_tokens":0,"completion_tokens":10,"total_tokens":10,"cost":0.04}}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	req := ImageRequest{
		Model: "microsoft/mai-image-2.6", Prompt: "a red panda", AspectRatio: "16:9",
		References: []ImageReference{
			{Type: "image_url", ImageURL: ImageURL{URL: "https://example.com/a.png"}},
			{Type: "image_url", ImageURL: ImageURL{URL: "data:image/png;base64,AA=="}},
		},
	}
	result, err := client.GenerateImage(context.Background(), srv.URL, "identity-key", req)
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if !bytes.Equal(result.Bytes, png) || result.MIMEType != "image/png" {
		t.Fatalf("result bytes/MIME = %d bytes, %q", len(result.Bytes), result.MIMEType)
	}
	if result.CostUSD == nil || *result.CostUSD != 0.04 {
		t.Fatalf("CostUSD = %v, want 0.04", result.CostUSD)
	}

	if len(server.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(server.requests))
	}
	r := server.requests[0]
	if r.Method != http.MethodPost || r.URL.Path != "/images/generations" {
		t.Fatalf("request = %s %s", r.Method, r.URL.Path)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer identity-key" {
		t.Fatalf("Authorization = %q, want the explicit identity key despite ambient env", got)
	}
	if r.Header.Get("X-Ambient") != "" {
		t.Fatal("ambient custom header leaked into the request")
	}
	body := server.bodies[0]
	if body["model"] != "microsoft/mai-image-2.6" || body["prompt"] != "a red panda" || body["aspect_ratio"] != "16:9" {
		t.Fatalf("body = %#v", body)
	}
	refs, ok := body["input_references"].([]any)
	if !ok || len(refs) != 2 {
		t.Fatalf("input_references = %#v", body["input_references"])
	}
}

func TestGenerateImageDecodesEachSupportedFormat(t *testing.T) {
	cases := []struct {
		name      string
		bytes     []byte
		mediaType string
	}{
		{"declared png", tinyPNG(t), "image/png"},
		{"declared jpeg", tinyJPEG(t), "image/jpeg"},
		{"declared webp", tinyWebP(), "image/webp"},
		{"sniffed png, no declared media_type", tinyPNG(t), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded := base64.StdEncoding.EncodeToString(tc.bytes)
			mediaTypeField := ""
			if tc.mediaType != "" {
				mediaTypeField = `,"media_type":"` + tc.mediaType + `"`
			}
			server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `"` + mediaTypeField + `}]}`}
			srv := httptest.NewServer(server.handler(t))
			defer srv.Close()

			client := NewClient(srv.Client(), 1<<20)
			result, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
			if err != nil {
				t.Fatalf("GenerateImage: %v", err)
			}
			wantMIME := tc.mediaType
			if wantMIME == "" {
				wantMIME = http.DetectContentType(tc.bytes)
			}
			if !bytes.Equal(result.Bytes, tc.bytes) || result.MIMEType != wantMIME {
				t.Fatalf("got MIME %q bytes %d, want %q bytes %d", result.MIMEType, len(result.Bytes), wantMIME, len(tc.bytes))
			}
			if result.CostUSD != nil {
				t.Fatalf("CostUSD = %v, want nil when usage is absent from the response", result.CostUSD)
			}
		})
	}
}

func TestGenerateImageExplicitZeroCostIsNotNil(t *testing.T) {
	png := tinyPNG(t)
	encoded := base64.StdEncoding.EncodeToString(png)
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"image/png"}],"usage":{"cost":0}}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	result, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if result.CostUSD == nil || *result.CostUSD != 0 {
		t.Fatalf("CostUSD = %v, want an explicit 0, not nil", result.CostUSD)
	}
}

func TestGenerateImageRejectsContradictoryMediaType(t *testing.T) {
	jpeg := tinyJPEG(t)
	encoded := base64.StdEncoding.EncodeToString(jpeg)
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"image/png"}]}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
	if ErrorCode(err) != "unsupported" {
		t.Fatalf("ErrorCode = %q, want unsupported for jpeg bytes declared as png", ErrorCode(err))
	}
}

func TestGenerateImageAcceptsDeclaredSVGWithoutSniffing(t *testing.T) {
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`)
	encoded := base64.StdEncoding.EncodeToString(svg)
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"image/svg+xml"}]}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	result, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
	if err != nil {
		t.Fatalf("GenerateImage: %v", err)
	}
	if result.MIMEType != "image/svg+xml" || !bytes.Equal(result.Bytes, svg) {
		t.Fatalf("MIME = %q, bytes = %d, want image/svg+xml as a downloadable asset", result.MIMEType, len(result.Bytes))
	}
}

func TestGenerateImageRejectsUnrecognizedMediaType(t *testing.T) {
	garbage := []byte("not an image at all")
	encoded := base64.StdEncoding.EncodeToString(garbage)
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `"}]}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
	if ErrorCode(err) != "unsupported" {
		t.Fatalf("ErrorCode = %q, want unsupported for unrecognized bytes", ErrorCode(err))
	}
}

func TestGenerateImageRejectsEmptyOutput(t *testing.T) {
	cases := map[string]string{
		"no data array":       `{"created":1,"data":[]}`,
		"empty b64_json":      `{"created":1,"data":[{"b64_json":""}]}`,
		"malformed JSON body": `{"created":1,"data":[`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			server := &imageGenServer{body: body}
			srv := httptest.NewServer(server.handler(t))
			defer srv.Close()

			client := NewClient(srv.Client(), 1<<20)
			_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
			if err == nil {
				t.Fatal("want an error")
			}
			if ErrorCode(err) != "job_failed" {
				t.Fatalf("ErrorCode = %q, want the unclassified job_failed fallback", ErrorCode(err))
			}
		})
	}
}

func TestGenerateImageRejectsMalformedBase64(t *testing.T) {
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"not-valid-base64!!"}]}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
	if err == nil || ErrorCode(err) != "job_failed" {
		t.Fatalf("err = %v, want an unclassified decode error", err)
	}
}

func TestGenerateImageRejectsOversizedOutput(t *testing.T) {
	png := tinyPNG(t)
	encoded := base64.StdEncoding.EncodeToString(png)
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"image/png"}]}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 4)
	_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
	if ErrorCode(err) != "too_large" {
		t.Fatalf("ErrorCode = %q, want too_large", ErrorCode(err))
	}
}

func TestGenerateImageInvalidByteLimit(t *testing.T) {
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"AA=="}]}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	for _, limit := range []int64{0, -1} {
		client := NewClient(srv.Client(), limit)
		_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
		if ErrorCode(err) != "too_large" {
			t.Fatalf("limit %d: ErrorCode = %q, want too_large", limit, ErrorCode(err))
		}
	}
}

func TestGenerateImageClassifiesProviderErrors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"400 bad request", http.StatusBadRequest, `{"error":{"code":400,"message":"Invalid request parameters"}}`, "model_rejected"},
		{"402 payment required", http.StatusPaymentRequired, `{"error":{"code":402,"message":"Insufficient credits. Add more using https://openrouter.ai/credits"}}`, "no_credit"},
		{"403 forbidden is not content policy", http.StatusForbidden, `{"error":{"code":403,"message":"Only management keys can perform this operation"}}`, "job_failed"},
		{"500 internal server error", http.StatusInternalServerError, `{"error":{"code":500,"message":"Internal Server Error"}}`, "job_failed"},
		{"502 bad gateway", http.StatusBadGateway, `{"error":{"code":502,"message":"Provider returned error"}}`, "job_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := &imageGenServer{status: tc.status, body: tc.body}
			srv := httptest.NewServer(server.handler(t))
			defer srv.Close()

			client := NewClient(srv.Client(), 1<<20)
			_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
			if ErrorCode(err) != tc.want {
				t.Fatalf("ErrorCode = %q, want %q (err=%v)", ErrorCode(err), tc.want, err)
			}
			if len(server.requests) != 1 {
				t.Fatalf("requests = %d, want exactly 1 (no retry)", len(server.requests))
			}
		})
	}
}

func TestGenerateImageRejectsANonImageTypeEvenWhenTheBytesMatchIt(t *testing.T) {
	clip := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isommp41\x00\x00\x00\x08free")
	encoded := base64.StdEncoding.EncodeToString(clip)
	server := &imageGenServer{body: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"video/mp4"}]}`}
	srv := httptest.NewServer(server.handler(t))
	defer srv.Close()

	client := NewClient(srv.Client(), 1<<20)
	_, err := client.GenerateImage(context.Background(), srv.URL, "k", ImageRequest{Model: "m", Prompt: "p"})
	if ErrorCode(err) != "unsupported" {
		t.Fatalf("ErrorCode = %q, want unsupported for MP4 bytes declared as video/mp4 in an image answer", ErrorCode(err))
	}
}
