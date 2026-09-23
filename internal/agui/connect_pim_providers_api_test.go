package agui

import (
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/pimprovider"
)

type fakePIMApps struct {
	apps      map[string]pimprovider.App
	getErr    error
	upsertErr error
	upserted  []pimprovider.App
	by        []string
}

func (f *fakePIMApps) List(context.Context) ([]pimprovider.App, error) {
	out := make([]pimprovider.App, 0, len(f.apps))
	for _, key := range slices.Sorted(maps.Keys(f.apps)) {
		app := f.apps[key]
		app.ClientSecret = ""
		out = append(out, app)
	}
	return out, nil
}

func (f *fakePIMApps) Get(_ context.Context, provider string) (pimprovider.App, error) {
	if f.getErr != nil {
		return pimprovider.App{}, f.getErr
	}
	app, ok := f.apps[provider]
	if !ok {
		return pimprovider.App{}, pimprovider.ErrNotConfigured
	}
	return app, nil
}

func (f *fakePIMApps) Upsert(_ context.Context, app pimprovider.App, updatedBy string) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = append(f.upserted, app)
	f.by = append(f.by, updatedBy)
	if f.apps == nil {
		f.apps = map[string]pimprovider.App{}
	}
	if app.ClientSecret == "" {
		app.ClientSecret = f.apps[app.Provider].ClientSecret
	}
	app.SecretSet = app.ClientSecret != ""
	f.apps[app.Provider] = app
	return nil
}

func connectPIMServerWithApps(baseURL string, apps pimProviderApps, admin identityAdmin) *httptest.Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if baseURL != "" {
		s.SetCalendarMCP(baseURL, "", staticMCPAccessTokenProvider{token: fakePIMToken})
	}
	if apps != nil {
		s.SetPIMProviderApps(apps)
	}
	if admin != nil {
		s.SetIdentityAdmin(admin)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.Mux().ServeHTTP(w, withPrincipal(r, fakePIMIdentity))
	}))
}

func adminOf(id string) identityAdmin {
	return &fakeIdentityAdmin{caps: map[string][]string{id: {identity.CapIdentityCreate}}}
}

func googleApp() map[string]pimprovider.App {
	return map[string]pimprovider.App{pimprovider.Google: {
		Provider: pimprovider.Google, ClientID: "g-cid", ClientSecret: "g-secret", SecretSet: true,
	}}
}

