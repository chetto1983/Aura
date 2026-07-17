package share

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// toolCall builds an llm.ToolCall fixture without repeating the anonymous
// Function struct literal (internal/llm/client.go:34-41) at every call site.
func toolCall(id, name, arguments string) llm.ToolCall {
	tc := llm.ToolCall{ID: id, Type: "function"}
	tc.Function.Name = name
	tc.Function.Arguments = arguments
	return tc
}

// Hostile fixture constants. Every value is traceable to a real leak shape
// from 37F-RESEARCH.md's Redaction Inventory (L-01..L-09), not invented:
// the send_file {"path":...} argument and the "cannot read %q" error shape
// (send_file.go:57,173), a shell_exec-style path-bearing argument, a
// container id, and the $AURA_RUN_DIR sidecar path template
// (conversations/store.go:122).
const (
	hostileOwnerIdentityID = "9f2e7dc0-1111-4444-8888-abcdef123456"
	hostileContainerID     = "c3a9f1b02e7d"
	hostileSidecarPath     = "$AURA_RUN_DIR/conversations/conv-42/3.content"
)

// hostileHistory is the SC3 RED fixture: a send_file call whose Arguments
// carry an absolute secret path, a shell_exec call whose Arguments carry
// /etc/passwd, a role="tool" message whose Content is raw stdout embedding
// /etc/passwd + a container id + a sidecar path, and a normal assistant
// turn that must survive.
func hostileHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleUser, Content: "Can you pull the report together and send it over?"},
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				toolCall("call_send", "send_file", `{"path":"/abs/secret/results.xlsx"}`),
			},
		},
		{Role: llm.RoleTool, Content: "queued results.xlsx for delivery", ToolCallID: "call_send"},
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				toolCall("call_shell", "shell_exec", `{"command":"cat /etc/passwd"}`),
			},
		},
		{
			Role: llm.RoleTool,
			Content: "reading /etc/passwd on container=" + hostileContainerID +
				" sidecar=" + hostileSidecarPath + " owner=" + hostileOwnerIdentityID,
			ToolCallID: "call_shell",
		},
		{Role: llm.RoleAssistant, Content: "Here is the summary you asked for."},
	}
}

// TestSnapshotRedactsHostPaths is the SC3 RED test: the hostile fixture's
// marshalled Snapshot must contain none of the forbidden substrings, while
// still carrying the assistant prose and both tool names. A projection that
// dropped everything would pass a pure negative test — the positive
// assertions below are the mutant that must not live.
func TestSnapshotRedactsHostPaths(t *testing.T) {
	snap, err := BuildSnapshot(ConvMeta{Title: "Report thread", Model: "deepseek-v4"}, hostileHistory(), nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	body := string(data)

	forbidden := []string{
		"/abs/", "/etc/", "AURA_RUN_DIR", "results.xlsx",
		hostileContainerID, hostileOwnerIdentityID,
	}
	for _, f := range forbidden {
		if strings.Contains(body, f) {
			t.Errorf("snapshot leaked forbidden substring %q: %s", f, body)
		}
	}

	if !strings.Contains(body, "Here is the summary you asked for.") {
		t.Errorf("snapshot dropped the assistant prose that must survive; body=%s", body)
	}
	if !strings.Contains(body, "send_file") || !strings.Contains(body, "shell_exec") {
		t.Errorf("snapshot dropped tool-name provenance; body=%s", body)
	}
}

// TestSnapshotStripsSendFilePath targets the send_file {"path":...} leak
// (L-03) specifically, on every surface: SnapshotTurn.Text, ToolNames, and
// the final marshalled JSON.
func TestSnapshotStripsSendFilePath(t *testing.T) {
	msgs := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				toolCall("call_send", "send_file", `{"path":"/abs/secret/results.xlsx","caption":"see /abs/secret too"}`),
			},
		},
		{Role: llm.RoleTool, Content: "queued results.xlsx for delivery", ToolCallID: "call_send"},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snap.Turns) != 1 {
		t.Fatalf("want 1 turn (tool role dropped), got %d: %#v", len(snap.Turns), snap.Turns)
	}
	turn := snap.Turns[0]
	if strings.Contains(turn.Text, "/abs/") {
		t.Errorf("SnapshotTurn.Text leaked the send_file path: %q", turn.Text)
	}
	for _, name := range turn.ToolNames {
		if strings.Contains(name, "/abs/") {
			t.Errorf("SnapshotTurn.ToolNames leaked the send_file path: %q", name)
		}
	}
	if len(turn.ToolNames) != 1 || turn.ToolNames[0] != "send_file" {
		t.Errorf("ToolNames = %v, want [send_file]", turn.ToolNames)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "/abs/") {
		t.Errorf("marshalled snapshot leaked the send_file path: %s", data)
	}
}

