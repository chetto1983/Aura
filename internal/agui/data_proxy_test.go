package agui

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeDataFetcher is a scripted DataFetcher for the proxy unit tests.
type fakeDataFetcher struct {
	body      []byte
	mediaType string
	err       error
	gotURL    string
	gotConv   string
}

func (f *fakeDataFetcher) FetchData(_ context.Context, convID, rawURL string) ([]byte, string, error) {
	f.gotConv = convID
	f.gotURL = rawURL
	if f.err != nil {
		return nil, "", f.err
	}
	return f.body, f.mediaType, nil
}

func dataProxyServer(data DataFetcher) *httptest.Server {
	s := NewServer(&scriptedRunner{}, nil, ServerConfig{})
	if data != nil {
		s.SetDataProxy(data)
	}
	return httptest.NewServer(s.Mux())
}

// TestDataProxySuccess: a valid url streams the bytes back with the fetcher's media
// type, nosniff, and no-store — live data must never be cached as if it were stable.
func TestDataProxySuccess(t *testing.T) {
	f := &fakeDataFetcher{body: []byte(`{"price":78511.27}`), mediaType: "application/json"}
	srv := dataProxyServer(f)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/fetch?url=https%3A%2F%2Fapi.test%2Fv1%2Fprice")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if no := resp.Header.Get("X-Content-Type-Options"); no != "nosniff" {
		t.Fatalf("nosniff = %q", no)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"price":78511.27}` {
		t.Fatalf("body = %q", body)
	}
	if f.gotURL != "https://api.test/v1/price" {
		t.Fatalf("fetcher got url %q", f.gotURL)
	}
}

// TestDataProxyMissingURL: no url param is a 400, not an empty 200.
func TestDataProxyMissingURL(t *testing.T) {
	srv := dataProxyServer(&fakeDataFetcher{})
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/fetch")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestDataProxyUnwired: an unconfigured proxy is 503, never a silent success.
func TestDataProxyUnwired(t *testing.T) {
	srv := dataProxyServer(nil)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/fetch?url=https%3A%2F%2Fapi.test%2Fx")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

// TestDataProxyFetchError: a blocked or failing fetch surfaces as 502 carrying the
// already-sanitized reason, never the SSRF internals.
func TestDataProxyFetchError(t *testing.T) {
	f := &fakeDataFetcher{err: errors.New("blocked_url")}
	srv := dataProxyServer(f)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/fetch?url=https%3A%2F%2Fevil.test%2Fx")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
}
