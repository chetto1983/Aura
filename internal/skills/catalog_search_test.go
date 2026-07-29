package skills

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSkillsCatalogAPIClientSearch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "go test" {
			t.Errorf("q = %q, want go test", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"query":"go test","unknown":true,"skills":[`+
			`{"source":"first/repo","skillId":"alpha","installs":153037,"future":"ignored"},`+
			`{"source":"","skillId":"drop-source","installs":9},`+
			`{"source":"drop/skill","skillId":"","installs":8},`+
			`{"source":"second/repo","skillId":"beta","installs":4638}`+
			`]}`)
	}))
	defer server.Close()

	client := newSkillsCatalogAPIClient(server.Client(), server.URL)
	hits, err := client.Search(t.Context(), "go test")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := []CatalogHit{
		{Source: "first/repo", Skill: "alpha", Installs: "153K"},
		{Source: "second/repo", Skill: "beta", Installs: "4.6K"},
	}
	if fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Fatalf("hits = %#v, want %#v", hits, want)
	}
}

func TestSkillsCatalogAPIClientCapsResultsAtTwenty(t *testing.T) {
	t.Parallel()
	var rows []string
	for i := range 25 {
		rows = append(rows, fmt.Sprintf(
			`{"source":"owner/repo","skillId":"skill-%02d","installs":%d}`, i, i,
		))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"skills":[%s]}`, strings.Join(rows, ","))
	}))
	defer server.Close()
	hits, err := newSkillsCatalogAPIClient(server.Client(), server.URL).Search(t.Context(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != catalogMaxHits || hits[0].Skill != "skill-00" || hits[19].Skill != "skill-19" {
		t.Fatalf("rank/cap = %#v", hits)
	}
}

func TestCompactInstallCount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"}, {999, "999"}, {1000, "1K"}, {4638, "4.6K"},
		{12000, "12K"}, {153037, "153K"}, {1250000, "1.3M"},
	} {
		if got := compactInstallCount(tc.in); got != tc.want {
			t.Errorf("compactInstallCount(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSkillsCatalogAPIClientErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no", http.StatusServiceUnavailable)
		}},
		{"malformed JSON", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{`) }},
		{"missing skills", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{}`) }},
		{"null skills", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"skills":null}`)
		}},
		{"wrong skills type", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"skills":{}}`)
		}},
		{"oversized", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"skills":[],"pad":"`+
				strings.Repeat("x", catalogResponseMaxBytes)+`"}`)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			_, err := newSkillsCatalogAPIClient(server.Client(), server.URL).
				Search(t.Context(), "docx")
			if err == nil {
				t.Fatal("Search error = nil")
			}
		})
	}
}

