package arcadedb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

const resolverIdentity = "10000000-0000-0000-0000-000000000001"

type tenantHTTPRecorder struct {
	testing *testing.T
	server  *httptest.Server

	mu              sync.Mutex
	provisioned     map[string]bool
	databaseCalls   int
	userCalls       int
	tenantUsers     map[string]string
	tenantPasswords map[string]string
	blockSchema     <-chan struct{}
	schemaStarted   chan<- struct{}
}

func newTenantHTTPRecorder(t *testing.T) *tenantHTTPRecorder {
	t.Helper()
	recorder := &tenantHTTPRecorder{
		testing: t, provisioned: make(map[string]bool),
		tenantUsers: make(map[string]string), tenantPasswords: make(map[string]string),
	}
	recorder.server = httptest.NewServer(http.HandlerFunc(recorder.serveHTTP))
	t.Cleanup(recorder.server.Close)
	return recorder
}

func (r *tenantHTTPRecorder) serveHTTP(w http.ResponseWriter, request *http.Request) {
	if strings.HasPrefix(request.URL.Path, "/api/v1/command/") ||
		strings.HasPrefix(request.URL.Path, "/api/v1/query/") {
		database := strings.TrimPrefix(request.URL.Path, "/api/v1/command/")
		database = strings.TrimPrefix(database, "/api/v1/query/")
		if r.blockSchema != nil {
			select {
			case r.schemaStarted <- struct{}{}:
			default:
			}
			<-r.blockSchema
		}
		user, password, _ := request.BasicAuth()
		r.mu.Lock()
		ready := r.provisioned[database]
		r.tenantUsers[database] = user
		r.tenantPasswords[database] = password
		r.mu.Unlock()
		if !ready {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "database does not exist"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
		return
	}
	if request.URL.Path != "/api/v1/server" {
		http.NotFound(w, request)
		return
	}
	user, _, _ := request.BasicAuth()
	if user != "root" {
		r.testing.Errorf("server command user = %q, want root", user)
	}
	var payload struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		r.testing.Errorf("decode server command: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	switch {
	case strings.HasPrefix(payload.Command, "create database "):
		database := strings.TrimPrefix(payload.Command, "create database ")
		r.provisioned[database] = true
		r.databaseCalls++
	case strings.HasPrefix(payload.Command, "create user "):
		r.userCalls++
	default:
		r.testing.Errorf("unexpected server command %q", payload.Command)
	}
	r.mu.Unlock()
	_ = json.NewEncoder(w).Encode(map[string]any{"result": []any{}})
}

func (r *tenantHTTPRecorder) counts() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.databaseCalls, r.userCalls
}

func (r *tenantHTTPRecorder) config(database, user, password string) Config {
	return Config{BaseURL: r.server.URL, Database: database, User: user, Password: password}
}

func resolverCredentials() *TenantCredentials {
	return &TenantCredentials{secret: []byte(strings.Repeat("s", 32))}
}

func TestTenantClientsProvisionAndCacheAnIsolatedClient(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	credentials := resolverCredentials()
	admin, err := New(recorder.config("admin", "root", "root-password"))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	resolver := NewTenantClients(
		recorder.config("template", "shared", "shared-password"), admin, nil, credentials,
	)

	first, err := resolver.For(t.Context(), resolverIdentity)
	if err != nil {
		t.Fatalf("For: %v", err)
	}
	database, _ := DatabaseFor(resolverIdentity)
	beforeDatabase, beforeUser := recorder.counts()
	second, err := resolver.For(t.Context(), resolverIdentity)
	if err != nil {
		t.Fatalf("cached For: %v", err)
	}
	afterDatabase, afterUser := recorder.counts()
	if first != second || beforeDatabase != 1 || beforeUser != 1 ||
		afterDatabase != beforeDatabase || afterUser != beforeUser {
		t.Fatalf("cache/provision counts = %p %p, before=(%d,%d) after=(%d,%d)",
			first, second, beforeDatabase, beforeUser, afterDatabase, afterUser)
	}
	recorder.mu.Lock()
	user := recorder.tenantUsers[database]
	password := recorder.tenantPasswords[database]
	recorder.mu.Unlock()
	if user != TenantUserFor(database) || password != credentials.PasswordFor(database) {
		t.Fatalf("tenant credential = %q/%q", user, password)
	}
}

