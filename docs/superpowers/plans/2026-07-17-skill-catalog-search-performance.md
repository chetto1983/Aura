# Skill Catalog Search Performance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the cockpit's per-keystroke `npx skills find` fan-out with one debounced, cancellable, cached skills.sh request while retaining the CLI fallback and proving the rebuilt localhost flow at Playwright score 10/10.

**Architecture:** `SkillInstallPanel` debounces raw input and forwards TanStack Query's abort signal through the existing same-origin API. `internal/skills` adds a bounded singleflight search service whose primary transport is the skills.sh JSON endpoint and whose fallback is the existing ANSI-parsed CLI; installation stays unchanged. A dedicated opt-in live Playwright spec records the ten-point acceptance score against a freshly rebuilt Aura process.

**Tech Stack:** Go 1.26, `net/http`, `golang.org/x/sync/singleflight`, React 19, TanStack Query 5, TypeScript 6, Vitest 4, Testing Library, Playwright 1.61, Docker Compose.

**Design contract:** `docs/superpowers/specs/2026-07-17-skill-catalog-search-performance-design.md` and `prd.md` Amendment #85 (`cc0e8af40`).

## Execution status (2026-07-17)

- Tasks 0–7 are complete and merged into `master` at `f571b37af`.
- Backend transport, bounded cache/singleflight, CLI fallback, frontend cancellation/debounce, live score harness, mutation hardening, and embedded web assets are committed.
- Task 8 remains open until the rebuilt `localhost:9080` container emits a Playwright `skill-catalog-score.json` result of exactly `10/10`.
- The first post-merge push was correctly stopped by the quality-snapshot hook because a concurrent AG-UI change needed re-measurement. Fresh `internal/agui` DB-integration race coverage is 86.5%; the snapshot follow-up is part of the push retry.

---

## File map

- Create `internal/skills/catalog_search.go`: direct skills.sh JSON client, compact count formatter, bounded TTL cache, same-query singleflight, and CLI fallback orchestration.
- Create `internal/skills/catalog_search_test.go`: transport, schema, cache, coalescing, cancellation, and fallback tests.
- Modify `internal/skills/installer.go`: inject the primary catalog function, enforce the two-character server floor, use the search service, and disable CLI telemetry without changing install argv.
- Modify `internal/skills/installer_test.go`: preserve existing fallback coverage and add direct-primary, short-query, dual-failure, and telemetry assertions.
- Modify `web/src/governance/governanceApi.ts`: accept and forward an optional `AbortSignal`.
- Modify `web/src/governance/__tests__/governanceWrite.coverage.test.tsx`: pin signal identity at the fetch boundary.
- Modify `web/src/governance/SkillInstallPanel.tsx`: debounce, minimum length, cancellation, stale-result suppression, and visible states.
- Modify `web/src/governance/__tests__/SkillInstallPanel.test.tsx`: fake-timer red/green coverage for every new behavior.
- Modify `web/src/i18n/resources.governance.ts`: English and Italian catalog status copy.
- Create `web/e2e/skill-catalog-search-live.spec.ts`: opt-in live 10/10 score against the real Aura endpoint.
- Refresh `internal/webui/dist/**`: committed Vite output embedded into the Go binary.

Unrelated `.planning/graphs/**`, AppShell, shell/share, and Phase 37F changes are never staged by this plan.

### Task 0: Create an isolated execution worktree and prove the baseline

**Files:**
- Read: `CLAUDE.md`
- Read: `docs/superpowers/specs/2026-07-17-skill-catalog-search-performance-design.md`
- Read: `prd.md:3035`
- No source changes

- [x] **Step 1: Create an isolated worktree at the current `master`**

Run from `D:\Aura`:

```powershell
git status --short
git worktree add .worktrees/skill-catalog-search -b fix/skill-catalog-search-performance master
```

Expected: the new branch starts at the current master commit; existing dirty files in `D:\Aura` remain only in the original worktree.

- [x] **Step 2: Verify the PRD gate is in the new worktree**

Run:

```powershell
git -C .worktrees/skill-catalog-search log -3 --oneline
rg -n "Amendment #85|Playwright 10/10 acceptance rubric" `
  .worktrees/skill-catalog-search/prd.md `
  .worktrees/skill-catalog-search/docs/superpowers/specs/2026-07-17-skill-catalog-search-performance-design.md
```

Expected: commit `cc0e8af40` is an ancestor and both required markers are present.

- [x] **Step 3: Run the narrow baseline from the worktree**

Run:

```powershell
go test ./internal/skills ./internal/agui
Set-Location web
npm exec vitest -- run src/governance/__tests__/SkillInstallPanel.test.tsx src/governance/__tests__/governanceWrite.coverage.test.tsx
Set-Location ..
```

Expected: Go packages pass and the existing 21 frontend tests pass before edits.

### Task 1: Add the direct skills.sh JSON transport

**Files:**
- Create: `internal/skills/catalog_search_test.go`
- Create: `internal/skills/catalog_search.go`

- [x] **Step 1: Write failing transport and formatter tests**

Create `internal/skills/catalog_search_test.go` with these tests first:

```go
package skills

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
```

Add the complete schema/error coverage:

```go
func TestSkillsCatalogAPIClientErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "no", 503) }},
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
		tc := tc
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
```

- [x] **Step 2: Run the tests and verify RED**

Run:

```powershell
go test ./internal/skills -run 'TestSkillsCatalogAPIClient|TestCompactInstallCount' -count=1
```

Expected: compile failure because `newSkillsCatalogAPIClient`, `catalogMaxHits`, `catalogResponseMaxBytes`, and `compactInstallCount` do not exist.

- [x] **Step 3: Implement the minimal direct client**

Create `internal/skills/catalog_search.go` with:

