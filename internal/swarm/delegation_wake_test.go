package swarm

import (
	"context"
	"errors"
	"testing"
)

type wakeRows struct {
	rows       []UndrainedResult
	err        error
	unfinished map[string]int
}

func (s *wakeRows) ListPendingDelegationResults(context.Context, int) ([]UndrainedResult, error) {
	return s.rows, s.err
}

func (s *wakeRows) CountUnfinishedDelegationJobs(_ context.Context, _ string, fanout string) (int, error) {
	return s.unfinished[fanout], nil
}

func TestWakeCompletedFanoutsGroupsAndDefersUnfinishedWork(t *testing.T) {
	store := &wakeRows{rows: []UndrainedResult{
		{IdentityID: "owner-a", ConversationID: "conv-a", FanoutKey: "fan-a"},
		{IdentityID: "owner-a", ConversationID: "conv-a", FanoutKey: "fan-a"},
		{IdentityID: "owner-b", ConversationID: "conv-b", FanoutKey: "fan-b"},
	}, unfinished: map[string]int{"fan-b": 1}}
	var called []string
	delivery := &DelegationDelivery{PendingResults: store, Counter: store, Resume: func(_ context.Context, owner, conv string) (bool, error) {
		called = append(called, owner+":"+conv)
		return true, nil
	}}
	n, err := delivery.WakeCompletedFanouts(context.Background(), 20)
	if err != nil || n != 1 || len(called) != 1 || called[0] != "owner-a:conv-a" {
		t.Fatalf("wake=%d called=%v err=%v", n, called, err)
	}
	delivery.Resume = func(context.Context, string, string) (bool, error) { return false, nil }
	if n, err := delivery.WakeCompletedFanouts(context.Background(), 20); err != nil || n != 0 {
		t.Fatalf("busy parent must defer: n=%d err=%v", n, err)
	}
}

func TestWakeCompletedFanoutsErrorsAndDisabledPath(t *testing.T) {
	if n, err := (*DelegationDelivery)(nil).WakeCompletedFanouts(context.Background(), 10); err != nil || n != 0 {
		t.Fatalf("nil delivery: %d %v", n, err)
	}
	failure := errors.New("store unavailable")
	store := &wakeRows{err: failure}
	delivery := &DelegationDelivery{PendingResults: store, Counter: store, Resume: func(context.Context, string, string) (bool, error) { return false, failure }}
	if _, err := delivery.WakeCompletedFanouts(context.Background(), 10); !errors.Is(err, failure) {
		t.Fatalf("lost read error: %v", err)
	}
	store.err = nil
	store.rows = []UndrainedResult{{IdentityID: "a", ConversationID: "c", FanoutKey: "f"}}
	if _, err := delivery.WakeCompletedFanouts(context.Background(), 10); !errors.Is(err, failure) {
		t.Fatalf("lost resume error: %v", err)
	}
}
