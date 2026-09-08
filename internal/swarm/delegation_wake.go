package swarm

import (
	"context"
	"errors"
	"fmt"
)

// PendingDelegationResults reads only the current identity's registered wake inputs.
type PendingDelegationResults interface {
	ListPendingDelegationResults(context.Context, int) ([]UndrainedResult, error)
}

// WakeCompletedFanouts reuses the saved steer rows as continuation intent. Busy
// parents leave them untouched; the normal runner's atomic drain consumes them once.
func (d *DelegationDelivery) WakeCompletedFanouts(ctx context.Context, limit int) (int, error) {
	if d == nil || d.Resume == nil || d.PendingResults == nil || d.Counter == nil {
		return 0, nil
	}
	rows, err := d.PendingResults.ListPendingDelegationResults(ctx, limit)
	if err != nil {
		return 0, err
	}
	groups, err := groupByFanout(rows)
	if err != nil {
		return 0, err
	}
	woken := 0
	var failures []error
	for _, group := range groups {
		unfinished, err := d.Counter.CountUnfinishedDelegationJobs(ctx, group.identityID, group.fanoutKey)
		if err != nil {
			failures = append(failures, fmt.Errorf("coordinator wake: count fan-out: %w", err))
			continue
		}
		if unfinished != 0 {
			continue
		}
		started, err := d.Resume(ctx, group.identityID, group.conversationID)
		if err != nil {
			failures = append(failures, fmt.Errorf("coordinator wake: resume: %w", err))
			continue
		}
		if started {
			woken++
		}
	}
	return woken, errors.Join(failures...)
}