```go
package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	skillsCatalogAPIURL     = "https://skills.sh/api/search"
	catalogResponseMaxBytes = 1 << 20
	catalogMaxHits          = 20
	catalogCacheTTL         = 60 * time.Second
	catalogCacheMaxEntries  = 128
	catalogHTTPTimeout      = 2 * time.Second
	catalogFallbackTimeout  = 5 * time.Second
)

type CatalogSearchFunc func(context.Context, string) ([]CatalogHit, error)

type skillsCatalogAPIClient struct {
	client   *http.Client
	endpoint string
}

func newSkillsCatalogAPIClient(client *http.Client, endpoint string) *skillsCatalogAPIClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &skillsCatalogAPIClient{client: client, endpoint: endpoint}
}

func (c *skillsCatalogAPIClient) Search(ctx context.Context, query string) ([]CatalogHit, error) {
	u, err := url.Parse(c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse skills catalog endpoint: %w", err)
	}
	values := u.Query()
	values.Set("q", query)
	u.RawQuery = values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build skills catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("skills catalog request: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("skills catalog status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, catalogResponseMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read skills catalog response: %w", err)
	}
	if len(data) > catalogResponseMaxBytes {
		return nil, fmt.Errorf("skills catalog response exceeds %d bytes", catalogResponseMaxBytes)
	}
	var envelope struct {
		Skills json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode skills catalog response: %w", err)
	}
	if len(envelope.Skills) == 0 || bytes.Equal(bytes.TrimSpace(envelope.Skills), []byte("null")) {
		return nil, fmt.Errorf("skills catalog response missing skills array")
	}
	var rows []struct {
		Source   string `json:"source"`
		SkillID  string `json:"skillId"`
		Installs int64  `json:"installs"`
	}
	if err := json.Unmarshal(envelope.Skills, &rows); err != nil {
		return nil, fmt.Errorf("decode skills catalog skills: %w", err)
	}
	hits := make([]CatalogHit, 0, min(len(rows), catalogMaxHits))
	for _, row := range rows {
		if strings.TrimSpace(row.Source) == "" || strings.TrimSpace(row.SkillID) == "" {
			continue
		}
		hits = append(hits, CatalogHit{
			Source: strings.TrimSpace(row.Source), Skill: strings.TrimSpace(row.SkillID),
			Installs: compactInstallCount(row.Installs),
		})
		if len(hits) == catalogMaxHits {
			break
		}
	}
	return hits, nil
}

func compactInstallCount(installs int64) string {
	if installs < 0 {
		installs = 0
	}
	switch {
	case installs >= 1_000_000:
		return compactUnit(float64(installs)/1_000_000, "M")
	case installs >= 1_000:
		return compactUnit(float64(installs)/1_000, "K")
	default:
		return strconv.FormatInt(installs, 10)
	}
}

func compactUnit(value float64, suffix string) string {
	precision := 1
	scale := 10.0
	if value >= 100 {
		precision = 0
		scale = 1
	}
	rounded := math.Round(value*scale) / scale
	return strings.TrimSuffix(strconv.FormatFloat(rounded, 'f', precision, 64), ".0") + suffix
}
```

Leave the cache-related imports in place only when Task 2 adds their users; until then remove `sync`, `time`, and `singleflight` so this commit passes lint.

- [x] **Step 4: Run the focused and package tests**

Run:

```powershell
gofmt -w internal/skills/catalog_search.go internal/skills/catalog_search_test.go
go test ./internal/skills -run 'TestSkillsCatalogAPIClient|TestCompactInstallCount' -count=1
go test ./internal/skills -count=1
```

Expected: all tests pass with no goleak failure.

- [x] **Step 5: Commit the transport slice**

```powershell
git add internal/skills/catalog_search.go internal/skills/catalog_search_test.go
git commit -m "feat(skills): add bounded catalog API client"
```

### Task 2: Add bounded cache, singleflight, cancellation, and fallback

**Files:**
- Modify: `internal/skills/catalog_search_test.go`
- Modify: `internal/skills/catalog_search.go`

- [x] **Step 1: Write failing search-service tests**

Append tests that construct:

```go
func testCatalogService(primary, fallback CatalogSearchFunc, now func() time.Time) *catalogSearchService {
	return newCatalogSearchService(primary, fallback, catalogSearchOptions{
		now: now, ttl: time.Minute, maxEntries: 128,
		httpTimeout: 50 * time.Millisecond, fallbackTimeout: 100 * time.Millisecond,
	})
}
```

Pin these behaviors:

```go
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
```

Add these concrete tests (and imports `errors`, `sync`, `sync/atomic`, and `time`):

```go
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
	time.Sleep(25 * time.Millisecond) // let all callers join the in-flight key
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
		t.Fatalf("calls = primary %d fallback %d",
			primaryCalls.Load(), fallbackCalls.Load())
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
```

- [x] **Step 2: Run service tests and verify RED**

```powershell
go test ./internal/skills -run TestCatalogSearchService -count=1
```

Expected: compile failure because `catalogSearchService`, `catalogSearchOptions`, and `newCatalogSearchService` do not exist.

- [x] **Step 3: Implement the service in `catalog_search.go`**

Add:

```go
type catalogSearchOptions struct {
	now             func() time.Time
	ttl             time.Duration
	maxEntries      int
	httpTimeout     time.Duration
	fallbackTimeout time.Duration
}

type catalogCacheEntry struct {
	hits      []CatalogHit
	expiresAt time.Time
	sequence  uint64
}

type catalogSearchService struct {
	primary  CatalogSearchFunc
	fallback CatalogSearchFunc
	options  catalogSearchOptions
	group    singleflight.Group
	mu       sync.Mutex
	cache    map[string]catalogCacheEntry
	sequence uint64
}

func newCatalogSearchService(
	primary, fallback CatalogSearchFunc,
	options catalogSearchOptions,
) *catalogSearchService {
	return &catalogSearchService{
		primary: primary, fallback: fallback, options: options,
		cache: make(map[string]catalogCacheEntry),
	}
}

func defaultCatalogSearchOptions() catalogSearchOptions {
	return catalogSearchOptions{
		now: time.Now, ttl: catalogCacheTTL, maxEntries: catalogCacheMaxEntries,
		httpTimeout: catalogHTTPTimeout, fallbackTimeout: catalogFallbackTimeout,
	}
}

func (s *catalogSearchService) Search(ctx context.Context, query string) ([]CatalogHit, error) {
	key := strings.ToLower(strings.TrimSpace(query))
	if hits, ok := s.cached(key); ok {
		return hits, nil
	}
	result := s.group.DoChan(key, func() (any, error) {
		if hits, ok := s.cached(key); ok {
			return hits, nil
		}
		primaryCtx, cancelPrimary := context.WithTimeout(context.Background(), s.options.httpTimeout)
		hits, primaryErr := s.primary(primaryCtx, query)
		cancelPrimary()
		if primaryErr != nil {
			fallbackCtx, cancelFallback := context.WithTimeout(
				context.Background(), s.options.fallbackTimeout,
			)
			hits, fallbackErr := s.fallback(fallbackCtx, query)
			cancelFallback()
			if fallbackErr != nil {
				return nil, fmt.Errorf(
					"catalog primary: %v; catalog fallback: %w", primaryErr, fallbackErr,
				)
			}
		}
		hits = cloneCatalogHits(hits)
		s.store(key, hits)
		return hits, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case completed := <-result:
		if completed.Err != nil {
			return nil, completed.Err
		}
		hits, ok := completed.Val.([]CatalogHit)
		if !ok {
			return nil, fmt.Errorf("catalog search returned unexpected result")
		}
		return cloneCatalogHits(hits), nil
	}
}
```

