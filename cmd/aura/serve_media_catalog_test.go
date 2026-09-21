package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mediagen"
)

const (
	mediaAdminIdentity  = "00000000-0000-0000-0000-00000000000a"
	mediaMemberIdentity = "00000000-0000-0000-0000-00000000000b"
	openRouterBaseURL   = "https://openrouter.ai/api/v1"
)

// fakeOpenRouter serves the captured video catalogue for the one host every request is
// rewritten to, counting list reads so a test can tell a cache hit from a provider call.
type fakeOpenRouter struct {
	mu        sync.Mutex
	listReads int
	client    *http.Client
}

func newFakeOpenRouter(t *testing.T) *fakeOpenRouter {
	t.Helper()
	fixture, err := os.ReadFile("../../internal/mediagen/testdata/video_models.json")
	if err != nil {
		t.Fatalf("read video catalogue fixture: %v", err)
	}
	fake := &fakeOpenRouter{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/videos/models" {
			http.NotFound(w, r)
			return
		}
		fake.mu.Lock()
		fake.listReads++
		fake.mu.Unlock()
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse fake server URL: %v", err)
	}
	fake.client = &http.Client{Transport: rewriteHost{target: target}}
	return fake
}

func (f *fakeOpenRouter) reads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listReads
}

// rewriteHost keeps the configured OpenRouter URL on the request the catalog builds, so the
// route classification is the production one, and delivers it to the local fake instead.
type rewriteHost struct{ target *url.URL }

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	out.URL.Scheme = r.target.Scheme
	out.URL.Host = r.target.Host
	out.Host = r.target.Host
	return http.DefaultTransport.RoundTrip(out)
}

func routeRuntime(provider, baseURL string) *llm.Runtime {
	return llm.NewRuntime(nil, llm.Config{Provider: provider, BaseURL: baseURL, Model: "z-ai/glm-5.3"})
}

func TestMediaCatalogRouteServesThePickerAndTheToolsFromOneCache(t *testing.T) {
	provider := newFakeOpenRouter(t)
	catalog := mediagen.NewCatalog(provider.client)
	route := mediaCatalogRoute{catalog: catalog, runtime: routeRuntime("openrouter", openRouterBaseURL)}

	models, err := route.List(context.Background(), mediagen.KindVideo, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !slices.ContainsFunc(models, func(m mediagen.Model) bool { return m.ID == "minimax/hailuo-3-max" }) {
		t.Fatalf("models = %d rows without minimax/hailuo-3-max", len(models))
	}
	// The video tool looks the model up with the base URL its credential resolved, which is
	// the same live route: the picker's read already filled that cache entry.
	hailuo, err := catalog.Find(context.Background(), openRouterBaseURL, mediagen.KindVideo, "minimax/hailuo-3-max")
	if err != nil || hailuo == nil || !slices.Contains(hailuo.FrameImages, "first_frame") {
		t.Fatalf("Find = %+v, %v, want the cached Hailuo row", hailuo, err)
	}
	if provider.reads() != 1 {
		t.Fatalf("provider list reads = %d, want 1: the tools must reuse the picker's cache", provider.reads())
	}

	if _, err := route.List(context.Background(), mediagen.KindVideo, true); err != nil {
		t.Fatalf("List(refresh): %v", err)
	}
	if provider.reads() != 2 {
		t.Fatalf("provider list reads after refresh = %d, want 2", provider.reads())
	}
}

func TestMediaCatalogRouteRefusesEveryRouteThatIsNotOpenRouter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime *llm.Runtime
	}{
		{"llama.cpp", routeRuntime("llamacpp", "http://aura-llm:8084/v1")},
		{"ollama", routeRuntime("ollama", "http://host.docker.internal:11434/v1")},
		{"openrouter provider on a local host", routeRuntime("openrouter", "http://host.docker.internal:8084/v1")},
		{"openrouter provider on a private address", routeRuntime("openrouter", "http://192.168.1.20/openrouter.ai/v1")},
		{"no published route", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := newFakeOpenRouter(t)
			route := mediaCatalogRoute{catalog: mediagen.NewCatalog(provider.client), runtime: tc.runtime}
			models, err := route.List(context.Background(), mediagen.KindImage, true)
			if !errors.Is(err, agui.ErrCatalogLocalRoute) || models != nil {
				t.Fatalf("List = %v, %v, want the local-route refusal", models, err)
			}
			if provider.reads() != 0 {
				t.Fatalf("a refused route still reached the provider %d times", provider.reads())
			}
		})
	}
}

