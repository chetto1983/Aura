package conversations

import (
	"crypto/rand"
	"fmt"

	"github.com/google/uuid"
)

// NewCompactionOperationID generates the durable idempotency identity before claim creation.
func NewCompactionOperationID() (string, error) {
	id, err := uuid.NewRandomFromReader(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("generate compaction operation id: %w", err)
	}
	return id.String(), nil
}

// ClaimPriority controls whether an unstarted automatic claim may be displaced.
type ClaimPriority string

const (
	// ClaimPriorityAutomatic is background work that may yield before inference starts.
	ClaimPriorityAutomatic ClaimPriority = "automatic"
	// ClaimPriorityManual is an operator-requested attempt.
	ClaimPriorityManual ClaimPriority = "manual"
)

// ClaimState is the durable lifecycle of one compaction operation.
type ClaimState string

const (
	// ClaimPending identifies an attempt eligible to finalize or be reclaimed.
	ClaimPending ClaimState = "pending"
	// ClaimCompleted identifies an attempt whose checkpoint is durable and active.
	ClaimCompleted ClaimState = "completed"
	// ClaimSuperseded identifies a stale or invalidated completion.
	ClaimSuperseded ClaimState = "superseded"
)