Add the concrete cache methods:

```go
func (s *catalogSearchService) cached(key string) ([]CatalogHit, bool) {
	now := s.options.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.cache[key]
	if !ok {
		return nil, false
	}
	if !now.Before(entry.expiresAt) {
		delete(s.cache, key)
		return nil, false
	}
	s.sequence++
	entry.sequence = s.sequence
	s.cache[key] = entry
	return cloneCatalogHits(entry.hits), true
}

func (s *catalogSearchService) store(key string, hits []CatalogHit) {
	if s.options.maxEntries <= 0 {
		return
	}
	now := s.options.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for cachedKey, entry := range s.cache {
		if !now.Before(entry.expiresAt) {
			delete(s.cache, cachedKey)
		}
	}
	if _, exists := s.cache[key]; !exists && len(s.cache) >= s.options.maxEntries {
		var oldestKey string
		oldestSequence := ^uint64(0)
		for cachedKey, entry := range s.cache {
			if entry.sequence < oldestSequence {
				oldestKey = cachedKey
				oldestSequence = entry.sequence
			}
		}
		delete(s.cache, oldestKey)
	}
	s.sequence++
	s.cache[key] = catalogCacheEntry{
		hits: cloneCatalogHits(hits),
		expiresAt: now.Add(s.options.ttl),
		sequence: s.sequence,
	}
}

func cloneCatalogHits(hits []CatalogHit) []CatalogHit {
	if hits == nil {
		return []CatalogHit{}
	}
	return append([]CatalogHit(nil), hits...)
}
```

- [x] **Step 4: Run RED→GREEN verification**

```powershell
gofmt -w internal/skills/catalog_search.go internal/skills/catalog_search_test.go
go test ./internal/skills -run TestCatalogSearchService -count=1
go test -race ./internal/skills -run TestCatalogSearchService -count=1
```

Expected: service tests and race detector pass; primary is called once in the concurrent test.

- [x] **Step 5: Commit the service slice**

```powershell
git add internal/skills/catalog_search.go internal/skills/catalog_search_test.go
git commit -m "feat(skills): cache and coalesce catalog search"
```

### Task 3: Wire Installer direct-primary behavior and retain CLI fallback

**Files:**
- Modify: `internal/skills/installer_test.go`
- Modify: `internal/skills/installer.go`

- [x] **Step 1: Write failing Installer tests**

Change `newTestInstaller` to inject a failing primary so existing search tests continue to cover the CLI fallback:

```go
CatalogSearch: func(context.Context, string) ([]CatalogHit, error) {
	return nil, errors.New("catalog API unavailable in CLI fallback test")
},
```

Add the direct-primary and short-query tests:

```go
func TestInstallerSearchUsesDirectCatalogBeforeCLI(t *testing.T) {
	t.Setenv(externalDiscoveryEnv, "")
	ranCLI := false
	inst := NewInstaller(InstallerConfig{
		CatalogSearch: func(_ context.Context, q string) ([]CatalogHit, error) {
			if q != "docx" {
				t.Fatalf("query = %q", q)
			}
			return []CatalogHit{{Source: "anthropics/skills", Skill: "docx", Installs: "153K"}}, nil
		},
		Run: func(context.Context, string, string, ...string) (string, error) {
			ranCLI = true
			return "", nil
		},
	})
	res, err := inst.Search(t.Context(), " docx ")
	if err != nil {
		t.Fatal(err)
	}
	if ranCLI || len(res.Hits) != 1 || res.Query != "docx" {
		t.Fatalf("result = %+v, ranCLI=%v", res, ranCLI)
	}
}

func TestInstallerSearchShortQueryDoesNoIO(t *testing.T) {
	t.Setenv(externalDiscoveryEnv, "")
	called := false
	inst := NewInstaller(InstallerConfig{
		CatalogSearch: func(context.Context, string) ([]CatalogHit, error) {
			called = true
			return nil, nil
		},
		Run: func(context.Context, string, string, ...string) (string, error) {
			called = true
			return "", nil
		},
	})
	for _, query := range []string{"", " ", "d", "界"} {
		res, err := inst.Search(t.Context(), query)
		if err != nil || !res.Enabled || len(res.Hits) != 0 {
			t.Fatalf("Search(%q) = %+v, %v", query, res, err)
		}
	}
	if called {
		t.Fatal("short query performed external I/O")
	}
}
```

Add the exact dual-failure and environment tests:

```go
func TestInstallerSearchDualFailure(t *testing.T) {
	t.Setenv(externalDiscoveryEnv, "")
	inst := NewInstaller(InstallerConfig{
		CatalogSearch: func(context.Context, string) ([]CatalogHit, error) {
			return nil, errors.New("api down")
		},
		Run: func(context.Context, string, string, ...string) (string, error) {
			return "", errors.New("cli down")
		},
	})
	_, err := inst.Search(t.Context(), "docx")
	if err == nil ||
		!strings.Contains(err.Error(), "catalog primary") ||
		!strings.Contains(err.Error(), "catalog fallback") {
		t.Fatalf("error = %v", err)
	}
}

func TestExecCommandEnvDisablesTelemetry(t *testing.T) {
	t.Setenv("DO_NOT_TRACK", "0")
	t.Setenv("GIT_TERMINAL_PROMPT", "1")
	lastValue := func(env []string, key string) string {
		prefix := key + "="
		value := ""
		for _, entry := range env {
			if strings.HasPrefix(entry, prefix) {
				value = strings.TrimPrefix(entry, prefix)
			}
		}
		return value
	}
	env := execCommandEnv()
	if got := lastValue(env, "DO_NOT_TRACK"); got != "1" {
		t.Fatalf("DO_NOT_TRACK = %q, want 1", got)
	}
	if got := lastValue(env, "GIT_TERMINAL_PROMPT"); got != "0" {
		t.Fatalf("GIT_TERMINAL_PROMPT = %q, want 0", got)
	}
}
```

- [x] **Step 2: Run Installer tests and verify RED**

```powershell
go test ./internal/skills -run 'TestInstallerSearch|TestExecCommandEnv' -count=1
```

Expected: compile failure because `InstallerConfig.CatalogSearch` and `execCommandEnv` do not exist.

