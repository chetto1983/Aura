package mediagen

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// maxEndpointReads bounds the per-model endpoint reads one image refresh runs at once.
const maxEndpointReads = 4

// NewCatalog returns a Catalog over OpenRouter's public image and video model lists. The
// lists need no credential and the cache is shared across identities, so none is sent.
func NewCatalog(httpClient *http.Client) *Catalog {
	return newCatalog(func(ctx context.Context, baseURL string, kind Kind) ([]Model, error) {
		client := sdkClient(httpClient, baseURL)
		switch kind {
		case KindImage:
			return fetchImageModels(ctx, client)
		case KindVideo:
			return fetchVideoModels(ctx, client)
		}
		return nil, fmt.Errorf("unknown media kind %q", kind)
	}, time.Now)
}

// sdkClient builds the one SDK client this package uses, from explicit options only.
// openai.NewClient would first load OPENAI_API_KEY, OPENAI_ADMIN_KEY and custom headers
// from the environment and send them to the operator's base URL whenever no identity key
// overrides them; the SDK's opt-out marker is internal API. Only Options is populated,
// so callers use the generic Get/Post/Execute methods. Retries are off: a paid POST must
// never be sent twice.
func sdkClient(httpClient *http.Client, baseURL string, opts ...option.RequestOption) *openai.Client {
	return &openai.Client{Options: append([]option.RequestOption{
		option.WithBaseURL(strings.TrimRight(baseURL, "/") + "/"),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	}, opts...)}
}

type imageModelRow struct {
	ID                  string                   `json:"id"`
	SupportedParameters map[string]parameterWire `json:"supported_parameters"`
}

type parameterWire struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
	Min    *float64 `json:"min"`
	Max    *float64 `json:"max"`
}

type videoModelRow struct {
	ID                    string            `json:"id"`
	SupportedResolutions  []string          `json:"supported_resolutions"`
	SupportedAspectRatios []string          `json:"supported_aspect_ratios"`
	SupportedDurations    []int             `json:"supported_durations"`
	SupportedFrameImages  []string          `json:"supported_frame_images"`
	GenerateAudio         bool              `json:"generate_audio"`
	PricingSKUs           map[string]string `json:"pricing_skus"`
}

type imageEndpointsWire struct {
	Endpoints []struct {
		Pricing []struct {
			Billable string   `json:"billable"`
			Unit     string   `json:"unit"`
			Variant  string   `json:"variant"`
			CostUSD  *float64 `json:"cost_usd"`
		} `json:"pricing"`
	} `json:"endpoints"`
}

func fetchImageModels(ctx context.Context, client *openai.Client) ([]Model, error) {
	rows, err := getDataList[imageModelRow](ctx, client, "images/models")
	if err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		models = append(models, Model{ID: row.ID, Kind: KindImage, Parameters: parameters(row.SupportedParameters)})
	}
	enrichImagePricing(ctx, client, models)
	return models, nil
}

func fetchVideoModels(ctx context.Context, client *openai.Client) ([]Model, error) {
	rows, err := getDataList[videoModelRow](ctx, client, "videos/models")
	if err != nil {
		return nil, err
	}
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" {
			continue
		}
		models = append(models, Model{
			ID: row.ID, Kind: KindVideo,
			Durations: row.SupportedDurations, Resolutions: row.SupportedResolutions,
			AspectRatios: row.SupportedAspectRatios, FrameImages: row.SupportedFrameImages,
			GenerateAudio: row.GenerateAudio, PricingSKUs: row.PricingSKUs,
		})
	}
	return models, nil
}

// getDataList reads a catalog's "data" array. encoding/json leaves a missing or null
// array nil but decodes [] as an empty slice, so nil is a malformed response and an
// empty slice is a provider that really lists nothing.
func getDataList[T any](ctx context.Context, client *openai.Client, path string) ([]T, error) {
	var wire struct {
		Data []T `json:"data"`
	}
	if err := getJSON(ctx, client, path, &wire); err != nil {
		return nil, err
	}
	if wire.Data == nil {
		return nil, fmt.Errorf("GET %s: response has no data list", path)
	}
	return wire.Data, nil
}

func getJSON(ctx context.Context, client *openai.Client, path string, into any) error {
	var raw []byte
	if err := client.Get(ctx, path, nil, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}

func parameters(wire map[string]parameterWire) map[string]Parameter {
	if len(wire) == 0 {
		return nil
	}
	out := make(map[string]Parameter, len(wire))
	for name, p := range wire {
		out[name] = Parameter{Type: p.Type, Values: p.Values, Min: wholeBound(p.Min), Max: wholeBound(p.Max)}
	}
	return out
}

// wholeBound keeps a range bound only when it is a whole number: the schema types bounds
// as number, and a fractional one cannot cap a count, so it stays undeclared.
func wholeBound(v *float64) *int {
	if v == nil || *v != math.Trunc(*v) || math.Abs(*v) > math.MaxInt32 {
		return nil
	}
	bound := int(*v)
	return &bound
}

// enrichImagePricing reads each model's endpoint records for their pricing lines. A
// failed read is deliberately silent: that model's price stays unknown while its
// capabilities, which came from the list, still clamp requests.
func enrichImagePricing(ctx context.Context, client *openai.Client, models []Model) {
	slots := make(chan struct{}, maxEndpointReads)
	var wg sync.WaitGroup
	for i := range models {
		path, ok := imageEndpointsPath(models[i].ID)
		if !ok {
			continue
		}
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			var wire imageEndpointsWire
			if getJSON(ctx, client, path, &wire) != nil {
				return
			}
			models[i].ImagePricing = priceLines(wire)
		})
	}
	wg.Wait()
}

func priceLines(wire imageEndpointsWire) []PriceLine {
	var lines []PriceLine
	for _, endpoint := range wire.Endpoints {
		for _, line := range endpoint.Pricing {
			if line.CostUSD == nil {
				continue
			}
			lines = append(lines, PriceLine{Billable: line.Billable, Unit: line.Unit, Variant: line.Variant, CostUSD: *line.CostUSD})
		}
	}
	return lines
}

// imageEndpointsPath builds images/models/{author}/{slug}/endpoints from the model ID,
// never from the list's endpoints URL. The SDK resolves the path against the base URL,
// so a dot segment would climb out of it: such an ID gets no pricing read.
func imageEndpointsPath(id string) (string, bool) {
	author, slug, found := strings.Cut(id, "/")
	if !found || !pathSegment(author) || !pathSegment(slug) {
		return "", false
	}
	return "images/models/" + url.PathEscape(author) + "/" + url.PathEscape(slug) + "/endpoints", true
}

func pathSegment(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.Contains(s, "/")
}
