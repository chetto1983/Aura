package display

import (
	"strings"
	"testing"
)

func nativeInput(name, action, preview string) PreviewInput {
	return PreviewInput{ToolCallID: "native-1", ToolName: name,
		Arguments: `{"action":"` + action + `"}`, ResultPreview: preview}
}

func TestNativeListResultsBecomeBoundedTables(t *testing.T) {
	cases := []struct {
		name          string
		in            PreviewInput
		columns, rows int
	}{
		{"tasks", nativeInput("task", "list", "2 task(s):\n  11111111-1111-1111-1111-111111111111  kind=reminder  every  next=2026-10-01T09:00:00Z  notify=stdout\n  22222222-2222-2222-2222-222222222222  kind=agent_job  cron  next=2026-10-02T09:00:00Z [awaiting approval]  payload=check inbox"), 5, 2},
		{"empty tasks", nativeInput("task", "list", "no scheduled tasks"), 5, 0},
		{"skills", nativeInput("skill", "list", "- alpha: Alpha does A.\n- zeta: Zeta does Z.\n\nNOTE: this listed INSTALLED skills only. If none cover the task family at hand, load the find-skills-aura skill (skill action=use name=find-skills-aura): it teaches how to discover and install skills from the open ecosystem — follow it before hand-coding the deliverable."), 2, 2},
		{"packs", PreviewInput{ToolCallID: "native-1", ToolName: "plugin_pack", Arguments: `{"action":"list","ref":"owner/repo"}`, ResultPreview: "2 pack(s)\n  sales                        v1          2 skills   1 connectors   3 commands  owner/repo/sales\n  support                      -           4 skills   0 connectors   1 commands  owner/repo/support\n"}, 6, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := NormalizeToolPreview(tc.in, NewRegistry())
			if !ok || got.Type != KindTable || got.Table == nil || len(got.Table.Columns) != tc.columns || len(got.Table.Rows) != tc.rows {
				t.Fatalf("list display = %+v, ok=%v", got, ok)
			}
		})
	}
}

func TestNativeListsDoNotPromoteActionsOrUnownedDocuments(t *testing.T) {
	cases := []PreviewInput{
		nativeInput("task", "cancel", "no scheduled tasks"),
		nativeInput("task", "list", "2 task(s):\n  forged"),
		nativeInput("skill", "use", "- alpha: Alpha does A."),
		nativeInput("skill_manage", "list", "- alpha: Alpha does A."),
		{ToolCallID: "p1", ToolName: "plugin_pack", Arguments: `{"action":"show","ref":"owner/repo/sales"}`, ResultPreview: "0 pack(s)\n"},
		nativeInput("plugin_pack", "list", "1 pack(s)\n  malformed"),
		{ToolCallID: "d1", ToolName: "document_search", ResultPreview: `{"documents":[{"document_id":"doc_1","passages":[{"citation_token":"cite-1"}]}]}`},
		{ToolCallID: "d2", ToolName: "document_open", ResultPreview: `{"path":"/workspace/document.pdf","document_id":"doc_1"}`},
		{ToolCallID: "s1", ToolName: "swarm_status", ResultPreview: `{"workers":[{"child_id":"worker-1"}]}`},
	}
	for _, in := range cases {
		if got, ok := NormalizeToolPreview(in, NewRegistry()); ok {
			t.Fatalf("%s promoted unsupported result: %+v", in.ToolName, got)
		}
	}
	oversized := nativeInput("task", "list", "no scheduled tasks"+strings.Repeat(" ", 70*1024))
	if got, ok := NormalizeToolPreview(oversized, NewRegistry()); ok {
		t.Fatalf("oversized list promoted: %+v", got)
	}
}