// TestSnapshotDropsToolRoleTurns asserts a role="tool" message contributes
// zero turns, and that Seq stays dense (0,1,2...) across the drop — Seq must
// never leak how many turns were removed.
func TestSnapshotDropsToolRoleTurns(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "hi"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{toolCall("call_1", "shell_exec", `{"command":"ls"}`)}},
		{Role: llm.RoleTool, Content: "total 0", ToolCallID: "call_1"},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if len(snap.Turns) != 3 {
		t.Fatalf("want 3 turns (tool role dropped), got %d: %#v", len(snap.Turns), snap.Turns)
	}
	for i, turn := range snap.Turns {
		if turn.Seq != i {
			t.Errorf("turn %d: Seq = %d, want dense %d (must not leak the dropped turn count)", i, turn.Seq, i)
		}
		if turn.Role == llm.RoleTool {
			t.Errorf("turn %d: role=tool reached the snapshot", i)
		}
	}
}

// TestSnapshotSpilledTurnNoSidecarPath simulates a history whose tool-result
// content originated from a spilled turn (conversations.Turn.ContentSidecarPath,
// L-07): the sidecar path template appears only inside a role="tool" message,
// which is dropped entirely, so it never reaches the snapshot.
func TestSnapshotSpilledTurnNoSidecarPath(t *testing.T) {
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "Show me the raw log."},
		{
			Role:       llm.RoleTool,
			Content:    "spilled tool output rehydrated from " + hostileSidecarPath,
			ToolCallID: "call_spill",
		},
		{Role: llm.RoleAssistant, Content: "Here is a summary of what I found."},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "AURA_RUN_DIR") || strings.Contains(string(data), hostileSidecarPath) {
		t.Fatalf("snapshot leaked a sidecar path: %s", data)
	}
	if len(snap.Turns) != 2 {
		t.Fatalf("want 2 turns (tool role dropped), got %d: %#v", len(snap.Turns), snap.Turns)
	}
}

// TestSnapshotOmitsIdentity has two legs: a structural guarantee (no
// exported field on any Snapshot-family type or ConvMeta names the owner
// identity) and a behavioral guarantee (even when a hostile tool argument
// happens to mention the owner's identity id, it never survives — because
// ToolCall.Arguments is never read at all, independent of its content).
func TestSnapshotOmitsIdentity(t *testing.T) {
	for _, v := range []any{Snapshot{}, SnapshotTurn{}, SnapshotArtifact{}, ConvMeta{}} {
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			name := strings.ToLower(rt.Field(i).Name)
			if strings.Contains(name, "identity") || strings.Contains(name, "owner") {
				t.Fatalf("%s.%s: field name suggests it could carry the owner identity id (L-08)", rt.Name(), rt.Field(i).Name)
			}
		}
	}

	msgs := []llm.Message{
		{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				toolCall("call_1", "shell_exec", `{"command":"whoami","identity":"`+hostileOwnerIdentityID+`"}`),
			},
		},
		{Role: llm.RoleAssistant, Content: "done"},
	}
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), hostileOwnerIdentityID) {
		t.Fatalf("snapshot leaked the owner identity id: %s", data)
	}
}

