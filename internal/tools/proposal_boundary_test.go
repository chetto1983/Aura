package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aura/aura/internal/conversation/summarizer"
)

type fakeProposalCreator struct {
	last summarizer.ProposalInput
	next int64
}

func (f *fakeProposalCreator) Propose(_ context.Context, in summarizer.ProposalInput) (summarizer.ProposedUpdate, error) {
	f.last = in
	if f.next == 0 {
		f.next = 1
	}
	return summarizer.ProposedUpdate{
		ID:         f.next,
		ChatID:     in.ChatID,
		Fact:       in.Fact,
		Action:     in.Action,
		TargetSlug: in.TargetSlug,
		Similarity: in.Similarity,
		Category:   in.Category,
		Provenance: in.Provenance,
		Status:     "pending",
	}, nil
}

func TestProposalToolsAcceptCreatorInterface(t *testing.T) {
	wikiCreator := &fakeProposalCreator{next: 41}
	wikiTool := NewProposeWikiChangeTool(wikiCreator)
	if wikiTool == nil {
		t.Fatal("expected propose_wiki_change tool")
	}
	out, err := wikiTool.Execute(WithUserID(t.Context(), "123"), map[string]any{
		"action":      "patch",
		"fact":        "Boundary fake proposal",
		"target_slug": "aura",
	})
	if err != nil {
		t.Fatalf("wiki Execute: %v", err)
	}
	var wikiResp proposeWikiChangeResponse
	if err := json.Unmarshal([]byte(out), &wikiResp); err != nil {
		t.Fatalf("wiki response JSON: %v", err)
	}
	if wikiResp.ID != 41 || wikiCreator.last.ChatID != 123 || wikiCreator.last.TargetSlug != "aura" {
		t.Fatalf("wiki proposal = %+v resp=%+v", wikiCreator.last, wikiResp)
	}

	skillCreator := &fakeProposalCreator{next: 42}
	skillTool := NewProposeSkillChangeTool(skillCreator)
	if skillTool == nil {
		t.Fatal("expected propose_skill_change tool")
	}
	content := "---\nname: boundary-skill\ndescription: Boundary skill\n---\n\n# Boundary\n"
	out, err = skillTool.Execute(t.Context(), map[string]any{
		"action":       "create",
		"name":         "boundary-skill",
		"content":      content,
		"smoke_prompt": "Use boundary skill",
	})
	if err != nil {
		t.Fatalf("skill Execute: %v", err)
	}
	var skillResp proposeSkillChangeResponse
	if err := json.Unmarshal([]byte(out), &skillResp); err != nil {
		t.Fatalf("skill response JSON: %v", err)
	}
	if skillResp.ID != 42 || skillCreator.last.Action != "skill_create" || !strings.Contains(skillCreator.last.Fact, "Boundary skill") {
		t.Fatalf("skill proposal = %+v resp=%+v", skillCreator.last, skillResp)
	}
}
