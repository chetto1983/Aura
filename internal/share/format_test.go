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
