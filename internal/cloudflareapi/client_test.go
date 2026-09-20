package cloudflareapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestTransportSafety(t *testing.T) {
	for _, tc := range []struct {
		err   error
		retry bool
	}{{context.DeadlineExceeded, true}, {context.Canceled, false}, {errors.New("secret-token"), false}} {
		c := New("", "secret-token", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, tc.err })})
		_, err := c.ListAccounts(t.Context())
		if err == nil || Retryable(err) != tc.retry || strings.Contains(err.Error(), "secret-token") {
			t.Fatalf("error=%v", err)
		}
		if c.http.Timeout != 15*time.Second || c.base != BaseURL {
			t.Fatal("client defaults")
		}
		for _, value := range []any{*c, c} {
			if strings.Contains(fmt.Sprintf("%#v", value), "secret-token") {
				t.Error("client formatting leaks credential")
			}
		}
	}
	if Retryable(nil) || Retryable(errors.New("other")) {
		t.Fatal("unrelated errors retry")
	}
}

func TestRedirectDoesNotForwardAuthorization(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("redirect followed") }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) }))
	defer s.Close()
	_, err := New(s.URL, "secret-token", s.Client()).ListAccounts(t.Context())
	if err == nil || Retryable(err) {
		t.Fatal(err)
	}
}

func TestRequestGuards(t *testing.T) {
	c := New("http://%invalid", "secret-token", nil)
	if _, err := c.ListAccounts(t.Context()); err == nil {
		t.Fatal("invalid base accepted")
	}
	c = New("", "", nil)
	if _, err := c.request(t.Context(), "GET", "/", nil, make(chan int), nil); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := c.request(t.Context(), "invalid\nmethod", "/", nil, nil, nil); err == nil {
		t.Fatal("invalid method accepted")
	}
	if _, err := c.GetTunnel(t.Context(), "../account", "id"); err == nil {
		t.Fatal("path traversal accepted")
	}
	if err := c.DeleteTunnel(t.Context(), "account", "id?all=1"); err == nil {
		t.Fatal("query injection accepted")
	}
	if _, err := c.ListTunnels(t.Context(), ""); err == nil {
		t.Fatal("empty account accepted")
	}
}

func TestPaginationContracts(t *testing.T) {
	for _, tc := range []struct {
		info    string
		wantErr bool
		calls   int
	}{
		{`{"page":3}`, true, 1},
		{`{"page":1,"total_pages":2}`, true, 1},
		{`{"page":1,"total_pages":1}`, false, 1},
		{`{"page":1,"total_count":2}`, true, 1},
	} {
		calls := 0
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			_, _ = fmt.Fprintf(w, `{"success":true,"result":[],"result_info":%s}`, tc.info)
		}))
		_, err := New(s.URL, "", s.Client()).ListAccounts(t.Context())
		s.Close()
		if (err != nil) != tc.wantErr || calls != tc.calls {
			t.Fatalf("info=%s calls=%d err=%v", tc.info, calls, err)
		}
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestListAccountsUsesBearerAndPaginates(t *testing.T) {
	calls := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer secret-token" || r.URL.Path != "/accounts" || r.URL.Query().Get("page") != fmt.Sprint(calls) {
			t.Errorf("unexpected request: %s", r.URL)
		}
		_, _ = w.Write(fixture(t, fmt.Sprintf("accounts-page-%d", calls)))
	}))
	defer s.Close()
	got, err := New(s.URL, "secret-token", s.Client()).ListAccounts(context.Background())
	if err != nil || len(got) != 2 || calls != 2 {
		t.Fatalf("accounts=%v calls=%d err=%v", got, calls, err)
	}
}

func TestErrorsAreSanitizedAndClassified(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		retry  bool
	}{
		{403, string(fixture(t, "error-permission")), false},
		{429, string(fixture(t, "error-rate-limit")), true},
		{502, "secret-token", true},
		{200, `{"success":false,"errors":[{"code":10000,"message":"secret-token"}]}`, false},
		{200, `{"result":[]}`, false},
		{200, `{"success":true,"result":null}`, false},
		{200, `{"success":true,"result":[],"padding":"` + strings.Repeat("x", 2*1024*1024) + `"}`, false},
	} {
		t.Run(fmt.Sprint(tc.status, len(tc.body)), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			_, err := New(s.URL, "secret-token", s.Client()).ListAccounts(t.Context())
			var apiErr *APIError
			if err == nil || strings.Contains(err.Error(), "secret-token") || !errors.As(err, &apiErr) || apiErr.Retryable != tc.retry {
				t.Fatalf("err=%v retry=%v", err, tc.retry)
			}
		})
	}
}

func TestSecretCannotBeFormattedOrMarshaled(t *testing.T) {
	s := Secret("secret-token")
	for _, format := range []string{"%s", "%v", "%+v", "%#v", "%q"} {
		if strings.Contains(fmt.Sprintf(format, s), "secret-token") {
			t.Fatalf("leak: %s", format)
		}
	}
	b, err := json.Marshal(s)
	if err != nil || strings.Contains(string(b), "secret-token") || s.Reveal() != "secret-token" {
		t.Fatal("secret contract")
	}
}
