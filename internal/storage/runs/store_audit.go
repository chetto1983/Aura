package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/identity"
)

func (s *Store) RecordAuthorizationDenial(ctx context.Context, decision identity.AuthorizationDecision) error {
	if decision.Decision != identity.DecisionDeny {
		return nil
	}
	if decision.RunID == "" {
		return errors.New("runs: authorization denial run id is required")
	}
	if decision.ActorID == "" {
		return errors.New("runs: authorization denial actor id is required")
	}
	if decision.Capability == "" {
		return errors.New("runs: authorization denial capability is required")
	}
	payload := authorizationDenialPayload(decision)
	event, err := s.AppendEvent(ctx, AppendEventParams{
		RunID:          decision.RunID,
		Type:           EventAuthorizationDenied,
		ActorID:        decision.ActorID,
		CausationID:    decision.EventID,
		IdempotencyKey: "authorization_denied:" + decision.ID,
		Payload:        payload,
		RedactionLevel: RedactionMetadata,
		CreatedAt:      decision.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("runs: record authorization denial run event: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("runs: marshal authorization denial audit payload: %w", err)
	}
	createdAt := decision.CreatedAt
	if createdAt.IsZero() {
		createdAt = event.CreatedAt
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO audit_events (
  id, run_id, event_id, type, actor_id, target_type, target_id,
  payload_json, redaction_level, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO NOTHING
`,
		authorizationDenialAuditID(decision.ID),
		decision.RunID,
		event.ID,
		EventAuthorizationDenied,
		decision.ActorID,
		"authorization",
		decision.ID,
		string(payloadJSON),
		RedactionMetadata,
		formatTime(createdAt),
	); err != nil {
		return fmt.Errorf("runs: record authorization denial audit event: %w", err)
	}
	return nil
}

func authorizationDenialPayload(decision identity.AuthorizationDecision) map[string]any {
	payload := map[string]any{
		"decision_id":     decision.ID,
		"actor_id":        decision.ActorID,
		"capability":      string(decision.Capability),
		"resource_type":   decision.Resource.Type,
		"resource_id":     decision.Resource.ID,
		"decision":        string(decision.Decision),
		"reason":          decision.Reason,
		"run_id":          decision.RunID,
		"redaction_level": RedactionMetadata,
	}
	if decision.EventID != "" {
		payload["causation_event_id"] = decision.EventID
	}
	return payload
}

func authorizationDenialAuditID(decisionID string) string {
	if decisionID == "" {
		return newID("audit")
	}
	return "audit_" + decisionID
}
