package swarm

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// delegation_fanout_test.go is delegation_fanout.go's own daemon-free coverage
// (51-11 Task 3): groupByFanout's own grouping, nudgeFanout's eligibility
// refusal, its claim-then-render-then-deliver happy path, its concurrency
// contract, the undecodable-body case, the owns-but-failed outbox write, and
// the N-line budget property already TelegramDelegationMessage's own test
// covers -- reasserted here through the grouped path's own render call, not a
// second implementation of the rendering itself.

// fakeFanoutJobCounter scripts CountUnfinishedDelegationJobs by (identityID,
// fanoutKey) key, and records every call so a test can assert the eligibility
// check ran (or, on the still-running case, that the claim method never did).
type fakeFanoutJobCounter struct {
	mu     sync.Mutex
	counts map[string]int
	err    error
	calls  []string
}

func (f *fakeFanoutJobCounter) CountUnfinishedDelegationJobs(_ context.Context, identityID, fanoutKey string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, identityID+"/"+fanoutKey)
	if f.err != nil {
		return 0, f.err
	}
	return f.counts[identityID+"/"+fanoutKey], nil
}

// TestGroupByFanoutPartitionsMixedRows: two fan-outs plus one legacy keyless
// row -- groups are keyed by (identityID, fanoutKey), row order within a
// group is preserved, and the keyless row is returned separately rather than
// as a third one-row group.
func TestGroupByFanoutPartitionsMixedRows(t *testing.T) {
	rows := []UndrainedResult{
		{ID: "r1", IdentityID: "id-1", FanoutKey: "f-a", Body: "one"},
		{ID: "r2", IdentityID: "id-1", FanoutKey: "f-b", Body: "two"},
		{ID: "r3", IdentityID: "id-1", FanoutKey: "f-a", Body: "three"},
		{ID: "r4", IdentityID: "id-1", Body: "legacy, no key"},
	}
	groups, keyless := groupByFanout(rows)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2", len(groups))
	}
	if len(keyless) != 1 || keyless[0].ID != "r4" {
		t.Fatalf("keyless = %+v, want exactly row r4", keyless)
	}
	var groupA, groupB *fanoutGroup
	for i := range groups {
		switch groups[i].fanoutKey {
		case "f-a":
			groupA = &groups[i]
		case "f-b":
			groupB = &groups[i]
		}
	}
	if groupA == nil || groupB == nil {
		t.Fatalf("groups = %+v, want one keyed f-a and one keyed f-b", groups)
	}
	if len(groupA.rows) != 2 || groupA.rows[0].ID != "r1" || groupA.rows[1].ID != "r3" {
		t.Fatalf("f-a group rows = %+v, want [r1, r3] in swept order", groupA.rows)
	}
	if len(groupB.rows) != 1 || groupB.rows[0].ID != "r2" {
		t.Fatalf("f-b group rows = %+v, want [r2]", groupB.rows)
	}
}

// TestGroupByFanoutScopesByIdentity: two DIFFERENT identities sharing the SAME
// fanout_key string (a theoretical collision, since delegationFanoutKey hashes
// identityID in) must never be merged into one group.
func TestGroupByFanoutScopesByIdentity(t *testing.T) {
	rows := []UndrainedResult{
		{ID: "r1", IdentityID: "id-1", FanoutKey: "f-shared"},
		{ID: "r2", IdentityID: "id-2", FanoutKey: "f-shared"},
	}
	groups, _ := groupByFanout(rows)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (scoped by identity, not fanout_key alone)", len(groups))
	}
}

// oneReportBody marshals a single-report []ChildReport body, the shape a
// aura.steer_queue delegation_result row actually carries.
func oneReportBody(t *testing.T, goalIndex int, status, goal string) string {
	t.Helper()
	body, err := marshalReports([]ChildReport{{GoalIndex: goalIndex, ChildID: "w" + strconv.Itoa(goalIndex+1), Status: status, Goal: goal, Summary: "summary"}})
	if err != nil {
		t.Fatalf("marshalReports: %v", err)
	}
	return body
}

