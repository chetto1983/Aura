package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/mediagen"
)

const imageCatalogFixture = `{"data":[{"id":"microsoft/mai-image-2.6","supported_parameters":{` +
	`"aspect_ratio":{"type":"enum","values":["1:1","16:9","9:16"]},` +
	`"input_references":{"type":"range","min":0,"max":1}}}]}`

func pngBytes(t *testing.T, width int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeOpenRouter serves the two OpenRouter endpoints image_generate reaches: the free
// image catalog and the paid generation. Every request is recorded, so a refusal can
// prove it spent nothing.
type fakeOpenRouter struct {
	server *httptest.Server

	catalogStatus int
	catalogHold   chan struct{}
	catalogHeld   chan struct{}
	imageStatus   int
	imageBody     string

	mu         sync.Mutex
	requests   []string
	generation map[string]any
	auth       string
}

// providerOption configures the fake before its server starts, so the handler goroutine
// never races the test for the configuration.
type providerOption func(p *fakeOpenRouter, generated []byte)

func newFakeOpenRouter(t *testing.T, generated []byte, opts ...providerOption) *fakeOpenRouter {
	t.Helper()
	f := &fakeOpenRouter{imageBody: `{"created":1,"data":[{"b64_json":"` +
		base64.StdEncoding.EncodeToString(generated) + `","media_type":"image/png"}],"usage":{"cost":0.04}}`}
	for _, opt := range opts {
		opt(f, generated)
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOpenRouter) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/images/models":
		if f.catalogHold != nil {
			close(f.catalogHeld)
			<-f.catalogHold
		}
		if f.catalogStatus != 0 {
			w.WriteHeader(f.catalogStatus)
			_, _ = io.WriteString(w, `{"error":{"message":"catalog down"}}`)
			return
		}
		_, _ = io.WriteString(w, imageCatalogFixture)
	case "/images/generations":
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		f.mu.Lock()
		f.generation, f.auth = decoded, r.Header.Get("Authorization")
		f.mu.Unlock()
		if f.imageStatus != 0 {
			w.WriteHeader(f.imageStatus)
			_, _ = io.WriteString(w, `{"error":{"message":"the model refused these options"}}`)
			return
		}
		_, _ = io.WriteString(w, f.imageBody)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeOpenRouter) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.requests)
}

func (f *fakeOpenRouter) generations() int {
	return strings.Count(strings.Join(f.seen(), "\n"), "POST /images/generations")
}

func (f *fakeOpenRouter) lastGeneration() (body map[string]any, authorization string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.generation, f.auth
}

type fakeMediaCredentials struct {
	baseURL string
	err     error

	calls int
	owner string
}

func (c *fakeMediaCredentials) For(_ context.Context, owner string) (string, string, error) {
	c.calls++
	c.owner = owner
	if c.err != nil {
		return "", "", c.err
	}
	return c.baseURL, "identity-key", nil
}

type fakeMediaSettings struct {
	model string
	err   error
}

func (s fakeMediaSettings) Model(_ context.Context, kind mediagen.Kind) (string, error) {
	if kind != mediagen.KindImage {
		return "", errors.New("image_generate asked for a non-image model")
	}
	return s.model, s.err
}

func (s fakeMediaSettings) VideoInlineWait(context.Context) (time.Duration, error) {
	return 0, errors.New("image_generate never reads the video wait")
}

type ownedReference struct {
	owner, mimeType, modality string
	data                      []byte
}

type fakeReferenceReader struct {
	assets map[string]ownedReference
	opened []string
}

func (r *fakeReferenceReader) Open(_ context.Context, owner, id string) (io.ReadCloser, mediagen.ReferenceMeta, error) {
	r.opened = append(r.opened, id)
	ref, ok := r.assets[id]
	if !ok || ref.owner != owner {
		return nil, mediagen.ReferenceMeta{}, errors.New("asset not visible to this identity")
	}
	meta := mediagen.ReferenceMeta{MIMEType: ref.mimeType, Modality: ref.modality, SizeBytes: int64(len(ref.data))}
	return io.NopCloser(bytes.NewReader(ref.data)), meta, nil
}

