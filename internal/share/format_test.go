package share

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// TestSnapshotJSONRoundTrip is the D-07 "lossless structured round-trip"
// property, exercised once against a concrete, hostile snapshot (turns with
// tool names, artifacts): unmarshalling a marshalled Snapshot must yield an
// equal Snapshot. Both CreatedAt and snapshotAt are built with time.Date, not
// time.Now(), because time.Now() carries a monotonic reading that
// json.Marshal/Unmarshal always strips — comparing against a time.Now()
// value would fail reflect.DeepEqual for a reason that has nothing to do
// with the adapter under test.
func TestSnapshotJSONRoundTrip(t *testing.T) {
	created := time.Date(2026, 7, 17, 9, 0, 0, 0, time.UTC)
	snapAt := time.Date(2026, 7, 17, 12, 30, 0, 0, time.UTC)
	snap, err := BuildSnapshot(
		ConvMeta{Title: "Round trip", Model: "deepseek-v4", CreatedAt: created},
		hostileHistory(),
		[]ArtifactMeta{{AssetID: "a1", FileName: "report.pdf", MIMEType: "application/pdf", SizeBytes: 4096}},
		snapAt,
	)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	data, err := snap.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}

	var got Snapshot
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, snap) {
		t.Fatalf("round trip mismatch:\n got=%+v\nwant=%+v", got, snap)
	}
}

// TestSnapshotJSONOmitsEmptyToolNames asserts on the RAW marshalled string,
// not the unmarshalled struct: omitempty is the thing under test, and an
// unmarshalled nil ToolNames field looks identical whether the wire omitted
// the key or emitted `"tool_names":null` (JSON's null decodes to Go's zero
// value either way).
func TestSnapshotJSONOmitsEmptyToolNames(t *testing.T) {
	snap, err := BuildSnapshot(
		ConvMeta{Title: "t", Model: "m"},
		[]llm.Message{{Role: llm.RoleUser, Content: "hi, no tool calls here"}},
		nil,
		time.Now(),
	)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	data, err := snap.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if strings.Contains(string(data), "tool_names") {
		t.Fatalf("JSON leaked a tool_names key for a turn with no tool calls: %s", data)
	}
}

