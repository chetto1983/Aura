// compaction-test-worker is a compiled subprocess fixture for cross-process claim tests.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/chetto1983/aura/internal/conversations"
	"github.com/jackc/pgx/v5/pgxpool"
)

type command struct {
	Action, DSN, OperationID, ConversationID, BranchID, IdempotencyKey, OwnerID string
	BaseGeneration, CapturedWatermark                                           int
	GovernanceWatermark                                                         int64
	Priority                                                                    conversations.ClaimPriority
	LeaseUntil                                                                  time.Time
}
type event struct{ State, OperationID, OwnerID, Error string }

func main() {
	ctx := context.Background()
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var c command
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			emit(enc, event{Error: err.Error()})
			continue
		}
		pool, err := pgxpool.New(ctx, c.DSN)
		if err != nil {
			emit(enc, event{Error: err.Error()})
			continue
		}
		store := conversations.New(pool, conversations.Config{})
		switch c.Action {
		case "claim":
			claim, e := store.ClaimCompaction(ctx, conversations.CompactionClaimParams{OperationID: c.OperationID, ConversationID: c.ConversationID, BranchID: c.BranchID, IdempotencyKey: c.IdempotencyKey, OwnerID: c.OwnerID, CapturedWatermark: c.CapturedWatermark, GovernanceWatermark: c.GovernanceWatermark, BaseGeneration: c.BaseGeneration, Priority: c.Priority, LeaseUntil: c.LeaseUntil})
			if e != nil {
				err = e
			} else {
				emit(enc, event{State: string(claim.State), OperationID: claim.OperationID, OwnerID: claim.OwnerID})
			}
		case "start":
			err = store.MarkCompactionInferenceStarted(ctx, c.OperationID, c.OwnerID)
			if err == nil {
				emit(enc, event{State: "inference_started", OperationID: c.OperationID, OwnerID: c.OwnerID})
			}
		default:
			err = fmt.Errorf("unknown action %q", c.Action)
		}
		if err != nil {
			emit(enc, event{Error: err.Error()})
		}
		pool.Close()
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(4)
	}
}
func emit(enc *json.Encoder, e event) {
	if err := enc.Encode(e); err != nil {
		os.Exit(3)
	}
}
