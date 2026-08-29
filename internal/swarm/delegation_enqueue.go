// delegation_enqueue.go holds the "write one durable row per goal and hand
// back a model-readable summary" concern EnqueueDelegation owns -- split out
// of delegation_queue.go (which grew past CLAUDE.md's 600-LOC ceiling once
// 51-11 added the child-id/fan-out-key minting) rather than grown inline,
// mirroring this package's own report.go/brief.go/swarm_depth.go/
// transcript_api.go/delegation_delivery.go concern-split precedent.
// DelegationClaimLoop's own lifecycle (claim/retry/succeed/dead_letter) stays
// in delegation_queue.go.
package swarm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/documents"
)

// DelegationEnqueuer writes one durable job_type=swarm_delegation row per
// goal. A zero-value Store is a wiring bug (real Go error), never a domain
// rejection.
type DelegationEnqueuer struct {
	Store DelegationJobStore
}

// delegationQueuedResult is EnqueueDelegation's typed return shape (51-11):
// a compact JSON object built via json.Marshal, never a hand-built string --
// the same rule renderSwarmSpawnParams (internal/agent/tools/swarm_spawn.go)
// already follows for the tool's own schema. Field names are fixed: plan
// 51-12a decodes this object in internal/agent/display/preview.go, and
// Workers' entries deliberately mirror ChildReport's own json tags so they
// decode straight into it.
type delegationQueuedResult struct {
	Queued  int                      `json:"queued"`
	Note    string                   `json:"note"`
	Workers []delegationQueuedWorker `json:"workers"`
}

// delegationQueuedWorker names one dispatched worker in EnqueueDelegation's
// result -- the model's only way to learn a child_id to pass to swarm_status
// (Task 4) before any report has arrived.
type delegationQueuedWorker struct {
	GoalIndex int    `json:"goal_index"`
	ChildID   string `json:"child_id"`
	Status    string `json:"status"`
	Goal      string `json:"goal"`
}

// delegationEnqueueNote tells the model not to wait for and not to re-dispatch
// workers it just queued, and names swarm_status as how it checks on them
// (the ADK "already returned a pending status, do not call again" vocabulary,
// .planning/research/adk-subagent-visibility-2026-08-29.md).
const delegationEnqueueNote = "These workers run on their own in the background -- do not wait for them here and do not " +
	"re-dispatch the same goals. Answer the user now; each worker's report will arrive on its own as it finishes. " +
	"Call swarm_status with a worker's child_id to check its progress."

// EnqueueDelegation writes one durable row per goal and returns a
// model-readable JSON summary immediately (51-11: queued/note/workers,
// replacing the prose string) -- no worker is constructed here (the claim
// loop does that out of band). An empty goal slice is a model-readable
// rejection, not a Go error (D-15's established domain-rejection idiom), so
// zero rows are enqueued and the model can self-correct without a stack
// trace.
func EnqueueDelegation(ctx context.Context, enq *DelegationEnqueuer, identityID string, goals []string, brief DelegationPayload) (string, error) {
	if len(goals) == 0 {
		return "error: no goals provided -- background delegation needs at least one goal", nil
	}
	if enq == nil || enq.Store == nil {
		return "", fmt.Errorf("swarm: delegation enqueuer is not configured")
	}
	// Computed ONCE, before the loop: every goal of this ONE swarm_spawn call
	// shares the SAME fan-out key (delegationFanoutKey's own doc).
	fanoutKey := delegationFanoutKey(identityID, brief.ConversationID, brief.ParentRunID, goals)
	workers := make([]delegationQueuedWorker, 0, len(goals))
	queued := 0
	for i, goal := range goals {
		key := delegationIdempotencyKey(identityID, brief.ConversationID, brief.ParentRunID, i, goal)
		childID := delegationChildID(key, i)
		payload := brief
		payload.Goal = goal
		payload.ChildID = childID
		payload.GoalIndex = i
		payload.FanoutKey = fanoutKey
		m, err := delegationPayloadMap(payload)
		if err != nil {
			return "", fmt.Errorf("swarm: delegation payload for goal %d: %w", i, err)
		}
		if _, err := enq.Store.Create(ctx, documents.CreateIngestionJobRequest{
			IdentityID:     identityID,
			JobType:        JobTypeSwarmDelegation,
			Status:         "queued",
			IdempotencyKey: key,
			MaxAttempts:    defaultDelegationMaxAttempts,
			Payload:        m,
		}); err != nil {
			return "", fmt.Errorf("swarm: enqueue delegation goal %d: %w", i, err)
		}
		queued++
		workers = append(workers, delegationQueuedWorker{GoalIndex: i, ChildID: childID, Status: StatusRunning, Goal: goal})
	}
	b, err := json.Marshal(delegationQueuedResult{Queued: queued, Note: delegationEnqueueNote, Workers: workers})
	if err != nil {
		return "", fmt.Errorf("swarm: delegation queued result: %w", err)
	}
	return string(b), nil
}

