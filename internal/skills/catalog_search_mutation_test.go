package skills

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type catalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn catalogRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type catalogTrackingBody struct {
	reader io.Reader
	err    error
	closed bool
}

func (b *catalogTrackingBody) Read(buffer []byte) (int, error) {
	if b.err != nil {
		return 0, b.err
	}
	return b.reader.Read(buffer)
}

func (b *catalogTrackingBody) Close() error {
	b.closed = true
	return nil
}

func TestSkillsCatalogAPIClientUsesDefaultClientAndPreservesEndpointQuery(t *testing.T) {
	original := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = original })

	var requested *http.Request
	http.DefaultClient = &http.Client{Transport: catalogRoundTripFunc(
		func(request *http.Request) (*http.Response, error) {
			requested = request
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"skills":[]}`)),
				Header:     make(http.Header),
			}, nil
		},
	)}

	hits, err := newSkillsCatalogAPIClient(nil, "https://catalog.test/search?keep=yes&q=old").
		Search(t.Context(), "new value")
	if err != nil {
		t.Fatal(err)
	}
	if hits == nil || len(hits) != 0 {
		t.Fatalf("hits = %#v, want non-nil empty slice", hits)
	}
	if requested == nil {
		t.Fatal("default client did not receive a request")
	}
	if got := requested.URL.Query().Get("keep"); got != "yes" {
		t.Fatalf("keep query = %q, want yes", got)
	}
	if got := requested.URL.Query().Get("q"); got != "new value" {
		t.Fatalf("q query = %q, want new value", got)
	}
	if got := requested.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q, want application/json", got)
	}
}

func TestSkillsCatalogAPIClientReportsErrorCategory(t *testing.T) {
	t.Run("endpoint", func(t *testing.T) {
		_, err := newSkillsCatalogAPIClient(http.DefaultClient, "%").
			Search(t.Context(), "docx")
		if err == nil || !strings.Contains(err.Error(), "parse skills catalog endpoint") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("nil context", func(t *testing.T) {
		_, err := newSkillsCatalogAPIClient(http.DefaultClient, "https://catalog.test/search").
			Search(nil, "docx") //nolint:staticcheck // exercises the request-construction guard
		if err == nil || !strings.Contains(err.Error(), "build skills catalog request") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("transport", func(t *testing.T) {
		client := &http.Client{Transport: catalogRoundTripFunc(
			func(*http.Request) (*http.Response, error) {
				return nil, errors.New("transport unavailable")
			},
		)}
		_, err := newSkillsCatalogAPIClient(client, "https://catalog.test/search").
			Search(t.Context(), "docx")
		if err == nil || !strings.Contains(err.Error(), "skills catalog request") {
			t.Fatalf("error = %v", err)
		}
	})

	for _, status := range []int{http.StatusContinue, http.StatusMultipleChoices} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := &catalogTrackingBody{reader: strings.NewReader(`{"skills":[]}`)}
			client := &http.Client{Transport: catalogRoundTripFunc(
				func(*http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status,
						Body:       body,
						Header:     make(http.Header),
					}, nil
				},
			)}
			_, err := newSkillsCatalogAPIClient(client, "https://catalog.test/search").
				Search(t.Context(), "docx")
			want := "skills catalog status " + http.StatusText(status)
			if err == nil || !strings.Contains(err.Error(), "skills catalog status") {
				t.Fatalf("error = %v, want category %q", err, want)
			}
			if !body.closed {
				t.Fatal("response body was not closed")
			}
		})
	}

	t.Run("body read", func(t *testing.T) {
		body := &catalogTrackingBody{err: errors.New("read failed")}
		client := &http.Client{Transport: catalogRoundTripFunc(
			func(*http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       body,
					Header:     make(http.Header),
				}, nil
			},
		)}
		_, err := newSkillsCatalogAPIClient(client, "https://catalog.test/search").
			Search(t.Context(), "docx")
		if err == nil || !strings.Contains(err.Error(), "read skills catalog response") {
			t.Fatalf("error = %v", err)
		}
		if !body.closed {
			t.Fatal("response body was not closed")
		}
	})
}

func TestSkillsCatalogAPIClientDistinguishesSchemaErrors(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "malformed envelope", body: `{`, want: "decode skills catalog response"},
		{name: "missing array", body: `{}`, want: "missing skills array"},
		{name: "null array", body: `{"skills":null}`, want: "missing skills array"},
		{name: "wrong array type", body: `{"skills":{}}`, want: "decode skills catalog skills"},
		{
			name: "oversized",
			body: `{"skills":[],"pad":"` + strings.Repeat("x", catalogResponseMaxBytes) + `"}`,
			want: "response exceeds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(
				func(w http.ResponseWriter, _ *http.Request) {
					_, _ = io.WriteString(w, tc.body)
				},
			))
			defer server.Close()

			_, err := newSkillsCatalogAPIClient(server.Client(), server.URL).
				Search(t.Context(), "docx")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want category %q", err, tc.want)
			}
		})
	}
}

func TestCompactInstallCountBoundaries(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		installs int64
		want     string
	}{
		{installs: -1, want: "0"},
		{installs: 4_649, want: "4.6K"},
		{installs: 4_650, want: "4.7K"},
		{installs: 153_499, want: "153K"},
		{installs: 100_000_000, want: "100M"},
	} {
		if got := compactInstallCount(tc.installs); got != tc.want {
			t.Errorf("compactInstallCount(%d) = %q, want %q", tc.installs, got, tc.want)
		}
	}
}

func TestDefaultCatalogSearchOptions(t *testing.T) {
	t.Parallel()
	options := defaultCatalogSearchOptions()
	if options.now == nil ||
		options.ttl != catalogCacheTTL ||
		options.maxEntries != catalogCacheMaxEntries ||
		options.httpTimeout != catalogHTTPTimeout ||
		options.fallbackTimeout != catalogFallbackTimeout {
		t.Fatalf("default options = %#v", options)
	}
}

func TestCatalogSearchServiceCanDisableCache(t *testing.T) {
	t.Parallel()
	var calls int
	service := newCatalogSearchService(
		func(context.Context, string) ([]CatalogHit, error) {
			calls++
			return []CatalogHit{{Source: "owner/repo", Skill: "docx"}}, nil
		},
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("fallback must not run")
		},
		catalogSearchOptions{
			now: time.Now, ttl: time.Minute, maxEntries: 0,
			httpTimeout: time.Second, fallbackTimeout: time.Second,
		},
	)
	for range 2 {
		if _, err := service.Search(t.Context(), "docx"); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 || len(service.cache) != 0 {
		t.Fatalf("calls = %d, cache = %#v", calls, service.cache)
	}
}

func TestCatalogSearchServiceReturnsDefensiveCopies(t *testing.T) {
	t.Parallel()
	source := []CatalogHit{{Source: "owner/repo", Skill: "docx", Installs: "12K"}}
	service := testCatalogService(
		func(context.Context, string) ([]CatalogHit, error) { return source, nil },
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("fallback must not run")
		},
		time.Now,
	)
	first, err := service.Search(t.Context(), "docx")
	if err != nil {
		t.Fatal(err)
	}
	source[0].Skill = "source-mutated"
	first[0].Skill = "caller-mutated"
	second, err := service.Search(t.Context(), "docx")
	if err != nil {
		t.Fatal(err)
	}
	if second[0].Skill != "docx" {
		t.Fatalf("cached hit was mutated: %#v", second)
	}
	empty := cloneCatalogHits(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("cloneCatalogHits(nil) = %#v, want non-nil empty", empty)
	}
}

func TestCatalogSearchServiceUsesLRUEviction(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	calls := make(map[string]int)
	service := newCatalogSearchService(
		func(_ context.Context, query string) ([]CatalogHit, error) {
			mu.Lock()
			calls[query]++
			mu.Unlock()
			return []CatalogHit{{Source: "owner/repo", Skill: query}}, nil
		},
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("fallback must not run")
		},
		catalogSearchOptions{
			now: func() time.Time { return time.Unix(100, 0) },
			ttl: time.Minute, maxEntries: 2,
			httpTimeout: time.Second, fallbackTimeout: time.Second,
		},
	)
	for _, query := range []string{"a", "b", "a", "c", "a", "b"} {
		if _, err := service.Search(t.Context(), query); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["a"] != 1 || calls["b"] != 2 || calls["c"] != 1 {
		t.Fatalf("primary calls = %#v, want a:1 b:2 c:1", calls)
	}
}

func TestCatalogSearchServiceCancelsCompletedAttempts(t *testing.T) {
	t.Parallel()
	var primaryContext, fallbackContext context.Context
	primaryService := testCatalogService(
		func(ctx context.Context, _ string) ([]CatalogHit, error) {
			primaryContext = ctx
			return []CatalogHit{{Source: "owner/repo", Skill: "docx"}}, nil
		},
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("fallback must not run")
		},
		time.Now,
	)
	if _, err := primaryService.Search(t.Context(), "docx"); err != nil {
		t.Fatal(err)
	}
	assertCatalogContextCanceled(t, primaryContext)

	fallbackService := testCatalogService(
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("primary unavailable")
		},
		func(ctx context.Context, _ string) ([]CatalogHit, error) {
			fallbackContext = ctx
			return []CatalogHit{{Source: "owner/repo", Skill: "docx"}}, nil
		},
		time.Now,
	)
	if _, err := fallbackService.Search(t.Context(), "docx"); err != nil {
		t.Fatal(err)
	}
	assertCatalogContextCanceled(t, fallbackContext)
}

func assertCatalogContextCanceled(t *testing.T, ctx context.Context) {
	t.Helper()
	if ctx == nil {
		t.Fatal("attempt context was not captured")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("attempt context was not canceled after completion")
	}
}
