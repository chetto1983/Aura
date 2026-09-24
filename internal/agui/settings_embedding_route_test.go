package agui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

type fakeEmbeddingRoutes struct {
	memory, documents embeddings.Space
	currentErr        error
	reports           []arcadedb.TenantSpaceReport
	reportsErr        error
	probe             embeddings.RouteProbe
	probeErr          error
	probed            []config.EmbedConfig
	work              arcadedb.CorpusWork
	workAsked         []int
	dims              int
}

func (f *fakeEmbeddingRoutes) Current(context.Context) (embeddings.Space, embeddings.Space, error) {
	return f.memory, f.documents, f.currentErr
}

func (f *fakeEmbeddingRoutes) Reports(context.Context, string, string) ([]arcadedb.TenantSpaceReport, error) {
	return f.reports, f.reportsErr
}

func (f *fakeEmbeddingRoutes) Probe(_ context.Context, embed config.EmbedConfig) (embeddings.RouteProbe, embeddings.Space, error) {
	f.probed = append(f.probed, embed)
	return f.probe, embeddings.Space{ID: "es1-mem-target"}, f.probeErr
}

func (f *fakeEmbeddingRoutes) Work(_ context.Context, _, _ string, limitChars int) (arcadedb.CorpusWork, error) {
	f.workAsked = append(f.workAsked, limitChars)
	return f.work, nil
}

func (f *fakeEmbeddingRoutes) Dimensions() int { return f.dims }

// cloudProbe is a hosted model that answers 1024 wide, reads 3,000 characters a second, takes
// 8,192 tokens and costs $0.02 per million.
func cloudProbe() embeddings.RouteProbe {
	return embeddings.RouteProbe{
		Space: embeddings.Space{ID: "es1-target", Label: "openrouter vendor/embed, 768d, recipe 1"}, NativeWidth: 1024,
		CharsPerSecond: 3000, InputLimit: 8192, PricePer1M: 0.02, HasPrice: true,
	}
}

func routeRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	raw, _ := json.Marshal(body)
	return withPrincipal(httptest.NewRequest(method, path, bytes.NewReader(raw)), "op-1")
}

func decodeInto[T any](t *testing.T, rr *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %s: %v", rr.Body.String(), err)
	}
	return out
}

var cloudRoute = EmbeddingRouteValues{BaseURL: "http://aura-llama-embed:8081", Model: "vendor/embed"}

// Spec §4: the three route keys change only through the preview and the confirmed apply.
func TestGenericSettingWritesRefuseTheEmbeddingRouteKeys(t *testing.T) {
	for _, key := range embeddingRouteKeys {
		store := &fakeSettingsStore{}
		s := &Server{settings: store}
		rr, r := putReq(t, key, "anything", "op-1")
		s.handlePutSetting(rr, r)
		if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "/api/settings/embedding-route") {
			t.Fatalf("PUT %s = %d %s, want 409 naming the route endpoint", key, rr.Code, rr.Body.String())
		}
		del := withPrincipal(httptest.NewRequest(http.MethodDelete, "/api/settings/"+key, nil), "op-1")
		del.SetPathValue("key", key)
		rr = httptest.NewRecorder()
		s.handleDeleteSetting(rr, del)
		if rr.Code != http.StatusConflict {
			t.Fatalf("DELETE %s = %d, want 409", key, rr.Code)
		}
		if len(store.upserted) != 0 || len(store.deleted) != 0 {
			t.Fatalf("%s reached the store: upserted %v deleted %v", key, store.upserted, store.deleted)
		}
	}
}

func TestEmbeddingSpaceReportsTheCurrentSpacesAndEveryTenant(t *testing.T) {
	routes := &fakeEmbeddingRoutes{
		memory: embeddings.Space{ID: "es1-e0aa6accf0b79c6b", Label: "local embeddinggemma"}, documents: embeddings.Space{ID: "es1-e0aa6accf0b79c6b"},
		reports: []arcadedb.TenantSpaceReport{{IdentityID: "id-1", StuckDocuments: []arcadedb.StuckDocument{{FileName: "scan.png"}}}},
	}
	s := &Server{embeddingRoutes: routes}
	rr := httptest.NewRecorder()
	s.handleEmbeddingSpace(rr, routeRequest(t, http.MethodGet, "/api/settings/embedding-space", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rr.Code, rr.Body.String())
	}
	got := decodeInto[embeddingSpaceDTO](t, rr)
	if got.Space != "es1-e0aa6accf0b79c6b" || !got.FloorsCalibrated || len(got.Tenants) != 1 ||
		got.Tenants[0].StuckDocuments[0].FileName != "scan.png" {
		t.Fatalf("state = %+v", got)
	}
}