type imageFixture struct {
	provider    *fakeOpenRouter
	credentials *fakeMediaCredentials
	references  *fakeReferenceReader
	deliverer   *fakeDeliverer
	tool        *ImageGenerate
	generated   []byte
	reference   []byte
	runDir      string
	ctx         context.Context
}

func newImageFixture(t *testing.T, opts ...providerOption) *imageFixture {
	t.Helper()
	generated, reference := pngBytes(t, 2), pngBytes(t, 3)
	provider := newFakeOpenRouter(t, generated, opts...)
	f := &imageFixture{
		provider:    provider,
		credentials: &fakeMediaCredentials{baseURL: provider.server.URL},
		references: &fakeReferenceReader{assets: map[string]ownedReference{
			"ref-1": {owner: "owner-1", mimeType: "image/png", modality: "image", data: reference},
			"ref-2": {owner: "owner-1", mimeType: "image/png", modality: "image", data: reference},
		}},
		deliverer: &fakeDeliverer{retID: "asset-image-1"},
		generated: generated,
		reference: reference,
		runDir:    t.TempDir(),
	}
	f.tool = &ImageGenerate{
		Credentials:   f.credentials,
		Settings:      fakeMediaSettings{model: "microsoft/mai-image-2.6"},
		Catalog:       mediagen.NewCatalog(provider.server.Client()),
		Client:        mediagen.NewClient(provider.server.Client(), 1<<20),
		References:    f.references,
		Assets:        f.deliverer,
		MaxImageBytes: 1 << 20,
	}
	f.ctx = WithToolCallContext(identityctx.WithIdentityID(context.Background(), "owner-1"), "thread", "call-image", f.runDir, 8192)
	return f
}

func (f *imageFixture) execute(t *testing.T, args string) ToolResult {
	t.Helper()
	res, err := f.tool.Execute(f.ctx, json.RawMessage(args))
	if err != nil {
		t.Fatalf("Execute returned a Go error, want a tool result: %v", err)
	}
	return res
}

func toolError(t *testing.T, res ToolResult) (code, message string) {
	t.Helper()
	if res.Meta != nil {
		t.Fatalf("an error result carried meta %#v", *res.Meta)
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(res.Preview), &body); err != nil || body["error"] == "" {
		t.Fatalf("preview %q is not an {error,message} result", res.Preview)
	}
	return body["error"], body["message"]
}

type imagePreview struct {
	AssetID  string   `json:"asset_id"`
	MIMEType string   `json:"mime_type"`
	Model    string   `json:"model"`
	CostUSD  *float64 `json:"cost_usd"`
	Used     struct {
		AspectRatio       string   `json:"aspect_ratio"`
		ReferenceAssetIDs []string `json:"reference_asset_ids"`
	} `json:"used"`
	Adjustments []string `json:"adjustments"`
	Delivered   string   `json:"delivered"`
}

func TestImageGenerateSpec(t *testing.T) {
	spec := (&ImageGenerate{}).Spec()
	if spec.Name != "image_generate" || !spec.Deferred || !spec.Mutating {
		t.Fatal("image generation must be a deferred, admitted mutation")
	}
	if spec.OperationScope != OperationScopeAgent || spec.OperationNormalizer != OperationNormalizerCanonical ||
		spec.ReplayPolicy != ReplayToolResult || spec.Destructive || spec.Multiplexed {
		t.Fatalf("operation metadata = %+v, want the standard agent mutation", spec)
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required             []string `json:"required"`
		AdditionalProperties *bool    `json:"additionalProperties"`
	}
	if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
		t.Fatal(err)
	}
	if _, found := schema.Properties["model"]; found {
		t.Fatal("agent cannot choose model")
	}
	if _, found := schema.Properties["reference_asset_ids"]; !found {
		t.Fatal("missing editing references")
	}
	if len(schema.Properties) != 3 || !slices.Equal(schema.Required, []string{"prompt"}) ||
		schema.AdditionalProperties == nil || *schema.AdditionalProperties {
		t.Fatalf("schema = %+v, want prompt required, three properties, no additional properties", schema)
	}
	if want := []string{"1:1", "16:9", "9:16", "4:3", "3:4", "3:2", "2:3"}; !slices.Equal(schema.Properties["aspect_ratio"].Enum, want) {
		t.Fatalf("aspect_ratio enum = %v, want %v", schema.Properties["aspect_ratio"].Enum, want)
	}
	if _, err := OperationFingerprint(spec, json.RawMessage("{\"prompt\":\"a picture\"}")); err != nil {
		t.Fatal(err)
	}
}

