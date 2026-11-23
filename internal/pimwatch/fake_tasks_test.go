package pimwatch

import (
	"context"
	"strings"
)

// fakeTasks is the scheduler in memory. err fails the listings, runNowErr fails RunNow.
type fakeTasks struct {
	watches   map[string]WatchTask
	digests   map[string]WatchTask
	specs     map[string]WatchSpec
	digest    map[string]DigestSpec
	created   int
	runNow    []string
	err       error
	runNowErr error
}

func newFakeTasks() *fakeTasks {
	return &fakeTasks{
		watches: map[string]WatchTask{}, digests: map[string]WatchTask{},
		specs: map[string]WatchSpec{}, digest: map[string]DigestSpec{},
	}
}

func (f *fakeTasks) list(from map[string]WatchTask, identityID string) []WatchTask {
	var out []WatchTask
	for key, task := range from {
		if strings.HasPrefix(key, identityID+"|") {
			out = append(out, task)
		}
	}
	return out
}

func (f *fakeTasks) Watches(_ context.Context, identityID string) ([]WatchTask, error) {
	return f.list(f.watches, identityID), f.err
}

func (f *fakeTasks) Digests(_ context.Context, identityID string) ([]WatchTask, error) {
	return f.list(f.digests, identityID), f.err
}

func (f *fakeTasks) CreateWatch(_ context.Context, identityID string, spec WatchSpec) error {
	f.created++
	key := identityID + "|" + spec.AccountID
	f.watches[key] = WatchTask{ID: key, AccountID: spec.AccountID}
	f.specs[key] = spec
	return nil
}

func (f *fakeTasks) CreateDigest(_ context.Context, identityID string, spec DigestSpec) error {
	f.created++
	key := identityID + "|digest"
	f.digests[key] = WatchTask{ID: key}
	f.digest[key] = spec
	return nil
}

func (f *fakeTasks) RescheduleDigest(_ context.Context, taskID string, spec DigestSpec) error {
	f.digest[taskID] = spec
	return nil
}

func (f *fakeTasks) Resume(_ context.Context, taskID string) error {
	for _, tasks := range []map[string]WatchTask{f.watches, f.digests} {
		if task, ok := tasks[taskID]; ok {
			task.Paused = false
			tasks[taskID] = task
		}
	}
	return nil
}

func (f *fakeTasks) Cancel(_ context.Context, taskID string) error {
	delete(f.watches, taskID)
	delete(f.digests, taskID)
	return nil
}

func (f *fakeTasks) RunNow(_ context.Context, taskID string) error {
	f.runNow = append(f.runNow, taskID)
	return f.runNowErr
}
