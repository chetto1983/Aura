package messagedrafts

import "encoding/json"

// PauseDraftID recognizes a host-authored review pause. The draft ID is only a
// lookup key; owner checks and one-send claims remain in Store and Runner.
func PauseDraftID(raw json.RawMessage) (string, bool) {
	var fields struct {
		Type    string `json:"type"`
		DraftID string `json:"draft_id"`
	}
	if json.Unmarshal(raw, &fields) != nil || fields.Type != "message_draft" || fields.DraftID == "" {
		return "", false
	}
	return fields.DraftID, true
}

// IsReviewPause hides all message-review pauses from the generic approval route,
// including a malformed draft identifier. Only the dedicated draft API may act.
func IsReviewPause(raw json.RawMessage) bool {
	var fields struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(raw, &fields) == nil && fields.Type == "message_draft"
}
