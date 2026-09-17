package mediagen

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// fakeImageProvider is OpenRouter's image catalog and generation endpoint. catalogStatus turns
// the catalog read into a failure without touching the generation path.
type fakeImageProvider struct {
	*httptest.Server
	catalog       string
	catalogStatus int
	generated     string
	generations   int
	bodies        []map[string]any
}

func newFakeImageProvider(t *testing.T, p *fakeImageProvider) *fakeImageProvider {
	t.Helper()
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if p.catalogStatus != 0 {
				respondJSON(w, p.catalogStatus, `{"error":"unavailable"}`)
				return
			}
			respondJSON(w, http.StatusOK, p.catalog)
			return
		}
		p.generations++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode generation body: %v", err)
		}
		p.bodies = append(p.bodies, body)
		respondJSON(w, http.StatusOK, p.generated)
	}))
	t.Cleanup(p.Close)
	return p
}

// maiImageCatalog offers three ratios and at most one reference, as the live row does.
const maiImageCatalog = `{"data":[{"id":"microsoft/mai-image-2.6","supported_parameters":{` +
	`"aspect_ratio":{"type":"enum","values":["1:1","16:9","9:16"]},` +
	`"input_references":{"type":"range","min":0,"max":1}}}]}`

func newImageGeneratorFixture(t *testing.T, provider *fakeImageProvider) (*ImageGenerator, *fakeReferenceReader) {
	t.Helper()
	png := tinyPNG(t)
	references := &fakeReferenceReader{assets: map[string]*fakeAsset{
		"owner/ref-1": {data: png, meta: ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: int64(len(png))}},
		"owner/ref-2": {data: png, meta: ReferenceMeta{MIMEType: "image/png", Modality: "image", SizeBytes: int64(len(png))}},
	}}
	return &ImageGenerator{
		Credentials:   &fakeCredentials{answers: []credentialAnswer{{baseURL: provider.URL}}},
		Catalog:       NewCatalog(provider.Client()),
		Client:        NewClient(provider.Client(), 1<<20),
		References:    references,
		MaxImageBytes: 1 << 20,
	}, references
}

func generatedPNGBody(t *testing.T) string {
	t.Helper()
	return `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(tinyPNG(t)) +
		`","media_type":"image/png"}],"usage":{"cost":0.04}}`
}