func TestImageGenerateDeliversAnOwnedArtifact(t *testing.T) {
	f := newImageFixture(t)
	const prompt = "un gatto sul tetto, città di notte 🌙"
	res := f.execute(t, `{"prompt":"`+prompt+`","aspect_ratio":"16:9","reference_asset_ids":["ref-1"]}`)

	descriptor := artifactMap(t, res)
	path, _ := descriptor["path"].(string)
	want := map[string]any{
		"path": path, "filename": "generated.png", "mime_type": "image/png", "asset_id": "asset-image-1",
		"caption": prompt, "tool_call_id": "call-image", "size_bytes": int64(len(f.generated)),
	}
	if len(descriptor) != len(want) {
		t.Fatalf("descriptor = %#v, want exactly the keys of %#v", descriptor, want)
	}
	for key, value := range want {
		if descriptor[key] != value {
			t.Fatalf("descriptor[%q] = %#v, want %#v", key, descriptor[key], value)
		}
	}
	if staged, err := os.ReadFile(path); err != nil || !bytes.Equal(staged, f.generated) {
		t.Fatalf("staged file %q = %d bytes, %v; want the generated image", path, len(staged), err)
	}
	d := f.deliverer
	if d.calls != 1 || d.gotID != "owner-1" || d.gotThread != "thread" || d.gotCall != "call-image" ||
		d.gotPath != path || d.gotName != "generated.png" || d.gotMIME != "image/png" || d.gotSize != int64(len(f.generated)) {
		t.Fatalf("delivery = %+v, want one owned ingest of the staged image", d)
	}

	var preview imagePreview
	if err := json.Unmarshal([]byte(res.Preview), &preview); err != nil {
		t.Fatalf("preview %q: %v", res.Preview, err)
	}
	if preview.AssetID != "asset-image-1" || preview.MIMEType != "image/png" || preview.Model != "microsoft/mai-image-2.6" ||
		preview.CostUSD == nil || *preview.CostUSD != 0.04 || preview.Used.AspectRatio != "16:9" ||
		!slices.Equal(preview.Used.ReferenceAssetIDs, []string{"ref-1"}) || preview.Adjustments == nil || len(preview.Adjustments) != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	// Measured live 2026-09-16: without this line the model went looking for the file it had
	// just delivered (tool_search, find /workspace, skill list) to send it again.
	if preview.Delivered != mediaDeliveredNote {
		t.Fatalf("preview.delivered = %q, want %q", preview.Delivered, mediaDeliveredNote)
	}
	if res.Bytes != len(res.Preview) {
		t.Fatalf("Bytes = %d, want %d", res.Bytes, len(res.Preview))
	}

	meta, err := json.Marshal(res.Meta)
	if err != nil {
		t.Fatal(err)
	}
	for _, payload := range []string{
		base64.StdEncoding.EncodeToString(f.generated), base64.StdEncoding.EncodeToString(f.reference), "base64",
	} {
		if strings.Contains(string(meta), payload) || strings.Contains(res.Preview, payload) {
			t.Fatalf("result carries image data (%.20q...)", payload)
		}
	}

	gen, auth := f.provider.lastGeneration()
	refs, _ := gen["input_references"].([]any)
	if f.provider.generations() != 1 || auth != "Bearer identity-key" || gen["model"] != "microsoft/mai-image-2.6" ||
		gen["prompt"] != prompt || gen["aspect_ratio"] != "16:9" || len(refs) != 1 {
		t.Fatalf("generation request = %#v (auth %q, %d POSTs)", gen, auth, f.provider.generations())
	}
	if f.credentials.owner != "owner-1" {
		t.Fatalf("credentials resolved for %q, want the conversation owner", f.credentials.owner)
	}
}

