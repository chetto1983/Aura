package agent

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/idempotency"
)

var errUnsupportedParentOperation = errors.New("unsupported parent operation scope")

// deriveToolOperationContext turns an ingress/scheduler operation into a stable
// child owned by the tool's Aura-declared scope and canonical argument intent.
// Request and tool-call IDs stay audit-only and therefore never participate.
func deriveToolOperationContext(ctx context.Context, spec tools.Spec, args json.RawMessage) (context.Context, error) {
	parent, ok := idempotency.OperationFromContext(ctx)
	if !ok || parent.Key.Scope == spec.OperationScope {
		return ctx, nil
	}
	switch parent.Key.Scope {
	case idempotency.ScopeHTTPMutation, idempotency.ScopeCLICommand,
		idempotency.ScopeSchedulerRun, idempotency.ScopeApproval:
	default:
		return nil, errUnsupportedParentOperation
	}
	toolFingerprint, err := tools.OperationFingerprint(spec, args)
	if err != nil {
		return nil, err
	}
	childKeyFingerprint, err := idempotency.FingerprintTyped(struct {
		Version           string            `json:"version"`
		ParentScope       idempotency.Scope `json:"parent_scope"`
		ParentKey         string            `json:"parent_key"`
		ParentFingerprint string            `json:"parent_fingerprint"`
		ToolScope         idempotency.Scope `json:"tool_scope"`
		ToolFingerprint   string            `json:"tool_fingerprint"`
	}{
		Version: "tool-child-v1", ParentScope: parent.Key.Scope, ParentKey: parent.Key.Key,
		ParentFingerprint: idempotency.FingerprintHex(parent.Fingerprint), ToolScope: spec.OperationScope,
		ToolFingerprint: idempotency.FingerprintHex(toolFingerprint),
	})
	if err != nil {
		return nil, err
	}
	return idempotency.WithOperation(ctx, idempotency.Operation{
		Key: idempotency.OperationKey{
			IdentityID: parent.Key.IdentityID,
			Scope:      spec.OperationScope,
			Key:        "child:" + idempotency.FingerprintHex(childKeyFingerprint),
		},
		Fingerprint: toolFingerprint,
		Correlation: idempotency.FingerprintHex(parent.Fingerprint),
	})
}
