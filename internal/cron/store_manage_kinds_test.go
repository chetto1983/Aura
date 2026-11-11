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
		{KindBackupObjects, true, false},
		{KindPIMWatch, true, true},
		{KindPIMDigest, true, true},
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

func TestTheAgentCanStopWhatTheOperatorCanExceptTheWatch(t *testing.T) {
	t.Parallel()
	for kind, want := range map[TaskKind]bool{
		KindReminder: true, KindAgentJob: true, KindPIMWatch: false, KindPIMDigest: false,
		KindBackupPostgres: false, KindBackupObjects: false, KindIdentityPurge: false,
	} {
		if got := IsAgentStoppableKind(kind); got != want {
			t.Errorf("IsAgentStoppableKind(%s) = %v, want %v", kind, got, want)
		}
	}
}

func TestABackupIsManageableAndNotCancellable(t *testing.T) {
	t.Parallel()
	for _, kind := range []TaskKind{KindBackupPostgres, KindBackupObjects} {
		if !IsUserManageableKind(kind) || IsCancellableKind(kind) {
			t.Errorf("%s: manageable %v, cancellable %v", kind, IsUserManageableKind(kind), IsCancellableKind(kind))
		}
	}
}

func TestTheMailWatchKindsPause(t *testing.T) {
	t.Parallel()
	for _, kind := range []TaskKind{KindPIMWatch, KindPIMDigest} {
		if !IsPausableKind(kind) {
			t.Errorf("IsPausableKind(%s) = false: a hard failure could not pause the account's task", kind)
		}
	}
}

func TestStoreFakeListLiveTasksOfKindProjectsItsRows(t *testing.T) {
	t.Parallel()
	row := schedulerTaskRow(t)
	row[1] = string(KindPIMWatch)
	f := &cronFakeDBTX{queryRows: &cronFakeRows{rows: [][]any{row}}}

	got, err := storeWithFake(f).ListLiveTasksOfKind(context.Background(), "identity-1", KindPIMWatch)
	if err != nil {
		t.Fatalf("ListLiveTasksOfKind: %v", err)
	}
	if len(got) != 1 || got[0].Kind != KindPIMWatch {
		t.Fatalf("tasks = %+v, want the one watch task", got)
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

// A scheduled message is the operator's: they and the agent may cancel it (spec Part 2).
func TestAMessageTaskIsCancellable(t *testing.T) {
	t.Parallel()
	if !IsCancellableKind(KindMessage) || !IsAgentStoppableKind(KindMessage) {
		t.Fatalf("message: cancellable %v, agent-stoppable %v", IsCancellableKind(KindMessage), IsAgentStoppableKind(KindMessage))
	}
}

// Decision 14: a message is cancelled, never paused; a resumed one would send text reviewed
// for another time.
func TestAMessageTaskIsNotPausable(t *testing.T) {
	t.Parallel()
	if IsPausableKind(KindMessage) || !IsPausableKind(KindReminder) {
		t.Fatalf("pausable: message %v, reminder %v", IsPausableKind(KindMessage), IsPausableKind(KindReminder))
	}
}
