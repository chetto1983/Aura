package agui

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestDecodeRunAgentRequest_Skill proves the aura.skill pinned-skill name decodes onto the
// runAgentRequest.Aura struct alongside attachment_ids — the field rides BOTH the typed
// struct and the inner ext-decode struct (the existing double-decode idiom), so handleRun
// reads a populated req.Aura.Skill next to req.Aura.AttachmentIDs.
func TestDecodeRunAgentRequest_Skill(t *testing.T) {
	const body = `{"threadId":"t1","messages":[{"id":"m1","role":"user","content":"hi"}],"aura":{"attachment_ids":["a1"],"skill":"skill-creator","effort":"high"}}`

	req, err := decodeRunAgentRequest(json.NewDecoder(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Aura.Skill != "skill-creator" {
		t.Fatalf("Aura.Skill = %q, want %q", req.Aura.Skill, "skill-creator")
	}
	if len(req.Aura.AttachmentIDs) != 1 || req.Aura.AttachmentIDs[0] != "a1" {
		t.Fatalf("Aura.AttachmentIDs = %v, want [a1] (both fields decode together)", req.Aura.AttachmentIDs)
	}
	// 37E: the effort symbol rides the SAME aura envelope, decoding alongside skill/attachment_ids.
	if req.Aura.Effort != "high" {
		t.Fatalf("Aura.Effort = %q, want %q", req.Aura.Effort, "high")
	}
}

// TestDecodeRunAgentRequest_NoSkill proves the field is optional: a body with no aura.skill
// decodes to an empty Skill (handleRun then no-ops the pinned-skill path).
func TestDecodeRunAgentRequest_NoSkill(t *testing.T) {
	const body = `{"threadId":"t1","messages":[{"id":"m1","role":"user","content":"hi"}]}`

	req, err := decodeRunAgentRequest(json.NewDecoder(strings.NewReader(body)))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.Aura.Skill != "" {
		t.Fatalf("Aura.Skill = %q, want empty when aura.skill is absent", req.Aura.Skill)
	}
	// 37E: an absent aura.effort decodes to "" (handleRun then treats it as auto — no override).
	if req.Aura.Effort != "" {
		t.Fatalf("Aura.Effort = %q, want empty when aura.effort is absent", req.Aura.Effort)
	}
}