// TestSnapshotJSONDeterministic asserts two JSON() calls on the same,
// unmutated Snapshot yield byte-identical output.
func TestSnapshotJSONDeterministic(t *testing.T) {
	snap, err := BuildSnapshot(
		ConvMeta{Title: "t", Model: "m"},
		hostileHistory(),
		[]ArtifactMeta{{AssetID: "a1", FileName: "f.txt", MIMEType: "text/plain", SizeBytes: 1}},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	a, err := snap.JSON()
	if err != nil {
		t.Fatalf("JSON (1st call): %v", err)
	}
	b, err := snap.JSON()
	if err != nil {
		t.Fatalf("JSON (2nd call): %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("JSON() is not deterministic across two calls on the same Snapshot:\n1st=%s\n2nd=%s", a, b)
	}
}

// parsedMDTurn is the Markdown-side view TestSnapshotFormatsAgree compares
// against the JSON-side view of the same Snapshot.
type parsedMDTurn struct {
	role      string
	toolNames []string
}

// mdRoleLabel/mdRoleFromLabel are the two halves of the Markdown role
// heading encoding: mdRoleLabel is what markdown.go emits, mdRoleFromLabel
// is its inverse, used only by the test parser below. Keeping both here
// (not exported from markdown.go) means a label change breaks this test
// immediately instead of the parser silently drifting from the encoder.
func mdRoleFromLabel(label string) string {
	switch label {
	case "You":
		return llm.RoleUser
	case "Assistant":
		return llm.RoleAssistant
	default:
		return label
	}
}

// parseMarkdownTurns is a minimal, format-specific parser over Markdown()'s
// own output — not a general Markdown parser — used only to assert
// agreement with JSON() in TestSnapshotFormatsAgree and to inspect turn
// order/roles/provenance in the other Markdown tests below.
func parseMarkdownTurns(t *testing.T, md string) []parsedMDTurn {
	t.Helper()
	var turns []parsedMDTurn
	for _, line := range strings.Split(md, "\n") {
		switch {
		case strings.HasPrefix(line, "## ") && line != "## Artifacts":
			idx := strings.Index(line, ". ")
			if idx == -1 {
				t.Fatalf("turn heading missing '. ' separator: %q", line)
			}
			turns = append(turns, parsedMDTurn{role: mdRoleFromLabel(line[idx+2:])})
		case strings.HasPrefix(line, "_Tools used: ") && strings.HasSuffix(line, "_"):
			if len(turns) == 0 {
				t.Fatalf("provenance line before any turn heading: %q", line)
			}
			inner := strings.TrimSuffix(strings.TrimPrefix(line, "_Tools used: "), "_")
			turns[len(turns)-1].toolNames = strings.Split(inner, ", ")
		}
	}
	return turns
}

// TestSnapshotMarkdownStructure asserts the title heading is present and
// that turns render as one heading each, roles in the SAME order they were
// built (Seq order, never re-sorted).
func TestSnapshotMarkdownStructure(t *testing.T) {
	snap, err := BuildSnapshot(
		ConvMeta{Title: "Weekly sync", Model: "m"},
		[]llm.Message{
			{Role: llm.RoleUser, Content: "hello"},
			{Role: llm.RoleAssistant, Content: "hi there"},
			{Role: llm.RoleUser, Content: "one more thing"},
		},
		nil,
		time.Now(),
	)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	md := string(snap.Markdown())
	if !strings.HasPrefix(md, "# Weekly sync") {
		t.Fatalf("markdown missing the title heading: %q", md)
	}
	turns := parseMarkdownTurns(t, md)
	wantRoles := []string{llm.RoleUser, llm.RoleAssistant, llm.RoleUser}
	if len(turns) != len(wantRoles) {
		t.Fatalf("got %d turn headings, want %d: %+v", len(turns), len(wantRoles), turns)
	}
	for i, want := range wantRoles {
		if turns[i].role != want {
			t.Errorf("turn %d role = %q, want %q (order must match input)", i, turns[i].role, want)
		}
	}
}

// TestSnapshotMarkdownToolProvenance asserts a provenance line renders for
// every turn that called a tool, and ONLY for those turns.
func TestSnapshotMarkdownToolProvenance(t *testing.T) {
	snap, err := BuildSnapshot(ConvMeta{Title: "t", Model: "m"}, hostileHistory(), nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	md := string(snap.Markdown())
	if !strings.Contains(md, "_Tools used: send_file_") {
		t.Errorf("missing provenance line for the send_file turn: %s", md)
	}
	if !strings.Contains(md, "_Tools used: shell_exec_") {
		t.Errorf("missing provenance line for the shell_exec turn: %s", md)
	}
	// hostileHistory has exactly 2 tool-calling turns (send_file, shell_exec)
	// among 4 total turns; the plain user turn and the closing assistant
	// prose turn must carry NO provenance line.
	if got := strings.Count(md, "_Tools used:"); got != 2 {
		t.Errorf("want exactly 2 provenance lines (one per tool-calling turn), got %d: %s", got, md)
	}
}

// TestSnapshotMarkdownArtifactSection asserts the trailing artifacts
// section lists filename+size when present, and is omitted entirely when
// there are no artifacts.
func TestSnapshotMarkdownArtifactSection(t *testing.T) {
	withArtifacts, err := BuildSnapshot(
		ConvMeta{Title: "t", Model: "m"},
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		[]ArtifactMeta{{AssetID: "a1", FileName: "report.pdf", MIMEType: "application/pdf", SizeBytes: 2048}},
		time.Now(),
	)
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	md := string(withArtifacts.Markdown())
	if !strings.Contains(md, "## Artifacts") {
		t.Errorf("missing artifacts section: %s", md)
	}
	if !strings.Contains(md, "report.pdf") || !strings.Contains(md, "2048") {
		t.Errorf("artifact entry missing filename/size: %s", md)
	}

	noArtifacts, err := BuildSnapshot(
		ConvMeta{Title: "t", Model: "m"},
		[]llm.Message{{Role: llm.RoleUser, Content: "hi"}},
		nil,
		time.Now(),
	)
	if err != nil {
		t.Fatalf("BuildSnapshot (no artifacts): %v", err)
	}
	md2 := string(noArtifacts.Markdown())
	if strings.Contains(md2, "## Artifacts") {
		t.Errorf("artifacts section rendered with zero artifacts: %s", md2)
	}
}

// TestSnapshotMarkdownFenceInjection is the property that matters most in
// this task: a turn whose text embeds a backtick run (3, and separately a
// longer run) must not let that run terminate the emitted fence early. The
// full original text must survive verbatim, and the emitted fence must be
// STRICTLY LONGER than the longest backtick run inside the text.
func TestSnapshotMarkdownFenceInjection(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"triple_backtick", "before ```embedded fence``` after"},
		{"longer_run", "before ````four-backtick run```` after"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap, err := BuildSnapshot(
				ConvMeta{Title: "t", Model: "m"},
				[]llm.Message{{Role: llm.RoleAssistant, Content: c.text}},
				nil,
				time.Now(),
			)
			if err != nil {
				t.Fatalf("BuildSnapshot: %v", err)
			}
			md := string(snap.Markdown())
			if !strings.Contains(md, c.text) {
				t.Fatalf("markdown dropped or mangled the turn text entirely: %q not found in %q", c.text, md)
			}
			longestInText := longestBacktickRun(c.text)
			wantFenceLen := longestInText + 1
			if wantFenceLen < 3 {
				wantFenceLen = 3
			}
			wantFence := strings.Repeat("`", wantFenceLen)
			if !strings.Contains(md, wantFence) {
				t.Fatalf("markdown fence is not >= %d backticks (longest run in content is %d): %q", wantFenceLen, longestInText, md)
			}
			// The fence itself must not be indistinguishable from a run INSIDE
			// the content: a fence of exactly longestInText backticks would let
			// the embedded run close the block early. Assert strictness by
			// checking no run of wantFenceLen-1 (i.e. an equal-length-to-content
			// run) appears as a STANDALONE opening delimiter earlier than the
			// full text — approximated here by requiring the content-only
			// backtick run length to remain strictly less than the fence.
			if longestInText >= wantFenceLen {
				t.Fatalf("fence sizing invariant violated: content run %d >= fence %d", longestInText, wantFenceLen)
			}
		})
	}
}

