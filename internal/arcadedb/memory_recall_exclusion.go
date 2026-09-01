package arcadedb

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	recallExclusionMarker = "/*aura:active-conversation-exclusion*/"
	recallExclusionMaxIDs = 8
)

func canonicalRecallExclusions(values []string) ([]string, error) {
	if len(values) > recallExclusionMaxIDs {
		return nil, fmt.Errorf("arcadedb: memory recall exclusions exceed %d conversations", recallExclusionMaxIDs)
	}
	canonical := append([]string(nil), values...)
	for _, value := range canonical {
		if value == "" || value != strings.TrimSpace(value) || !validRecallCursorID(value) {
			return nil, fmt.Errorf("arcadedb: memory recall exclusion is invalid")
		}
	}
	sort.Strings(canonical)
	for index := 1; index < len(canonical); index++ {
		if canonical[index] == canonical[index-1] {
			return nil, fmt.Errorf("arcadedb: memory recall exclusion is duplicated")
		}
	}
	return canonical, nil
}

func applyRecallExclusions(statement string, params map[string]any, conversationIDs []string) string {
	var clause strings.Builder
	for index, conversationID := range conversationIDs {
		name := fmt.Sprintf("excluded_conversation_id_%d", index)
		clause.WriteString(" AND conversation_id <> :")
		clause.WriteString(name)
		params[name] = conversationID
	}
	return strings.ReplaceAll(statement, recallExclusionMarker, clause.String())
}

func recallExcludedConversationSet(conversationIDs []string) map[string]struct{} {
	excluded := make(map[string]struct{}, len(conversationIDs))
	for _, conversationID := range conversationIDs {
		excluded[conversationID] = struct{}{}
	}
	return excluded
}

func recallConversationExcluded(conversationIDs []string, conversationID string) bool {
	return slices.Contains(conversationIDs, conversationID)
}

func activeConversationExcludedRecallResult() RecallResult {
	return RecallResult{
		Evidence:  make([]RecallEvidence, 0),
		Abstained: true,
		Reason:    "active_conversation_excluded",
		Retrieval: RecallRetrieval{Path: retrievalPathGraph},
	}
}