- [x] **Step 3: Modify `Installer` and `NewInstaller`**

Add to `Installer`:

```go
catalog *catalogSearchService
```

Add to `InstallerConfig`:

```go
// CatalogSearch overrides only the primary JSON transport. Nil uses skills.sh.
CatalogSearch CatalogSearchFunc
```

In `NewInstaller`, after resolving `run`, build:

```go
primary := cfg.CatalogSearch
if primary == nil {
	primary = newSkillsCatalogAPIClient(http.DefaultClient, skillsCatalogAPIURL).Search
}
fallback := func(ctx context.Context, query string) ([]CatalogHit, error) {
	out, err := run(ctx, "", "npx", "skills", "find", query)
	if err != nil {
		return nil, fmt.Errorf("npx skills find: %w", err)
	}
	return parseCatalogHits(out), nil
}
```

Set:

```go
catalog: newCatalogSearchService(primary, fallback, defaultCatalogSearchOptions()),
```

Replace the subprocess block in `Installer.Search` with:

```go
if utf8.RuneCountInString(q) < 2 {
	return CatalogResult{Enabled: true, Query: q, Hits: []CatalogHit{}}, nil
}
hits, err := i.catalog.Search(ctx, q)
if err != nil {
	return CatalogResult{}, fmt.Errorf("skills search %q: %w", q, err)
}
if hits == nil {
	hits = []CatalogHit{}
}
return CatalogResult{Enabled: true, Query: q, Hits: hits}, nil
```

Import `net/http` and `unicode/utf8`.

Extract and use:

```go
func execCommandEnv() []string {
	return append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "DO_NOT_TRACK=1")
}
```

`execCommandRunner` sets `cmd.Env = execCommandEnv()`. Do not change
`npx skills add <source> --copy -y`.

- [x] **Step 4: Run Installer and handler regression tests**

```powershell
gofmt -w internal/skills/installer.go internal/skills/installer_test.go
go test ./internal/skills -count=1
go test ./internal/agui -run GovernanceWriteSkills -count=1
go test ./cmd/aura -run Skills -count=1
```

Expected: direct-primary and CLI fallback tests pass; install argv remains unchanged.

- [x] **Step 5: Commit Installer wiring**

```powershell
git add internal/skills/installer.go internal/skills/installer_test.go
git commit -m "feat(skills): prefer fast catalog search with CLI fallback"
```

### Task 4: Forward browser cancellation through the API helper

**Files:**
- Modify: `web/src/governance/__tests__/governanceWrite.coverage.test.tsx`
- Modify: `web/src/governance/governanceApi.ts`

- [x] **Step 1: Make the API test require signal identity**

Replace the catalog test with:

```ts
it('searchSkillCatalog GETs the encoded query and forwards its AbortSignal', async () => {
  const calls = captureFetch(okJSON({ enabled: true, query: 'go test', hits: [] }));
  const controller = new AbortController();
  const res = await searchSkillCatalog('go test', controller.signal);
  expect(res.enabled).toBe(true);
  expect(first(calls).url).toBe('/api/governance/skills/catalog?q=go%20test');
  expect(first(calls).init.signal).toBe(controller.signal);
});
```

- [x] **Step 2: Run and verify RED**

```powershell
Set-Location web
npm exec vitest -- run src/governance/__tests__/governanceWrite.coverage.test.tsx
```

Expected: TypeScript compile failure because `searchSkillCatalog` accepts one argument.

- [x] **Step 3: Add the optional signal**

Use:

```ts
export async function searchSkillCatalog(
  query: string,
  signal?: AbortSignal,
): Promise<SkillsCatalogResult> {
  return getJSON<SkillsCatalogResult>(
    `${GOV_SKILLS_PATH}/catalog?q=${encodeURIComponent(query)}`,
    signal,
  );
}
```

Also correct the nearby stale comment from “off by default” to “explicit deployment opt-out”.

- [x] **Step 4: Verify and commit**

```powershell
npm exec vitest -- run src/governance/__tests__/governanceWrite.coverage.test.tsx
npm run typecheck
Set-Location ..
git add web/src/governance/governanceApi.ts web/src/governance/__tests__/governanceWrite.coverage.test.tsx
git commit -m "feat(web): propagate skill search cancellation"
```

### Task 5: Debounce the panel and expose accessible states

**Files:**
- Modify: `web/src/governance/__tests__/SkillInstallPanel.test.tsx`
- Modify: `web/src/governance/SkillInstallPanel.tsx`
- Modify: `web/src/i18n/resources.governance.ts`

- [x] **Step 1: Write failing fake-timer behavior tests**

Update the search mock to preserve both arguments:

```ts
searchSkillCatalog: (...args: unknown[]) =>
  searchSkillCatalog(...args) as Promise<SkillsCatalogResult>,
```

Add `act` to the existing Testing Library import so deferred promises settle inside
React's test boundary.

Ensure `afterEach` calls `vi.useRealTimers()`. Add tests for:

```ts
it('requires two characters and collapses rapid typing to one request after 250ms', async () => {
  vi.useFakeTimers();
  renderPanel();
  const search = screen.getByPlaceholderText('Search the skills.sh catalog');
  for (const value of ['d', 'do', 'doc', 'docx']) {
    fireEvent.change(search, { target: { value } });
  }
  expect(searchSkillCatalog).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(249);
  expect(searchSkillCatalog).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(1);
  expect(searchSkillCatalog).toHaveBeenCalledTimes(1);
  expect(searchSkillCatalog.mock.calls[0]?.[0]).toBe('docx');
  expect(searchSkillCatalog.mock.calls[0]?.[1]).toBeInstanceOf(AbortSignal);
});

it('hides stale hits immediately while the next query debounces', async () => {
  vi.useFakeTimers();
  searchSkillCatalog.mockResolvedValue({
    enabled: true, query: 'xlsx',
    hits: [{ source: 'anthropics/skills', skill: 'xlsx', installs: '12K' }],
  });
  renderPanel();
  const search = screen.getByPlaceholderText('Search the skills.sh catalog');
  fireEvent.change(search, { target: { value: 'xlsx' } });
  await vi.advanceTimersByTimeAsync(250);
  vi.useRealTimers();
  expect(await screen.findByRole('button', { name: /anthropics\/skills@xlsx/ })).toBeTruthy();
  fireEvent.change(search, { target: { value: 'docx' } });
  expect(screen.queryByRole('button', { name: /anthropics\/skills@xlsx/ })).toBeNull();
  expect(screen.getByRole('status').textContent).toContain('Searching skills');
});
```

