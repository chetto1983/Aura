package pimprovider

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

func TestCanonicalIsByteExact(t *testing.T) {
	for _, p := range []string{"google", "microsoft365", "outlook.com", "imap", "ics", "json"} {
		if !Canonical(p) {
			t.Errorf("Canonical(%q) = false", p)
		}
	}
	for _, p := range []string{"Google", "GOOGLE", " google", "google ", "Outlook.com", "", "m365"} {
		if Canonical(p) {
			t.Errorf("Canonical(%q) = true; the sidecar folds case, so only the exact id may pass", p)
		}
	}
}

func TestManagedIsTheThreeOAuthProviders(t *testing.T) {
	if got := strings.Join(ManagedProviders(), ","); got != "google,microsoft365,outlook.com" {
		t.Fatalf("ManagedProviders() = %s", got)
	}
	for _, p := range []string{"imap", "ics", "json", "Google"} {
		if Managed(p) {
			t.Errorf("Managed(%q) = true", p)
		}
	}
}

func TestIsOwnedKeyIgnoresCase(t *testing.T) {
	for _, k := range []string{"clientId", "ClientId", "CLIENTSECRET", "tenantid"} {
		if !IsOwnedKey(k) {
			t.Errorf("IsOwnedKey(%q) = false", k)
		}
	}
	for _, k := range []string{"icsUrl", "clientIdx", ""} {
		if IsOwnedKey(k) {
			t.Errorf("IsOwnedKey(%q) = true", k)
		}
	}
}

func TestProviderConfigCarriesOnlyTheProvidersKeys(t *testing.T) {
	g := App{Provider: Google, ClientID: "cid", ClientSecret: "sec", TenantID: "ignored"}
	if want := map[string]string{"clientId": "cid", "clientSecret": "sec"}; !maps.Equal(g.ProviderConfig(), want) {
		t.Fatalf("google config = %v, want %v", g.ProviderConfig(), want)
	}
	m := App{Provider: OutlookCom, ClientID: "cid", TenantID: "consumers", ClientSecret: "never"}
	if want := map[string]string{"clientId": "cid", "tenantId": "consumers"}; !maps.Equal(m.ProviderConfig(), want) {
		t.Fatalf("outlook config = %v, want %v", m.ProviderConfig(), want)
	}
}

func TestValidate(t *testing.T) {
	storedGoogle := &App{Provider: Google, ClientID: "cid", ClientSecret: "sec", SecretSet: true}
	for _, tc := range []struct {
		name   string
		next   App
		stored *App
		ok     bool
	}{
		{"google first save with secret", App{Provider: Google, ClientID: "cid", ClientSecret: "sec"}, nil, true},
		{"google first save without secret", App{Provider: Google, ClientID: "cid"}, nil, false},
		{"google same client keeps secret", App{Provider: Google, ClientID: "cid"}, storedGoogle, true},
		{"google new client without secret", App{Provider: Google, ClientID: "other"}, storedGoogle, false},
		{"google new client with secret", App{Provider: Google, ClientID: "other", ClientSecret: "s2"}, storedGoogle, true},
		{"google with tenant", App{Provider: Google, ClientID: "cid", ClientSecret: "sec", TenantID: "t"}, nil, false},
		{"google empty client", App{Provider: Google, ClientSecret: "sec"}, nil, false},
		{"outlook ok", App{Provider: OutlookCom, ClientID: "cid", TenantID: "consumers"}, nil, true},
		{"m365 ok", App{Provider: Microsoft365, ClientID: "cid", TenantID: "t"}, nil, true},
		{"microsoft without tenant", App{Provider: Microsoft365, ClientID: "cid"}, nil, false},
		{"microsoft with secret", App{Provider: OutlookCom, ClientID: "cid", TenantID: "consumers", ClientSecret: "x"}, nil, false},
		{"unmanaged", App{Provider: "imap", ClientID: "cid"}, nil, false},
	} {
		err := Validate(tc.next, tc.stored)
		if tc.ok && err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
		}
		if !tc.ok && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", tc.name, err)
		}
	}
}
