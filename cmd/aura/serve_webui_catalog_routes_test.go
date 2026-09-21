package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
)

// Every picker catalogue the AG-UI mux serves must also be mounted here, or the cockpit
// 404s a route the daemon is serving. Measured 2026-09-21 on the live deployment with a
// real session: GET /api/settings/embeddings-models returned a bare "404 page not found"
// while speech, transcription, image and video returned models -- because the route was
// added to internal/agui/settings_api.go and not to the parallel list this file used to
// keep. The two lists are one list now; this proves they stay that way.
func TestServeWebuiMountsEveryCatalogueRoute(t *testing.T) {
	const localID = "00000000-0000-0000-0000-000000000001"
	auth := authulaTestDeps(localID, wiringIdentities{id: localID})

	var aguiHits []string
	aguiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		aguiHits = append(aguiHits, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"agui":true}`)
	})

	handler, err := newServeHandler(aguiHandler, auth, &fakeAuthulaProvider{})
	if err != nil {
		t.Fatalf("newServeHandler: %v", err)
	}

	routes := agui.SettingsCatalogRoutes()
	if len(routes) == 0 {
		t.Fatal("agui.SettingsCatalogRoutes() is empty, so the cockpit would mount no catalogue at all")
	}
	embeddingsMounted := false
	for _, pattern := range routes {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok {
			t.Fatalf("catalogue pattern %q carries no method", pattern)
		}
		if path == "/api/settings/embeddings-models" {
			embeddingsMounted = true
		}
		aguiHits = nil
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, nil)
		addAuthulaSession(req)
		handler.ServeHTTP(rec, req)
		if rec.Code == http.StatusNotFound {
			t.Errorf("%s is not mounted on the cockpit mux (404), so the picker cannot list models", pattern)
			continue
		}
		if len(aguiHits) != 1 || aguiHits[0] != path {
			t.Errorf("%s did not reach the AG-UI handler: hits=%v code=%d", pattern, aguiHits, rec.Code)
		}
	}
	if !embeddingsMounted {
		t.Error("the embedding catalogue is missing from SettingsCatalogRoutes; it is the one that shipped unmounted")
	}
}