func TestImageGenerateClampsToTheCatalogAndNeverOpensDroppedReferences(t *testing.T) {
	f := newImageFixture(t)
	res := f.execute(t, `{"prompt":"a lighthouse","aspect_ratio":"4:3","reference_asset_ids":["ref-1","ref-2"]}`)
	artifactMap(t, res)

	var preview imagePreview
	if err := json.Unmarshal([]byte(res.Preview), &preview); err != nil {
		t.Fatal(err)
	}
	wantNotes := []string{
		"aspect ratio 4:3 is not offered; used 1:1",
		"2 reference images requested; this model accepts at most 1, used the first 1",
	}
	if !slices.Equal(preview.Adjustments, wantNotes) {
		t.Fatalf("adjustments = %q, want %q", preview.Adjustments, wantNotes)
	}
	if preview.Used.AspectRatio != "1:1" || !slices.Equal(preview.Used.ReferenceAssetIDs, []string{"ref-1"}) {
		t.Fatalf("used = %+v, want the clamped request", preview.Used)
	}
	if !slices.Equal(f.references.opened, []string{"ref-1"}) {
		t.Fatalf("opened references = %v, want only the kept ref-1", f.references.opened)
	}
	if gen, _ := f.provider.lastGeneration(); gen["aspect_ratio"] != "1:1" {
		t.Fatalf("sent aspect_ratio %v, want the clamped 1:1", gen["aspect_ratio"])
	}
}

func TestImageGenerateProceedsUncheckedWhenTheCatalogIsUnavailable(t *testing.T) {
	f := newImageFixture(t, func(p *fakeOpenRouter, generated []byte) {
		p.catalogStatus = http.StatusServiceUnavailable
		p.imageBody = `{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(generated) + `"}]}`
	})
	res := f.execute(t, `{"prompt":"a lighthouse","aspect_ratio":"4:3","reference_asset_ids":["ref-1","ref-2"]}`)
	artifactMap(t, res)

	var preview imagePreview
	if err := json.Unmarshal([]byte(res.Preview), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Adjustments) != 1 || !strings.Contains(preview.Adjustments[0], "not checked") {
		t.Fatalf("adjustments = %q, want the single unchecked-options note", preview.Adjustments)
	}
	if preview.CostUSD != nil || !strings.Contains(res.Preview, `"cost_usd":null`) {
		t.Fatalf("preview %q: an unreported cost must stay null, never zero", res.Preview)
	}
	gen, _ := f.provider.lastGeneration()
	if refs, _ := gen["input_references"].([]any); gen["aspect_ratio"] != "4:3" || len(refs) != 2 {
		t.Fatalf("generation = %#v, want the unclamped request", gen)
	}
	if !slices.Equal(f.references.opened, []string{"ref-1", "ref-2"}) {
		t.Fatalf("opened = %v, want both references", f.references.opened)
	}
}

func TestImageGenerateFailsWithoutChargingWhenTheCatalogLookupIsAbandoned(t *testing.T) {
	f := newImageFixture(t, func(p *fakeOpenRouter, _ []byte) {
		p.catalogHold, p.catalogHeld = make(chan struct{}), make(chan struct{})
	})
	refreshing := make(chan struct{})
	go func() {
		defer close(refreshing)
		_, _ = f.tool.Catalog.List(context.Background(), f.provider.server.URL, mediagen.KindImage, false)
	}()
	<-f.provider.catalogHeld

	ctx, cancel := context.WithCancel(f.ctx)
	cancel()
	res, err := f.tool.Execute(ctx, json.RawMessage(`{"prompt":"a lighthouse"}`))
	close(f.provider.catalogHold)
	<-refreshing
	if err != nil {
		t.Fatal(err)
	}
	if code, _ := toolError(t, res); code != "job_failed" {
		t.Fatalf("code = %q, want job_failed for a catalog error other than unavailability", code)
	}
	if f.provider.generations() != 0 || f.deliverer.calls != 0 {
		t.Fatal("an abandoned catalog lookup still charged or delivered")
	}
}