// TestFanoutSkipsWhileAWorkerRuns (acceptance criteria's own named test): the
// eligibility check refuses the claim while a sibling is still unfinished --
// ZERO rows claimed, ZERO deliveries, and the claim method (MarkFanoutNudged)
// is never called for that group, the named cost of D-15's decision.
func TestFanoutSkipsWhileAWorkerRuns(t *testing.T) {
	channel := &fakeChannelDeliverer{delivered: true}
	nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{
		{ID: "r1", IdentityID: "id-1", FanoutKey: "f-a", Body: oneReportBody(t, 0, StatusOK, "goal 1")},
	}}
	counter := &fakeFanoutJobCounter{counts: map[string]int{"id-1/f-a": 1}}
	d := &DelegationDelivery{Channel: channel, Nudge: nudge, Counter: counter, NudgeAfter: time.Minute}

	n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
	if err != nil {
		t.Fatalf("NudgeUndrained = %v", err)
	}
	if n != 0 {
		t.Fatalf("nudged = %d, want 0 while a sibling is unfinished", n)
	}
	if len(channel.calls) != 0 {
		t.Fatalf("channel calls = %d, want 0", len(channel.calls))
	}
	if len(nudge.claimed) != 0 {
		t.Fatalf("claimed rows = %v, want none -- the claim method must never run while unfinished > 0", nudge.claimed)
	}
	if len(counter.calls) != 1 || counter.calls[0] != "id-1/f-a" {
		t.Fatalf("counter calls = %v, want exactly one call for id-1/f-a", counter.calls)
	}
}

// TestFanoutClaimsRendersOnceAndDelivers: the eligible, happy path -- every
// row of the group is claimed in one MarkFanoutNudged call, decoded and
// concatenated into one []ChildReport slice sorted by GoalIndex, rendered
// ONCE through TelegramDelegationMessage, and delivered ONCE.
func TestFanoutClaimsRendersOnceAndDelivers(t *testing.T) {
	channel := &fakeChannelDeliverer{delivered: true}
	nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{
		{ID: "r1", IdentityID: "id-1", FanoutKey: "f-a", Body: oneReportBody(t, 1, StatusFailed, "second goal")},
		{ID: "r2", IdentityID: "id-1", FanoutKey: "f-a", Body: oneReportBody(t, 0, StatusOK, "first goal")},
	}}
	counter := &fakeFanoutJobCounter{counts: map[string]int{"id-1/f-a": 0}}
	d := &DelegationDelivery{Channel: channel, Nudge: nudge, Counter: counter, NudgeAfter: time.Minute}

	n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
	if err != nil {
		t.Fatalf("NudgeUndrained = %v", err)
	}
	if n != 1 {
		t.Fatalf("nudged = %d, want 1 (one send for the whole fan-out)", n)
	}
	if len(channel.calls) != 1 {
		t.Fatalf("channel calls = %d, want exactly 1 -- one message for the whole fan-out, never one per worker", len(channel.calls))
	}
	want := TelegramDelegationMessage([]ChildReport{
		{GoalIndex: 0, ChildID: "w1", Status: StatusOK, Goal: "first goal", Summary: "summary"},
		{GoalIndex: 1, ChildID: "w2", Status: StatusFailed, Goal: "second goal", Summary: "summary"},
	})
	if channel.calls[0].text != want {
		t.Fatalf("delivered text = %q, want the reports concatenated and sorted by GoalIndex: %q", channel.calls[0].text, want)
	}
	if len(nudge.claimed) != 2 || !nudge.claimed["r1"] || !nudge.claimed["r2"] {
		t.Fatalf("claimed = %v, want both r1 and r2 claimed", nudge.claimed)
	}
}