func TestTenantClientsFailClosedWithoutIdentityOrProvisioner(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	resolver := NewTenantClients(
		recorder.config("template", "shared", "shared-password"), nil, nil, resolverCredentials(),
	)
	if _, err := resolver.For(t.Context(), ""); err == nil {
		t.Fatal("empty identity accepted")
	}
	if _, err := resolver.For(t.Context(), resolverIdentity); err == nil {
		t.Fatal("unprovisioned tenant accepted without an admin")
	}
	if databaseCalls, userCalls := recorder.counts(); databaseCalls != 0 || userCalls != 0 {
		t.Fatalf("unexpected provisioning: database=%d user=%d", databaseCalls, userCalls)
	}
}

func TestTenantClientsUseAPreprovisionedDatabaseWithoutAdmin(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	database, _ := DatabaseFor(resolverIdentity)
	recorder.provisioned[database] = true
	resolver := NewTenantClients(
		recorder.config("template", "shared", "shared-password"), nil, nil, resolverCredentials(),
	)
	if _, err := resolver.For(t.Context(), resolverIdentity); err != nil {
		t.Fatalf("For preprovisioned tenant: %v", err)
	}
	if databaseCalls, userCalls := recorder.counts(); databaseCalls != 0 || userCalls != 0 {
		t.Fatalf("unexpected provisioning: database=%d user=%d", databaseCalls, userCalls)
	}
}

func TestTenantClientsRefuseSharedCredentials(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	resolver := NewTenantClients(
		recorder.config("template", "shared", "shared-password"), nil, nil, nil,
	)
	if _, err := resolver.For(t.Context(), resolverIdentity); err == nil ||
		!strings.Contains(err.Error(), "tenant credentials") {
		t.Fatalf("missing credential error = %v", err)
	}
}

func TestTenantClientsCoalesceConcurrentColdStarts(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	admin, err := New(recorder.config("admin", "root", "root-password"))
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	resolver := NewTenantClients(
		recorder.config("template", "shared", "shared-password"), admin, nil, resolverCredentials(),
	)

	const callers = 12
	start := make(chan struct{})
	errors := make(chan error, callers)
	var wait sync.WaitGroup
	for range callers {
		wait.Go(func() {
			<-start
			_, err := resolver.For(context.Background(), resolverIdentity)
			errors <- err
		})
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent For: %v", err)
		}
	}
	if databaseCalls, userCalls := recorder.counts(); databaseCalls != 1 || userCalls != 1 {
		t.Fatalf("provisioning counts = database=%d user=%d", databaseCalls, userCalls)
	}
}

func TestTenantClientsWaitingCallerHonorsContext(t *testing.T) {
	recorder := newTenantHTTPRecorder(t)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	recorder.blockSchema = release
	recorder.schemaStarted = started
	resolver := NewTenantClients(
		recorder.config("template", "shared", "shared-password"), nil, nil, resolverCredentials(),
	)
	firstDone := make(chan error, 1)
	go func() {
		_, err := resolver.For(context.Background(), resolverIdentity)
		firstDone <- err
	}()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := resolver.For(ctx, resolverIdentity); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting For error = %v", err)
	}
	close(release)
	if err := <-firstDone; err == nil {
		t.Fatal("unprovisioned first call unexpectedly succeeded")
	}
}

func TestTenantAlreadyExistsClassification(t *testing.T) {
	if !tenantAlreadyExists(errors.New("User ALREADY EXISTS")) {
		t.Fatal("already-exists error not recognized")
	}
	if tenantAlreadyExists(nil) || tenantAlreadyExists(errors.New("permission denied")) {
		t.Fatal("unrelated error recognized as already-exists")
	}
}
