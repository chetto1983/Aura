package learningretention

import (
	"context"
	"time"
)

// Store is the bounded storage seam used by the compactor.
type Store interface {
	Name() string
	Expired(context.Context, time.Time, int) ([]Candidate, error)
	Buckets(context.Context, int) ([]string, error)
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
}

// CompactBatch runs one bounded pass.
func (c *Compactor) CompactBatch(context.Context) (Report, error) { return Report{}, nil }