func TestSkillsCatalogAPIClientAllowsEmptySkillsArray(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"skills":[]}`)
	}))
	defer server.Close()
	hits, err := newSkillsCatalogAPIClient(server.Client(), server.URL).
		Search(t.Context(), "no-match")
	if err != nil {
		t.Fatal(err)
	}
	if hits == nil || len(hits) != 0 {
		t.Fatalf("hits = %#v, want non-nil empty slice", hits)
	}
}

func TestSkillsCatalogAPIClientHonorsCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		_, err := newSkillsCatalogAPIClient(server.Client(), server.URL).Search(ctx, "docx")
		errCh <- err
	}()
	<-started
	cancel()
	if err := <-errCh; err == nil {
		t.Fatal("Search error = nil after cancellation")
	}
}

func testCatalogService(
	primary, fallback CatalogSearchFunc,
	now func() time.Time,
) *catalogSearchService {
	return newCatalogSearchService(primary, fallback, catalogSearchOptions{
		now: now, ttl: time.Minute, maxEntries: 128,
		httpTimeout: 50 * time.Millisecond, fallbackTimeout: 100 * time.Millisecond,
	})
}

func TestCatalogSearchServiceCachesFallbackAndExpires(t *testing.T) {
	now := time.Unix(100, 0)
	primaryCalls, fallbackCalls := 0, 0
	service := testCatalogService(
		func(context.Context, string) ([]CatalogHit, error) {
			primaryCalls++
			return nil, errors.New("primary down")
		},
		func(context.Context, string) ([]CatalogHit, error) {
			fallbackCalls++
			return []CatalogHit{{Source: "owner/repo", Skill: "docx", Installs: "12K"}}, nil
		},
		func() time.Time { return now },
	)
	for range 2 {
		if _, err := service.Search(t.Context(), " DocX "); err != nil {
			t.Fatal(err)
		}
	}
	if primaryCalls != 1 || fallbackCalls != 1 {
		t.Fatalf("calls = primary %d fallback %d", primaryCalls, fallbackCalls)
	}
	now = now.Add(time.Minute + time.Nanosecond)
	if _, err := service.Search(t.Context(), "docx"); err != nil {
		t.Fatal(err)
	}
	if primaryCalls != 2 || fallbackCalls != 2 {
		t.Fatalf("expired calls = primary %d fallback %d", primaryCalls, fallbackCalls)
	}
}

func TestCatalogSearchServiceCoalescesConcurrentMisses(t *testing.T) {
	t.Parallel()
	start := make(chan struct{})
	release := make(chan struct{})
	primaryStarted := make(chan struct{})
	var startOnce sync.Once
	var calls atomic.Int32
	service := testCatalogService(
		func(context.Context, string) ([]CatalogHit, error) {
			calls.Add(1)
			startOnce.Do(func() { close(primaryStarted) })
			<-release
			return []CatalogHit{{Source: "owner/repo", Skill: "docx"}}, nil
		},
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("fallback must not run")
		},
		time.Now,
	)

	const workers = 16
	errs := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for range workers {
		go func() {
			ready.Done()
			<-start
			hits, err := service.Search(t.Context(), "docx")
			if err == nil && (len(hits) != 1 || hits[0].Skill != "docx") {
				err = fmt.Errorf("unexpected hits: %#v", hits)
			}
			errs <- err
		}()
	}
	ready.Wait()
	close(start)
	<-primaryStarted
	time.Sleep(25 * time.Millisecond)
	close(release)
	for range workers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("primary calls = %d, want 1", got)
	}
}

func TestCatalogSearchServiceCancelledWaiterDoesNotCancelSharedFetch(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var calls atomic.Int32
	service := testCatalogService(
		func(context.Context, string) ([]CatalogHit, error) {
			calls.Add(1)
			once.Do(func() { close(started) })
			<-release
			return []CatalogHit{{Source: "owner/repo", Skill: "docx"}}, nil
		},
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("fallback must not run")
		},
		time.Now,
	)

	firstCtx, cancelFirst := context.WithCancel(t.Context())
	firstErr := make(chan error, 1)
	go func() {
		_, err := service.Search(firstCtx, "docx")
		firstErr <- err
	}()
	<-started
	secondResult := make(chan error, 1)
	go func() {
		hits, err := service.Search(t.Context(), "docx")
		if err == nil && len(hits) != 1 {
			err = fmt.Errorf("hits = %#v", hits)
		}
		secondResult <- err
	}()
	time.Sleep(25 * time.Millisecond)
	cancelFirst()
	if err := <-firstErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("first error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-secondResult; err != nil {
		t.Fatalf("second error = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("primary calls = %d, want 1", got)
	}
}

func TestCatalogSearchServiceBoundsCache(t *testing.T) {
	t.Parallel()
	service := testCatalogService(
		func(_ context.Context, query string) ([]CatalogHit, error) {
			return []CatalogHit{{Source: "owner/repo", Skill: query}}, nil
		},
		func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("fallback must not run")
		},
		time.Now,
	)
	for i := range 129 {
		if _, err := service.Search(t.Context(), fmt.Sprintf("query-%03d", i)); err != nil {
			t.Fatal(err)
		}
	}
	service.mu.Lock()
	got := len(service.cache)
	service.mu.Unlock()
	if got != 128 {
		t.Fatalf("cache size = %d, want 128", got)
	}
}

func TestCatalogSearchServiceDoesNotCacheDualFailure(t *testing.T) {
	t.Parallel()
	var primaryCalls, fallbackCalls atomic.Int32
	service := testCatalogService(
		func(context.Context, string) ([]CatalogHit, error) {
			primaryCalls.Add(1)
			return nil, errors.New("api unavailable")
		},
		func(context.Context, string) ([]CatalogHit, error) {
			fallbackCalls.Add(1)
			return nil, errors.New("cli unavailable")
		},
		time.Now,
	)
	for range 2 {
		_, err := service.Search(t.Context(), "docx")
		if err == nil ||
			!strings.Contains(err.Error(), "catalog primary") ||
			!strings.Contains(err.Error(), "catalog fallback") {
			t.Fatalf("error = %v", err)
		}
	}
	if primaryCalls.Load() != 2 || fallbackCalls.Load() != 2 {
		t.Fatalf(
			"calls = primary %d fallback %d",
			primaryCalls.Load(),
			fallbackCalls.Load(),
		)
	}
}

func TestCatalogSearchServiceTimesOutPrimaryBeforeFallback(t *testing.T) {
	t.Parallel()
	fallbackCalled := false
	service := newCatalogSearchService(
		func(ctx context.Context, _ string) ([]CatalogHit, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		func(context.Context, string) ([]CatalogHit, error) {
			fallbackCalled = true
			return []CatalogHit{{Source: "owner/repo", Skill: "docx"}}, nil
		},
		catalogSearchOptions{
			now: time.Now, ttl: time.Minute, maxEntries: 128,
			httpTimeout: 20 * time.Millisecond, fallbackTimeout: 100 * time.Millisecond,
		},
	)
	hits, err := service.Search(t.Context(), "docx")
	if err != nil {
		t.Fatal(err)
	}
	if !fallbackCalled || len(hits) != 1 {
		t.Fatalf("fallbackCalled=%v hits=%#v", fallbackCalled, hits)
	}
}
