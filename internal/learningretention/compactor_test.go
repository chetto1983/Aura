package learningretention

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestReservoirBoundariesNewestQuarterAndInputOrder(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	for _, size := range []int{0, 1, 511, 512, 513} {
		in := makeCandidates(size, now)
		got := Select("reasoning", "high", "learning-v1", in, 512)
		want := size
		if want > 512 {
			want = 512
		}
		if len(got) != want {
			t.Fatalf("size %d: retained %d, want %d", size, len(got), want)
		}
	}

	in := makeCandidates(600, now)
	got := Select("reasoning", "high", "learning-v1", in, 512)
	kept := hashSet(got)
	for i := 472; i < 600; i++ { // ceil(512*25/100) = 128 newest entries.
		if !kept[fmt.Sprintf("hash-%04d", i)] {
			t.Fatalf("newest reservation omitted candidate %d", i)
		}
	}
	reversed := append([]Candidate(nil), in...)
	for left, right := 0, len(reversed)-1; left < right; left, right = left+1, right-1 {
		reversed[left], reversed[right] = reversed[right], reversed[left]
	}
	if !reflect.DeepEqual(sortedHashes(got), sortedHashes(Select("reasoning", "high", "learning-v1", reversed, 512))) {
		t.Fatal("selection changed when input order changed")
	}
}

func TestReservoirGoldenClampsScoresAndUsesStableSeed(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	in := []Candidate{
		{Hash: "a", UpdatedAt: now.Add(-5 * time.Hour), Quality: -5, Novelty: 3},
		{Hash: "b", UpdatedAt: now.Add(-4 * time.Hour), Quality: .9, Novelty: .8},
		{Hash: "c", UpdatedAt: now.Add(-3 * time.Hour), Quality: .1, Novelty: .2},
		{Hash: "d", UpdatedAt: now.Add(-2 * time.Hour), Quality: .5, Novelty: .5},
		{Hash: "e", UpdatedAt: now.Add(-time.Hour), Quality: 1.5, Novelty: -2},
	}
	// One newest slot is reserved; two weighted slots use SHA-256(store|bucket|policy|hash)
	// divided by fixed-point weight 1+3*quality_milli+2*novelty_milli.
	if got := sortedHashes(Select("tool_selection", "other", "learning-v1", in, 3)); !reflect.DeepEqual(got, []string{"a", "b", "e"}) {
		t.Fatalf("golden selection = %v", got)
	}
}

func TestCompactorExpiryBeforeCapsAndBoundedDeletes(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	store := newFakeStore("reasoning", append(
		[]Candidate{{Hash: "expired", Bucket: "high", UpdatedAt: now.Add(-91 * 24 * time.Hour)}},
		makeBucketCandidates("high", 7, now)...,
	))
	var metrics []Metric
	c := Compactor{Stores: []Store{store}, Config: Config{
		TTL: 90 * 24 * time.Hour, BucketCap: 5, StoreCap: 10, BatchSize: 2,
		BucketLimit: 8, PolicyVersion: "learning-v1",
	}, Now: func() time.Time { return now }, Metrics: func(m Metric) { metrics = append(metrics, m) }}
	report, err := c.CompactBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Expired != 1 || report.Evicted != 2 || store.maxRead > 7 || store.maxDelete > 2 {
		t.Fatalf("report/bounds = %+v, read=%d delete=%d", report, store.maxRead, store.maxDelete)
	}
	if len(metrics) == 0 {
		t.Fatal("no learning compaction metrics emitted")
	}
	for _, metric := range metrics {
		if metric.Operation != "learning_compact" || (metric.ToolClass != "data" && metric.ToolClass != "other") {
			t.Fatalf("unbounded metric labels: %+v", metric)
		}
	}
}