func TestEmbeddingSpaceSaysWhyItCannotNameTheSpace(t *testing.T) {
	s := &Server{embeddingRoutes: &fakeEmbeddingRoutes{currentErr: errors.New("sidecar down")}}
	rr := httptest.NewRecorder()
	s.handleEmbeddingSpace(rr, routeRequest(t, http.MethodGet, "/api/settings/embedding-space", nil))
	got := decodeInto[embeddingSpaceDTO](t, rr)
	if rr.Code != http.StatusOK || got.SpaceError != "sidecar down" || len(got.Tenants) != 0 {
		t.Fatalf("status %d state %+v, want the error and no tenant read against no space", rr.Code, got)
	}
	s = &Server{embeddingRoutes: &fakeEmbeddingRoutes{reportsErr: errors.New("arcadedb down")}}
	rr = httptest.NewRecorder()
	s.handleEmbeddingSpace(rr, routeRequest(t, http.MethodGet, "/api/settings/embedding-space", nil))
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("a failed report = %d, want 502", rr.Code)
	}
}

func TestEmbeddingRoutePreviewPricesTheWork(t *testing.T) {
	routes := &fakeEmbeddingRoutes{probe: cloudProbe(), dims: 768, work: arcadedb.CorpusWork{
		Types:             []arcadedb.TypeWork{{Type: "FACT", Rows: 10, Chars: 5000}, {Type: "Passage", Rows: 20, Chars: 25000}},
		PassagesOverLimit: 3,
	}}
	s := &Server{embeddingRoutes: routes}
	rr := httptest.NewRecorder()
	s.handleEmbeddingRoutePreview(rr, routeRequest(t, http.MethodPost, "/api/settings/embedding-route/preview", cloudRoute))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rr.Code, rr.Body.String())
	}
	got := decodeInto[embeddingRoutePreview](t, rr)
	// 30,000 characters ÷ 3 = 10,000 tokens; × $0.02 per 1M = $0.0002; ÷ 3,000 chars/s = 10 s.
	if got.Tokens != 10000 || got.CostUSD == nil || math.Abs(*got.CostUSD-0.0002) > 1e-12 ||
		got.DurationSeconds != 10 || got.Local || len(got.Refusals) != 0 {
		t.Fatalf("preview = %+v", got)
	}
	if got.Space != "es1-target" || got.MemorySpace != "es1-mem-target" || !got.WidthWarning ||
		got.Work.PassagesOverLimit != 3 || got.FloorsCalibrated {
		t.Fatalf("preview = %+v, want the target, a width warning, the cut and uncalibrated floors", got)
	}
	if len(routes.workAsked) != 1 || routes.workAsked[0] != 8192 {
		t.Fatalf("work asked with limits %v, want the hosted limit", routes.workAsked)
	}
	if embed := routes.probed[0]; embed.CloudModel != "vendor/embed" || embed.BaseURL != cloudRoute.BaseURL {
		t.Fatalf("probed %+v", embed)
	}
}

func TestEmbeddingRoutePreviewOfTheLocalRouteCostsNothingAndCutsNothing(t *testing.T) {
	probe := embeddings.RouteProbe{Space: embeddings.Space{ID: "es1-local"}, NativeWidth: 768, CharsPerSecond: 1000, InputLimit: 2048}
	routes := &fakeEmbeddingRoutes{probe: probe, dims: 768, work: arcadedb.CorpusWork{Types: []arcadedb.TypeWork{{Chars: 7}}}}
	s := &Server{embeddingRoutes: routes}
	rr := httptest.NewRecorder()
	s.handleEmbeddingRoutePreview(rr, routeRequest(t, http.MethodPost, "/api/settings/embedding-route/preview",
		EmbeddingRouteValues{BaseURL: "http://aura-llama-embed:8081"}))
	got := decodeInto[embeddingRoutePreview](t, rr)
	if !got.Local || got.CostUSD == nil || *got.CostUSD != 0 || got.Tokens != 3 || got.WidthWarning || routes.workAsked[0] != 0 {
		t.Fatalf("preview = %+v, work limits %v", got, routes.workAsked)
	}
}

