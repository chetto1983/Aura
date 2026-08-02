package main

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
)

const graphSchemaTestSecret = "0123456789abcdef0123456789abcdef"

// graphSchemaProbe records the database path and credential every schema read arrives
// with — the two things per-identity isolation actually rests on.
type graphSchemaProbe struct {
	path string
	auth string
}

func newGraphSchemaProbe(t *testing.T, body string) (*httptest.Server, *graphSchemaProbe) {
	t.Helper()
	seen := &graphSchemaProbe{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.path = r.URL.Path
		seen.auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, seen
}

func graphSchemaReader(t *testing.T, baseURL string) arcadeTenantSchema {
	t.Helper()
	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", graphSchemaTestSecret)
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("NewTenantCredentials: %v", err)
	}
	return arcadeTenantSchema{base: arcadedb.Config{BaseURL: baseURL}, credentials: credentials}
}

// The identity picks the DATABASE, not a WHERE clause: the read must land on that
// identity's own database path.
func TestArcadeTenantSchemaReadsTheIdentitysOwnDatabase(t *testing.T) {
	srv, seen := newGraphSchemaProbe(t, `{"result":[{"name":"Entity","type":"vertex","records":3}]}`)
	identity := "6b3f8f0e-9c2a-4d4b-8f1a-2d5e7c9a1b34"

	got, err := graphSchemaReader(t, srv.URL).Schema(context.Background(), identity)
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	database, err := arcadedb.DatabaseFor(identity)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	if !strings.HasSuffix(seen.path, "/"+database) {
		t.Fatalf("read hit %q, want the identity's database %q", seen.path, database)
	}
	if len(got.Vertices) != 1 || got.Vertices[0].Name != "Entity" {
		t.Fatalf("catalogue = %+v", got)
	}
}

// It must bind as the TENANT user, not as the shared application credential: a shared
// credential allowed everywhere puts isolation back on our own code passing the right
// database name.
func TestArcadeTenantSchemaBindsAsTheTenantUser(t *testing.T) {
	srv, seen := newGraphSchemaProbe(t, `{"result":[]}`)
	identity := "6b3f8f0e-9c2a-4d4b-8f1a-2d5e7c9a1b34"

	if _, err := graphSchemaReader(t, srv.URL).Schema(context.Background(), identity); err != nil {
		t.Fatalf("Schema: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(seen.auth, "Basic "))
	if err != nil {
		t.Fatalf("decode credential: %v", err)
	}
	database, err := arcadedb.DatabaseFor(identity)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	user, _, found := strings.Cut(string(raw), ":")
	if !found || user != arcadedb.TenantUserFor(database) {
		t.Fatalf("bound as %q, want the tenant user %q", user, arcadedb.TenantUserFor(database))
	}
}

// An absent or malformed identity is not "the shared database" — it is a refusal. That is
// arcadedb.DatabaseFor's fail-closed contract and the route must inherit it.
func TestArcadeTenantSchemaRefusesAnAbsentIdentity(t *testing.T) {
	srv, seen := newGraphSchemaProbe(t, `{"result":[]}`)
	reader := graphSchemaReader(t, srv.URL)
	for _, identity := range []string{"", "not-a-uuid"} {
		if _, err := reader.Schema(context.Background(), identity); err == nil {
			t.Fatalf("identity %q was accepted; it must fail closed", identity)
		}
	}
	if seen.path != "" {
		t.Fatalf("an unscoped read reached the server at %q", seen.path)
	}
}

func TestBuildArcadeGraphViewNeedsAServerAndASecret(t *testing.T) {
	t.Run("no server configured", func(t *testing.T) {
		t.Setenv("AURA_ARCADEDB_TENANT_SECRET", graphSchemaTestSecret)
		if view := buildArcadeGraphView(&config.Config{}); view != nil {
			t.Fatal("expected nil so the routes answer 503")
		}
	})
	t.Run("no tenant secret", func(t *testing.T) {
		t.Setenv("AURA_ARCADEDB_TENANT_SECRET", "")
		cfg := &config.Config{ArcadeDB: config.ArcadeDBConfig{BaseURL: "http://127.0.0.1:2480"}}
		if view := buildArcadeGraphView(cfg); view != nil {
			t.Fatal("expected nil: without the derivation secret no tenant credential exists")
		}
	})
	t.Run("wired", func(t *testing.T) {
		t.Setenv("AURA_ARCADEDB_TENANT_SECRET", graphSchemaTestSecret)
		cfg := &config.Config{ArcadeDB: config.ArcadeDBConfig{BaseURL: "http://127.0.0.1:2480"}}
		if view := buildArcadeGraphView(cfg); view == nil {
			t.Fatal("expected a wired graph view")
		}
	})
}
