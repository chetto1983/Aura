package agui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/documents"
)

type runAgentRequest struct {
	RunAgentInput types.RunAgentInput
	Aura          runAuraOptions
}

// runAuraOptions is the run envelope's aura namespace: what Aura reads beside the AG-UI
// RunAgentInput.
type runAuraOptions struct {
	AttachmentIDs []string `json:"attachment_ids"`
	// DocumentScope is the operator's structured @file/@folder selection. It narrows
	// document_search for this run; identity is always taken from the server context.
	DocumentScope []documents.SourceScope `json:"document_scope"`
	// Skill is the optional pinned skill NAME (37D / WEBSKILL-02). handleRun resolves it
	// to a body via the loader snapshot and applies Mechanism A; it is a loader key only,
	// never a filesystem path (T-37D-02).
	Skill string `json:"skill"`
	// Effort is the optional per-turn reasoning-effort SYMBOL (37E / WEBMODEL-02, D-05):
	// one of {auto,off,low,mid,high,extra,max} ("" == auto). handleRun two-stage-validates
	// it (parseEffortSymbol enum + capability), threads a fixed level via
	// runner.WithReasoningOverride, and persists the symbol owner-scoped. The client sends a
	// symbol only, NEVER a raw ReasoningConfig — that is the WEBMODEL-03 no-bypass control.
	Effort string `json:"effort"`
	// ParentSeq is the last turn of the branch the client has on screen, which the new user
	// turn continues (spec 2026-10-27, D2). Absent, the turn continues the one written last.
	ParentSeq *int `json:"parent_seq"`
}

func strictDecodeRunAgentRequest(w http.ResponseWriter, r *http.Request) (runAgentRequest, error) {
	var raw json.RawMessage
	if err := strictDecodeJSON(w, r, &raw, decodeOpts{}); err != nil {
		return runAgentRequest{}, err
	}
	return decodeRunAgentRequest(json.NewDecoder(bytes.NewReader(raw)))
}

func decodeRunAgentRequest(dec *json.Decoder) (runAgentRequest, error) {
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return runAgentRequest{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return runAgentRequest{}, err
	}
	for key := range fields {
		if !runAgentFieldAllowed(key) {
			return runAgentRequest{}, fmt.Errorf("json: unknown field %q", key)
		}
	}
	if aura, ok := fields["aura"]; ok {
		var auraFields map[string]json.RawMessage
		if err := json.Unmarshal(aura, &auraFields); err != nil {
			return runAgentRequest{}, err
		}
		for key := range auraFields {
			switch key {
			case "attachment_ids", "document_scope", "skill", "effort", "parent_seq":
			default:
				return runAgentRequest{}, fmt.Errorf("json: unknown field %q in aura", key)
			}
		}
		if scope, ok := auraFields["document_scope"]; ok {
			if err := strictDecodeDocumentScope(scope); err != nil {
				return runAgentRequest{}, err
			}
		}
	}
	var in types.RunAgentInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return runAgentRequest{}, err
	}
	var ext struct {
		Aura runAuraOptions `json:"aura"`
	}
	if err := json.Unmarshal(raw, &ext); err != nil {
		return runAgentRequest{}, err
	}
	return runAgentRequest{RunAgentInput: in, Aura: ext.Aura}, nil
}

func strictDecodeDocumentScope(raw json.RawMessage) error {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return fmt.Errorf("decode aura.document_scope: %w", err)
	}
	for i, entry := range entries {
		dec := json.NewDecoder(bytes.NewReader(entry))
		dec.DisallowUnknownFields()
		var scope documents.SourceScope
		if err := dec.Decode(&scope); err != nil {
			return fmt.Errorf("decode aura.document_scope[%d]: %w", i, err)
		}
	}
	return nil
}

func runAgentFieldAllowed(key string) bool {
	switch key {
	case "threadId", "thread_id",
		"runId", "run_id",
		"parentRunId", "parent_run_id",
		"state", "messages", "tools", "context",
		"forwardedProps", "forwarded_props",
		"resume", "aura":
		return true
	default:
		return false
	}
}
