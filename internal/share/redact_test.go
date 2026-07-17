package share

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// TestSnapshotKeepsToolNamesDropsArgs asserts both send_file and shell_exec
// survive in ToolNames while their Arguments JSON (L-01) never appears
// anywhere in the marshalled snapshot.
func TestSnapshotKeepsToolNamesDropsArgs(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				toolCall("call_send", "send_file", `{"path":"/abs/secret/results.xlsx"}`),
				toolCall("call_shell", "shell_exec", `{"command":"cat /etc/passwd"}`),
			},
		},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snap.Turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(snap.Turns))
	}
	got := snap.Turns[0].ToolNames
	want := []string{"send_file", "shell_exec"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ToolNames = %v, want %v", got, want)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(data)
	if strings.Contains(body, "/abs/") || strings.Contains(body, "/etc/") {
		t.Errorf("marshalled snapshot leaked tool-call argument content: %s", body)
	}
}

// TestSnapshotToolNamesOrderAndDuplicates: call order is preserved, a
// duplicate tool name is reported twice (collapsing would misreport
// provenance), and a blank/whitespace-only name is dropped.
func TestSnapshotToolNamesOrderAndDuplicates(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				toolCall("call_1", "send_file", `{"path":"/a"}`),
				toolCall("call_2", "send_file", `{"path":"/b"}`),
				toolCall("call_3", "   ", `{}`),
				toolCall("call_4", "shell_exec", `{}`),
			},
		},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	want := []string{"send_file", "send_file", "shell_exec"}
	got := snap.Turns[0].ToolNames
	if len(got) != len(want) {
		t.Fatalf("ToolNames = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ToolNames[%d] = %q, want %q (order/dedup must be preserved, not collapsed)", i, got[i], want[i])
		}
	}
}

// TestSnapshotToolNamesOmittedWhenEmpty: no tool calls ⇒ ToolNames is
// nil/empty and the `tool_names` key is omitted from the marshalled JSON
// (omitempty).
func TestSnapshotToolNamesOmittedWhenEmpty(t *testing.T) {
	msgs := []llm.Message{{Role: llm.RoleAssistant, Content: "no tools here"}}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snap.Turns[0].ToolNames) != 0 {
		t.Fatalf("ToolNames = %v, want empty", snap.Turns[0].ToolNames)
	}
	data, err := json.Marshal(snap.Turns[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "tool_names") {
		t.Fatalf("tool_names key present despite omitempty: %s", data)
	}
}

// TestSnapshotToolNamesNilWhenAllBlank pins toolNames' normalization
// guarantee precisely: when every call's name is blank/whitespace (so
// nothing survives the loop), ToolNames must be exactly nil — not a
// non-nil empty slice `make` would otherwise leave behind. A mutation that
// deletes the trailing `if len(names) == 0 { return nil }` guard is
// wire-invisible (omitempty drops both nil and empty-non-nil identically)
// but IS a real Go-value regression; this test pins the value directly via
// `!= nil`, not just len==0, so that mutant is caught here rather than
// waved through as equivalent.
func TestSnapshotToolNamesNilWhenAllBlank(t *testing.T) {
	msgs := []llm.Message{
		{
			Role:      llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{toolCall("call_1", "   ", `{}`)},
		},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snap.Turns[0].ToolNames != nil {
		t.Fatalf("ToolNames = %#v, want exactly nil (not a non-nil empty slice)", snap.Turns[0].ToolNames)
	}
}

// TestSnapshotArtifactAllowlist: only the four allowlisted keys survive.
// ArtifactMeta itself carries no Path field, so a caller has nowhere to
// smuggle one through — this is asserted both behaviorally (the built
// SnapshotArtifact matches exactly) and structurally (reflection over the
// ArtifactMeta type).
func TestSnapshotArtifactAllowlist(t *testing.T) {
	artifacts := []ArtifactMeta{
		{AssetID: "asset-1", FileName: "results.xlsx", MIMEType: "application/vnd.ms-excel", SizeBytes: 4096},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, nil, artifacts, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snap.Artifacts) != 1 {
		t.Fatalf("want 1 artifact, got %d", len(snap.Artifacts))
	}
	want := SnapshotArtifact{AssetID: "asset-1", FileName: "results.xlsx", MIMEType: "application/vnd.ms-excel", SizeBytes: 4096}
	if snap.Artifacts[0] != want {
		t.Fatalf("SnapshotArtifact = %+v, want %+v", snap.Artifacts[0], want)
	}

	rt := reflect.TypeOf(ArtifactMeta{})
	for i := 0; i < rt.NumField(); i++ {
		if strings.EqualFold(rt.Field(i).Name, "path") {
			t.Fatalf("ArtifactMeta must not have a Path field: found %s", rt.Field(i).Name)
		}
	}
}
