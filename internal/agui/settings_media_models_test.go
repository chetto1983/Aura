package agui

// Handler tests for GET /api/settings/image-models and /video-models. The shared catalog is
// replaced by a recording MediaCatalogLister, so every branch runs without a network; the
// route classification and the real cache are exercised in cmd/aura.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mediagen"
)

type mediaListCall struct {
	kind    mediagen.Kind
	refresh bool
}

type fakeMediaCatalog struct {
	models []mediagen.Model
	err    error
	calls  []mediaListCall
}

func (f *fakeMediaCatalog) List(_ context.Context, kind mediagen.Kind, refresh bool) ([]mediagen.Model, error) {
	f.calls = append(f.calls, mediaListCall{kind: kind, refresh: refresh})
	return f.models, f.err
}

func getMediaModels(t *testing.T, s *Server, target string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.Mux().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, target, nil))
	return rr
}

// decodeMediaRows keeps the raw JSON objects: whether a key is present at all is part of the
// contract, because an absent price is unknown and a present zero is free.
func decodeMediaRows(t *testing.T, rr *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rr.Code, rr.Body.String())
	}
	var out struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rr.Body.String(), err)
	}
	return out.Models
}

func mediaServer(catalog MediaCatalogLister) *Server {
	s := NewServer(nil, nil, ServerConfig{})
	s.SetMediaCatalog(catalog)
	return s
}

func assertRow(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("row = %s\nwant  %s", gotJSON, wantJSON)
	}
}

func TestMediaModelRoutesAskTheSharedCatalogForTheirOwnKind(t *testing.T) {
	for _, tc := range []struct {
		target string
		want   mediaListCall
	}{
		{"/api/settings/image-models", mediaListCall{kind: mediagen.KindImage}},
		{"/api/settings/video-models", mediaListCall{kind: mediagen.KindVideo}},
		{"/api/settings/image-models?refresh=1", mediaListCall{kind: mediagen.KindImage, refresh: true}},
		{"/api/settings/video-models?refresh=1", mediaListCall{kind: mediagen.KindVideo, refresh: true}},
		// Only refresh=1 bypasses the cache; the picker never sends anything else.
		{"/api/settings/video-models?refresh=0", mediaListCall{kind: mediagen.KindVideo}},
		{"/api/settings/image-models?refresh=true", mediaListCall{kind: mediagen.KindImage}},
		// The browser cannot point the daemon at a host: a base_url is simply not read.
		{"/api/settings/image-models?base_url=http%3A%2F%2Fattacker.test%2Fv1", mediaListCall{kind: mediagen.KindImage}},
	} {
		t.Run(tc.target, func(t *testing.T) {
			catalog := &fakeMediaCatalog{models: []mediagen.Model{}}
			rr := getMediaModels(t, mediaServer(catalog), tc.target)
			if rows := decodeMediaRows(t, rr); len(rows) != 0 {
				t.Fatalf("rows = %v, want the empty list the catalog returned", rows)
			}
			if len(catalog.calls) != 1 || catalog.calls[0] != tc.want {
				t.Fatalf("catalog calls = %+v, want exactly %+v", catalog.calls, tc.want)
			}
		})
	}
}

func TestImageModelsCarryTheReferenceLimitAndOnlyPerImagePrices(t *testing.T) {
	catalog := &fakeMediaCatalog{models: []mediagen.Model{
		{
			// MAI as measured in planning: five references, output priced per token.
			ID: "microsoft/mai-image-2.6", Kind: mediagen.KindImage,
			Parameters: map[string]mediagen.Parameter{
				"input_references": {Type: "integer", Max: new(5)},
				"aspect_ratio":     {Type: "enum", Values: []string{"1:1", "16:9"}},
			},
			ImagePricing: []mediagen.PriceLine{{Billable: "output_image", Unit: "token", CostUSD: 0.000038}},
		},
		{
			ID: "black-forest-labs/flux-3-pro", Kind: mediagen.KindImage,
			Parameters: map[string]mediagen.Parameter{"input_references": {Type: "integer", Min: new(0), Max: new(8)}},
			ImagePricing: []mediagen.PriceLine{
				{Billable: "output_image", Unit: "image", Variant: "1k", CostUSD: 0.04},
				{Billable: "output_image", Unit: "image", Variant: "2k", CostUSD: 0.06},
				{Billable: "input_image", Unit: "image", CostUSD: 0.5},
			},
		},
		{
			ID: "vendor/free-image", Kind: mediagen.KindImage,
			Parameters:   map[string]mediagen.Parameter{"aspect_ratio": {Type: "enum", Values: []string{"1:1"}}},
			ImagePricing: []mediagen.PriceLine{{Billable: "output_image", Unit: "image", CostUSD: 0}},
		},
		{
			// References declared with no bound, and pricing enrichment that failed.
			ID: "vendor/unbounded", Kind: mediagen.KindImage,
			Parameters: map[string]mediagen.Parameter{"input_references": {Type: "integer"}},
		},
	}}

	rows := decodeMediaRows(t, getMediaModels(t, mediaServer(catalog), "/api/settings/image-models"))

	if len(rows) != 4 {
		t.Fatalf("rows = %v, want 4", rows)
	}
	assertRow(t, rows[0], map[string]any{
		"id": "microsoft/mai-image-2.6", "kind": "image", "reference_max": 5, "has_price": false,
	})
	assertRow(t, rows[1], map[string]any{
		"id": "black-forest-labs/flux-3-pro", "kind": "image", "reference_max": 8,
		"image_min_usd": 0.04, "image_max_usd": 0.06, "has_price": true,
	})
	assertRow(t, rows[2], map[string]any{
		"id": "vendor/free-image", "kind": "image", "image_min_usd": 0, "image_max_usd": 0, "has_price": true,
	})
	assertRow(t, rows[3], map[string]any{"id": "vendor/unbounded", "kind": "image", "has_price": false})
}

