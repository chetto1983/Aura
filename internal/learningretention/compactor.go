package learningretention

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Store is the bounded storage seam used by the compactor.
type Store interface {
	Name() string
	Expired(context.Context, time.Time, int) ([]Candidate, error)
	Buckets(context.Context, string, int) ([]string, error)
	BucketCount(context.Context, string) (int, error)
	Candidates(context.Context, string, int) ([]Candidate, error)
	TotalCount(context.Context) (int, error)
	GlobalCandidates(context.Context, int) ([]Candidate, error)
	Delete(context.Context, []Candidate) (int, error)
}

// Metric is a content-free, finite-label learning capacity signal.
type Metric struct {
	Operation, ToolClass, Outcome, ErrorClass, State string
	Size, Count                                      int
	OldestAge                                        time.Duration
}

// Config defines one bounded compaction pass.
type Config struct {
	TTL           time.Duration
	BucketCap     int
	StoreCap      int
	BatchSize     int
	BucketLimit   int
	PolicyVersion string
}

// Report summarizes a bounded pass.
type Report struct {
	Scanned, Expired, Evicted int
}

// Compactor removes expired and over-cap learned examples in bounded pages.
type Compactor struct {
	Stores  []Store
	Config  Config
	Now     func() time.Time
	Metrics func(Metric)

	cursorMu     sync.Mutex
	bucketCursor map[string]string
}

// CompactBatch runs one bounded pass. Expiry is always processed before cap
// pressure, and a full expiry page defers cap work until the next pass so an
// expired candidate is never selected for retention.
func (c *Compactor) CompactBatch(ctx context.Context) (Report, error) {
	var report Report
	if err := ctx.Err(); err != nil {
		return report, err
	}
	cfg := c.config()
	for _, store := range c.Stores {
		if store == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return report, err
		}
		expired, err := store.Expired(ctx, c.now().Add(-cfg.TTL), cfg.BatchSize)
		if err != nil {
			c.record(store, "error", "failed", "internal", len(expired), -1, oldestAge(c.now(), expired))
			return report, fmt.Errorf("learning compaction %s expiry read: %w", store.Name(), err)
		}
		report.Scanned += len(expired)
		if len(expired) > 0 {
			deleted, err := store.Delete(ctx, expired[:min(len(expired), cfg.BatchSize)])
			if err != nil {
				c.record(store, "error", "failed", "internal", deleted, -1, oldestAge(c.now(), expired))
				return report, fmt.Errorf("learning compaction %s expiry delete: %w", store.Name(), err)
			}
			report.Expired += deleted
			c.record(store, "success", "expired", "none", deleted, -1, oldestAge(c.now(), expired))
			if len(expired) == cfg.BatchSize {
				continue
			}
		}

		after := c.currentBucketCursor(store.Name())
		buckets, err := store.Buckets(ctx, after, cfg.BucketLimit)
		if err != nil {
			return report, fmt.Errorf("learning compaction %s buckets: %w", store.Name(), err)
		}
		c.advanceBucketCursor(store.Name(), buckets, cfg.BucketLimit)
		for _, bucket := range buckets {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			count, err := store.BucketCount(ctx, bucket)
			if err != nil {
				return report, fmt.Errorf("learning compaction %s bucket count: %w", store.Name(), err)
			}
			if count <= cfg.BucketCap {
				continue
			}
			window := min(count, cfg.BucketCap+cfg.BatchSize)
			candidates, err := store.Candidates(ctx, bucket, window)
			if err != nil {
				return report, fmt.Errorf("learning compaction %s candidates: %w", store.Name(), err)
			}
			report.Scanned += len(candidates)
			victims := rejected(candidates, Select(store.Name(), bucket, cfg.PolicyVersion, candidates, cfg.BucketCap), cfg.BatchSize)
			deleted, err := store.Delete(ctx, victims)
			if err != nil {
				c.record(store, "error", "failed", "internal", deleted, count, oldestAge(c.now(), candidates))
				return report, fmt.Errorf("learning compaction %s bucket delete: %w", store.Name(), err)
			}
			report.Evicted += deleted
			c.record(store, "success", "completed", "none", deleted, max(0, count-deleted), oldestAge(c.now(), candidates))
		}

		if err := ctx.Err(); err != nil {
			return report, err
		}
		total, err := store.TotalCount(ctx)
		if err != nil {
			return report, fmt.Errorf("learning compaction %s total count: %w", store.Name(), err)
		}
		if total <= cfg.StoreCap {
			c.record(store, "skipped", "completed", "none", 0, total, 0)
			continue
		}
		window := min(total, cfg.StoreCap+cfg.BatchSize)
		candidates, err := store.GlobalCandidates(ctx, window)
		if err != nil {
			return report, fmt.Errorf("learning compaction %s global candidates: %w", store.Name(), err)
		}
		report.Scanned += len(candidates)
		victims := rejected(candidates, Select(store.Name(), "global", cfg.PolicyVersion, candidates, cfg.StoreCap), cfg.BatchSize)
		deleted, err := store.Delete(ctx, victims)
		if err != nil {
			c.record(store, "error", "failed", "internal", deleted, total, oldestAge(c.now(), candidates))
			return report, fmt.Errorf("learning compaction %s global delete: %w", store.Name(), err)
		}
		report.Evicted += deleted
		c.record(store, "success", "completed", "none", deleted, max(0, total-deleted), oldestAge(c.now(), candidates))
	}
	return report, nil
}

