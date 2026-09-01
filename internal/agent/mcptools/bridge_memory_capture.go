package mcptools

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/mcp"
)

const memoryUpsertFactModelName = "memory__memory_upsert_fact"

func acceptedFactEvidence(
	ctx context.Context,
	toolName string,
	args map[string]any,
	payload mcp.ToolPayload,
) (tools.AcceptedFactEvidence, bool) {
	if toolName != memoryUpsertFactModelName || len(payload.Structured) == 0 {
		return tools.AcceptedFactEvidence{}, false
	}
	var outcome struct {
		Statement string `json:"statement"`
		Refused   bool   `json:"refused"`
	}
	if err := json.Unmarshal(payload.Structured, &outcome); err != nil || outcome.Refused {
		return tools.AcceptedFactEvidence{}, false
	}
	actor := actorFromContext(ctx)
	evidence := tools.AcceptedFactEvidence{
		Subject:    stringArg(args, "subject"),
		Predicate:  stringArg(args, "predicate"),
		Object:     stringArg(args, "object"),
		Statement:  strings.TrimSpace(outcome.Statement),
		ActorRunID: actor.RunID,
		ActorRole:  actor.Role,
	}
	if source, ok := args["source"].(map[string]any); ok {
		evidence.SourceMemoryIDs = stringSliceArg(source["memory_ids"])
	}
	if evidence.Subject == "" || evidence.Predicate == "" || evidence.Object == "" || evidence.Statement == "" ||
		evidence.ActorRunID == "" {
		return tools.AcceptedFactEvidence{}, false
	}
	return evidence, true
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return strings.TrimSpace(value)
}

func stringSliceArg(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if direct, directOK := value.([]string); directOK {
			return append([]string(nil), direct...)
		}
		return nil
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || strings.TrimSpace(text) == "" {
			continue
		}
		result = append(result, strings.TrimSpace(text))
	}
	return result
}