Add these exact tests:

```ts
it('shows the two-character hint without searching one character', async () => {
  vi.useFakeTimers();
  renderPanel();
  fireEvent.change(screen.getByPlaceholderText('Search the skills.sh catalog'), {
    target: { value: 'd' },
  });
  await vi.advanceTimersByTimeAsync(300);
  expect(screen.getByText('Type at least 2 characters.')).toBeTruthy();
  expect(searchSkillCatalog).not.toHaveBeenCalled();
});

it('shows an accessible loading status while the catalog request is pending', async () => {
  vi.useFakeTimers();
  let resolveSearch: (value: SkillsCatalogResult) => void = () => undefined;
  searchSkillCatalog.mockImplementation(
    () =>
      new Promise<SkillsCatalogResult>((resolve) => {
        resolveSearch = resolve;
      }),
  );
  renderPanel();
  fireEvent.change(screen.getByPlaceholderText('Search the skills.sh catalog'), {
    target: { value: 'docx' },
  });
  await vi.advanceTimersByTimeAsync(250);
  expect(screen.getByRole('status').textContent).toContain('Searching skills');
  await act(async () => {
    resolveSearch({ enabled: true, query: 'docx', hits: [] });
  });
});

it('aborts an abandoned query and renders only the latest result', async () => {
  vi.useFakeTimers();
  let resolveFirst: (value: SkillsCatalogResult) => void = () => undefined;
  const signals: AbortSignal[] = [];
  searchSkillCatalog.mockImplementation((query: string, signal: AbortSignal) => {
    signals.push(signal);
    if (query === 'do') {
      return new Promise<SkillsCatalogResult>((resolve) => {
        resolveFirst = resolve;
      });
    }
    return Promise.resolve({
      enabled: true,
      query: 'docx',
      hits: [{ source: 'anthropics/skills', skill: 'docx', installs: '153K' }],
    });
  });
  renderPanel();
  const search = screen.getByPlaceholderText('Search the skills.sh catalog');
  fireEvent.change(search, { target: { value: 'do' } });
  await vi.advanceTimersByTimeAsync(250);
  expect(signals).toHaveLength(1);
  fireEvent.change(search, { target: { value: 'docx' } });
  await vi.advanceTimersByTimeAsync(250);
  expect(signals[0]?.aborted).toBe(true);
  vi.useRealTimers();
  await act(async () => {
    resolveFirst({
      enabled: true,
      query: 'do',
      hits: [{ source: 'owner/repo', skill: 'do-only', installs: '1' }],
    });
  });
  expect(await screen.findByRole('button', { name: /anthropics\/skills@docx/ })).toBeTruthy();
  expect(screen.queryByRole('button', { name: /owner\/repo@do-only/ })).toBeNull();
});

it('renders the empty result state', async () => {
  searchSkillCatalog.mockResolvedValue({ enabled: true, query: 'none', hits: [] });
  renderPanel();
  fireEvent.change(screen.getByPlaceholderText('Search the skills.sh catalog'), {
    target: { value: 'none' },
  });
  expect(await screen.findByText('No skills found.')).toBeTruthy();
});

it('renders a catalog error and recovers on the next query', async () => {
  searchSkillCatalog
    .mockRejectedValueOnce(new Error('HTTP 502'))
    .mockResolvedValueOnce({
      enabled: true,
      query: 'xlsx',
      hits: [{ source: 'anthropics/skills', skill: 'xlsx', installs: '12K' }],
    });
  renderPanel();
  const search = screen.getByPlaceholderText('Search the skills.sh catalog');
  fireEvent.change(search, { target: { value: 'fail' } });
  expect(
    await screen.findByText("Couldn't search the skills catalog. Try again."),
  ).toBeTruthy();
  fireEvent.change(search, { target: { value: 'xlsx' } });
  expect(await screen.findByRole('button', { name: /anthropics\/skills@xlsx/ })).toBeTruthy();
  expect(
    screen.queryByText("Couldn't search the skills catalog. Try again."),
  ).toBeNull();
});

it('keeps the explicit deployment-disabled state', async () => {
  searchSkillCatalog.mockResolvedValue({ enabled: false, query: 'docx', hits: [] });
  renderPanel();
  fireEvent.change(screen.getByPlaceholderText('Search the skills.sh catalog'), {
    target: { value: 'docx' },
  });
  expect(
    await screen.findByText('External discovery is disabled on this deployment.'),
  ).toBeTruthy();
});
```

The empty/error/disabled tests intentionally use real timers; their 250 ms wait exercises
the public behavior and keeps fake-timer cleanup out of those assertions.

Update the existing happy-path test to advance the 250 ms debounce and expect the optional signal as the second call argument.

- [x] **Step 2: Run and verify RED**

```powershell
Set-Location web
npm exec vitest -- run src/governance/__tests__/SkillInstallPanel.test.tsx
```

Expected: failures for immediate call count, missing status/error/empty copy, and no aborted signal.

- [x] **Step 3: Add localized copy**

Under both `governance.skills.install` objects add:

```ts
// English
searchMinChars: 'Type at least 2 characters.',
searching: 'Searching skills…',
searchEmpty: 'No skills found.',
searchError: "Couldn't search the skills catalog. Try again.",

// Italian
searchMinChars: 'Digita almeno 2 caratteri.',
searching: 'Ricerca skill in corso…',
searchEmpty: 'Nessuna skill trovata.',
searchError: 'Impossibile cercare nel catalogo skill. Riprova.',
```

- [x] **Step 4: Implement debounce, cancellation, and state rendering**

Import `useEffect` and add:

```ts
const catalogSearchDelayMs = 250;

function useDebouncedValue(value: string, delayMs: number): string {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [delayMs, value]);
  return debounced;
}
```

In the component derive:

```ts
const trimmedSearch = searchQuery.trim();
const debouncedSearch = useDebouncedValue(trimmedSearch, catalogSearchDelayMs);
const searchEligible = Array.from(trimmedSearch).length >= 2;
const debouncedEligible = Array.from(debouncedSearch).length >= 2;
const searchIsDebouncing = searchEligible && trimmedSearch !== debouncedSearch;
```

Replace the query with:

```ts
const catalog = useQuery({
  queryKey: ['governance', 'skills', 'catalog', debouncedSearch],
  queryFn: ({ signal }) => searchSkillCatalog(debouncedSearch, signal),
  retry: false,
  enabled: debouncedEligible,
});
```

Derive:

