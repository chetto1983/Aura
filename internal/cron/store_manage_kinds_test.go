package cron

// store_manage_kinds_test.go pins which kinds the operator and the agent may manage and
// cancel, and that the cockpit board leaves system sweeps out. No database: the board read
// runs over the sqlc fake from store_fake_test.go.

import (
	"context"
	"testing"
)

func TestManageableAndCancellableKinds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		kind        TaskKind
		manageable  bool
		cancellable bool
	}{
		{KindReminder, true, true},
		{KindAgentJob, true, true},
		{KindBackupPostgres, true, false},
		{KindIdentityPurge, false, false},
		{KindRetentionSweep, false, false},
		{KindMemoryEmbedBackfill, false, false},
	}
	for _, c := range cases {
		if got := IsUserManageableKind(c.kind); got != c.manageable {
			t.Errorf("IsUserManageableKind(%s) = %v, want %v", c.kind, got, c.manageable)
		}
		if got := IsCancellableKind(c.kind); got != c.cancellable {
			t.Errorf("IsCancellableKind(%s) = %v, want %v", c.kind, got, c.cancellable)
		}
	}
}

func TestStoreFakeListManageableTasksLeavesSystemSweepsOut(t *testing.T) {
	t.Parallel()
	sweep := schedulerTaskRow(t)
	sweep[1] = string(KindIdentityPurge)
	f := &cronFakeDBTX{queryRows: &cronFakeRows{rows: [][]any{schedulerTaskRow(t), sweep}}}

	got, err := storeWithFake(f).ListManageableTasks(context.Background())
	if err != nil {
		t.Fatalf("ListManageableTasks: %v", err)
	}
	if len(got) != 1 || got[0].Kind != KindReminder {
		t.Fatalf("board = %+v, want only the reminder", got)
	}
}