func getProviders(t *testing.T, srv *httptest.Server) (int, string, []map[string]any) {
	t.Helper()
	resp, err := http.Get(srv.URL + "/api/connect/pim/providers")
	if err != nil {
		t.Fatalf("GET providers: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var body struct {
		Providers []map[string]any `json:"providers"`
	}
	_ = json.Unmarshal(raw, &body)
	return resp.StatusCode, string(raw), body.Providers
}

func putProvider(t *testing.T, srv *httptest.Server, provider, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/api/connect/pim/providers/"+provider, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", provider, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestPIMProvidersMemberSeesOnlyConfigured(t *testing.T) {
	srv := connectPIMServerWithApps("", &fakePIMApps{apps: googleApp()}, &fakeIdentityAdmin{})
	defer srv.Close()
	code, raw, providers := getProviders(t, srv)
	if code != http.StatusOK || len(providers) != 3 {
		t.Fatalf("GET = %d %s", code, raw)
	}
	for i, want := range []string{"google", "microsoft365", "outlook.com"} {
		keys := slices.Sorted(maps.Keys(providers[i]))
		if providers[i]["provider"] != want || !slices.Equal(keys, []string{"configured", "provider"}) {
			t.Fatalf("member row %d = %v, want only provider+configured for %s", i, providers[i], want)
		}
	}
	if providers[0]["configured"] != true || providers[1]["configured"] != false {
		t.Fatalf("configured flags = %v", providers)
	}
	if strings.Contains(raw, "g-cid") || strings.Contains(raw, "g-secret") {
		t.Fatalf("member view leaks the client: %s", raw)
	}
}

func TestPIMProvidersAdminSeesClientButNeverSecret(t *testing.T) {
	srv := connectPIMServerWithApps("", &fakePIMApps{apps: googleApp()}, adminOf(fakePIMIdentity))
	defer srv.Close()
	_, raw, providers := getProviders(t, srv)
	g := providers[0]
	if g["clientId"] != "g-cid" || g["secretSet"] != true || g["redirectUri"] != PIMGoogleRelayRedirectURI {
		t.Fatalf("admin google row = %v", g)
	}
	if _, ok := providers[1]["redirectUri"]; ok {
		t.Fatalf("microsoft row carries a redirect URI: %v", providers[1])
	}
	if strings.Contains(raw, "g-secret") {
		t.Fatalf("admin view leaks the secret: %s", raw)
	}
}

func TestPIMProviderPutSavesAndStampsThePrincipal(t *testing.T) {
	apps := &fakePIMApps{}
	srv := connectPIMServerWithApps("", apps, adminOf(fakePIMIdentity))
	defer srv.Close()
	code, raw := putProvider(t, srv, "google", `{"clientId":" g-cid ","clientSecret":"g-secret"}`)
	if code != http.StatusOK || strings.Contains(raw, "g-secret") || !strings.Contains(raw, `"secretSet":true`) {
		t.Fatalf("PUT = %d %s", code, raw)
	}
	if len(apps.upserted) != 1 || apps.upserted[0].ClientID != "g-cid" || apps.by[0] != fakePIMIdentity {
		t.Fatalf("upserted %+v by %v", apps.upserted, apps.by)
	}
}

func TestPIMProviderPutSecretRules(t *testing.T) {
	apps := &fakePIMApps{apps: googleApp()}
	srv := connectPIMServerWithApps("", apps, adminOf(fakePIMIdentity))
	defer srv.Close()
	if code, raw := putProvider(t, srv, "google", `{"clientId":"new-cid"}`); code != http.StatusBadRequest || !strings.Contains(raw, "clientSecret") {
		t.Fatalf("new client without secret = %d %s, want 400 naming clientSecret", code, raw)
	}
	if code, _ := putProvider(t, srv, "google", `{"clientId":"g-cid"}`); code != http.StatusOK {
		t.Fatalf("same client without secret = %d, want 200", code)
	}
	if apps.apps["google"].ClientSecret != "g-secret" {
		t.Fatal("the stored secret was not kept")
	}
	if code, _ := putProvider(t, srv, "outlook.com", `{"clientId":"m","tenantId":"consumers","clientSecret":"x"}`); code != http.StatusBadRequest {
		t.Fatalf("microsoft with secret = %d, want 400", code)
	}
	if code, _ := putProvider(t, srv, "microsoft365", `{"clientId":"m"}`); code != http.StatusBadRequest {
		t.Fatalf("microsoft without tenant = %d, want 400", code)
	}
	if code, _ := putProvider(t, srv, "google", `{`); code != http.StatusBadRequest {
		t.Fatalf("malformed JSON = %d, want 400", code)
	}
}

func TestPIMProviderPutStaleKeepSecretIs409(t *testing.T) {
	apps := &fakePIMApps{apps: googleApp(), upsertErr: pimprovider.ErrStale}
	srv := connectPIMServerWithApps("", apps, adminOf(fakePIMIdentity))
	defer srv.Close()
	if code, raw := putProvider(t, srv, "google", `{"clientId":"g-cid"}`); code != http.StatusConflict {
		t.Fatalf("stale keep-secret save = %d %s, want 409", code, raw)
	}
}

// The admin and member views differ per caller, so no cache between them may keep one.
func TestPIMProvidersListIsNotCached(t *testing.T) {
	srv := connectPIMServerWithApps("", &fakePIMApps{apps: googleApp()}, adminOf(fakePIMIdentity))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/connect/pim/providers")
	if err != nil {
		t.Fatalf("GET providers: %v", err)
	}
	_ = resp.Body.Close()
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestPIMProviderPutUnknownProvider404(t *testing.T) {
	srv := connectPIMServerWithApps("", &fakePIMApps{}, adminOf(fakePIMIdentity))
	defer srv.Close()
	for _, p := range []string{"imap", "Google", "nope"} {
		if code, _ := putProvider(t, srv, p, `{"clientId":"c"}`); code != http.StatusNotFound {
			t.Errorf("PUT %s = %d, want 404", p, code)
		}
	}
}

func TestPIMProvidersWithoutStore503(t *testing.T) {
	srv := connectPIMServerWithApps("", nil, adminOf(fakePIMIdentity))
	defer srv.Close()
	if code, _, _ := getProviders(t, srv); code != http.StatusServiceUnavailable {
		t.Fatalf("GET without store = %d, want 503", code)
	}
	if code, _ := putProvider(t, srv, "google", `{"clientId":"c","clientSecret":"s"}`); code != http.StatusServiceUnavailable {
		t.Fatalf("PUT without store = %d, want 503", code)
	}
}