```ts
const catalogIsCurrent =
  !searchIsDebouncing && catalog.data?.query === debouncedSearch;
const catalogDisabled = catalogIsCurrent && catalog.data !== undefined && !catalog.data.enabled;
const hits = catalogIsCurrent ? (catalog.data?.hits ?? []) : [];
const showCatalogLoading = searchEligible && (searchIsDebouncing || catalog.isFetching);
const showCatalogEmpty =
  catalogIsCurrent && catalog.isSuccess && catalog.data.enabled && hits.length === 0;
```

Render states in this order below the search input:

```tsx
{trimmedSearch.length === 1 ? (
  <p role="note" className="text-[13px] text-text-muted">
    {t('governance.skills.install.searchMinChars')}
  </p>
) : showCatalogLoading ? (
  <p role="status" className="flex items-center gap-2 text-[13px] text-text-muted">
    <Spinner />
    {t('governance.skills.install.searching')}
  </p>
) : catalog.isError && !searchIsDebouncing ? (
  <Alert variant="destructive">
    <AlertDescription>{t('governance.skills.install.searchError')}</AlertDescription>
  </Alert>
) : catalogDisabled ? (
  // existing disabled note
) : hits.length > 0 ? (
  // existing result list
) : showCatalogEmpty ? (
  <p role="note" className="text-[13px] text-text-muted">
    {t('governance.skills.install.searchEmpty')}
  </p>
) : null}
```

Keep source selection and install mutation code byte-for-byte except for formatting required by Prettier.

- [x] **Step 5: Run focused frontend gates**

```powershell
npm exec vitest -- run src/governance/__tests__/SkillInstallPanel.test.tsx src/governance/__tests__/governanceWrite.coverage.test.tsx
npm run typecheck
npm run lint
npm run format:check
```

Expected: all focused tests pass; no TypeScript, ESLint, or formatting errors.

- [x] **Step 6: Commit the panel slice**

```powershell
Set-Location ..
git add web/src/governance/SkillInstallPanel.tsx `
  web/src/governance/__tests__/SkillInstallPanel.test.tsx `
  web/src/i18n/resources.governance.ts
git commit -m "feat(web): debounce skill catalog search"
```

### Task 6: Add the reproducible live Playwright 10/10 score

**Files:**
- Create: `web/e2e/skill-catalog-search-live.spec.ts`

- [x] **Step 1: Create the opt-in live score spec**

Create the complete file below. It is skipped unless
`AURA_E2E_LIVE_CATALOG=1`, so normal hermetic CI does not depend on skills.sh.

