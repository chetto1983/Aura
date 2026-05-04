package tools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/scheduler"
)

func TestProposeSkillChangeTool_Create(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store := summarizer.NewSummariesStore(db)
	tool := NewProposeSkillChangeTool(store)
	if tool.Name() != "propose_skill_change" || tool.Description() == "" || tool.Parameters()["type"] != "object" {
		t.Fatal("propose_skill_change metadata is incomplete")
	}

	content := `---
name: morning-brief
description: Build the operator morning briefing.
---

# Morning Brief

Use daily_briefing first, then propose wiki or skill changes only when stable patterns appear.
`
	out, err := tool.Execute(WithUserID(t.Context(), "12345"), map[string]any{
		"action":        "create",
		"name":          "morning-brief",
		"content":       content,
		"allowed_tools": []any{"daily_briefing", "search_memory", "propose_wiki_change", "daily_briefing"},
		"smoke_prompt":  "Prepara il mio briefing mattutino e segnala solo cio' che conta.",
		"reason":        "The user keeps asking for the same morning routine.",
		"origin_tool":   "search_memory",
		"origin_reason": "repeated workflow should become procedural memory",
		"evidence": []any{
			map[string]any{"kind": "archive", "id": "conversation:42", "snippet": "briefing mattutino"},
		},
		"confidence": 0.9,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp proposeSkillChangeResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("response JSON: %v", err)
	}
	if !resp.OK || resp.ID == 0 || resp.Action != "skill_create" || resp.Name != "morning-brief" {
		t.Fatalf("response = %+v", resp)
	}

	got, err := store.Get(t.Context(), resp.ID)
	if err != nil {
		t.Fatalf("Get proposal: %v", err)
	}
	if got.ChatID != 12345 || got.Action != "skill_create" || got.Category != "skill" {
		t.Fatalf("proposal = %+v", got)
	}
	if got.Provenance.ProposalKind != "skill" || got.Provenance.Skill == nil {
		t.Fatalf("provenance = %+v", got.Provenance)
	}
	sp := got.Provenance.Skill
	if sp.Action != "create" || sp.Name != "morning-brief" || sp.Description != "Build the operator morning briefing." {
		t.Fatalf("skill proposal = %+v", sp)
	}
	if len(sp.AllowedTools) != 3 || sp.AllowedTools[0] != "daily_briefing" || sp.AllowedTools[2] != "propose_wiki_change" {
		t.Fatalf("allowed tools = %+v", sp.AllowedTools)
	}
	if !strings.Contains(got.Fact, "```markdown") || !strings.Contains(got.Fact, "Smoke prompt:") {
		t.Fatalf("fact does not carry reviewable skill draft:\n%s", got.Fact)
	}
	if len(got.Provenance.Evidence) != 1 || got.Provenance.Evidence[0].ID != "conversation:42" {
		t.Fatalf("evidence = %+v", got.Provenance.Evidence)
	}
}

func TestProposeSkillChangeTool_Delete(t *testing.T) {
	db := scheduler.NewTestDB(t)
	tool := NewProposeSkillChangeTool(summarizer.NewSummariesStore(db))

	out, err := tool.Execute(t.Context(), map[string]any{
		"action": "delete",
		"name":   "stale-flow",
		"reason": "The workflow has been replaced by daily_briefing.",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var resp proposeSkillChangeResponse
	_ = json.Unmarshal([]byte(out), &resp)
	if resp.Action != "skill_delete" || resp.Name != "stale-flow" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestProposeSkillChangeToolValidation(t *testing.T) {
	db := scheduler.NewTestDB(t)
	tool := NewProposeSkillChangeTool(summarizer.NewSummariesStore(db))

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "bad name",
			args: map[string]any{"action": "create", "name": "../bad"},
			want: "invalid skill name",
		},
		{
			name: "missing content",
			args: map[string]any{"action": "create", "name": "alpha", "smoke_prompt": "do alpha"},
			want: "content is required",
		},
		{
			name: "mismatched frontmatter name",
			args: map[string]any{
				"action":       "create",
				"name":         "alpha",
				"smoke_prompt": "do alpha",
				"content":      "---\nname: beta\ndescription: Beta\n---\n\n# Beta\n",
			},
			want: "does not match",
		},
		{
			name: "missing smoke",
			args: map[string]any{
				"action":  "update",
				"name":    "alpha",
				"content": "---\nname: alpha\ndescription: Alpha\n---\n\n# Alpha\n",
			},
			want: "smoke_prompt is required",
		},
		{
			name: "delete missing reason",
			args: map[string]any{"action": "delete", "name": "alpha"},
			want: "reason is required",
		},
		{
			name: "search memory requires evidence",
			args: map[string]any{
				"action":       "create",
				"name":         "alpha",
				"content":      "---\nname: alpha\ndescription: Alpha\n---\n\n# Alpha\n",
				"smoke_prompt": "do alpha",
				"origin_tool":  "search_memory",
			},
			want: "evidence refs are required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tool.Execute(t.Context(), tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}
}