// TestFanoutUndecodableBodyStillSendsAndCounts: a row whose body fails to
// decode contributes nothing to the rendered reports but still counts as
// claimed -- an all-undecodable group still sends the no-report shape rather
// than nothing.
func TestFanoutUndecodableBodyStillSendsAndCounts(t *testing.T) {
	channel := &fakeChannelDeliverer{delivered: true}
	nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{
		{ID: "r1", IdentityID: "id-1", FanoutKey: "f-a", Body: "not json"},
		{ID: "r2", IdentityID: "id-1", FanoutKey: "f-a", Body: "also not json"},
	}}
	d := &DelegationDelivery{Channel: channel, Nudge: nudge, NudgeAfter: time.Minute}

	n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
	if err != nil {
		t.Fatalf("NudgeUndrained = %v", err)
	}
	if n != 1 {
		t.Fatalf("nudged = %d, want 1", n)
	}
	if len(channel.calls) != 1 {
		t.Fatalf("channel calls = %d, want 1", len(channel.calls))
	}
	if want := TelegramDelegationMessage(nil); channel.calls[0].text != want {
		t.Fatalf("delivered text = %q, want the no-report shape %q", channel.calls[0].text, want)
	}
	if len(nudge.claimed) != 2 {
		t.Fatalf("claimed = %v, want both rows claimed despite neither decoding", nudge.claimed)
	}
}

// TestFanoutOwnsButFailedWritesOneOutboxRow: the owns-but-failed branch writes
// ONE pending_notifications row for the WHOLE fan-out, referencing the FIRST
// claimed row's id -- never N rows and never the raw report.
func TestFanoutOwnsButFailedWritesOneOutboxRow(t *testing.T) {
	channel := &fakeChannelDeliverer{err: errors.New("telegram down")}
	nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{
		{ID: "r1", IdentityID: "id-1", FanoutKey: "f-a", Body: oneReportBody(t, 0, StatusOK, "goal")},
		{ID: "r2", IdentityID: "id-1", FanoutKey: "f-a", Body: oneReportBody(t, 1, StatusFailed, "goal 2")},
	}}
	pending := &fakePendingNotificationStore{}
	d := &DelegationDelivery{Channel: channel, Nudge: nudge, Pending: pending, NudgeAfter: time.Minute}

	n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
	if err != nil {
		t.Fatalf("NudgeUndrained = %v", err)
	}
	if n != 1 {
		t.Fatalf("nudged = %d, want 1 -- the row(s) stay claimed even though delivery failed", n)
	}
	if len(pending.calls) != 1 {
		t.Fatalf("pending calls = %d, want exactly 1 for the whole fan-out", len(pending.calls))
	}
	if pending.calls[0].steerQueueID != "r1" {
		t.Fatalf("pending steerQueueID = %q, want the FIRST claimed row's id r1", pending.calls[0].steerQueueID)
	}
	if strings.Contains(pending.calls[0].body, `"goal_index"`) {
		t.Fatalf("pending body = %q, want the rendered message, never the raw report JSON", pending.calls[0].body)
	}
}

// TestFanoutSendsOnceUnderConcurrency (acceptance criteria's own named test):
// two goroutines racing the SAME complete fan-out through NudgeUndrained
// produce exactly one recorded delivery -- MarkFanoutNudged's claim-before-
// push ordering is the mutex, not a lock this test takes itself.
func TestFanoutSendsOnceUnderConcurrency(t *testing.T) {
	defer goleak.VerifyNone(t)
	channel := &fakeChannelDeliverer{delivered: true}
	nudge := &fakeSteerNudgeStore{rows: []UndrainedResult{
		{ID: "r1", IdentityID: "id-1", FanoutKey: "f-a", Body: oneReportBody(t, 0, StatusOK, "goal")},
		{ID: "r2", IdentityID: "id-1", FanoutKey: "f-a", Body: oneReportBody(t, 1, StatusOK, "goal 2")},
	}}
	counter := &fakeFanoutJobCounter{counts: map[string]int{"id-1/f-a": 0}}
	d := &DelegationDelivery{Channel: channel, Nudge: nudge, Counter: counter, NudgeAfter: time.Minute}

	var wg sync.WaitGroup
	results := make([]int, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n, err := d.NudgeUndrained(context.Background(), time.Now(), 10)
			if err != nil {
				t.Errorf("NudgeUndrained = %v", err)
			}
			results[i] = n
		}(i)
	}
	wg.Wait()

	total := results[0] + results[1]
	if total != 1 {
		t.Fatalf("total nudged across both passes = %d, want exactly 1", total)
	}
	if len(channel.calls) != 1 {
		t.Fatalf("channel calls = %d, want exactly 1 recorded delivery", len(channel.calls))
	}
}