func (c *Compactor) currentBucketCursor(store string) string {
	c.cursorMu.Lock()
	defer c.cursorMu.Unlock()
	return c.bucketCursor[store]
}

func (c *Compactor) advanceBucketCursor(store string, buckets []string, limit int) {
	c.cursorMu.Lock()
	defer c.cursorMu.Unlock()
	if c.bucketCursor == nil {
		c.bucketCursor = make(map[string]string)
	}
	if len(buckets) == limit {
		c.bucketCursor[store] = buckets[len(buckets)-1]
		return
	}
	delete(c.bucketCursor, store)
}

func (c *Compactor) config() Config {
	cfg := c.Config
	if cfg.TTL <= 0 {
		cfg.TTL = 90 * 24 * time.Hour
	}
	if cfg.BucketCap <= 0 {
		cfg.BucketCap = 512
	}
	if cfg.StoreCap <= 0 {
		cfg.StoreCap = 10_000
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 128
	}
	if cfg.BucketLimit <= 0 {
		cfg.BucketLimit = 128
	}
	if cfg.PolicyVersion == "" {
		cfg.PolicyVersion = "learning-v1"
	}
	return cfg
}

func (c *Compactor) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Compactor) record(store Store, outcome, state, errorClass string, count, size int, age time.Duration) {
	if c.Metrics == nil {
		return
	}
	toolClass := "other"
	if store.Name() == "reasoning" {
		toolClass = "data"
	}
	c.Metrics(Metric{Operation: "learning_compact", ToolClass: toolClass, Outcome: outcome,
		ErrorClass: errorClass, State: state, Size: size, Count: count, OldestAge: age})
}

func rejected(candidates, retained []Candidate, limit int) []Candidate {
	keep := make(map[string]bool, len(retained))
	for _, candidate := range retained {
		keep[candidate.Hash] = true
	}
	removed := make([]Candidate, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		if !keep[candidate.Hash] {
			removed = append(removed, candidate)
			if len(removed) == limit {
				break
			}
		}
	}
	return removed
}

func oldestAge(now time.Time, candidates []Candidate) time.Duration {
	var oldest time.Time
	for _, candidate := range candidates {
		if oldest.IsZero() || candidate.UpdatedAt.Before(oldest) {
			oldest = candidate.UpdatedAt
		}
	}
	if oldest.IsZero() || !oldest.Before(now) {
		return 0
	}
	return now.Sub(oldest)
}
