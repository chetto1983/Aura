package mediagen

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const pricedImageEndpoints = `{
  "id": "openai/gpt-image-2.5-sunburst",
  "endpoints": [
    {"provider_name": "OpenAI", "provider_slug": "openai", "provider_tag": "openai",
     "supported_parameters": {}, "allowed_passthrough_parameters": [], "supports_streaming": true,
     "pricing": [
       {"billable": "output_image", "unit": "image", "variant": "low", "cost_usd": 0.04},
       {"billable": "input_text", "unit": "token", "cost_usd": 0.000005}
     ]},
    {"provider_name": "Azure", "provider_slug": "azure", "provider_tag": "azure",
     "supported_parameters": {}, "allowed_passthrough_parameters": [], "supports_streaming": false,
     "pricing": [
       {"billable": "output_image", "unit": "image", "variant": "high", "cost_usd": 0.17},
       {"billable": "output_image", "unit": "image", "cost_usd": null}
     ]}
  ]
}`

type catalogServer struct {
	mu       sync.Mutex
	requests []*http.Request
	inflight int
	peak     int
	full     chan struct{}
	models   map[string]string
}

func newCatalogServer(t *testing.T) (*catalogServer, *httptest.Server) {
	t.Helper()
	s := &catalogServer{full: make(chan struct{}), models: map[string]string{
		"/api/v1/images/models":                                         readFixture(t, "image_models.json"),
		"/api/v1/videos/models":                                         readFixture(t, "video_models.json"),
		"/api/v1/images/models/microsoft/mai-image-2.6/endpoints":       readFixture(t, "image_endpoints.json"),
		"/api/v1/images/models/openai/gpt-image-2.5-sunburst/endpoints": pricedImageEndpoints,
	}}
	srv := httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *catalogServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Clone(context.Background()))
	body, found := s.models[r.URL.Path]
	s.mu.Unlock()
	if strings.HasSuffix(r.URL.Path, "/endpoints") {
		s.holdEndpointRead()
	}
	if !found {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"code":404,"message":"Resource not found"}}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

// holdEndpointRead keeps each endpoint read open until maxEndpointReads are in flight
// together, so the recorded peak proves the bound instead of depending on timing.
func (s *catalogServer) holdEndpointRead() {
	s.mu.Lock()
	s.inflight++
	s.peak = max(s.peak, s.inflight)
	if s.inflight == maxEndpointReads {
		select {
		case <-s.full:
		default:
			close(s.full)
		}
	}
	s.mu.Unlock()
	select {
	case <-s.full:
	case <-time.After(2 * time.Second):
	}
	s.mu.Lock()
	s.inflight--
	s.mu.Unlock()
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCatalogReadsImageModelsAndEndpointPricing(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "ambient-services-key")
	t.Setenv("OPENAI_ADMIN_KEY", "ambient-admin-key")
	t.Setenv("OPENAI_BASE_URL", "http://127.0.0.1:1/v1")
	t.Setenv("OPENAI_CUSTOM_HEADERS", "X-Ambient: leaked")
	server, srv := newCatalogServer(t)
	catalog := NewCatalog(srv.Client())

	models, err := catalog.List(context.Background(), srv.URL+"/api/v1/", KindImage, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 52 {
		t.Fatalf("image models = %d, want the fixture's 52", len(models))
	}
	mai := findModel(t, models, "microsoft/mai-image-2.6")
	if mai.Kind != KindImage || !slices.Contains(mai.Parameters["aspect_ratio"].Values, "3:2") ||
		*mai.Parameters["input_references"].Min != 0 || *mai.Parameters["input_references"].Max != 5 ||
		*mai.Parameters["n"].Max != 1 || len(mai.ImagePricing) != 3 {
		t.Fatalf("MAI row = %#v", mai)
	}
	if _, _, ok := ImagePrice(mai.ImagePricing); ok {
		t.Fatal("the observed token-priced MAI endpoint must not carry a per-image price")
	}
	sunburst := findModel(t, models, "openai/gpt-image-2.5-sunburst")
	if low, high, ok := ImagePrice(sunburst.ImagePricing); !ok || low != 0.04 || high != 0.17 || len(sunburst.ImagePricing) != 3 {
		t.Fatalf("per-image price across endpoints = %v %v %v from %#v", low, high, ok, sunburst.ImagePricing)
	}
	unpriced := findModel(t, models, "openai/gpt-image-2.5-flare")
	if unpriced.ImagePricing != nil || unpriced.Parameters["input_references"].Max == nil {
		t.Fatalf("a failed endpoint read must leave capabilities and an unknown price: %#v", unpriced)
	}

	endpointReads := 0
	for _, r := range server.requests {
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Ambient") != "" {
			t.Fatalf("%s carried ambient credentials or headers", r.URL.Path)
		}
		if strings.HasSuffix(r.URL.Path, "/endpoints") {
			endpointReads++
		}
	}
	if endpointReads != 52 || server.peak != maxEndpointReads {
		t.Fatalf("endpoint reads = %d with peak concurrency %d, want 52 at %d", endpointReads, server.peak, maxEndpointReads)
	}
}

