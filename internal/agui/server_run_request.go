package agui

import (
	"encoding/json"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
)

type runAgentRequest struct {
	RunAgentInput types.RunAgentInput
	Aura          struct {
		AttachmentIDs []string `json:"attachment_ids"`
		// Skill is the optional pinned skill NAME (37D / WEBSKILL-02). handleRun resolves it
		// to a body via the loader snapshot and applies Mechanism A; it is a loader key only,
		// never a filesystem path (T-37D-02).
		Skill string `json:"skill"`
	}
}

func decodeRunAgentRequest(dec *json.Decoder) (runAgentRequest, error) {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return runAgentRequest{}, err
	}
	var in types.RunAgentInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return runAgentRequest{}, err
	}
	var ext struct {
		Aura struct {
			AttachmentIDs []string `json:"attachment_ids"`
			Skill         string   `json:"skill"`
		} `json:"aura"`
	}
	if err := json.Unmarshal(raw, &ext); err != nil {
		return runAgentRequest{}, err
	}
	return runAgentRequest{RunAgentInput: in, Aura: ext.Aura}, nil
}