func TestMediaCatalogRouteFollowsARouteSwitchWithoutARestart(t *testing.T) {
	provider := newFakeOpenRouter(t)
	runtime := routeRuntime("llamacpp", "http://aura-llm:8084/v1")
	route := mediaCatalogRoute{catalog: mediagen.NewCatalog(provider.client), runtime: runtime}

	if _, err := route.List(context.Background(), mediagen.KindVideo, false); !errors.Is(err, agui.ErrCatalogLocalRoute) {
		t.Fatalf("List on the local route = %v, want the refusal", err)
	}
	runtime.Replace(nil, llm.Config{Provider: "openrouter", BaseURL: openRouterBaseURL})
	if _, err := route.List(context.Background(), mediagen.KindVideo, false); err != nil {
		t.Fatalf("List after switching to OpenRouter = %v, want the catalogue", err)
	}
}

func TestWireMediaCatalogMountsTheSharedCatalogOnlyWhenMediaIsServed(t *testing.T) {
	provider := newFakeOpenRouter(t)
	chat := &chatEnv{llmRuntime: routeRuntime("openrouter", openRouterBaseURL)}
	for _, tc := range []struct {
		name  string
		media *mediaDeps
		want  int
	}{
		{"without media dependencies", nil, http.StatusServiceUnavailable},
		{"with the shared catalog", &mediaDeps{catalog: mediagen.NewCatalog(provider.client)}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := agui.NewServer(nil, nil, agui.ServerConfig{})
			wireMediaCatalog(server, chat, tc.media)
			rec := httptest.NewRecorder()
			server.Mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/settings/video-models", nil))
			if rec.Code != tc.want {
				t.Fatalf("GET /api/settings/video-models = %d (%s), want %d", rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}

// governanceReadOnly may read governance but not write it: the media catalogues are mounted
// behind governance.write like llm-models, so it must be refused.
type governanceReadOnly struct{ id string }

func (g governanceReadOnly) GetIdentityByID(ctx context.Context, id string) (agui.Identity, error) {
	return wiringIdentities(g).GetIdentityByID(ctx, id)
}

func (g governanceReadOnly) HasCapability(_ context.Context, id, capability string) (bool, error) {
	return id == g.id && capability == governanceReadCapability, nil
}

func TestMediaModelRoutesRequireGovernanceWrite(t *testing.T) {
	var hits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.RequestURI())
		_, _ = io.WriteString(w, `{"models":[]}`)
	})
	for _, tc := range []struct {
		name       string
		identities aguiIdentityStore
		wantCode   int
	}{
		{"no capability", uncapableIdentities{id: mediaAdminIdentity}, http.StatusForbidden},
		{"governance.read only", governanceReadOnly{id: mediaAdminIdentity}, http.StatusForbidden},
		{"governance.write", wiringIdentities{id: mediaAdminIdentity}, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := newServeHandler(aguiHandler, authulaTestDeps(mediaAdminIdentity, tc.identities), &fakeAuthulaProvider{})
			if err != nil {
				t.Fatalf("newServeHandler: %v", err)
			}
			for _, target := range []string{"/api/settings/image-models", "/api/settings/video-models?refresh=1"} {
				hits = nil
				req := httptest.NewRequest(http.MethodGet, target, nil)
				addAuthulaSession(req)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)
				wantHits := 0
				if tc.wantCode == http.StatusOK {
					wantHits = 1
				}
				if rec.Code != tc.wantCode || len(hits) != wantHits || (wantHits == 1 && hits[0] != target) {
					t.Fatalf("GET %s = %d with hits %v, want %d reaching the AG-UI handler %d time(s)",
						target, rec.Code, hits, tc.wantCode, wantHits)
				}
			}
		})
	}
}

// mediaSettingsStore is the aura.settings table in memory: the Settings API writes it and
// the generation tools' settings port reads it, exactly as both share one store in serve.
type mediaSettingsStore struct {
	mu   sync.Mutex
	rows map[string]string
}

func (s *mediaSettingsStore) List(context.Context) ([]sqlc.AuraSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := make([]sqlc.AuraSettings, 0, len(s.rows))
	for key, value := range s.rows {
		rows = append(rows, sqlc.AuraSettings{Key: key, Value: value})
	}
	return rows, nil
}

func (s *mediaSettingsStore) Upsert(_ context.Context, key, value, _ string) (sqlc.AuraSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rows[key] = value
	return sqlc.AuraSettings{Key: key, Value: value}, nil
}

func (s *mediaSettingsStore) ReplaceMany(ctx context.Context, values map[string]string, deletes []string, by string) ([]sqlc.AuraSettings, error) {
	for _, key := range deletes {
		_ = s.Delete(ctx, key)
	}
	rows := make([]sqlc.AuraSettings, 0, len(values))
	for key, value := range values {
		row, _ := s.Upsert(ctx, key, value, by)
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *mediaSettingsStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, key)
	return nil
}

// mediaSettingsAdmins answers the settings API's admin check: only the admin identity holds
// identity.create; the member holds governance.write alone, like every identity (D-01).
type mediaSettingsAdmins struct{}

