package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/plugins"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

// The plugin board lists for every member; reviewing, installing and removing administer the
// deployment, since a plugin's servers run in every identity's box.
func TestPluginWriteRoutesRequireIdentityCreate(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	var hits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	})
	handler, err := newServeHandler(aguiHandler, authulaTestDeps(localID, memberIdentities{id: localID}), &fakeAuthulaProvider{}, "")
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}
	for _, route := range []struct {
		method, path string
		reaches      bool
	}{
		{http.MethodGet, "/api/governance/plugins", true},
		{http.MethodPost, "/api/governance/plugins/review", false},
		{http.MethodPost, "/api/governance/plugins", false},
		{http.MethodDelete, "/api/governance/plugins/broker-analysis", false},
	} {
		hits = nil
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
		addAuthulaSession(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		reached := len(hits) == 1
		if reached != route.reaches || (!route.reaches && rec.Code != http.StatusForbidden) {
			t.Errorf("member %s %s = %d, reached the handler %v, want %v", route.method, route.path, rec.Code, reached, route.reaches)
		}
	}
}

func TestPluginTreesComeFromRecordsOnly(t *testing.T) {
	store := plugins.Store{Root: t.TempDir()}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "plugin.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	for name, phase := range map[string]string{
		"installed":  plugins.PhaseInstalled,
		"installing": plugins.PhaseInstalling,
		"removing":   plugins.PhaseRemoving,
	} {
		rec := plugins.Record{Name: name, Commit: strings.Repeat("c", 40)}
		if err := store.Claim(rec, src); err != nil {
			t.Fatalf("claim %s: %v", name, err)
		}
		rec.Phase = phase
		if err := store.WriteRecord(rec); err != nil {
			t.Fatalf("record %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(store.Root, "stray", "tree"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	trees, err := pluginTreeResolver(store)(context.Background())
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	want := []usersandbox.PluginTree{
		{Name: "installed", HostDir: store.TreeDir("installed"), Version: strings.Repeat("c", 40)},
		{Name: "installing", HostDir: store.TreeDir("installing"), Version: strings.Repeat("c", 40)},
	}
	if !slices.Equal(trees, want) {
		t.Errorf("trees = %+v, want %+v", trees, want)
	}
}