```ts
import { setTimeout as delay } from 'node:timers/promises';
import { expect, test, type Request, type Route } from '@playwright/test';
import { gotoAuthenticated } from './auth';

const live = process.env.AURA_E2E_LIVE_CATALOG === '1';
const catalogPattern = '**/api/governance/skills/catalog?*';

interface CatalogRequestRecord {
  query: string;
  startedAt: number;
  status?: number;
  failed?: string;
}

interface HTTPProblem {
  url: string;
  status: number;
}

function json(body: unknown, status = 200) {
  return { status, contentType: 'application/json', body: JSON.stringify(body) };
}

function compactInstallCount(installs: number): string {
  const safe = Math.max(0, installs);
  if (safe < 1000) return String(safe);
  const divisor = safe >= 1_000_000 ? 1_000_000 : 1000;
  const suffix = divisor === 1_000_000 ? 'M' : 'K';
  const value = safe / divisor;
  const scale = value >= 100 ? 1 : 10;
  return `${String(Math.round(value * scale) / scale)}${suffix}`;
}

function catalogQuery(url: string): string {
  return new URL(url).searchParams.get('q') ?? '';
}

test.describe('live skill catalog performance score', () => {
  test.skip(!live, 'set AURA_E2E_LIVE_CATALOG=1 against a rebuilt Aura');

  test('passes the approved rubric at 10/10', async ({ page, request }, testInfo) => {
    test.setTimeout(45_000);
    const passed = new Set<number>();
    const catalogRequests: CatalogRequestRecord[] = [];
    const requestRecords = new Map<Request, CatalogRequestRecord>();
    const consoleProblems: string[] = [];
    const pageProblems: string[] = [];
    const sameOriginFailures: HTTPProblem[] = [];
    const failedRequests: { url: string; error: string }[] = [];
    let coldMs = 0;
    let warmMs = 0;
    let rapidDocxRequestCount = 0;

    page.on('console', (message) => {
      if (message.type() !== 'error') return;
      const location = message.location().url;
      consoleProblems.push(`${message.text()} ${location}`.trim());
    });
    page.on('pageerror', (error) => pageProblems.push(error.message));
    page.on('request', (browserRequest) => {
      const url = new URL(browserRequest.url());
      if (url.pathname !== '/api/governance/skills/catalog') return;
      const record = {
        query: url.searchParams.get('q') ?? '',
        startedAt: Date.now(),
      };
      catalogRequests.push(record);
      requestRecords.set(browserRequest, record);
    });
    page.on('response', (response) => {
      const record = requestRecords.get(response.request());
      if (record !== undefined) record.status = response.status();
      if (response.status() >= 400) {
        sameOriginFailures.push({ url: response.url(), status: response.status() });
      }
    });
    page.on('requestfailed', (browserRequest) => {
      const error = browserRequest.failure()?.errorText ?? 'request failed';
      const record = requestRecords.get(browserRequest);
      if (record !== undefined) record.failed = error;
      failedRequests.push({ url: browserRequest.url(), error });
    });

    let panel = page.locator('section').first();
    let search = page.getByPlaceholder('Search the skills.sh catalog');

    await test.step('1/10 opens the skill install catalog', async () => {
      await gotoAuthenticated(page, '/');
      await page.getByRole('button', { name: 'Governance', exact: true }).first().click();
      await page.getByRole('tab', { name: 'Skills' }).click();
      await page.getByRole('button', { name: 'Install skill' }).click();
      const heading = page.getByRole('heading', { name: 'Install skill' });
      await expect(heading).toBeVisible();
      panel = page.locator('section').filter({ has: heading }).last();
      search = panel.getByPlaceholder('Search the skills.sh catalog');
      await expect(search).toBeVisible();
      passed.add(1);
    });

    await test.step('2/10 one character emits no request', async () => {
      const before = catalogRequests.length;
      await search.fill('d');
      await delay(350);
      expect(catalogRequests.slice(before)).toHaveLength(0);
      await expect(panel.getByText('Type at least 2 characters.')).toBeVisible();
      passed.add(2);
    });

    let docxResult = panel.getByRole('button', { name: /anthropics\/skills@docx/ }).first();
    await test.step('3/10 rapid docx emits one final request', async () => {
      await search.fill('');
      const before = catalogRequests.length;
      await search.pressSequentially('doc', { delay: 50 });
      const responsePromise = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/governance/skills/catalog' &&
          catalogQuery(response.url()) === 'docx',
      );
      await search.press('x');
      const finalKeystrokeAt = Date.now();

      await test.step('4/10 loading status is accessible', async () => {
        await expect(
          panel.getByRole('status').filter({ hasText: 'Searching skills' }),
        ).toBeVisible();
        passed.add(4);
      });

      await test.step('5/10 cold result is under 1.5 seconds', async () => {
        docxResult = panel.getByRole('button', { name: /anthropics\/skills@docx/ }).first();
        await expect(docxResult).toBeVisible({ timeout: 1500 });
        coldMs = Date.now() - finalKeystrokeAt;
        expect(coldMs).toBeLessThan(1500);
        passed.add(5);
      });

      const response = await responsePromise;
      expect(response.status()).toBe(200);
      const emitted = catalogRequests.slice(before);
      rapidDocxRequestCount = emitted.length;
      expect(emitted).toHaveLength(1);
      expect(emitted[0]?.query).toBe('docx');
      passed.add(3);
    });

    await test.step('6/10 result matches the current official catalog', async () => {
      const officialResponse = await request.get('https://skills.sh/api/search?q=docx', {
        failOnStatusCode: false,
      });
      expect(officialResponse.ok()).toBe(true);
      const official = (await officialResponse.json()) as {
        skills?: { source?: string; skillId?: string; installs?: number }[];
      };
      const top = official.skills?.[0];
      expect(top?.source).toBe('anthropics/skills');
      expect(top?.skillId).toBe('docx');
      const expectedCount = compactInstallCount(top?.installs ?? -1);
      await expect(docxResult).toContainText(expectedCount);
      passed.add(6);
    });

    await test.step('7/10 selecting the hit fills the installable source', async () => {
      await docxResult.click();
      await expect(panel.getByLabel('Source')).toHaveValue('anthropics/skills@docx');
      passed.add(7);
    });

    await test.step('8/10 an abandoned request cannot render stale rows', async () => {
      const staleHandler = async (route: Route) => {
        const query = catalogQuery(route.request().url());
        if (query === 'slow') {
          await delay(700);
          await route
            .fulfill(
              json({
                enabled: true,
                query,
                hits: [{ source: 'owner/repo', skill: 'slow-only', installs: '1' }],
              }),
            )
            .catch(() => undefined);
          return;
        }
        if (query === 'latest') {
          await route.fulfill(
            json({
              enabled: true,
              query,
              hits: [{ source: 'owner/repo', skill: 'latest-only', installs: '2' }],
            }),
          );
          return;
        }
        await route.fallback();
      };
      await page.route(catalogPattern, staleHandler);
      const slowRequest = page.waitForRequest(
        (browserRequest) => catalogQuery(browserRequest.url()) === 'slow',
      );
      await search.fill('slow');
      await slowRequest;
      const latestRequest = page.waitForRequest(
        (browserRequest) => catalogQuery(browserRequest.url()) === 'latest',
      );
      await search.fill('latest');
      await latestRequest;
      await expect(panel.getByRole('button', { name: /owner\/repo@latest-only/ })).toBeVisible();
      await delay(800);
      await expect(panel.getByRole('button', { name: /owner\/repo@slow-only/ })).toHaveCount(0);
      await page.unroute(catalogPattern, staleHandler);
      passed.add(8);
    });

    await test.step('9/10 a 502 is visible and the next query recovers', async () => {
      let failedOnce = false;
      const errorHandler = async (route: Route) => {
        if (catalogQuery(route.request().url()) === 'fail' && !failedOnce) {
          failedOnce = true;
          await route.fulfill(json({ error: 'forced live rubric failure' }, 502));
          return;
        }
        await route.fallback();
      };
      await page.route(catalogPattern, errorHandler);
      await search.fill('fail');
      await expect(
        panel.getByText("Couldn't search the skills catalog. Try again."),
      ).toBeVisible();
      await page.unroute(catalogPattern, errorHandler);
      await search.fill('xlsx');
      await expect(
        panel.getByRole('button', { name: /anthropics\/skills@xlsx/ }).first(),
      ).toBeVisible({ timeout: 2500 });
      await expect(
        panel.getByText("Couldn't search the skills catalog. Try again."),
      ).toHaveCount(0);
      passed.add(9);
    });

    await test.step('10/10 warm response is under 500ms and browser health is clean', async () => {
      await search.fill('');
      const warmResponse = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/governance/skills/catalog' &&
          catalogQuery(response.url()) === 'docx',
      );
      await search.fill('docx');
      const finalInputAt = Date.now();
      const response = await warmResponse;
      warmMs = Date.now() - finalInputAt;
      expect(response.status()).toBe(200);
      expect(warmMs).toBeLessThan(500);
      docxResult = panel.getByRole('button', { name: /anthropics\/skills@docx/ }).first();
      await expect(docxResult).toBeVisible();
      await docxResult.click();
      await expect(panel.getByRole('button', { name: /^Install$/ })).toBeEnabled();

      const appOrigin = new URL(page.url()).origin;
      const unexpectedHTTP = sameOriginFailures.filter((problem) => {
        const url = new URL(problem.url);
        if (url.origin !== appOrigin) return false;
        return !(
          url.pathname === '/api/governance/skills/catalog' &&
          url.searchParams.get('q') === 'fail' &&
          problem.status === 502
        );
      });
      const unexpectedFailed = failedRequests.filter((problem) => {
        const url = new URL(problem.url);
        return !(
          url.origin === appOrigin &&
          url.pathname === '/api/governance/skills/catalog' &&
          url.searchParams.get('q') === 'slow'
        );
      });
      const unexpectedConsole = consoleProblems.filter(
        (problem) =>
          !(
            problem.includes('/api/governance/skills/catalog') &&
            problem.includes('502')
          ),
      );
      expect(unexpectedHTTP).toEqual([]);
      expect(unexpectedFailed).toEqual([]);
      expect(unexpectedConsole).toEqual([]);
      expect(pageProblems).toEqual([]);
      passed.add(10);
    });

    expect([...passed].sort((a, b) => a - b)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    const evidence = {
      score: `${passed.size}/10`,
      coldMs,
      warmMs,
      rapidDocxRequestCount,
      requests: catalogRequests,
      consoleProblems,
      pageProblems,
      sameOriginFailures,
      failedRequests,
    };
    await testInfo.attach('skill-catalog-score.json', {
      contentType: 'application/json',
      body: Buffer.from(JSON.stringify(evidence, null, 2)),
    });
    testInfo.annotations.push({ type: 'score', description: `${passed.size}/10` });
  });
});
```

