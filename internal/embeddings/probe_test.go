package embeddings

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// probeServer answers the catalogue at catalogPath with catalogue and every embedding request
// with vectors of the widths given, one per input in turn. It records what was embedded and
// whether a request named a width.
type probeServer struct {
	*httptest.Server
	requests     atomic.Int32
	embedded     []string
	askedWidth   bool
	bearer       string
	catalogPath  string
	catalogue    string
	embedPath    string
	vectorWidths []int
}

func newProbeServer(t *testing.T, catalogPath, catalogue, embedPath string, widths ...int) *probeServer {
	t.Helper()
	probe := &probeServer{catalogPath: catalogPath, catalogue: catalogue, embedPath: embedPath, vectorWidths: widths}
	probe.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probe.requests.Add(1)
		probe.bearer = r.Header.Get("Authorization")
		switch r.URL.Path {
		case probe.catalogPath:
			_, _ = io.WriteString(w, probe.catalogue)
		case probe.embedPath:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, probe.askedWidth = body["dimensions"]
			inputs, _ := body["input"].([]any)
			data := make([]map[string]any, len(inputs))
			for index, input := range inputs {
				text, _ := input.(string)
				probe.embedded = append(probe.embedded, text)
				width := probe.vectorWidths[min(index, len(probe.vectorWidths)-1)]
				vector := make([]float64, width)
				vector[0] = 1
				data[index] = map[string]any{"index": index, "embedding": vector}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(probe.Close)
	return probe
}

func TestProbeRouteMeasuresTheLocalSidecar(t *testing.T) {
	server := newProbeServer(t, "/v1/models", measuredModels, "/v1/embeddings", 768)
	got, err := ProbeRoute(t.Context(), server.Client(), config.EmbedConfig{BaseURL: server.URL}, "", 768)
	if err != nil {
		t.Fatalf("ProbeRoute: %v", err)
	}
	const artifact = "embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0"
	if want := SpaceFor(config.EmbedLocal, "", "", 768, artifact); got.Space != want {
		t.Fatalf("space = %+v, want %+v", got.Space, want)
	}
	if got.NativeWidth != 768 || got.InputLimit != 2048 || got.HasPrice || got.CharsPerSecond <= 0 {
		t.Fatalf("probe = %+v, want width 768, limit 2048, no price, a measured speed", got)
	}
	if !slices.Equal(server.embedded, probeTexts) {
		t.Fatalf("embedded %q, want the synthetic batch and nothing else", server.embedded)
	}
	if server.askedWidth || server.bearer != "" {
		t.Fatalf("the local probe asked a width (%v) or sent a key (%q)", server.askedWidth, server.bearer)
	}
}

// The width a model answers with no `dimensions` field is its own: that is what decides
// whether truncation to the stored width is needed at all.
func TestProbeRouteReadsTheHostedPriceLimitAndNativeWidth(t *testing.T) {
	server := newProbeServer(t, "/api/v1/embeddings/models",
		`{"data":[{"id":"vendor/embed","context_length":8192,"pricing":{"prompt":"0.00000002","completion":"0"}}]}`,
		"/api/v1/embeddings", 4096)
	embed := config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL + "/api"}
	got, err := ProbeRoute(t.Context(), server.Client(), embed, "key", 768)
	if err != nil {
		t.Fatalf("ProbeRoute: %v", err)
	}
	if want := SpaceFor(config.EmbedEndpoint, "vendor/embed", server.URL+"/api", 768, ""); got.Space != want {
		t.Fatalf("space = %+v, want %+v", got.Space, want)
	}
	if got.NativeWidth != 4096 || got.InputLimit != 8192 || !got.HasPrice || math.Abs(got.PricePer1M-0.02) > 1e-9 {
		t.Fatalf("probe = %+v, want width 4096, limit 8192, $0.02 per 1M tokens", got)
	}
	if server.askedWidth || server.bearer != "Bearer key" {
		t.Fatalf("asked width %v, bearer %q: want the native width and the key", server.askedWidth, server.bearer)
	}
}

func TestProbeRouteRefusesVectorsOfMixedWidths(t *testing.T) {
	server := newProbeServer(t, "/v1/models", measuredModels, "/v1/embeddings", 768, 512)
	if _, err := ProbeRoute(t.Context(), server.Client(), config.EmbedConfig{BaseURL: server.URL}, "", 768); err == nil {
		t.Fatal("ProbeRoute accepted vectors of two widths")
	}
}

func TestProbeRouteWithoutACredentialSendsNothing(t *testing.T) {
	server := newProbeServer(t, "/api/v1/embeddings/models", `{"data":[]}`, "/api/v1/embeddings", 768)
	embed := config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL + "/api"}
	if _, err := ProbeRoute(t.Context(), server.Client(), embed, " ", 768); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("err = %v, want ErrNoCredential", err)
	}
	if n := server.requests.Load(); n != 0 {
		t.Fatalf("requests = %d, want none", n)
	}
}

func TestProbeRouteWithNoRouteIsErrNoRoute(t *testing.T) {
	if _, err := ProbeRoute(t.Context(), nil, config.EmbedConfig{}, "", 768); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("err = %v, want ErrNoRoute", err)
	}
}
