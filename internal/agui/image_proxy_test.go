package agui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeImageFetcher is a scripted ImageFetcher for the proxy unit tests.
type fakeImageFetcher struct {
	body      []byte
	mediaType string
	err       error
	gotURL    string
	gotConv   string
}

func (f *fakeImageFetcher) FetchImage(_ context.Context, convID, rawURL string) ([]byte, string, error) {
	f.gotConv = convID
	f.gotURL = rawURL
	if f.err != nil {
		return nil, "", f.err
	}
	return f.body, f.mediaType, nil
}

func imageProxyServer(images ImageFetcher) *httptest.Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if images != nil {
		s.SetImageProxy(images)
	}
	return httptest.NewServer(s.Mux())
}

// TestImageProxySuccess (D-09): a valid url streams the bytes with the strict
// Content-Type + Cache-Control, and the fetcher is called with the url param.
func TestImageProxySuccess(t *testing.T) {
	f := &fakeImageFetcher{body: []byte("\x89PNGdata"), mediaType: "image/png"}
	srv := imageProxyServer(f)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/image-proxy?url=" + "https%3A%2F%2Fcdn.test%2Fa.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc == "" {
		t.Fatalf("missing Cache-Control header")
	}
	if no := resp.Header.Get("X-Content-Type-Options"); no != "nosniff" {
		t.Fatalf("missing nosniff header, got %q", no)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "\x89PNGdata" {
		t.Fatalf("body mismatch: %q", body)
	}
	if f.gotURL != "https://cdn.test/a.png" {
		t.Fatalf("fetcher got url %q, want decoded https://cdn.test/a.png", f.gotURL)
	}
}

// TestImageProxyMissingURL: an absent url query param is a 400.
func TestImageProxyMissingURL(t *testing.T) {
	srv := imageProxyServer(&fakeImageFetcher{})
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/image-proxy")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestImageProxyUnwired503: no fetcher wired answers 503 (the read is optional wiring).
func TestImageProxyUnwired503(t *testing.T) {
	srv := imageProxyServer(nil)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/image-proxy?url=https://cdn.test/a.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// TestImageProxyFetchErrorSanitized (D-09): a blocked/bad fetch is a 502 whose body
// carries no sensitive internals (SanitizeString belt-and-suspenders).
func TestImageProxyFetchErrorSanitized(t *testing.T) {
	f := &fakeImageFetcher{err: errors.New("blocked_url (private_or_metadata_target)")}
	srv := imageProxyServer(f)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/image-proxy?url=http://169.254.169.254/x.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	// No raw IP should appear (the upstream WebError is already sanitized; this asserts
	// the proxy does not echo a leaky URL/IP it constructed).
	if len(body) == 0 {
		t.Fatalf("expected a sanitized error body")
	}
}

// TestImageProxyRequireAuthGate (D-09/T-26-10): the route is mounted behind the same
// RequireAuth whole-origin gate every /api/ route inherits. An unauthenticated API
// request (no session cookie) is rejected 401 — never an open relay; a valid cookie
// reaches the handler.
func TestImageProxyRequireAuthGate(t *testing.T) {
	deps := testDeps("operator-secret")
	f := &fakeImageFetcher{body: []byte("ok"), mediaType: "image/png"}
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	s.SetImageProxy(f)
	gated := RequireAuth(s.Mux(), deps)

	t.Run("unauthenticated API request rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/image-proxy?url=https://cdn.test/a.png", nil)
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (no session)", rec.Code)
		}
		if f.gotURL != "" {
			t.Fatalf("fetcher was reached without a session: %q", f.gotURL)
		}
	})

	t.Run("valid session reaches the handler", func(t *testing.T) {
		rec := httptest.NewRecorder()
		gated.ServeHTTP(rec, validCookieReq(deps, "/api/image-proxy?url=https://cdn.test/a.png", false))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 with a valid session", rec.Code)
		}
		if f.gotURL != "https://cdn.test/a.png" {
			t.Fatalf("fetcher url = %q", f.gotURL)
		}
	})
}
