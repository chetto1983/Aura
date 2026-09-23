package agui

import (
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/pimprovider"
)

func postAccount(t *testing.T, srv *httptest.Server, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/connect/pim/accounts", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST accounts: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func forwardedConfig(t *testing.T, p *fakePIM) (map[string]json.RawMessage, map[string]string) {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(p.gotBody), &fields); err != nil {
		t.Fatalf("forwarded body %q: %v", p.gotBody, err)
	}
	var config map[string]string
	if err := json.Unmarshal(fields["providerConfig"], &config); err != nil {
		t.Fatalf("forwarded providerConfig: %v", err)
	}
	return fields, config
}

func TestPIMCreateInjectsTheGoogleAppAndDropsBrowserKeysInAnyCase(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, &fakePIMApps{apps: googleApp()}, nil)
	defer srv.Close()

	in := `{"id":"work","displayName":"Work","provider":"google","priority":3,
	  "providerConfig":{"clientId":"mine","ClientSecret":"mine","CLIENTSECRET":"x","TenantId":"t"}}`
	if code, raw := postAccount(t, srv, in); code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	fields, config := forwardedConfig(t, p)
	if want := map[string]string{"clientId": "g-cid", "clientSecret": "g-secret"}; !maps.Equal(config, want) {
		t.Fatalf("forwarded providerConfig = %v, want exactly %v", config, want)
	}
	if string(fields["priority"]) != "3" || string(fields["id"]) != `"work"` {
		t.Fatalf("other fields not kept: %s", p.gotBody)
	}
}

func TestPIMCreateInjectsTheMicrosoftAppWithoutASecretKey(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	apps := &fakePIMApps{apps: map[string]pimprovider.App{pimprovider.OutlookCom: {
		Provider: pimprovider.OutlookCom, ClientID: "ms-cid", TenantID: "consumers",
	}}}
	srv := connectPIMServerWithApps(sidecar.URL, apps, nil)
	defer srv.Close()

	if code, raw := postAccount(t, srv, `{"id":"home","displayName":"Home","provider":"outlook.com","providerConfig":{"clientSecret":"x"}}`); code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, raw)
	}
	_, config := forwardedConfig(t, p)
	if want := map[string]string{"clientId": "ms-cid", "tenantId": "consumers"}; !maps.Equal(config, want) {
		t.Fatalf("forwarded providerConfig = %v, want exactly %v", config, want)
	}
}

func TestPIMCreateRejectsANonCanonicalProvider(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, &fakePIMApps{apps: googleApp()}, nil)
	defer srv.Close()
	for _, provider := range []string{`"Google"`, `"GOOGLE"`, `" google"`, `""`, `null`, `7`} {
		body := `{"id":"x","displayName":"X","provider":` + provider + `,"providerConfig":{"clientId":"mine","clientSecret":"mine"}}`
		if code, _ := postAccount(t, srv, body); code != http.StatusBadRequest {
			t.Errorf("provider %s = %d, want 400", provider, code)
		}
	}
	if code, _ := postAccount(t, srv, `{"id":"x"}`); code != http.StatusBadRequest {
		t.Errorf("missing provider = %d, want 400", code)
	}
	if code, _ := postAccount(t, srv, `not json`); code != http.StatusBadRequest {
		t.Errorf("malformed body = %d, want 400", code)
	}
	if p.gotPath != "" {
		t.Fatalf("a rejected create reached the sidecar: %s", p.gotPath)
	}
}

func TestPIMCreateUnconfiguredManagedProviderIs409WithoutTheSidecar(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, &fakePIMApps{}, nil)
	defer srv.Close()
	code, raw := postAccount(t, srv, `{"id":"w","displayName":"W","provider":"microsoft365","providerConfig":{}}`)
	if code != http.StatusConflict || !strings.Contains(raw, `"provider_not_configured"`) {
		t.Fatalf("unconfigured = %d %s, want 409 provider_not_configured", code, raw)
	}
	if p.gotPath != "" {
		t.Fatal("an unconfigured create reached the sidecar")
	}
}

func TestPIMCreateManagedProviderWithoutStoreIs503(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, nil, nil)
	defer srv.Close()
	if code, _ := postAccount(t, srv, `{"id":"w","displayName":"W","provider":"google","providerConfig":{}}`); code != http.StatusServiceUnavailable {
		t.Fatalf("no store = %d, want 503", code)
	}
}

func TestPIMCreateUnmanagedProviderPassesThroughUnchanged(t *testing.T) {
	p := &fakePIM{}
	sidecar := httptest.NewServer(p.handler())
	defer sidecar.Close()
	srv := connectPIMServerWithApps(sidecar.URL, nil, nil)
	defer srv.Close()
	in := `{"id":"cal","displayName":"Cal","provider":"ics","providerConfig":{"icsUrl":"https://example.com/a.ics"}}`
	if code, raw := postAccount(t, srv, in); code != http.StatusCreated {
		t.Fatalf("ics create = %d %s", code, raw)
	}
	if p.gotBody != in {
		t.Fatalf("unmanaged body changed: %q", p.gotBody)
	}
}