func TestEmbeddingRoutePreviewSaysUnknownWithoutAPrice(t *testing.T) {
	probe := cloudProbe()
	probe.HasPrice = false
	s := &Server{embeddingRoutes: &fakeEmbeddingRoutes{probe: probe, dims: 768}}
	rr := httptest.NewRecorder()
	s.handleEmbeddingRoutePreview(rr, routeRequest(t, http.MethodPost, "/api/settings/embedding-route/preview", cloudRoute))
	if got := decodeInto[embeddingRoutePreview](t, rr); got.CostUSD != nil {
		t.Fatalf("cost = %v, want unknown", *got.CostUSD)
	}
}

func TestEmbeddingRoutePreviewRefusals(t *testing.T) {
	narrow, short := cloudProbe(), cloudProbe()
	narrow.NativeWidth, short.InputLimit = 512, 512
	for _, test := range []struct {
		name  string
		probe embeddings.RouteProbe
		err   error
		dims  int
		want  string
	}{
		{"narrower than the stored width", narrow, nil, 768, refusalWidthTooNarrow},
		{"narrower than a wider document width", cloudProbe(), nil, 1536, refusalWidthTooNarrow},
		{"input limit under 2048", short, nil, 768, refusalInputLimitTooSmall},
		// Review Focus 1: a hosted route with no key is a refusal the cockpit shows, not a 5xx.
		{"no credential", embeddings.RouteProbe{}, embeddings.ErrNoCredential, 768, refusalKeyMissing},
		{"no route", embeddings.RouteProbe{}, embeddings.ErrNoRoute, 768, refusalNoRoute},
		{"probe failed", embeddings.RouteProbe{}, errors.New("HTTP 503"), 768, refusalProbeFailed},
	} {
		routes := &fakeEmbeddingRoutes{probe: test.probe, probeErr: test.err, dims: test.dims}
		s := &Server{embeddingRoutes: routes}
		rr := httptest.NewRecorder()
		s.handleEmbeddingRoutePreview(rr, routeRequest(t, http.MethodPost, "/api/settings/embedding-route/preview", cloudRoute))
		got := decodeInto[embeddingRoutePreview](t, rr)
		if rr.Code != http.StatusOK || len(got.Refusals) == 0 || got.Refusals[0].Code != test.want {
			t.Fatalf("%s: status %d refusals %+v, want %s", test.name, rr.Code, got.Refusals, test.want)
		}
		if test.err != nil && len(routes.workAsked) != 0 {
			t.Fatalf("%s: measured the corpus against a route that has no space", test.name)
		}
	}
}

func applyRequest(t *testing.T, route EmbeddingRouteValues, confirm string) *http.Request {
	t.Helper()
	return routeRequest(t, http.MethodPost, "/api/settings/embedding-route", map[string]string{
		"AURA_EMBED_BASE_URL": route.BaseURL, "AURA_EMBED_MODEL": route.Model,
		"AURA_EMBED_CLOUD_BASE_URL": route.CloudBaseURL, "confirm_space": confirm,
	})
}

