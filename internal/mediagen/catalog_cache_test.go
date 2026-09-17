package mediagen

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeCatalogSource struct {
	calls  atomic.Int32
	models []Model
	err    error
}

func (s *fakeCatalogSource) fetch(ctx context.Context, baseURL string, kind Kind) ([]Model, error) {
	s.calls.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	out := make([]Model, 0, len(s.models))
	for _, m := range s.models {
		m.Kind = kind
		out = append(out, m)
	}
	return out, nil
}

// fakeClock is a settable clock that a supervisor goroutine may read while a test advances it.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(by)
}

func catalogFixture() []Model {
	return []Model{
		{
			ID: "zeta/video", Durations: []int{5, 10}, Resolutions: []string{"720p"},
			PricingSKUs: map[string]string{"duration_seconds": "0.1"},
		},
		{
			ID: "alpha/image",
			Parameters: map[string]Parameter{
				"aspect_ratio":     {Type: "enum", Values: []string{"1:1", "16:9"}},
				"input_references": {Type: "range", Min: new(0), Max: new(4)},
			},
			ImagePricing: []PriceLine{{Billable: "output_image", Unit: "image", CostUSD: 0.04}},
		},
	}
}

func TestCatalogCachesForFiveMinutes(t *testing.T) {
	source := &fakeCatalogSource{models: catalogFixture()}
	clock := &fakeClock{at: time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)}
	catalog := newCatalog(source.fetch, clock.now)
	ctx := context.Background()

	first, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{first[0].ID, first[1].ID}; !slices.Equal(got, []string{"alpha/image", "zeta/video"}) {
		t.Fatalf("models not sorted by id: %q", got)
	}
	clock.advance(catalogTTL - time.Nanosecond)
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1/", KindImage, false); err != nil {
		t.Fatal(err)
	}
	if calls := source.calls.Load(); calls != 1 {
		t.Fatalf("fetches within the TTL = %d, want 1", calls)
	}
	clock.advance(time.Nanosecond)
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false); err != nil {
		t.Fatal(err)
	}
	if calls := source.calls.Load(); calls != 2 {
		t.Fatalf("fetches after expiry = %d, want 2", calls)
	}
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, true); err != nil {
		t.Fatal(err)
	}
	if calls := source.calls.Load(); calls != 3 {
		t.Fatalf("fetches after explicit refresh = %d, want 3", calls)
	}
}

func TestCatalogSeparatesOriginsAndKinds(t *testing.T) {
	source := &fakeCatalogSource{models: catalogFixture()}
	catalog := newCatalog(source.fetch, (&fakeClock{at: time.Now()}).now)
	ctx := context.Background()
	for _, call := range []struct {
		baseURL string
		kind    Kind
	}{
		{"https://openrouter.ai/api/v1", KindImage},
		{"https://openrouter.ai/api/v1", KindVideo},
		{"https://proxy.example/api/v1", KindImage},
		{" https://openrouter.ai/api/v1/ ", KindImage},
		{"https://proxy.example/api/v1//", KindImage},
	} {
		models, err := catalog.List(ctx, call.baseURL, call.kind, false)
		if err != nil {
			t.Fatal(err)
		}
		if models[0].Kind != call.kind {
			t.Fatalf("%s %s: served kind %s", call.baseURL, call.kind, models[0].Kind)
		}
	}
	if calls := source.calls.Load(); calls != 3 {
		t.Fatalf("fetches = %d, want one per normalized origin and kind (3)", calls)
	}
}