- [x] **Step 2: Typecheck and list the opt-in test**

```powershell
Set-Location web
npm run typecheck
npm run test:e2e -- e2e/skill-catalog-search-live.spec.ts --project=chrome --list
```

Expected: one test is listed; no TypeScript error.

- [x] **Step 3: Commit the score harness**

```powershell
Set-Location ..
git add web/e2e/skill-catalog-search-live.spec.ts
git commit -m "test(e2e): score live skill catalog search"
```

### Task 7: Run full verification and refresh the embedded web build

**Files:**
- Refresh: `internal/webui/dist/**`

- [x] **Step 1: Run fresh Go verification**

```powershell
go vet ./...
go build ./...
go test -count=1 ./...
go test -race -count=1 ./internal/skills ./internal/agui ./cmd/aura
```

Expected: every command exits 0; no race or goleak report.

- [x] **Step 2: Run fresh frontend verification**

```powershell
Set-Location web
npm run lint
npm run typecheck
npm run format:check
npm run test
npm run build
Set-Location ..
```

Expected: lint/typecheck/format/build exit 0 and the full Vitest coverage suite passes its configured floor.

- [x] **Step 3: Verify generated assets and source limits**

```powershell
go test ./internal/webui -count=1
bash scripts/check-file-size.sh
git diff --check
git status --short
```

Expected: embedded handler tests and size gate pass; only intended source plus
`internal/webui/dist/**` changes appear.

- [x] **Step 4: Commit the reproducible embedded dist**

```powershell
git add internal/webui/dist
git commit -m "build(web): refresh embedded skill search UI"
```

Do not stage `.planning/graphs/**` or any unrelated original-worktree file.

**Execution evidence (2026-07-17):**

- Full untagged Go suite passed under WSL; touched-package race runs passed.
- Frontend lint, typecheck, format, build, and the worker-bounded full Vitest suite passed: 181 files / 1,484 tests, with 92.91% statements, 86.98% branches, 93.13% functions, and 94.74% lines.
- `internal/skills` measured 88.1% on the full integration matrix; the repository-wide matrix exposed only the unrelated stale `TestMigration0039RoundTrip` expectation after migration 0040.
- `catalog_search.go` mutation score is 80.56% (58/72), above the 70% gate.
- Embedded web handler tests, the generated binary marker, source-size gate, and post-merge focused verification passed.
- Delivery commits: `6aef4e2c6` (quality hardening), `7a969db64` (embedded build), merged by `f571b37af`.

### Task 8: Rebuild localhost and prove score 10/10

**Files:**
- No new source files
- Evidence: Playwright terminal output and `skill-catalog-score.json` attachment

- [ ] **Step 1: Rebuild and recreate only the Aura service**

Run from the execution worktree:

```powershell
Set-Location D:\Aura\.worktrees\skill-catalog-search
docker compose --env-file D:\Aura\.env -f .\compose.yaml build aura
docker compose --env-file D:\Aura\.env -f .\compose.yaml up -d --no-deps --force-recreate aura
$healthy = $false
foreach ($attempt in 1..60) {
  try {
    $response = Invoke-WebRequest -UseBasicParsing http://localhost:9080/healthz -TimeoutSec 2
    if ($response.StatusCode -eq 200) {
      $healthy = $true
      break
    }
  } catch {
    Start-Sleep -Seconds 1
  }
}
if (-not $healthy) {
  throw 'rebuilt Aura did not become healthy within 60 seconds'
}
```

Expected: Compose uses the implementation worktree as its build context, the image
contains the implementation commit, and the recreated container answers health checks.

- [ ] **Step 2: Prove the running binary serves the new UI**

```powershell
Invoke-WebRequest -UseBasicParsing http://localhost:9080/healthz
docker exec aura aura version
docker exec aura sh -lc "grep -aF 'Searching skills' /usr/local/bin/aura >/dev/null"
```

Expected: all commands exit 0; the binary marker rejects a stale embedded SPA.

- [ ] **Step 3: Run the real Chrome Playwright score**

```powershell
Set-Location web
$env:AURA_E2E_ORIGIN = 'http://localhost:9080'
$env:AURA_E2E_LIVE_CATALOG = '1'
npm run test:e2e -- e2e/skill-catalog-search-live.spec.ts --project=chrome
```

Expected: `1 passed`; attachment reports `"score": "10/10"`, one `q=docx` request for
rapid typing, cold latency `<1500`, warm latency `<500`, and no unexpected browser or
same-origin failures.

- [ ] **Step 4: Inspect and assert the emitted score evidence**

```powershell
$evidencePath = Get-ChildItem test-results -Recurse -File -Filter 'skill-catalog-score*.json' |
  Sort-Object LastWriteTime -Descending |
  Select-Object -First 1
if ($null -eq $evidencePath) {
  throw 'Playwright score attachment was not produced'
}
$evidence = Get-Content -Raw $evidencePath.FullName | ConvertFrom-Json
if ($evidence.score -ne '10/10') { throw "score was $($evidence.score)" }
if ($evidence.rapidDocxRequestCount -ne 1) {
  throw "rapid docx emitted $($evidence.rapidDocxRequestCount) requests"
}
if ($evidence.coldMs -ge 1500) { throw "cold latency was $($evidence.coldMs)ms" }
if ($evidence.warmMs -ge 500) { throw "warm latency was $($evidence.warmMs)ms" }
$evidence | ConvertTo-Json -Depth 8
```

Expected: score `10/10`, exactly one rapid-typing request, cold `<1500 ms`, warm
`<500 ms`, and the printed request list explains the original symptom's correction.

- [ ] **Step 5: Inspect the final diff and commit state**

```powershell
Set-Location ..
git status --short
git log --oneline --decorate master..HEAD
git diff --check master...HEAD
git diff --stat master...HEAD
```

Expected: implementation commits only, no unrelated dirty files, and every approved
design requirement maps to passing test or live score evidence.

- [ ] **Step 6: Integrate the verified branch without overwriting user work**

Return to `D:\Aura`, confirm its worktree state, then fast-forward or merge
`fix/skill-catalog-search-performance` only if the current master has not diverged in
overlapping files. If it has diverged, merge normally and rerun Tasks 7–8 from the
merged state. Never use reset, checkout-discard, or stash on the user's files.