func TestCompactorCancellationAndPartialFailureConvergeOnRerun(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	store := newFakeStore("tool_selection", makeBucketCandidates("secret-tool-name", 7, now))
	store.deleteErr = errors.New("neo4j interrupted")
	store.partialDelete = 1
	c := Compactor{Stores: []Store{store}, Config: Config{TTL: 90 * 24 * time.Hour, BucketCap: 5, StoreCap: 10, BatchSize: 2, BucketLimit: 8, PolicyVersion: "learning-v1"}, Now: func() time.Time { return now }}
	if _, err := c.CompactBatch(context.Background()); !errors.Is(err, store.deleteErr) {
		t.Fatalf("partial failure = %v", err)
	}
	store.deleteErr = nil
	store.partialDelete = 0
	if _, err := c.CompactBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if count, _ := store.BucketCount(context.Background(), "secret-tool-name"); count != 5 {
		t.Fatalf("rerun count = %d, want 5", count)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	before := store.calls
	if _, err := c.CompactBatch(cancelled); !errors.Is(err, context.Canceled) || store.calls != before {
		t.Fatalf("cancel = %v, calls %d -> %d", err, before, store.calls)
	}
}

func TestCompactorGlobalPressureUsesBoundedWindow(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	all := append(makeBucketCandidates("a", 5_001, now), makeBucketCandidates("b", 5_001, now.Add(time.Hour))...)
	store := newFakeStore("reasoning", all)
	c := Compactor{Stores: []Store{store}, Config: Config{TTL: 90 * 24 * time.Hour, BucketCap: 6_000, StoreCap: 10_000, BatchSize: 2, BucketLimit: 8, PolicyVersion: "learning-v1"}, Now: func() time.Time { return now }}
	report, err := c.CompactBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if report.Evicted != 2 || len(store.items) != 10_000 || store.maxRead > 10_002 || store.maxDelete > 2 {
		t.Fatalf("global report/items/bounds = %+v/%d/%d/%d", report, len(store.items), store.maxRead, store.maxDelete)
	}
}

func TestCompactorRotatesPastFirstBucketPage(t *testing.T) {
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	all := append(makeBucketCandidates("a", 1, now), makeBucketCandidates("b", 1, now)...)
	all = append(all, makeBucketCandidates("c", 3, now)...)
	store := newFakeStore("reasoning", all)
	c := Compactor{Stores: []Store{store}, Config: Config{
		TTL: 90 * 24 * time.Hour, BucketCap: 2, StoreCap: 100, BatchSize: 1,
		BucketLimit: 2, PolicyVersion: "learning-v1",
	}, Now: func() time.Time { return now }}

	first, err := c.CompactBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.Evicted != 0 {
		t.Fatalf("first page evicted = %d, want 0", first.Evicted)
	}
	second, err := c.CompactBatch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Evicted != 1 {
		t.Fatalf("second page evicted = %d, want 1", second.Evicted)
	}
	if count, _ := store.BucketCount(context.Background(), "c"); count != 2 {
		t.Fatalf("second-page bucket count = %d, want 2", count)
	}
}

func makeCandidates(n int, now time.Time) []Candidate { return makeBucketCandidates("bucket", n, now) }

func makeBucketCandidates(bucket string, n int, now time.Time) []Candidate {
	out := make([]Candidate, n)
	for i := range out {
		out[i] = Candidate{Hash: fmt.Sprintf("hash-%s-%04d", bucket, i), Bucket: bucket, UpdatedAt: now.Add(time.Duration(i) * time.Second), Quality: float64(i%10) / 10, Novelty: float64((i*3)%10) / 10, PolicyVersion: "learning-v1"}
		if bucket == "bucket" {
			out[i].Hash = fmt.Sprintf("hash-%04d", i)
		}
	}
	return out
}

func sortedHashes(in []Candidate) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = in[i].Hash
	}
	sort.Strings(out)
	return out
}

func hashSet(in []Candidate) map[string]bool {
	out := make(map[string]bool, len(in))
	for _, candidate := range in {
		out[candidate.Hash] = true
	}
	return out
}

type fakeStore struct {
	mu                        sync.Mutex
	name                      string
	items                     map[string]Candidate
	deleteErr                 error
	partialDelete             int
	calls, maxRead, maxDelete int
}

func newFakeStore(name string, candidates []Candidate) *fakeStore {
	f := &fakeStore{name: name, items: make(map[string]Candidate)}
	for _, candidate := range candidates {
		f.items[candidate.Hash] = candidate
	}
	return f
}

func (f *fakeStore) Name() string { return f.name }
func (f *fakeStore) Expired(ctx context.Context, cutoff time.Time, limit int) ([]Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var out []Candidate
	for _, candidate := range f.items {
		if candidate.UpdatedAt.Before(cutoff) {
			out = append(out, candidate)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].Hash < out[j].Hash
		}
		return out[i].UpdatedAt.Before(out[j].UpdatedAt)
	})
	return f.bound(out, limit), ctx.Err()
}
func (f *fakeStore) Buckets(ctx context.Context, after string, limit int) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	set := map[string]bool{}
	for _, c := range f.items {
		set[c.Bucket] = true
	}
	var out []string
	for bucket := range set {
		if bucket > after {
			out = append(out, bucket)
		}
	}
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, ctx.Err()
}
func (f *fakeStore) BucketCount(ctx context.Context, bucket string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	n := 0
	for _, c := range f.items {
		if c.Bucket == bucket {
			n++
		}
	}
	return n, ctx.Err()
}
func (f *fakeStore) Candidates(ctx context.Context, bucket string, limit int) ([]Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var out []Candidate
	for _, c := range f.items {
		if c.Bucket == bucket {
			out = append(out, c)
		}
	}
	return f.bound(out, limit), ctx.Err()
}
func (f *fakeStore) TotalCount(ctx context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return len(f.items), ctx.Err()
}
func (f *fakeStore) GlobalCandidates(ctx context.Context, limit int) ([]Candidate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	var out []Candidate
	for _, c := range f.items {
		out = append(out, c)
	}
	return f.bound(out, limit), ctx.Err()
}
func (f *fakeStore) Delete(ctx context.Context, candidates []Candidate) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if len(candidates) > f.maxDelete {
		f.maxDelete = len(candidates)
	}
	if f.deleteErr != nil {
		deleted := min(f.partialDelete, len(candidates))
		for _, candidate := range candidates[:deleted] {
			delete(f.items, candidate.Hash)
		}
		return deleted, f.deleteErr
	}
	for _, candidate := range candidates {
		delete(f.items, candidate.Hash)
	}
	return len(candidates), ctx.Err()
}
func (f *fakeStore) bound(in []Candidate, limit int) []Candidate {
	if len(in) > f.maxRead {
		f.maxRead = len(in)
	}
	if len(in) > limit {
		in = in[:limit]
	}
	return in
}
