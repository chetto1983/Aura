package display

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestPayloadRoundTripIdentity pins decode(encode)==identity for a populated
// Payload of each kind: marshalling then unmarshalling yields a struct that
// re-marshals to the same bytes (the JSON wire contract is stable).
func TestPayloadRoundTripIdentity(t *testing.T) {
	published := "2026-06-18"
	cases := []struct {
		name string
		in   Payload
	}{
		{
			name: "web_result",
			in: Payload{
				Type: KindWebResult, ToolCallID: "call-1", Title: "meteo",
				WebResults: []WebItem{{
					Title: "Forecast", URL: "https://example.com/a", Snippet: "sunny",
					Domain: "example.com", Score: 0.9, PublishedAt: &published,
					Thumbnail: "https://example.com/t.png", RefID: "src-1",
				}},
				Sources: []Source{{RefID: "src-1", Index: 1, Type: KindWebResult, Title: "Forecast", URL: "https://example.com/a", Cited: true}},
			},
		},
		{name: "document", in: Payload{Type: KindDocument, ToolCallID: "c2", Document: &Document{Title: "Doc", URL: "https://x.test", ContentMD: "# Hello"}}},
		{name: "code", in: Payload{Type: KindCode, ToolCallID: "c3", Code: &Code{Body: "print(1)", Lang: "python"}}},
		{name: "local_artifact", in: Payload{Type: KindLocalArtifact, ToolCallID: "c4", Artifact: &Artifact{Filename: "out.csv", SizeBytes: 42, Path: "/run/out.csv"}}},
		{name: "table", in: Payload{Type: KindTable, ToolCallID: "c5", Table: &Table{Columns: []string{"a", "b"}, Rows: [][]string{{"1", "2"}}}}},
		{name: "chart", in: Payload{Type: KindChart, ToolCallID: "c6", Chart: &Chart{XLabels: []string{"q1"}, YValues: []float64{1.5}, XAxisLabel: "quarter"}}},
		{name: "system_event", in: Payload{Type: KindSystemEvent, ToolCallID: "c7", System: &System{Class: "blocked_url", Reason: "private_or_metadata_target", Message: "blocked", Severity: "error"}}},
		{name: "swarm_report", in: Payload{Type: KindSwarmReport, ToolCallID: "c8", Swarm: []ChildReport{{GoalIndex: 0, ChildID: "w1", Status: "ok", Summary: "done"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b1, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got Payload
			if err := json.Unmarshal(b1, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			b2, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("re-marshal: %v", err)
			}
			if string(b1) != string(b2) {
				t.Fatalf("decode(encode) not identity:\n first: %s\nsecond: %s", b1, b2)
			}
		})
	}
}

// TestPayloadKindWireLiterals asserts each Kind constant equals the exact frontend
// wire literal (web/src/chat/displays/types.ts, 26-02). A drift here silently
// breaks the cockpit router.
func TestPayloadKindWireLiterals(t *testing.T) {
	want := map[Kind]string{
		KindWebResult:     "web_result",
		KindDocument:      "document",
		KindCode:          "code",
		KindLocalArtifact: "local_artifact",
		KindTable:         "table",
		KindChart:         "chart",
		KindSystemEvent:   "system_event",
		KindSwarmReport:   "swarm_report",
	}
	for k, lit := range want {
		if string(k) != lit {
			t.Fatalf("Kind %q wire literal = %q, want %q", k, string(k), lit)
		}
	}
}

// TestPayloadOmitsUnsetTypeFields asserts a single-kind Payload omits every other
// type's key on the wire (the union stays flat + lean).
func TestPayloadOmitsUnsetTypeFields(t *testing.T) {
	b, err := json.Marshal(Payload{Type: KindCode, ToolCallID: "c1", Code: &Code{Body: "x"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	wire := string(b)
	for _, absent := range []string{"web_results", "document", "artifact", "table", "chart", "system", "swarm", "sources"} {
		if strings.Contains(wire, "\""+absent+"\"") {
			t.Fatalf("unset field %q present on wire: %s", absent, wire)
		}
	}
	if !strings.Contains(wire, "\"code\"") {
		t.Fatalf("set field code missing on wire: %s", wire)
	}
}

// TestPayloadSourceCitedSerializes asserts the registry distinguishes cited vs
// consulted: the cited boolean is always present (it is the chip/explorer filter).
func TestPayloadSourceCitedSerializes(t *testing.T) {
	for _, cited := range []bool{true, false} {
		b, err := json.Marshal(Source{RefID: "src-1", Index: 1, Cited: cited})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		want := "\"cited\":" + map[bool]string{true: "true", false: "false"}[cited]
		if !strings.Contains(string(b), want) {
			t.Fatalf("Source(cited=%v) wire = %s, want substring %q", cited, b, want)
		}
	}
}