// delegationIdempotencyKey is deterministic over its inputs: the same
// (identity, conversation, parent run, goal index, goal text) always produces
// the same key, and a different goal index always produces a different one
// (the ON CONFLICT (identity_id, job_type, idempotency_key) unique key is what
// makes a re-run of the same enqueue add no second row).
func delegationIdempotencyKey(identityID, convID, parentRunID string, goalIndex int, goal string) string {
	h := sha256.New()
	for _, part := range []string{identityID, convID, parentRunID, strconv.Itoa(goalIndex), goal} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "swarm_delegation:" + hex.EncodeToString(h.Sum(nil))
}

// delegationChildID derives one goal's unique, stable child id from the SAME
// deterministic idempotency key delegationIdempotencyKey already computed for
// it -- so a re-enqueue of the identical swarm_spawn call reproduces the same
// id, matching the ON CONFLICT idempotency the queue row already has. Without
// this every background worker shares runChild's own "w1" fallback (idx is
// always 0 -- runWithHeartbeat's hardcoded call), so two concurrently claimed
// jobs of one conversation would interleave into the SAME
// <runDir>/<conv>/swarm/w1.jsonl. Made only of 'w', digits, '-' and lowercase
// hex, so validatePathSegment (transcript_api.go) accepts it by construction
// -- no separator, no traversal segment, no NUL byte is reachable from this
// alphabet regardless of what the goal text itself contains.
func delegationChildID(idempotencyKey string, goalIndex int) string {
	digest := strings.TrimPrefix(idempotencyKey, "swarm_delegation:")
	if len(digest) > 8 {
		digest = digest[:8]
	}
	return fmt.Sprintf("w%d-%s", goalIndex+1, digest)
}

// delegationFanoutKey is the ONE identity every goal of a SINGLE swarm_spawn
// call shares -- computed ONCE, before EnqueueDelegation's per-goal loop,
// over the same inputs delegationIdempotencyKey covers EXCEPT the per-goal
// index: identity, conversation, parent run id, then every goal in order.
// conversation_id alone cannot express this grouping: two swarm_spawn calls
// in one conversation are two DIFFERENT fan-outs. A re-enqueue of the
// identical call reproduces the same key, matching the per-goal ON CONFLICT
// idempotency. "f-" plus 16 hex characters is short enough to index and
// carries no goal text, conversation id or identity in readable form
// (T-51-64) -- a leaked row discloses nothing beyond "these rows belong
// together".
func delegationFanoutKey(identityID, convID, parentRunID string, goals []string) string {
	h := sha256.New()
	for _, part := range []string{identityID, convID, parentRunID} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	for _, goal := range goals {
		h.Write([]byte(goal))
		h.Write([]byte{0})
	}
	return "f-" + hex.EncodeToString(h.Sum(nil))[:16]
}

func delegationPayloadMap(p DelegationPayload) (map[string]any, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func delegationPayloadFromJob(job documents.IngestionJob) (DelegationPayload, error) {
	b, err := json.Marshal(job.Payload)
	if err != nil {
		return DelegationPayload{}, fmt.Errorf("delegation payload encode: %w", err)
	}
	var p DelegationPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return DelegationPayload{}, fmt.Errorf("delegation payload decode: %w", err)
	}
	if p.Goal == "" {
		return DelegationPayload{}, errors.New("delegation payload missing goal")
	}
	if p.ConversationID == "" {
		return DelegationPayload{}, errors.New("delegation payload missing conversation_id")
	}
	return p, nil
}
