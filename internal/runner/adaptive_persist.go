package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
)

var runnerAdaptiveNamespace = uuid.MustParse("c2e64869-7054-4aa6-b5db-cb26341b592b")

func runnerAdaptiveOutcome(ctx context.Context, source *agent.Event, point string, fields map[string]any) (adaptive.Event, error) {
	if source == nil || source.RequestID == uuid.Nil {
		return adaptive.Event{}, errors.New("adaptive outcome requires a Runner request event")
	}
	ownerID, err := uuid.Parse(identityctx.IdentityID(ctx))
	if err != nil {
		return adaptive.Event{}, fmt.Errorf("adaptive outcome owner: %w", err)
	}
	payload := make(map[string]any, len(fields)+2)
	payload["schema_version"] = "1.0"
	payload["evidence_type"] = "operational"
	for key, value := range fields {
		payload[key] = value
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return adaptive.Event{}, fmt.Errorf("marshal adaptive outcome: %w", err)
	}
	createdAt := source.Timestamp.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	decisionPoint := point
	if point == "assistant" {
		decisionPoint = "reasoning"
	}
	return adaptive.NewEvent(adaptive.EventParams{
		ID:          uuid.NewSHA1(runnerAdaptiveNamespace, []byte(source.RequestID.String()+":"+point)),
		OwnerID:     ownerID,
		AggregateID: source.RequestID.String(),
		DecisionID:  adaptive.DecisionIDForPoint(source.RequestID, decisionPoint),
		Kind:        adaptive.EventOutcome,
		Payload:     raw,
		CreatedAt:   createdAt,
	})
}
