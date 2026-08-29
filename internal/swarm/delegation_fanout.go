// delegation_fanout.go holds the whole grouped-delivery concern the absent-operator
// nudge sweep needs once one swarm_spawn call may fan out to N workers (51-11 Task 3,
// CONTEXT D-15, PRD Amendment #172 point 2: "uno per fan-out"): grouping the sweep's
// candidate rows by fan-out key, the eligibility check ("is any sibling still running or
// parked"), and the grouped claim-then-render-then-deliver NudgeUndrained's own doc
// comment describes. Split out of delegation_delivery.go rather than grown inline,
// mirroring this package's own report.go/brief.go/delegation_artifact.go/
// delegation_enqueue.go concern-split precedent.
package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
)

// FanoutJobCounter is nudgeFanout's eligibility seam: how many swarm_delegation jobs of
// one identity/fan-out key have not yet reached a terminal status. Declared HERE
// (primitive-typed: identityID/fanoutKey strings, an int count), adapted at cmd/aura onto
// documents.PostgresIngestionJobStore.CountUnfinishedDelegationJobs -- this package still
// gains no import edge into internal/documents beyond the one DelegationJobStore
// (delegation_queue.go) already opens.
type FanoutJobCounter interface {
	CountUnfinishedDelegationJobs(ctx context.Context, identityID, fanoutKey string) (int, error)
}

// fanoutGroup is one fan-out's swept rows, all sharing one (identityID, fanoutKey) pair.
type fanoutGroup struct {
	identityID string
	fanoutKey  string
	rows       []UndrainedResult
}

// groupByFanout partitions the sweep's candidate rows into fan-out groups (a non-empty
// FanoutKey, one group per distinct (identityID, fanoutKey) pair, rows kept in swept
// order) and a leftover slice of keyless rows -- a legacy row written before migration
// 0108, or a worker's own question pushed through Deliver rather than DeliverReport
// (Deliver never writes a fanout_key). The caller routes the keyless slice through the
// pre-existing per-row nudgeOne path unchanged; only a fan-out group is new behaviour.
func groupByFanout(rows []UndrainedResult) (groups []fanoutGroup, keyless []UndrainedResult) {
	index := make(map[string]int, len(rows))
	for _, row := range rows {
		if row.FanoutKey == "" {
			keyless = append(keyless, row)
			continue
		}
		key := row.IdentityID + "\x00" + row.FanoutKey
		if i, ok := index[key]; ok {
			groups[i].rows = append(groups[i].rows, row)
			continue
		}
		index[key] = len(groups)
		groups = append(groups, fanoutGroup{identityID: row.IdentityID, fanoutKey: row.FanoutKey, rows: []UndrainedResult{row}})
	}
	return groups, keyless
}

// nudgeFanout claims and delivers ONE fan-out group's message -- the grouped counterpart
// to nudgeOne. It counts the fan-out's unfinished jobs FIRST (a nil d.Counter degrades to
// "always eligible", DelegationDelivery's own doc) and returns claimed=false, touching no
// row, while the count is non-zero: this is the named cost of D-15's decision
// (NudgeUndrained's own doc) -- a fan-out with a sibling still running or parked in
// awaiting_input stays silent on the phone until every sibling reaches a terminal status,
// while each finished sibling's own cockpit card (DeliverReport) lands immediately either
// way.
//
// Once eligible, it claims every unclaimed row of the group through MarkFanoutNudged in
// ONE statement -- the same claim-before-push ordering nudgeOne uses, generalized from one
// row to the set (NudgeUndrained's own doc). The UPDATE returns every claimed row's body,
// including a sibling inserted after the candidate SELECT but before the claim; rendering
// from the pre-claim group would mark that late row without ever including it. It then
// decodes and concatenates every CLAIMED row's body into []ChildReport, sorts by GoalIndex,
// and renders ONCE through
// TelegramDelegationMessage: the same bounded, glyph-vocabulary rendering nudgeOne already
// uses for a fan-out of one, generalized to N. A row whose body fails to decode
// contributes nothing to the slice but still counts as claimed (row.ID matched
// MarkFanoutNudged's returned set); an all-undecodable group still sends the no-report
// message rather than nothing. An empty claim (every row already claimed by a concurrent
// pass) returns claimed=false with no further action -- the SAME race nudgeOne's own doc
// describes, generalized from a single-row conditional UPDATE to a set-claiming one.
func (d *DelegationDelivery) nudgeFanout(ctx context.Context, group fanoutGroup) (bool, error) {
	if d.Counter != nil {
		unfinished, err := d.Counter.CountUnfinishedDelegationJobs(ctx, group.identityID, group.fanoutKey)
		if err != nil {
			return false, fmt.Errorf("count unfinished delegation jobs: %w", err)
		}
		if unfinished > 0 {
			return false, nil
		}
	}
	claimedRows, err := d.Nudge.MarkFanoutNudged(ctx, group.identityID, group.fanoutKey)
	if err != nil {
		return false, fmt.Errorf("mark fanout nudged: %w", err)
	}
	if len(claimedRows) == 0 {
		return false, nil
	}
	var reports []ChildReport
	firstClaimedRowID := claimedRows[0].ID
	for _, row := range claimedRows {
		if row.ID < firstClaimedRowID {
			firstClaimedRowID = row.ID
		}
		var rowReports []ChildReport
		if err := json.Unmarshal([]byte(row.Body), &rowReports); err != nil {
			continue // undecodable body contributes nothing but still counts as claimed
		}
		reports = append(reports, rowReports...)
	}
	sort.Slice(reports, func(i, j int) bool { return reports[i].GoalIndex < reports[j].GoalIndex })
	message := TelegramDelegationMessage(reports)
	// delivered=true and delivered=false-nobody-owns both need nothing further here, the
	// SAME reasoning nudgeOne's own doc gives for its identical DeliverToIdentity call.
	if _, err := d.Channel.DeliverToIdentity(ctx, group.identityID, message); err != nil {
		if d.Pending != nil && firstClaimedRowID != "" {
			if perr := d.Pending.InsertPendingNotification(ctx, firstClaimedRowID, group.identityID, message, err.Error()); perr != nil {
				slog.Warn("swarm.delegation.fanout_retry_persist_failed", "steer_row", firstClaimedRowID, "err", perr)
			}
		}
	}
	return true, nil
}