func TestCatalogFailureIsNeverAnEmptyCatalog(t *testing.T) {
	boom := errors.New("GET images/models: 503")
	source := &fakeCatalogSource{err: boom}
	clock := &fakeClock{at: time.Now()}
	catalog := newCatalog(source.fetch, clock.now)
	ctx := context.Background()

	models, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false)
	if !errors.Is(err, ErrCatalogUnavailable) || !errors.Is(err, boom) || models != nil {
		t.Fatalf("List = %v, %v; want an unavailable error and no models", models, err)
	}
	if _, err := catalog.Find(ctx, "https://openrouter.ai/api/v1", KindImage, "alpha/image"); !errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("Find on an unavailable catalog = %v, want error", err)
	}

	source.err = nil
	source.models = catalogFixture()
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false); err != nil {
		t.Fatalf("retry after a failed fetch: %v", err)
	}

	source.err = boom
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, true); !errors.Is(err, boom) {
		t.Fatalf("failed refresh = %v, want error", err)
	}
	calls := source.calls.Load()
	kept, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false)
	if err != nil || len(kept) != 2 || source.calls.Load() != calls {
		t.Fatalf("failed refresh erased the valid entry: %v, %v", kept, err)
	}

	clock.advance(catalogTTL)
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false); !errors.Is(err, boom) {
		t.Fatalf("expired entry with a failing source = %v, want error, never stale models", err)
	}
}

func TestCatalogFindReturnsCopyOrNil(t *testing.T) {
	source := &fakeCatalogSource{models: catalogFixture()}
	catalog := newCatalog(source.fetch, (&fakeClock{at: time.Now()}).now)
	ctx := context.Background()

	found, err := catalog.Find(ctx, "https://openrouter.ai/api/v1", KindImage, "alpha/image")
	if err != nil || found == nil || found.ID != "alpha/image" || *found.Parameters["input_references"].Max != 4 {
		t.Fatalf("Find = %#v, %v", found, err)
	}
	missing, err := catalog.Find(ctx, "https://openrouter.ai/api/v1", KindImage, "vendor/unknown")
	if err != nil || missing != nil {
		t.Fatalf("Find(missing) = %#v, %v; want nil, nil", missing, err)
	}
}

func TestCatalogCallersCannotMutateTheCache(t *testing.T) {
	source := &fakeCatalogSource{models: catalogFixture()}
	catalog := newCatalog(source.fetch, (&fakeClock{at: time.Now()}).now)
	ctx := context.Background()

	models, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false)
	if err != nil {
		t.Fatal(err)
	}
	image, video := &models[0], &models[1]
	image.Parameters["aspect_ratio"].Values[0] = "9:16"
	*image.Parameters["input_references"].Max = 99
	image.Parameters["seed"] = Parameter{Type: "boolean"}
	image.ImagePricing[0].CostUSD = 9
	video.Durations[0] = 60
	video.Resolutions[0] = "4K"
	video.PricingSKUs["duration_seconds"] = "9"
	source.models[1].Parameters["aspect_ratio"].Values[1] = "2:3"

	again, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false)
	if err != nil {
		t.Fatal(err)
	}
	image, video = &again[0], &again[1]
	if image.Parameters["aspect_ratio"].Values[0] != "1:1" || image.Parameters["aspect_ratio"].Values[1] != "16:9" ||
		*image.Parameters["input_references"].Max != 4 || len(image.Parameters) != 2 ||
		image.ImagePricing[0].CostUSD != 0.04 || video.Durations[0] != 5 || video.Resolutions[0] != "720p" ||
		video.PricingSKUs["duration_seconds"] != "0.1" {
		t.Fatalf("cache mutated through a returned or fetched value: %#v", again)
	}
}

func TestCatalogSerializesSameKeyRefreshes(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	var calls atomic.Int32
	fetch := func(ctx context.Context, baseURL string, kind Kind) ([]Model, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
			<-release
		}
		return catalogFixture(), nil
	}
	catalog := newCatalog(fetch, (&fakeClock{at: time.Now()}).now)
	ctx := context.Background()

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			_, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindVideo, false)
			errs <- err
		})
	}
	<-started
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, false); err != nil {
		t.Fatalf("a different key waited on a running refresh: %v", err)
	}
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("fetches = %d, want one per key (2)", got)
	}
}

