package mcp

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// sdkclient_auth_test.go closes an expected gap named in plan 45.1-03 Task 3:
// "OpenSDKSession's HTTP branch auth-header injection and the bearer-token path
// — previously covered by http_client_test.go" (deleted with the bespoke client
// in this same plan). httpAuthFromEnv/withAuthHeaders/headerRoundTripper had no
// SDK-era test at all until this file — measured against the coverage profile,
// not assumed.

func TestHTTPAuthFromEnv(t *testing.T) {
	cases := []struct {
		name        string
		env         []string
		wantBearer  string
		wantHeaders map[string]string
	}{
		{name: "empty env yields no credentials"},
		{
			name:       "bearer token only",
			env:        []string{"MCP_BEARER_TOKEN=secret-token"},
			wantBearer: "secret-token",
		},
		{
			name:        "header only, underscores become hyphens",
			env:         []string{"MCP_HEADER_X_API_KEY=abc123"},
			wantHeaders: map[string]string{"X-API-KEY": "abc123"},
		},
		{
			name:        "bearer and header together; unrelated env ignored",
			env:         []string{"MCP_BEARER_TOKEN=tok", "MCP_HEADER_X_TENANT=t1", "UNRELATED=ignored"},
			wantBearer:  "tok",
			wantHeaders: map[string]string{"X-TENANT": "t1"},
		},
		{
			name: "malformed entry (no '=') is skipped, not an error",
			env:  []string{"MALFORMED"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers, bearer := httpAuthFromEnv(tc.env)
			if bearer != tc.wantBearer {
				t.Errorf("bearer = %q, want %q", bearer, tc.wantBearer)
			}
			if len(headers) != len(tc.wantHeaders) {
				t.Fatalf("headers = %#v, want %#v", headers, tc.wantHeaders)
			}
			for k, want := range tc.wantHeaders {
				if got := headers[k]; got != want {
					t.Errorf("header %q = %q, want %q", k, got, want)
				}
			}
		})
	}
}

// TestWithAuthHeadersInjectsCredentialsOnTheWire asserts what the SERVER received,
// not the client's internal struct — a middleware/wrapper that mutated a copy would
// pass a struct-level assertion and fail on the wire (the pattern plan 45.1-02's
// middleware tests already established).
func TestWithAuthHeadersInjectsCredentialsOnTheWire(t *testing.T) {
	var gotAuth, gotCustom string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	base := &http.Client{}
	client := withAuthHeaders(base, map[string]string{"X-Custom": "value"}, "the-token")

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	if gotAuth != "Bearer the-token" {
		t.Errorf("server saw Authorization = %q, want %q", gotAuth, "Bearer the-token")
	}
	if gotCustom != "value" {
		t.Errorf("server saw X-Custom = %q, want %q", gotCustom, "value")
	}
	// headerRoundTripper.RoundTrip clones before setting — the caller's own request
	// object must come back untouched, since http.RoundTripper implementations must
	// not mutate the request they were handed.
	if req.Header.Get("Authorization") != "" || req.Header.Get("X-Custom") != "" {
		t.Errorf("caller's request was mutated by RoundTrip: %#v", req.Header)
	}
}

// TestWithAuthHeadersNoopWhenNothingToInject proves the base *http.Client — and
// whatever hardened Transport it carries underneath — survives untouched when there
// is nothing to inject, rather than being silently wrapped and potentially replaced.
func TestWithAuthHeadersNoopWhenNothingToInject(t *testing.T) {
	base := &http.Client{}
	got := withAuthHeaders(base, nil, "")
	if got != base {
		t.Error("withAuthHeaders with no headers/bearer must return the SAME *http.Client unchanged")
	}
}
