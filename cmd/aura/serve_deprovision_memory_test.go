package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/config"
)

// serve_deprovision_memory_test.go exercises the ArcadeDB memory purge against an
// httptest server rather than a live daemon: the arcadedb_integration tier is not
// in the coverage matrix, so the logic that decides whether an erasure was PROVEN
// has to be covered here or it is not covered at all.

const (
	testMemoryIdentityID = "22222222-2222-4222-8222-222222222222"
	testTenantSecret     = "0123456789abcdef0123456789abcdef" // 32 chars, test-only
	testArcadeAdminUser  = "root"
	testArcadeAdminPass  = "root-pw"
)

// fakeArcadeServer models the three endpoints the purge touches: the server
// command post, the existence read, and the auth-guarded readiness probe.
type fakeArcadeServer struct {
	mu        sync.Mutex
	databases map[string]bool
	users     map[string]string // user -> password
	commands  []string

	// ignoreDropDatabase / ignoreDropUser answer the command 200 while changing
	// nothing — a server that says yes and does nothing, which is precisely what
	// the postcondition assertions exist to catch.
	ignoreDropDatabase bool
	ignoreDropUser     bool
	existsStatus       int    // non-zero overrides the computed 200
	existsBody         string // non-empty replaces the computed body
}

func (f *fakeArcadeServer) recordedCommands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func (f *fakeArcadeServer) authorized(r *http.Request) bool {
	raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Basic ")
	if !ok {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return false
	}
	user, password, ok := strings.Cut(string(decoded), ":")
	if !ok {
		return false
	}
	if user == testArcadeAdminUser {
		return password == testArcadeAdminPass
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, live := f.users[user]
	return live && stored == password
}

func (f *fakeArcadeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !f.authorized(r) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"unauthorized"}`)
		return
	}
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/server":
		f.serveCommand(w, r)
	case r.URL.Path == "/api/v1/ready":
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(r.URL.Path, "/api/v1/exists/"):
		f.serveExists(w, strings.TrimPrefix(r.URL.Path, "/api/v1/exists/"))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (f *fakeArcadeServer) serveCommand(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Command string `json:"command"`
	}
	_ = json.NewDecoder(r.Body).Decode(&payload)
	f.mu.Lock()
	f.commands = append(f.commands, payload.Command)
	name, isDropDB := strings.CutPrefix(payload.Command, "drop database ")
	user, isDropUser := strings.CutPrefix(payload.Command, "drop user ")
	var refuse string
	switch {
	case isDropDB && !f.databases[name]:
		refuse = "Database '" + name + "' not found"
	case isDropDB && !f.ignoreDropDatabase:
		delete(f.databases, name)
	case isDropUser:
		if _, live := f.users[user]; !live {
			refuse = "User '" + user + "' not found on server"
		} else if !f.ignoreDropUser {
			delete(f.users, user)
		}
	}
	f.mu.Unlock()
	if refuse != "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"`+refuse+`"}`)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, `{"result":"ok"}`)
}

func (f *fakeArcadeServer) serveExists(w http.ResponseWriter, name string) {
	f.mu.Lock()
	present := f.databases[name]
	status, body := f.existsStatus, f.existsBody
	f.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	if body == "" {
		body = `{"result":false}`
		if present {
			body = `{"result":true}`
		}
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// newArcadeMemoryPurger stands the fake server up with one identity already
// provisioned and returns the adapter under test alongside its derived names.
func newArcadeMemoryPurger(t *testing.T, fake *fakeArcadeServer) (arcadeMemoryPurgeAdapter, string, string) {
	t.Helper()
	database, err := arcadedb.DatabaseFor(testMemoryIdentityID)
	if err != nil {
		t.Fatalf("DatabaseFor: %v", err)
	}
	user := arcadedb.TenantUserFor(database)

	t.Setenv("AURA_ARCADEDB_TENANT_SECRET", testTenantSecret)
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("NewTenantCredentials: %v", err)
	}
	if fake.databases == nil {
		fake.databases = map[string]bool{database: true}
	}
	if fake.users == nil {
		fake.users = map[string]string{user: credentials.PasswordFor(database)}
	}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	base := arcadedb.Config{BaseURL: srv.URL, Database: "aura_memory"}
	adminCfg := base
	adminCfg.User = testArcadeAdminUser
	adminCfg.Password = testArcadeAdminPass
	admin, err := arcadedb.New(adminCfg)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	return arcadeMemoryPurgeAdapter{admin: admin, base: base, credentials: credentials}, database, user
}

func TestArcadeMemoryPurgeDropsTheDatabaseAndRevokesTheCredential(t *testing.T) {
	fake := &fakeArcadeServer{}
	purger, database, user := newArcadeMemoryPurger(t, fake)

	if err := purger.PurgeMemory(context.Background(), testMemoryIdentityID); err != nil {
		t.Fatalf("PurgeMemory: %v", err)
	}
	want := []string{"drop database " + database, "drop user " + user}
	got := fake.recordedCommands()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("server commands = %v, want %v", got, want)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.databases[database] {
		t.Fatal("the memory database survived the purge")
	}
	if _, live := fake.users[user]; live {
		t.Fatal("the tenant credential survived the purge")
	}
}

// The saga journal re-runs a step that failed, so a purge that already finished
// must converge rather than fail forever on a drop the server now refuses.
func TestArcadeMemoryPurgeIsIdempotentOverAnAlreadyPurgedIdentity(t *testing.T) {
	fake := &fakeArcadeServer{databases: map[string]bool{}, users: map[string]string{}}
	purger, _, _ := newArcadeMemoryPurger(t, fake)

	if err := purger.PurgeMemory(context.Background(), testMemoryIdentityID); err != nil {
		t.Fatalf("re-run over an already-purged identity: %v", err)
	}
	if len(fake.recordedCommands()) != 2 {
		t.Fatalf("commands = %v, want both drops attempted", fake.recordedCommands())
	}
}

func TestArcadeMemoryPurgeFailsClosedWhenErasureIsNotProven(t *testing.T) {
	tests := []struct {
		name     string
		fake     *fakeArcadeServer
		wantText string
	}{
		{
			// The drop is answered 200 and the database is still there: an exit code
			// is not an erasure proof, and this is the case that says so.
			name:     "database survives a successful drop",
			fake:     &fakeArcadeServer{ignoreDropDatabase: true},
			wantText: "still holds it",
		},
		{
			name:     "credential survives a successful drop",
			fake:     &fakeArcadeServer{ignoreDropUser: true},
			wantText: "still accepts it",
		},
		{
			// A server that cannot answer must never read as a purge.
			name:     "existence check is refused",
			fake:     &fakeArcadeServer{existsStatus: http.StatusInternalServerError, existsBody: `{"error":"boom"}`},
			wantText: "verify memory database",
		},
		{
			// A 200 with no `result` is an unreadable answer, not a "false".
			name:     "existence check answers without a result",
			fake:     &fakeArcadeServer{existsBody: `{}`},
			wantText: "carries no result",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			purger, _, _ := newArcadeMemoryPurger(t, tt.fake)
			err := purger.PurgeMemory(context.Background(), testMemoryIdentityID)
			if err == nil {
				t.Fatal("an unproven erasure was reported as a successful purge")
			}
			if !strings.Contains(err.Error(), tt.wantText) {
				t.Fatalf("error = %q, want it to mention %q", err, tt.wantText)
			}
		})
	}
}

