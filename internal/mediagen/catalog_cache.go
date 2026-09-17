package mediagen

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"
)

// ErrCatalogUnavailable marks a media model catalog that could not be read. Callers get
// this error, never an empty list that would look like a provider with no models.
var ErrCatalogUnavailable = errors.New("media model catalog unavailable")

const (
	catalogTTL = 5 * time.Minute
	// catalogTimeout bounds one refresh, list plus endpoint enrichment, with the same
	// budget as the LLM model picker's catalogue probe.
	catalogTimeout = 20 * time.Second
)

type catalogFetchFunc func(ctx context.Context, baseURL string, kind Kind) ([]Model, error)

type catalogCacheKey struct {
	BaseURL string
	Kind    Kind
}

type catalogCacheEntry struct {
	Models    []Model
	ExpiresAt time.Time
}

// Catalog is the daemon-owned media model cache that both the tools' clamp and the
// settings picker read, keyed by normalized base URL and kind.
type Catalog struct {
	fetch catalogFetchFunc
	now   func() time.Time

	mu      sync.Mutex
	entries map[catalogCacheKey]catalogCacheEntry
	// refreshes holds a one-slot semaphore per key: same-key refreshes queue and
	// re-check the cache, other keys never wait, and a waiter can still give up.
	refreshes map[catalogCacheKey]chan struct{}
}

func newCatalog(fetch catalogFetchFunc, now func() time.Time) *Catalog {
	return &Catalog{
		fetch:     fetch,
		now:       now,
		entries:   make(map[catalogCacheKey]catalogCacheEntry),
		refreshes: make(map[catalogCacheKey]chan struct{}),
	}
}

// List returns kind's models at baseURL sorted by ID: from the cache while it is fresh,
// from the provider when it is stale or refresh is set. A failed fetch leaves any
// still-valid entry in place.
func (c *Catalog) List(ctx context.Context, baseURL string, kind Kind, refresh bool) ([]Model, error) {
	key := catalogCacheKey{BaseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"), Kind: kind}
	if !refresh {
		if models, ok := c.fresh(key); ok {
			return models, nil
		}
	}
	slot := c.refreshSlot(key)
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-slot }()
	if !refresh {
		if models, ok := c.fresh(key); ok {
			return models, nil
		}
	}

	fetchCtx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	fetched, err := c.fetch(fetchCtx, key.BaseURL, kind)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrCatalogUnavailable, err)
	}
	models := cloneModels(fetched)
	slices.SortFunc(models, func(a, b Model) int { return strings.Compare(a.ID, b.ID) })
	c.mu.Lock()
	c.entries[key] = catalogCacheEntry{Models: models, ExpiresAt: c.now().Add(catalogTTL)}
	c.mu.Unlock()
	return cloneModels(models), nil
}

// Find returns the catalog row for id, or nil when the catalog does not list it: a model
// missing from the catalog is sent unclamped and OpenRouter validates the request.
func (c *Catalog) Find(ctx context.Context, baseURL string, kind Kind, id string) (*Model, error) {
	models, err := c.List(ctx, baseURL, kind, false)
	if err != nil {
		return nil, err
	}
	for i := range models {
		if models[i].ID == id {
			return &models[i], nil
		}
	}
	return nil, nil
}

// uncheckedOptionsNote tells the caller the request went out as asked because the catalog could
// not be read, not because the model declared every option in it.
const uncheckedOptionsNote = "the model catalog is unavailable, so the options were not checked against the model; OpenRouter validates them"

// Entry finds model in the catalog. An unreadable catalog is treated like a model the catalog
// does not list: no entry, so the request goes out unclamped, and one note, because refusing a
// paid call over a free lookup helps nobody. Any other catalog error is returned.
func (c *Catalog) Entry(ctx context.Context, baseURL string, kind Kind, model string) (*Model, []string, error) {
	entry, err := c.Find(ctx, baseURL, kind, model)
	switch {
	case errors.Is(err, ErrCatalogUnavailable):
		return nil, []string{uncheckedOptionsNote}, nil
	case err != nil:
		return nil, nil, err
	}
	return entry, []string{}, nil
}

func (c *Catalog) fresh(key catalogCacheKey) ([]Model, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || !c.now().Before(entry.ExpiresAt) {
		return nil, false
	}
	return cloneModels(entry.Models), true
}

func (c *Catalog) refreshSlot(key catalogCacheKey) chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	slot, ok := c.refreshes[key]
	if !ok {
		slot = make(chan struct{}, 1)
		c.refreshes[key] = slot
	}
	return slot
}

func cloneModels(models []Model) []Model {
	out := make([]Model, len(models))
	for i, m := range models {
		m.Parameters = cloneParameters(m.Parameters)
		m.Durations = slices.Clone(m.Durations)
		m.Resolutions = slices.Clone(m.Resolutions)
		m.AspectRatios = slices.Clone(m.AspectRatios)
		m.FrameImages = slices.Clone(m.FrameImages)
		m.PricingSKUs = maps.Clone(m.PricingSKUs)
		m.ImagePricing = slices.Clone(m.ImagePricing)
		out[i] = m
	}
	return out
}

func cloneParameters(params map[string]Parameter) map[string]Parameter {
	if params == nil {
		return nil
	}
	out := make(map[string]Parameter, len(params))
	for name, p := range params {
		p.Values = slices.Clone(p.Values)
		p.Min = cloneInt(p.Min)
		p.Max = cloneInt(p.Max)
		out[name] = p
	}
	return out
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	copied := *v
	return &copied
}