func TestCatalogReadsVideoModels(t *testing.T) {
	server, srv := newCatalogServer(t)
	models, err := NewCatalog(srv.Client()).List(context.Background(), srv.URL+"/api/v1", KindVideo, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 29 || len(server.requests) != 1 {
		t.Fatalf("video models = %d from %d requests, want 29 from 1", len(models), len(server.requests))
	}
	hailuo := findModel(t, models, "minimax/hailuo-3-max")
	if hailuo.Kind != KindVideo || !slices.Equal(hailuo.Durations, []int{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}) ||
		!slices.Equal(hailuo.Resolutions, []string{"768p", "480p"}) || !slices.Contains(hailuo.AspectRatios, "21:9") ||
		!slices.Equal(hailuo.FrameImages, []string{"first_frame", "last_frame"}) || hailuo.GenerateAudio || hailuo.Parameters != nil {
		t.Fatalf("Hailuo row = %#v", hailuo)
	}
	if low, high, ok := VideoPrice(hailuo.PricingSKUs); !ok || low != 0.05 || high != 0.08 {
		t.Fatalf("Hailuo price = %v-%v %v", low, high, ok)
	}
	edit := findModel(t, models, "black-forest-labs/flux-video-edit")
	if edit.Durations != nil || edit.Resolutions != nil || edit.FrameImages != nil || edit.GenerateAudio {
		t.Fatalf("null capability sets must stay undeclared: %#v", edit)
	}
	if low, _, ok := VideoPrice(edit.PricingSKUs); !ok || low != 0.03 {
		t.Fatalf("cents-per-second price = %v %v", low, ok)
	}
}

func TestCatalogFetchFailures(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   int
		fails  bool
	}{
		{name: "server error is not retried", status: http.StatusInternalServerError, body: `{"error":{"code":500,"message":"Internal Server Error"}}`, fails: true},
		{name: "malformed JSON", status: http.StatusOK, body: `{"data":[`, fails: true},
		{name: "missing data list", status: http.StatusOK, body: `{}`, fails: true},
		{name: "null data list", status: http.StatusOK, body: `{"data":null}`, fails: true},
		{name: "empty data list is a real empty catalog", status: http.StatusOK, body: `{"data":[]}`},
		{name: "rows without an id are skipped", status: http.StatusOK, body: `{"data":[{"id":" "},{"id":"vendor/clip","supported_durations":[4]}]}`, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			models, err := NewCatalog(srv.Client()).List(context.Background(), srv.URL, KindVideo, false)
			if tc.fails {
				if !errors.Is(err, ErrCatalogUnavailable) || models != nil || calls != 1 {
					t.Fatalf("List = %v, %v after %d calls; want one unretried failure", models, err, calls)
				}
				return
			}
			if err != nil || len(models) != tc.want {
				t.Fatalf("List = %v, %v; want %d models", models, err, tc.want)
			}
		})
	}
}

func TestCatalogRejectsUnknownKind(t *testing.T) {
	_, srv := newCatalogServer(t)
	if _, err := NewCatalog(srv.Client()).List(context.Background(), srv.URL+"/api/v1", Kind("audio"), false); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("unknown kind = %v, want unavailable", err)
	}
}

func TestImageEndpointsPath(t *testing.T) {
	cases := map[string]string{
		"microsoft/mai-image-2.6": "images/models/microsoft/mai-image-2.6/endpoints",
		"vendor/model?x=1#frag":   "images/models/vendor/model%3Fx=1%23frag/endpoints",
		"vendor/%2E%2E":           "images/models/vendor/%252E%252E/endpoints",
		"vendor/back\\slash":      "images/models/vendor/back%5Cslash/endpoints",
		"no-author":               "",
		"/slug":                   "",
		"author/":                 "",
		"a/b/c":                   "",
		"../endpoints":            "",
		"vendor/..":               "",
		"./model":                 "",
	}
	for id, want := range cases {
		got, ok := imageEndpointsPath(id)
		if got != want || ok != (want != "") {
			t.Errorf("imageEndpointsPath(%q) = %q, %v; want %q", id, got, ok, want)
		}
	}
}

func findModel(t *testing.T, models []Model, id string) Model {
	t.Helper()
	for _, m := range models {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("catalog has no %s", id)
	return Model{}
}