func TestEmbeddingRouteApplyWritesTheThreeRowsAndRestarts(t *testing.T) {
	store := &fakeSettingsStore{}
	restarts := 0
	s := &Server{settings: store, embeddingRoutes: &fakeEmbeddingRoutes{probe: cloudProbe(), dims: 768},
		restartTrigger: func() { restarts++ }}
	rr := httptest.NewRecorder()
	s.handleApplyEmbeddingRoute(rr, applyRequest(t, cloudRoute, "es1-target"))
	if rr.Code != http.StatusOK || restarts != 1 {
		t.Fatalf("status %d restarts %d: %s", rr.Code, restarts, rr.Body.String())
	}
	want := map[string]string{
		"AURA_EMBED_BASE_URL": cloudRoute.BaseURL, "AURA_EMBED_MODEL": "vendor/embed", "AURA_EMBED_CLOUD_BASE_URL": "",
	}
	if len(store.upserted) != 3 {
		t.Fatalf("upserted = %v, want exactly the three route rows", store.upserted)
	}
	for key, value := range want {
		if got, ok := store.upserted[key]; !ok || got != value {
			t.Fatalf("upserted = %v, want %v", store.upserted, want)
		}
	}
}

func TestEmbeddingRouteApplyWithoutARestarterSaysARestartIsNeeded(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, embeddingRoutes: &fakeEmbeddingRoutes{probe: cloudProbe(), dims: 768}}
	rr := httptest.NewRecorder()
	s.handleApplyEmbeddingRoute(rr, applyRequest(t, cloudRoute, "es1-target"))
	if got := decodeInto[embeddingRouteApplied](t, rr); rr.Code != http.StatusOK || !got.RestartRequired || got.Restarting {
		t.Fatalf("status %d applied %+v", rr.Code, got)
	}
}

// Review Focus 3: a preview made before the route's space moved does not confirm the new one.
func TestEmbeddingRouteApplyRefusesAStaleConfirmation(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, embeddingRoutes: &fakeEmbeddingRoutes{probe: cloudProbe(), dims: 768},
		restartTrigger: func() { t.Fatal("restarted on a stale confirmation") }}
	rr := httptest.NewRecorder()
	s.handleApplyEmbeddingRoute(rr, applyRequest(t, cloudRoute, "es1-what-the-operator-saw"))
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "es1-target") || len(store.upserted) != 0 {
		t.Fatalf("status %d body %s upserted %v", rr.Code, rr.Body.String(), store.upserted)
	}
}

func TestEmbeddingRouteApplyRefusesARefusedRoute(t *testing.T) {
	store := &fakeSettingsStore{}
	s := &Server{settings: store, embeddingRoutes: &fakeEmbeddingRoutes{probeErr: embeddings.ErrNoCredential, dims: 768}}
	rr := httptest.NewRecorder()
	s.handleApplyEmbeddingRoute(rr, applyRequest(t, cloudRoute, ""))
	if rr.Code != http.StatusUnprocessableEntity || len(store.upserted) != 0 {
		t.Fatalf("status %d upserted %v, want 422 and nothing written", rr.Code, store.upserted)
	}
}

func TestEmbeddingRouteEndpointsGuardTheirPreconditions(t *testing.T) {
	routes := &fakeEmbeddingRoutes{probe: cloudProbe(), dims: 768}
	for _, test := range []struct {
		name    string
		server  *Server
		request *http.Request
		want    int
	}{
		{"space without seam", &Server{}, routeRequest(t, http.MethodGet, "/x", nil), http.StatusServiceUnavailable},
		{"preview without seam", &Server{}, routeRequest(t, http.MethodPost, "/x", cloudRoute), http.StatusServiceUnavailable},
		{"apply without store", &Server{embeddingRoutes: routes}, applyRequest(t, cloudRoute, "es1-target"), http.StatusServiceUnavailable},
		{"preview unauthenticated", &Server{embeddingRoutes: routes},
			httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{}")), http.StatusUnauthorized},
		{"preview bad JSON", &Server{embeddingRoutes: routes},
			withPrincipal(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{")), "op-1"), http.StatusBadRequest},
	} {
		rr := httptest.NewRecorder()
		switch {
		case strings.HasPrefix(test.name, "space"):
			test.server.handleEmbeddingSpace(rr, test.request)
		case strings.HasPrefix(test.name, "preview"):
			test.server.handleEmbeddingRoutePreview(rr, test.request)
		default:
			test.server.handleApplyEmbeddingRoute(rr, test.request)
		}
		if rr.Code != test.want {
			t.Fatalf("%s: status %d, want %d", test.name, rr.Code, test.want)
		}
	}
}