// An unreachable server is the difference between "refused" and "unknown". It must
// fail, because a down server would otherwise certify every purge.
func TestArcadeMemoryPurgeFailsClosedOnAnUnreachableServer(t *testing.T) {
	fake := &fakeArcadeServer{}
	purger, _, _ := newArcadeMemoryPurger(t, fake)
	srv := httptest.NewServer(fake)
	srv.Close() // a listener that is definitely gone
	unreachable, err := arcadedb.New(arcadedb.Config{
		BaseURL: srv.URL, Database: "aura_memory", User: testArcadeAdminUser, Password: testArcadeAdminPass,
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	purger.admin = unreachable

	if err := purger.PurgeMemory(context.Background(), testMemoryIdentityID); err == nil {
		t.Fatal("an unreachable server was reported as a successful purge")
	}
}

func TestArcadeMemoryPurgeRefusesAnUnusableIdentityOrConfig(t *testing.T) {
	fake := &fakeArcadeServer{}
	purger, _, _ := newArcadeMemoryPurger(t, fake)

	if err := purger.PurgeMemory(context.Background(), "not-a-uuid"); err == nil {
		t.Fatal("a non-identity was accepted")
	}
	if len(fake.recordedCommands()) != 0 {
		t.Fatalf("a non-identity reached the server: %v", fake.recordedCommands())
	}
	if err := (arcadeMemoryPurgeAdapter{}).PurgeMemory(context.Background(), testMemoryIdentityID); err == nil {
		t.Fatal("an unconfigured purger reported a successful purge")
	}
}

// A missing credential must yield a NIL purger, never an inert one: the
// de-provisioning preflight refuses identity deletion on nil, whereas an inert
// purger would let the identity row go while its memory database survived.
func TestBuildArcadeMemoryPurgerIsNilWithoutItsCredentials(t *testing.T) {
	configured := config.ArcadeDBConfig{
		BaseURL:       "http://arcadedb:2480",
		Database:      "aura_memory",
		AdminUser:     testArcadeAdminUser,
		AdminPassword: testArcadeAdminPass,
	}
	tests := []struct {
		name   string
		secret string
		mutate func(*config.ArcadeDBConfig)
	}{
		{name: "no admin user", secret: testTenantSecret, mutate: func(c *config.ArcadeDBConfig) { c.AdminUser = " " }},
		{name: "no admin password", secret: testTenantSecret, mutate: func(c *config.ArcadeDBConfig) { c.AdminPassword = "" }},
		{name: "no base URL", secret: testTenantSecret, mutate: func(c *config.ArcadeDBConfig) { c.BaseURL = "" }},
		{name: "no tenant secret", secret: "", mutate: func(*config.ArcadeDBConfig) {}},
		{name: "short tenant secret", secret: "too-short", mutate: func(*config.ArcadeDBConfig) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("AURA_ARCADEDB_TENANT_SECRET", tt.secret)
			server := configured
			tt.mutate(&server)
			if p := buildArcadeMemoryPurger(&config.Config{ArcadeDB: server}); p != nil {
				t.Fatalf("buildArcadeMemoryPurger = %v, want nil", p)
			}
		})
	}
	t.Run("fully configured", func(t *testing.T) {
		t.Setenv("AURA_ARCADEDB_TENANT_SECRET", testTenantSecret)
		if buildArcadeMemoryPurger(&config.Config{ArcadeDB: configured}) == nil {
			t.Fatal("a fully configured memory purger came back nil")
		}
	})
	if buildArcadeMemoryPurger(nil) != nil {
		t.Fatal("buildArcadeMemoryPurger(nil) is not nil")
	}
}
