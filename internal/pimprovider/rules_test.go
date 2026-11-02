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
	m := App{Provider: Microsoft365, ClientID: "cid", TenantID: "contoso.onmicrosoft.com", ClientSecret: "never"}
	if want := map[string]string{"clientId": "cid", "tenantId": "contoso.onmicrosoft.com"}; !maps.Equal(m.ProviderConfig(), want) {
		t.Fatalf("microsoft365 config = %v, want %v", m.ProviderConfig(), want)
	}
}

func TestOutlookComAlwaysSignsInAsAPersonalAccount(t *testing.T) {
	want := map[string]string{"clientId": "cid", "tenantId": "consumers"}
	for _, saved := range []string{"", "common", "consumer", "consumers"} {
		a := App{Provider: OutlookCom, ClientID: "cid", TenantID: saved}
		if got := a.ProviderConfig(); !maps.Equal(got, want) {
			t.Errorf("a row saved with tenant %q gives %v, want %v", saved, got, want)
		}
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
		{"outlook ok", App{Provider: OutlookCom, ClientID: "cid"}, nil, true},
		{"outlook with tenant", App{Provider: OutlookCom, ClientID: "cid", TenantID: "consumers"}, nil, false},
		{"outlook with secret", App{Provider: OutlookCom, ClientID: "cid", ClientSecret: "x"}, nil, false},
		{"m365 directory id", App{Provider: Microsoft365, ClientID: "cid", TenantID: "aaaabbbb-0000-cccc-1111-dddd2222eeee"}, nil, true},
		{"m365 domain", App{Provider: Microsoft365, ClientID: "cid", TenantID: "contoso.onmicrosoft.com"}, nil, true},
		{"m365 any work account", App{Provider: Microsoft365, ClientID: "cid", TenantID: "organizations"}, nil, true},
		{"m365 any account", App{Provider: Microsoft365, ClientID: "cid", TenantID: "common"}, nil, true},
		{"m365 personal accounts", App{Provider: Microsoft365, ClientID: "cid", TenantID: "consumers"}, nil, true},
		{"m365 ignores case", App{Provider: Microsoft365, ClientID: "cid", TenantID: "Organizations"}, nil, true},
		{"m365 misspelt, measured as AADSTS900023", App{Provider: Microsoft365, ClientID: "cid", TenantID: "consumer"}, nil, false},
		{"m365 a bare word", App{Provider: Microsoft365, ClientID: "cid", TenantID: "contoso"}, nil, false},
		{"m365 a short id", App{Provider: Microsoft365, ClientID: "cid", TenantID: "aaaabbbb-0000-cccc-1111"}, nil, false},
		{"m365 a url", App{Provider: Microsoft365, ClientID: "cid", TenantID: "https://login.microsoftonline.com/contoso.com"}, nil, false},
		{"m365 a dot alone", App{Provider: Microsoft365, ClientID: "cid", TenantID: "."}, nil, false},
		{"m365 without tenant", App{Provider: Microsoft365, ClientID: "cid"}, nil, false},
		{"m365 with secret", App{Provider: Microsoft365, ClientID: "cid", TenantID: "organizations", ClientSecret: "x"}, nil, false},
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
