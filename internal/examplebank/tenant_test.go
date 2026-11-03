package examplebank

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
)

// existsServer answers ArcadeDB's exists call with exists and records every other request, which
// a read or a forget must never make.
func existsServer(t *testing.T, status int, exists bool) (*TenantStore, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var other []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/exists/") {
			w.WriteHeader(status)
			if exists {
				_, _ = io.WriteString(w, `{"result":true}`)
			} else {
				_, _ = io.WriteString(w, `{"result":false}`)
			}
			return
		}
		mu.Lock()
		other = append(other, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	admin, err := arcadedb.New(arcadedb.Config{BaseURL: server.URL, Database: "aura", User: "root", Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	store := &TenantStore{Clients: arcadedb.NewTenantClients(arcadedb.Config{BaseURL: server.URL}, admin, nil, nil)}
	return store, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return other
	}
}

const tenantIdentity = "00000000-0000-4000-8000-000000000001"

func TestATenantWithNoMemoryDatabaseHasNoExamples(t *testing.T) {
	store, other := existsServer(t, http.StatusOK, false)
	ctx := context.Background()
	examples, err := store.Examples(ctx, tenantIdentity, "tier", "s1")
	if err != nil || examples != nil {
		t.Fatalf("examples = %v, %v", examples, err)
	}
	if n, err := store.ForgetExamples(ctx, tenantIdentity, "tier", "a"); err != nil || n != 0 {
		t.Fatalf("forget = %d, %v", n, err)
	}
	if calls := other(); len(calls) != 0 {
		t.Fatalf("a read and a forget made %v: a database would have been created to find nothing", calls)
	}
}

func TestATenantWhoseMemoryCannotBeAskedFails(t *testing.T) {
	store, _ := existsServer(t, http.StatusServiceUnavailable, false)
	ctx := context.Background()
	if _, err := store.Examples(ctx, tenantIdentity, "tier", "s1"); err == nil {
		t.Fatal("examples read nothing from a server that could not answer, and said nothing")
	}
	if _, err := store.ForgetExamples(ctx, tenantIdentity, "tier", "a"); err == nil {
		t.Fatal("a forget a server could not answer reported success")
	}
}

func TestATenantStoreRefusesAMissingIdentity(t *testing.T) {
	store, other := existsServer(t, http.StatusOK, true)
	if err := store.PutExample(context.Background(), "", arcadedb.Example{Domain: "tier"}); err == nil {
		t.Fatal("an example was put with no identity")
	}
	if calls := other(); len(calls) != 0 {
		t.Fatalf("a put with no identity reached the server: %v", calls)
	}
}