// TestImageGeneratorReturnsThePaidImageAndWhatProducedIt pins the whole answer the callers need:
// the bytes and their type, the cost the provider reported, the input as the provider received it
// after the clamp, and the notes explaining the difference. The requested 4:3 is not offered, so
// the nearest declared ratio is used and said so — an answer that returned the asked-for ratio
// would misreport what was generated.
func TestImageGeneratorReturnsThePaidImageAndWhatProducedIt(t *testing.T) {
	provider := newFakeImageProvider(t, &fakeImageProvider{catalog: maiImageCatalog, generated: generatedPNGBody(t)})
	generator, references := newImageGeneratorFixture(t, provider)

	got, err := generator.Generate(context.Background(), ImageGeneration{
		Owner: "owner", Model: "microsoft/mai-image-2.6",
		Input: ImageInput{Prompt: "a red panda", AspectRatio: "4:3", ReferenceAssetIDs: []string{"ref-1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Result.Bytes, tinyPNG(t)) || got.Result.MIMEType != "image/png" {
		t.Fatalf("result = %d bytes of %q, want the generated PNG", len(got.Result.Bytes), got.Result.MIMEType)
	}
	if got.Result.CostUSD == nil || *got.Result.CostUSD != 0.04 {
		t.Fatalf("cost = %v, want the reported 0.04", got.Result.CostUSD)
	}
	if got.Prompt != "a red panda" || got.Used.AspectRatio != "1:1" ||
		!slices.Equal(got.Used.ReferenceAssetIDs, []string{"ref-1"}) {
		t.Fatalf("used = %+v (prompt %q), want the clamped input", got.Used, got.Prompt)
	}
	if !slices.Equal(got.Adjustments, []string{"aspect ratio 4:3 is not offered; used 1:1"}) {
		t.Fatalf("adjustments = %q, want the ratio note", got.Adjustments)
	}
	if provider.generations != 1 {
		t.Fatalf("generations = %d, want exactly one paid call", provider.generations)
	}
	body := provider.bodies[0]
	refs, _ := body["input_references"].([]any)
	if body["aspect_ratio"] != "1:1" || len(refs) != 1 {
		t.Fatalf("generation body = %#v, want the clamped ratio and the one reference", body)
	}
	if !slices.Equal(references.opened, []string{"owner/ref-1"}) {
		t.Fatalf("opened = %v, want only the kept reference", references.opened)
	}
}

// TestImageGeneratorRefusesExtraReferencesBeforeSpending pins that a request the model cannot
// honour never reaches the provider. Dropping the extra reference would bill an edit that
// silently ignored a photo the caller named.
func TestImageGeneratorRefusesExtraReferencesBeforeSpending(t *testing.T) {
	provider := newFakeImageProvider(t, &fakeImageProvider{catalog: maiImageCatalog, generated: generatedPNGBody(t)})
	generator, references := newImageGeneratorFixture(t, provider)

	_, err := generator.Generate(context.Background(), ImageGeneration{
		Owner: "owner", Model: "microsoft/mai-image-2.6",
		Input: ImageInput{Prompt: "make it night", ReferenceAssetIDs: []string{"ref-1", "ref-2"}},
	})
	if ErrorCode(err) != "unsupported" {
		t.Fatalf("err = %v (code %q), want unsupported", err, ErrorCode(err))
	}
	if provider.generations != 0 {
		t.Fatalf("generations = %d, want none: nothing may be billed", provider.generations)
	}
	if len(references.opened) != 0 {
		t.Fatalf("opened = %v, want none before a refused call", references.opened)
	}
}

// TestImageGeneratorProceedsUncheckedWhenTheCatalogIsUnavailable pins that a free lookup failing
// never cancels a generation the caller asked for: the request goes out exactly as asked, and the
// note tells the caller its options were not checked.
func TestImageGeneratorProceedsUncheckedWhenTheCatalogIsUnavailable(t *testing.T) {
	provider := newFakeImageProvider(t, &fakeImageProvider{
		catalogStatus: http.StatusServiceUnavailable, generated: generatedPNGBody(t),
	})
	generator, _ := newImageGeneratorFixture(t, provider)

	got, err := generator.Generate(context.Background(), ImageGeneration{
		Owner: "owner", Model: "microsoft/mai-image-2.6",
		Input: ImageInput{Prompt: "a red panda", AspectRatio: "4:3", ReferenceAssetIDs: []string{"ref-1", "ref-2"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Adjustments, []string{uncheckedOptionsNote}) {
		t.Fatalf("adjustments = %q, want only the unchecked-options note", got.Adjustments)
	}
	if got.Used.AspectRatio != "4:3" || len(got.Used.ReferenceAssetIDs) != 2 {
		t.Fatalf("used = %+v, want the unclamped input", got.Used)
	}
	body := provider.bodies[0]
	refs, _ := body["input_references"].([]any)
	if provider.generations != 1 || body["aspect_ratio"] != "4:3" || len(refs) != 2 {
		t.Fatalf("generation body = %#v after %d calls, want the unclamped request", body, provider.generations)
	}
}

func TestImageGeneratorConfigured(t *testing.T) {
	var nilGenerator *ImageGenerator
	if nilGenerator.Configured() || (&ImageGenerator{}).Configured() {
		t.Fatal("a nil or zero generator must never report itself configured")
	}
	provider := newFakeImageProvider(t, &fakeImageProvider{catalog: maiImageCatalog, generated: generatedPNGBody(t)})
	full, _ := newImageGeneratorFixture(t, provider)
	if !full.Configured() {
		t.Fatal("a fully wired generator must report itself configured")
	}
	for name, breakOne := range map[string]func(*ImageGenerator){
		"credentials":   func(g *ImageGenerator) { g.Credentials = nil },
		"catalog":       func(g *ImageGenerator) { g.Catalog = nil },
		"client":        func(g *ImageGenerator) { g.Client = nil },
		"references":    func(g *ImageGenerator) { g.References = nil },
		"image ceiling": func(g *ImageGenerator) { g.MaxImageBytes = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			broken, _ := newImageGeneratorFixture(t, provider)
			breakOne(broken)
			if broken.Configured() {
				t.Fatalf("a generator missing its %s must not report itself configured", name)
			}
		})
	}
}

func TestImageRecordReadsBackAsAJobRow(t *testing.T) {
	record, err := ImageRecord(GeneratedImage{
		Prompt: "a cat https://cdn.example.com/ref.png?sig=deadbeef",
		Used: ImageInput{
			Prompt:            "a cat https://cdn.example.com/ref.png?sig=deadbeef",
			AspectRatio:       "1:1",
			ReferenceAssetIDs: []string{"ref-1", "ref-2"},
		},
		Adjustments: []string{"aspect ratio narrowed to 1:1"},
	}, "https://openrouter.ai/api/v1?key=sk-secret")
	if err != nil {
		t.Fatalf("ImageRecord() error = %v", err)
	}

	request, audit, err := Job{ID: "job-1", Request: record}.Submission()
	if err != nil {
		t.Fatalf("Submission() error = %v", err)
	}
	if request.AspectRatio != "1:1" {
		t.Fatalf("aspect ratio = %q, want 1:1", request.AspectRatio)
	}
	if !slices.Equal(audit.ReferenceAssetIDs, []string{"ref-1", "ref-2"}) {
		t.Fatalf("reference asset ids = %#v", audit.ReferenceAssetIDs)
	}
	if !slices.Equal(audit.Adjustments, []string{"aspect ratio narrowed to 1:1"}) {
		t.Fatalf("adjustments = %#v", audit.Adjustments)
	}
	// The origin is reduced the way a video row reduces it: a key in the query never lands in
	// the record, and the prompt's signed URL is redacted by the same rule.
	if audit.Origin != "https://openrouter.ai/api/v1" {
		t.Fatalf("origin = %q", audit.Origin)
	}
	if !strings.Contains(request.Prompt, "a cat") {
		t.Fatalf("prompt lost its text: %q", request.Prompt)
	}
	for _, leaked := range []string{"sk-secret", "deadbeef", "cdn.example.com"} {
		if strings.Contains(string(record), leaked) {
			t.Fatalf("the record kept %q: %s", leaked, record)
		}
	}
}

func TestImageRecordRefusesAnOriginItCannotReduce(t *testing.T) {
	if _, err := ImageRecord(GeneratedImage{}, "not-a-url"); err == nil {
		t.Fatal("ImageRecord() accepted an origin that is not an absolute http(s) URL")
	}
}