func TestCatalogWaiterHonoursCancellation(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	fetch := func(ctx context.Context, baseURL string, kind Kind) ([]Model, error) {
		close(started)
		<-release
		return catalogFixture(), nil
	}
	catalog := newCatalog(fetch, (&fakeClock{at: time.Now()}).now)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = catalog.List(context.Background(), "https://openrouter.ai/api/v1", KindImage, false)
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := catalog.List(ctx, "https://openrouter.ai/api/v1", KindImage, true); !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting refresh with a cancelled context = %v, want context.Canceled", err)
	}
	close(release)
	<-done
}

func TestCatalogBoundsEachRefresh(t *testing.T) {
	var deadline time.Time
	fetch := func(ctx context.Context, baseURL string, kind Kind) ([]Model, error) {
		var ok bool
		if deadline, ok = ctx.Deadline(); !ok {
			return nil, errors.New("refresh context has no deadline")
		}
		return nil, nil
	}
	catalog := newCatalog(fetch, time.Now)
	before := time.Now()
	if _, err := catalog.List(context.Background(), "https://openrouter.ai/api/v1", KindImage, true); err != nil {
		t.Fatal(err)
	}
	after := time.Now()
	if deadline.Before(before.Add(catalogTimeout)) || deadline.After(after.Add(catalogTimeout)) {
		t.Fatalf("refresh deadline %v is not %v after the call (%v..%v)", deadline, catalogTimeout, before, after)
	}
}

// TestCatalogEntryTreatsAnUnreadableCatalogAsAnUnlistedModel pins the one decision Entry adds
// over Find: refusing a paid generation because a free lookup failed helps nobody, so an
// unreadable catalog reads as a model the catalog does not list — no entry, and one note the
// caller shows. Any other failure is still an error, because it is not a catalog verdict.
func TestCatalogEntryTreatsAnUnreadableCatalogAsAnUnlistedModel(t *testing.T) {
	const baseURL = "https://openrouter.ai/api/v1"
	listed := &fakeCatalogSource{models: catalogFixture()}
	catalog := newCatalog(listed.fetch, time.Now)

	entry, notes, err := catalog.Entry(context.Background(), baseURL, KindImage, "alpha/image")
	if err != nil || entry == nil || entry.ID != "alpha/image" || len(notes) != 0 {
		t.Fatalf("listed model = %v, %q, %v; want the entry and no note", entry, notes, err)
	}

	entry, notes, err = catalog.Entry(context.Background(), baseURL, KindImage, "acme/unlisted")
	if err != nil || entry != nil || len(notes) != 0 {
		t.Fatalf("unlisted model = %v, %q, %v; want no entry and no note", entry, notes, err)
	}

	down := &fakeCatalogSource{err: errors.New("503 Service Unavailable")}
	entry, notes, err = newCatalog(down.fetch, time.Now).Entry(context.Background(), baseURL, KindImage, "alpha/image")
	if err != nil || entry != nil || !slices.Equal(notes, []string{uncheckedOptionsNote}) {
		t.Fatalf("unreadable catalog = %v, %q, %v; want no entry and the unchecked-options note", entry, notes, err)
	}

	// A refusal that is not the catalog's verdict must still be an error. The only such error
	// List returns is a waiter giving up, so the slot is held by an in-flight refresh: with the
	// slot free, the send and the cancellation are both ready and select picks either one.
	release, started := make(chan struct{}), make(chan struct{})
	blocking := newCatalog(func(context.Context, string, Kind) ([]Model, error) {
		close(started)
		<-release
		return catalogFixture(), nil
	}, time.Now)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = blocking.List(context.Background(), baseURL, KindImage, false)
	}()
	<-started
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := blocking.Entry(cancelled, baseURL, KindImage, "alpha/image"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled lookup = %v, want context.Canceled: only a catalog verdict is swallowed", err)
	}
	close(release)
	<-done
}