// TestSnapshotRoleAllowlist is table-driven: user/assistant emit a turn;
// tool/system/empty/unrecognized roles never do (fail-closed default).
func TestSnapshotRoleAllowlist(t *testing.T) {
	cases := []struct {
		role  string
		emits bool
	}{
		{llm.RoleUser, true},
		{llm.RoleAssistant, true},
		{llm.RoleTool, false},
		{llm.RoleSystem, false},
		{"", false},
		{"garbage", false},
	}
	for i, c := range cases {
		name := c.role
		if name == "" {
			name = "empty"
		}
		t.Run(fmt.Sprintf("%d_%s", i, name), func(t *testing.T) {
			msgs := []llm.Message{{Role: c.role, Content: "x"}}
			snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, msgs, nil, time.Now())
			if err != nil {
				t.Fatalf("BuildSnapshot: %v", err)
			}
			got := len(snap.Turns) == 1
			if got != c.emits {
				t.Errorf("role=%q: emitted=%v, want %v", c.role, got, c.emits)
			}
		})
	}
}

// TestSnapshotRedactionIdempotent: projecting an already-projected turn set
// is a fixpoint. SnapshotTurn carries no ToolCalls to re-derive (by design —
// there is nowhere on llm.Message to put a redacted argument back), so a
// second pass over the re-expressed turns must leave Role and Text
// unchanged and must not resurrect anything forbidden.
func TestSnapshotRedactionIdempotent(t *testing.T) {
	first, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, hostileHistory(), nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	reprojected := make([]llm.Message, 0, len(first.Turns))
	for _, turn := range first.Turns {
		reprojected = append(reprojected, llm.Message{Role: turn.Role, Content: turn.Text})
	}
	second, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, reprojected, nil, first.SnapshotAt)
	if err != nil {
		t.Fatalf("BuildSnapshot (second pass): %v", err)
	}
	if len(second.Turns) != len(first.Turns) {
		t.Fatalf("second pass turn count = %d, want %d (fixpoint)", len(second.Turns), len(first.Turns))
	}
	for i, turn := range second.Turns {
		if turn.Text != first.Turns[i].Text || turn.Role != first.Turns[i].Role {
			t.Fatalf("turn %d changed on reprojection: got %+v, want role/text of %+v", i, turn, first.Turns[i])
		}
	}
	data, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second pass: %v", err)
	}
	if strings.Contains(string(data), "/abs/") || strings.Contains(string(data), "/etc/") {
		t.Fatalf("second pass resurrected a forbidden substring: %s", data)
	}
}

// TestSnapshotSchemaVersion asserts SchemaVersion == 1 and that the
// marshalled key set matches the OQ4 wire contract exactly — a json tag
// rename here must fail this test rather than silently break plan 37F-05's
// TypeScript mirror.
func TestSnapshotSchemaVersion(t *testing.T) {
	snap, err := BuildSnapshot(
		ConvMeta{Title: "Weekly sync", Model: "deepseek-v4"},
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		[]ArtifactMeta{{AssetID: "a", FileName: "f", MIMEType: "text/plain", SizeBytes: 1}},
		time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	if snap.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion = %d, want 1", snap.SchemaVersion)
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatalf("unmarshal into wire map: %v", err)
	}
	wantKeys := []string{"schema_version", "title", "model", "created_at", "snapshot_at", "turns", "artifacts"}
	if len(wire) != len(wantKeys) {
		t.Fatalf("top-level key set has %d keys, want %d: %v", len(wire), len(wantKeys), wire)
	}
	for _, k := range wantKeys {
		if _, ok := wire[k]; !ok {
			t.Fatalf("wire contract missing top-level key %q", k)
		}
	}

	var turns []map[string]json.RawMessage
	if err := json.Unmarshal(wire["turns"], &turns); err != nil {
		t.Fatalf("unmarshal turns: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("want 1 turn, got %d", len(turns))
	}
	for _, k := range []string{"seq", "role", "text"} {
		if _, ok := turns[0][k]; !ok {
			t.Fatalf("turn wire contract missing key %q", k)
		}
	}
	if _, ok := turns[0]["tool_names"]; ok {
		t.Fatalf("tool_names must be omitted when empty (omitempty), got present: %v", turns[0])
	}

	var artifacts []map[string]json.RawMessage
	if err := json.Unmarshal(wire["artifacts"], &artifacts); err != nil {
		t.Fatalf("unmarshal artifacts: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("want 1 artifact, got %d", len(artifacts))
	}
	for _, k := range []string{"asset_id", "filename", "mime_type", "size_bytes"} {
		if _, ok := artifacts[0][k]; !ok {
			t.Fatalf("artifact wire contract missing key %q", k)
		}
	}
}