func TestVideoModelsCarryCapabilitiesAndOnlyPerSecondPrices(t *testing.T) {
	catalog := &fakeMediaCatalog{models: []mediagen.Model{
		{
			// Hailuo as measured in planning.
			ID: "minimax/hailuo-3-max", Kind: mediagen.KindVideo,
			Durations:    []int{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
			Resolutions:  []string{"768p", "480p"},
			FrameImages:  []string{"first_frame", "last_frame"},
			PricingSKUs:  map[string]string{"duration_seconds": "0.08", "duration_seconds_480p": "0.05", "duration_seconds_768p": "0.08"},
			AspectRatios: []string{"16:9"},
		},
		{
			ID: "vendor/cents", Kind: mediagen.KindVideo,
			Durations:   []int{10, 4, 8},
			FrameImages: []string{"last_frame"},
			PricingSKUs: map[string]string{"cents_per_second": "12", "input_tokens": "0.000002"},
		},
		{
			// Token-priced and declaring nothing else: every capability stays unstated.
			ID: "vendor/token-video", Kind: mediagen.KindVideo,
			PricingSKUs: map[string]string{"output_tokens": "0.00001"},
		},
	}}

	rows := decodeMediaRows(t, getMediaModels(t, mediaServer(catalog), "/api/settings/video-models"))

	if len(rows) != 3 {
		t.Fatalf("rows = %v, want 3", rows)
	}
	assertRow(t, rows[0], map[string]any{
		"id": "minimax/hailuo-3-max", "kind": "video", "duration_min": 5, "duration_max": 15,
		"resolutions": []string{"768p", "480p"}, "image_to_video": true,
		"second_min_usd": 0.05, "second_max_usd": 0.08, "has_price": true,
	})
	assertRow(t, rows[1], map[string]any{
		"id": "vendor/cents", "kind": "video", "duration_min": 4, "duration_max": 10,
		"image_to_video": false, "second_min_usd": 0.12, "second_max_usd": 0.12, "has_price": true,
	})
	assertRow(t, rows[2], map[string]any{
		"id": "vendor/token-video", "kind": "video", "image_to_video": false, "has_price": false,
	})
}

func TestMediaModelsRefuseALocalRouteWithTheWayOut(t *testing.T) {
	catalog := &fakeMediaCatalog{err: ErrMediaCatalogLocalRoute}
	for _, target := range []string{"/api/settings/image-models", "/api/settings/video-models?refresh=1"} {
		rr := getMediaModels(t, mediaServer(catalog), target)
		if rr.Code != http.StatusConflict {
			t.Fatalf("%s: status = %d (%s), want 409", target, rr.Code, rr.Body.String())
		}
		if !strings.Contains(rr.Body.String(), "OpenRouter") || !strings.Contains(rr.Body.String(), "Cloud") {
			t.Fatalf("%s: body = %s, want it to say how to reach the OpenRouter route", target, rr.Body.String())
		}
	}
}

func TestMediaModelsMapCatalogFailuresToBadGateway(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("%w: %w", mediagen.ErrCatalogUnavailable, errors.New("GET images/models: 503 Service Unavailable")),
		context.DeadlineExceeded,
	} {
		catalog := &fakeMediaCatalog{err: err}
		rr := getMediaModels(t, mediaServer(catalog), "/api/settings/image-models")
		if rr.Code != http.StatusBadGateway {
			t.Fatalf("%v: status = %d, want 502", err, rr.Code)
		}
		var body map[string]string
		if jsonErr := json.Unmarshal(rr.Body.Bytes(), &body); jsonErr != nil || body["error"] != err.Error() {
			t.Fatalf("%v: body = %s, want the catalog's reason", err, rr.Body.String())
		}
	}
}

func TestMediaModelsAnswerUnavailableUntilTheCatalogIsWired(t *testing.T) {
	s := NewServer(nil, nil, ServerConfig{})
	for _, target := range []string{"/api/settings/image-models", "/api/settings/video-models"} {
		if rr := getMediaModels(t, s, target); rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s: status = %d, want 503", target, rr.Code)
		}
	}
}