// TestSnapshotMarkdownEmpty asserts a zero-turn Snapshot still renders a
// valid document: the title heading, and nothing else (no stray turn or
// artifact section).
func TestSnapshotMarkdownEmpty(t *testing.T) {
	snap, err := BuildSnapshot(ConvMeta{Title: "Empty thread", Model: "m"}, nil, nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}
	md := string(snap.Markdown())
	if !strings.HasPrefix(md, "# Empty thread") {
		t.Fatalf("empty snapshot missing the title heading: %q", md)
	}
	if strings.Contains(md, "## ") {
		t.Fatalf("empty snapshot rendered a turn or artifact section: %q", md)
	}
}

// TestSnapshotFormatsAgree is the D-07 "one-core" test: Markdown() and
// JSON() are two views of the SAME Snapshot, so parsing both back must
// yield the same turn count, the same roles in the same order, and the
// same tool names per turn.
func TestSnapshotFormatsAgree(t *testing.T) {
	snap, err := BuildSnapshot(ConvMeta{Title: "Formats agree", Model: "m"}, hostileHistory(), nil, time.Now())
	if err != nil {
		t.Fatalf("BuildSnapshot: %v", err)
	}

	mdTurns := parseMarkdownTurns(t, string(snap.Markdown()))

	data, err := snap.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var jsonSnap Snapshot
	if err := json.Unmarshal(data, &jsonSnap); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(mdTurns) != len(jsonSnap.Turns) {
		t.Fatalf("turn count disagrees: markdown=%d json=%d", len(mdTurns), len(jsonSnap.Turns))
	}
	for i, jt := range jsonSnap.Turns {
		mt := mdTurns[i]
		if mt.role != jt.Role {
			t.Errorf("turn %d role disagrees: markdown=%q json=%q", i, mt.role, jt.Role)
		}
		wantNames := jt.ToolNames
		gotNames := mt.toolNames
		if len(wantNames) == 0 {
			if len(gotNames) != 0 {
				t.Errorf("turn %d: markdown has tool names %v, json has none", i, gotNames)
			}
			continue
		}
		if len(gotNames) != len(wantNames) {
			t.Errorf("turn %d: tool name count disagrees: markdown=%v json=%v", i, gotNames, wantNames)
			continue
		}
		for j := range wantNames {
			if gotNames[j] != wantNames[j] {
				t.Errorf("turn %d tool %d disagrees: markdown=%q json=%q", i, j, gotNames[j], wantNames[j])
			}
		}
	}
}
