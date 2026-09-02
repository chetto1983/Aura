package main

// What changes when more than one authorization server is trusted. These tests exist
// in both directions on purpose: a trusted issuer must get in, and everything that
// merely LOOKS like one must not.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/chetto1983/aura/internal/arcadedb"
)

const testResource = "http://memory.example/mcp/"

// twoIssuerConfig trusts `home` first and `foreign` second — the order the
// protected-resource metadata and the identity rule both read.
func twoIssuerConfig(home, foreign *arcadeAuthFixture) oauthResourceConfig {
	return oauthResourceConfig{
		Issuers: []trustedIssuer{
			{Issuer: home.issuer, JWKSURL: home.server.URL},
			{Issuer: foreign.issuer, JWKSURL: foreign.server.URL},
		},
		Resource: testResource,
		Scope:    defaultOAuthScope,
	}
}

func verifyToken(t *testing.T, config oauthResourceConfig, raw string) (string, error) {
	t.Helper()
	verifier := newArcadeTokenVerifier(config, http.DefaultClient)
	info, err := verifier.Verify(context.Background(), raw,
		httptest.NewRequest(http.MethodPost, testResource, nil))
	if err != nil {
		return "", err
	}
	return info.UserID, nil
}

// The whole point of the change, stated as the attack it prevents: a trusted foreign
// issuer asserts a `sub` equal to somebody's Aura identity. RFC 7519 §4.1.2 promises
// `sub` is unique only within ONE issuer's namespace, so nothing stops a foreign
// account from being named after an Aura one — and keying the tenant on `sub` alone
// would hand it that person's memory.
func TestTwoIssuersSharingASubjectReachDifferentMemories(t *testing.T) {
	home := newArcadeAuthFixtureFor(t, "https://home.example")
	foreign := newArcadeAuthFixtureFor(t, "https://foreign.example")
	config := twoIssuerConfig(home, foreign)
	const subject = "38d43554-e7d7-4ac5-868a-d1efa9299e24"

	expiry := time.Now().Add(time.Hour)
	fromHome, err := verifyToken(t, config, home.token(t, testResource, subject, defaultOAuthScope, expiry))
	if err != nil {
		t.Fatalf("home issuer was refused: %v", err)
	}
	fromForeign, err := verifyToken(t, config, foreign.token(t, testResource, subject, defaultOAuthScope, expiry))
	if err != nil {
		t.Fatalf("trusted foreign issuer was refused: %v", err)
	}
	if fromHome == fromForeign {
		t.Fatalf("a foreign account named %q reached the Aura identity of the same name", subject)
	}
	// And both must be usable as tenants, or "different" would just mean "broken".
	for name, identity := range map[string]string{"home": fromHome, "foreign": fromForeign} {
		if _, err := arcadedb.DatabaseFor(identity); err != nil {
			t.Fatalf("%s identity %q is not a usable tenant: %v", name, identity, err)
		}
	}
}

// The home issuer's subjects are Aura identity UUIDs and every existing mem_<uuid>
// database is named after one. If this ever stops passing through verbatim, widening
// the trusted set silently orphans every memory already written.
func TestHomeSubjectsKeepTheDatabaseTheyAlreadyHave(t *testing.T) {
	home := newArcadeAuthFixtureFor(t, "https://home.example")
	foreign := newArcadeAuthFixtureFor(t, "https://foreign.example")
	config := twoIssuerConfig(home, foreign)
	const identity = "38d43554-e7d7-4ac5-868a-d1efa9299e24"

	got, err := verifyToken(t, config,
		home.token(t, testResource, identity, defaultOAuthScope, time.Now().Add(time.Hour)))
	if err != nil {
		t.Fatalf("home issuer was refused: %v", err)
	}
	if got != identity {
		t.Fatalf("home identity = %q, want the subject unchanged (%q)", got, identity)
	}
	database, err := arcadedb.DatabaseFor(got)
	if err != nil {
		t.Fatal(err)
	}
	if database != "mem_38d43554_e7d7_4ac5_868a_d1efa9299e24" {
		t.Fatalf("database = %q — an existing tenant would be orphaned", database)
	}
}

// A correctly signed token from an issuer nobody trusts is still a refusal. Its keys
// are fetchable and its signature is valid; being unlisted is the whole objection.
func TestAnUnlistedIssuerIsRefusedEvenWhenItsSignatureIsGood(t *testing.T) {
	home := newArcadeAuthFixtureFor(t, "https://home.example")
	stranger := newArcadeAuthFixtureFor(t, "https://stranger.example")
	config := oauthResourceConfig{
		Issuers:  []trustedIssuer{{Issuer: home.issuer, JWKSURL: home.server.URL}},
		Resource: testResource,
		Scope:    defaultOAuthScope,
	}

	_, err := verifyToken(t, config,
		stranger.token(t, testResource, "someone", defaultOAuthScope, time.Now().Add(time.Hour)))
	if err == nil {
		t.Fatal("a token from an unlisted issuer was accepted")
	}
	if !strings.Contains(err.Error(), "https://stranger.example") {
		t.Fatalf("the refusal does not name the rejected issuer: %v", err)
	}
	if stranger.calls.Load() != 0 {
		t.Fatalf("the stranger's JWKS was fetched %d times — an unlisted issuer must never be dialled",
			stranger.calls.Load())
	}
}