func (mediaSettingsAdmins) ListIdentities(context.Context) ([]identity.Identity, error) {
	return []identity.Identity{{ID: mediaAdminIdentity}, {ID: mediaMemberIdentity}}, nil
}

func (mediaSettingsAdmins) GetIdentityByID(_ context.Context, id string) (identity.Identity, error) {
	return identity.Identity{ID: id}, nil
}

func (mediaSettingsAdmins) ListCapabilities(_ context.Context, id string) ([]string, error) {
	if id == mediaAdminIdentity {
		return []string{identity.CapIdentityCreate, identity.CapGovernanceWrite}, nil
	}
	return []string{identity.CapGovernanceWrite}, nil
}

func (mediaSettingsAdmins) GrantCapability(context.Context, string, string) error {
	return errors.New("not granted by these tests")
}

func (mediaSettingsAdmins) RevokeCapability(context.Context, string, string) error {
	return errors.New("not revoked by these tests")
}

func (a mediaSettingsAdmins) HasCapability(ctx context.Context, id, capability string) (bool, error) {
	caps, err := a.ListCapabilities(ctx, id)
	return slices.Contains(caps, capability), err
}

// settingsDaemon is one daemon instance's settings surface: the served handler for principal
// and the media settings port the tools were built with, over the same store.
func settingsDaemon(t *testing.T, principal string, store *mediaSettingsStore) (http.Handler, mediagen.Settings) {
	t.Helper()
	server := agui.NewServer(nil, nil, agui.ServerConfig{})
	server.SetSettingsStore(store)
	server.SetIdentityAdmin(mediaSettingsAdmins{})
	handler, err := newServeHandler(server.Mux(), authulaTestDeps(principal, wiringIdentities{id: principal}), &fakeAuthulaProvider{})
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}
	return handler, newMediaSettings(store)
}

func sendSetting(t *testing.T, handler http.Handler, method, key, value string) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if method == http.MethodPut {
		encoded, _ := json.Marshal(map[string]string{"value": value})
		body = strings.NewReader(string(encoded))
	}
	req := httptest.NewRequest(method, "/api/settings/"+key, body)
	addAuthulaSession(req)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestMemberWithGovernanceWriteCannotChangeTheMediaModels(t *testing.T) {
	store := &mediaSettingsStore{rows: map[string]string{videoModelSettingKey: "google/veo-4"}}
	handler, port := settingsDaemon(t, mediaMemberIdentity, store)

	for _, tc := range []struct{ method, key string }{
		{http.MethodPut, imageModelSettingKey},
		{http.MethodPut, videoModelSettingKey},
		{http.MethodDelete, imageModelSettingKey},
		{http.MethodDelete, videoModelSettingKey},
	} {
		// The member passes the governance.write mount; the refusal must come from the settings
		// API's admin check behind it.
		rec := sendSetting(t, handler, tc.method, tc.key, "vendor/member-choice")
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "only an admin") {
			t.Fatalf("%s %s by a member = %d (%s), want the admin-only 403", tc.method, tc.key, rec.Code, rec.Body.String())
		}
	}
	image, err := port.Model(context.Background(), mediagen.KindImage)
	if err != nil || image != mediagen.DefaultImageModel {
		t.Fatalf("image model = %q, %v, want the untouched default", image, err)
	}
	video, err := port.Model(context.Background(), mediagen.KindVideo)
	if err != nil || video != "google/veo-4" {
		t.Fatalf("video model = %q, %v, want the admin's earlier choice", video, err)
	}
}

func TestAdminMediaModelChangeReachesTheNextToolCall(t *testing.T) {
	store := &mediaSettingsStore{rows: map[string]string{}}
	handler, port := settingsDaemon(t, mediaAdminIdentity, store)
	ctx := context.Background()

	if rec := sendSetting(t, handler, http.MethodPut, imageModelSettingKey, "black-forest-labs/flux-3-pro"); rec.Code != http.StatusOK {
		t.Fatalf("PUT image model = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if rec := sendSetting(t, handler, http.MethodPut, videoModelSettingKey, "google/veo-4"); rec.Code != http.StatusOK {
		t.Fatalf("PUT video model = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if image, err := port.Model(ctx, mediagen.KindImage); err != nil || image != "black-forest-labs/flux-3-pro" {
		t.Fatalf("next image call model = %q, %v, want the saved choice with no restart", image, err)
	}
	if video, err := port.Model(ctx, mediagen.KindVideo); err != nil || video != "google/veo-4" {
		t.Fatalf("next video call model = %q, %v, want the saved choice with no restart", video, err)
	}

	if rec := sendSetting(t, handler, http.MethodDelete, imageModelSettingKey, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE image model = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if image, err := port.Model(ctx, mediagen.KindImage); err != nil || image != mediagen.DefaultImageModel {
		t.Fatalf("image model after reset = %q, %v, want the default again", image, err)
	}
}
