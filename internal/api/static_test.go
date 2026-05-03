package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticHandler_ReturnsErrWhenDistEmpty(t *testing.T) {
	h, err := StaticHandler()
	if err == nil {
		t.Skip("dist/ has been populated; this test only meaningful before npm run build")
	}
	if err != ErrNoStaticAssets {
		t.Fatalf("err = %v, want ErrNoStaticAssets", err)
	}
	if h != nil {
		t.Errorf("handler = %v, want nil", h)
	}
}

// TestStaticHandler_ServesIndexAndFallsBack only runs once dist/index.html
// exists. We don't fail when it's missing — that's the pre-build state we
// also need to support.
func TestStaticHandler_ServesIndexAndFallsBack(t *testing.T) {
	h, err := StaticHandler()
	if err == ErrNoStaticAssets {
		t.Skip("dist/ not built; skip until `make web-build`")
	}
	if err != nil {
		t.Fatalf("StaticHandler: %v", err)
	}
	cases := []struct {
		name        string
		path        string
		wantStatus  int
		wantBodyHas string // substring must appear in body
	}{
		{"root serves index", "/", http.StatusOK, "<!doctype html"},
		{"deep link falls back to index", "/wiki/some-slug", http.StatusOK, "<!doctype html"},
		{"unknown app route falls back to index", "/totally/fake/route", http.StatusOK, "<!doctype html"},
		{"unknown root asset is not shadowed", "/totally-fake.js", http.StatusNotFound, ""},
		{"unknown nested asset is not shadowed", "/assets/old-chunk.js", http.StatusNotFound, ""},
		{"reserved /api path is not shadowed", "/api/health", http.StatusNotFound, ""},
		{"reserved /health is not shadowed", "/health", http.StatusNotFound, ""},
		{"reserved /telegram is not shadowed", "/telegram", http.StatusNotFound, ""},
		{"reserved /telegram children are not shadowed", "/telegram/qr.png", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != tc.wantStatus {
				t.Fatalf("status %d, want %d, body %q", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantBodyHas != "" && !strings.Contains(strings.ToLower(rr.Body.String()), tc.wantBodyHas) {
				t.Errorf("body missing %q, got %q", tc.wantBodyHas, rr.Body.String())
			}
		})
	}
}

// TestStaticHandler_RejectsNonGet covers MethodNotAllowed shape.
func TestStaticHandler_RejectsNonGet(t *testing.T) {
	h, err := StaticHandler()
	if err == ErrNoStaticAssets {
		t.Skip("dist/ not built")
	}
	if err != nil {
		t.Fatalf("%v", err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST status %d, want 405", rr.Code)
	}
}