// The issuer is read unverified to pick a key set. That read must buy nothing else:
// a token that CLAIMS the home issuer but was signed by the foreign one has to fail,
// or the trusted-issuer list is decoration.
func TestClaimingAnIssuerIsNotBeingOne(t *testing.T) {
	home := newArcadeAuthFixtureFor(t, "https://home.example")
	foreign := newArcadeAuthFixtureFor(t, "https://foreign.example")
	config := twoIssuerConfig(home, foreign)

	// Signed with the foreign key, but its `iss` names the home issuer.
	forged := foreign.tokenIssuedAs(t, home.issuer, testResource, "someone",
		defaultOAuthScope, time.Now().Add(time.Hour))
	if _, err := verifyToken(t, config, forged); err == nil {
		t.Fatal("a token signed by the wrong issuer's key was accepted")
	}
}

// A foreign identity is a UUID (the tenant machinery is specified over UUIDs) and it
// is stable: the same person coming back must find the same memory, not a new one.
func TestForeignIdentitiesAreStableUUIDs(t *testing.T) {
	config := oauthResourceConfig{Issuers: []trustedIssuer{{Issuer: "https://home.example"}}}
	first := config.tenantIdentity("https://foreign.example", "1043")
	second := config.tenantIdentity("https://foreign.example", "1043")
	if first != second {
		t.Fatalf("the same caller derived two identities: %q then %q", first, second)
	}
	if _, err := uuid.Parse(first); err != nil {
		t.Fatalf("foreign identity %q is not a UUID: %v", first, err)
	}
	// The separator must not be forgeable: (a, b) and (a+sep, b) are different people.
	if config.tenantIdentity("https://foreign.example\n1043", "") != "" {
		t.Fatal("an empty subject produced an identity")
	}
	if config.tenantIdentity("https://foreign.example", "1043") ==
		config.tenantIdentity("https://foreign.example", "10430") {
		t.Fatal("two subjects collided")
	}
}

func TestTrustedIssuersAreReadFromTheEnvironment(t *testing.T) {
	t.Setenv("MCP_OAUTH_ISSUER", "https://home.example/")
	t.Setenv("MCP_OAUTH_JWKS_URL", "http://aura:9080/oauth/jwks")
	t.Setenv("MCP_OAUTH_TRUSTED_ISSUERS",
		" https://accounts.google.com=https://www.googleapis.com/oauth2/v3/certs , https://keycloak.example/realms/aura ,, ")

	got := oauthResourceConfigFromEnv()
	want := []trustedIssuer{
		// The home issuer keeps the split-horizon JWKS it is configured with: the
		// issuer is the name a client can reach, the JWKS the name this container can.
		{Issuer: "https://home.example", JWKSURL: "http://aura:9080/oauth/jwks"},
		{Issuer: "https://accounts.google.com", JWKSURL: "https://www.googleapis.com/oauth2/v3/certs"},
		// No `=`, so the default path applies — the same rule the home issuer follows.
		{Issuer: "https://keycloak.example/realms/aura", JWKSURL: "https://keycloak.example/realms/aura/oauth/jwks"},
	}
	if len(got.Issuers) != len(want) {
		t.Fatalf("issuers = %#v, want %d entries", got.Issuers, len(want))
	}
	for i, issuer := range want {
		if got.Issuers[i] != issuer {
			t.Fatalf("issuer %d = %#v, want %#v", i, got.Issuers[i], issuer)
		}
	}
}

// Clients discover where to authenticate from this document. Advertising only the home
// issuer would leave every other trusted account unreachable in practice.
func TestProtectedResourceMetadataAdvertisesEveryTrustedIssuer(t *testing.T) {
	config := oauthResourceConfig{
		Issuers: []trustedIssuer{
			{Issuer: "https://home.example"},
			{Issuer: "https://accounts.google.com"},
		},
		Resource: testResource,
		Scope:    defaultOAuthScope,
	}
	rec := httptest.NewRecorder()
	arcadeProtectedResourceMetadata(config).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "http://memory.example/.well-known/oauth-protected-resource/mcp/", nil))
	body := rec.Body.String()
	for _, want := range config.issuerNames() {
		if !strings.Contains(body, want) {
			t.Fatalf("metadata does not advertise %q: %s", want, body)
		}
	}
}
